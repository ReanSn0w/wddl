package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"time"

	"github.com/ReanSn0w/wddl/pkg/config"
	"github.com/ReanSn0w/wddl/pkg/control"
)

func executeCommand(ctx context.Context, parsed parsedCLI, out io.Writer, getenv func(string) string) error {
	if parsed.Command == "version" {
		value := control.Version{Revision: revision, GoVersion: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH}
		return printValue(out, parsed.Options.Version.JSON, value, formatVersion)
	}
	if parsed.Command == "config validate" {
		_, err := config.Load(parsed.Options.ConfigPath)
		if err != nil {
			if parsed.Options.Config.Validate.JSON {
				_ = json.NewEncoder(out).Encode(control.Envelope{Version: control.APIVersion, Error: &control.Error{Code: control.CodeInvalidRequest, Message: err.Error()}})
			}
			return err
		}
		return printValue(out, parsed.Options.Config.Validate.JSON, map[string]any{"valid": true, "path": parsed.Options.ConfigPath}, func(any) string { return "configuration is valid" })
	}
	if parsed.Command == "run" {
		return runDaemon(ctx, parsed.Options.ConfigPath, getenv)
	}

	conf, err := config.Load(parsed.Options.ConfigPath)
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	client := control.NewClient(conf.Control.Socket, conf.Control.RequestTimeout.Value())
	switch parsed.Command {
	case "status":
		value, err := client.Status(ctx)
		if err != nil {
			return err
		}
		return printValue(out, parsed.Options.Status.JSON, value, formatStatus)
	case "watch":
		return client.Watch(ctx, func(event control.Event) error {
			if parsed.Options.Watch.JSON {
				return json.NewEncoder(out).Encode(event)
			}
			_, err := fmt.Fprintf(out, "%s %-20s %s", event.Time.Format(time.RFC3339), event.Type, event.Message)
			if event.Dropped > 0 {
				_, _ = fmt.Fprintf(out, " (dropped %d)", event.Dropped)
			}
			_, _ = fmt.Fprintln(out)
			return err
		})
	case "scan remote", "scan local", "scan all":
		kind := control.ScanKind(parsed.Command[len("scan "):])
		value, err := client.Scan(ctx, kind)
		if err != nil {
			return err
		}
		return printValue(out, scanJSON(parsed, kind), value, func(v any) string { return v.(control.ScanAccepted).Message })
	case "queue list":
		value, err := client.Queue(ctx)
		if err != nil {
			return err
		}
		return printValue(out, parsed.Options.Queue.List.JSON, value, formatQueue)
	case "queue remove":
		value, err := client.QueueRemove(ctx, parsed.Options.Queue.Remove.Args.ID)
		if err != nil {
			return err
		}
		return printValue(out, parsed.Options.Queue.Remove.JSON, value, func(v any) string {
			return fmt.Sprintf("removed %s; a remote scan may add it again", v.(control.QueueItem).ID)
		})
	case "queue retry":
		value, err := client.QueueRetry(ctx, parsed.Options.Queue.Retry.Args.ID)
		if err != nil {
			return err
		}
		return printValue(out, parsed.Options.Queue.Retry.JSON, value, func(v any) string { return "resumed " + v.(control.QueueItem).ID })
	case "download cancel":
		value, err := client.Cancel(ctx, parsed.Options.Download.Cancel.Args.ID)
		if err != nil {
			return err
		}
		return printValue(out, parsed.Options.Download.Cancel.JSON, value, func(v any) string { return "cancelled " + v.(control.QueueItem).ID })
	case "id":
		value, err := client.RemoteID(ctx, parsed.Options.ID.Args.RemotePath)
		if err != nil {
			return err
		}
		return printValue(out, parsed.Options.ID.JSON, value, func(v any) string { x := v.(control.RemoteID); return fmt.Sprintf("%s  %d  %s", x.ID, x.Size, x.Path) })
	case "cleanup remote":
		value, err := client.Cleanup(ctx, parsed.Options.Cleanup.Remote.Confirm)
		if err != nil {
			return err
		}
		if result, ok := value.(control.CleanupResult); ok && result.Failed > 0 {
			_ = printValue(out, parsed.Options.Cleanup.Remote.JSON, value, formatCleanup)
			return &control.APIError{Code: control.CodePartial, Message: "cleanup completed with failures"}
		}
		return printValue(out, parsed.Options.Cleanup.Remote.JSON, value, formatCleanup)
	case "config reload":
		value, err := client.Reload(ctx, conf)
		if err != nil {
			return err
		}
		return printValue(out, parsed.Options.Config.Reload.JSON, value, func(v any) string { return v.(control.ReloadResult).Message })
	default:
		return fmt.Errorf("unknown command %q", parsed.Command)
	}
}

func printValue(out io.Writer, asJSON bool, value any, human func(any) string) error {
	if asJSON {
		return json.NewEncoder(out).Encode(control.Envelope{Version: control.APIVersion, Data: value})
	}
	_, err := fmt.Fprintln(out, human(value))
	return err
}

func formatVersion(value any) string {
	v := value.(control.Version)
	return fmt.Sprintf("wddl %s (%s, %s)", v.Revision, v.GoVersion, v.Platform)
}

func formatStatus(value any) string {
	v := value.(control.Status)
	return fmt.Sprintf("uptime: %s\nqueue: %d ready, %d suspended, %d active\nindex: %d files\nremote scan: running=%t scheduled=%t\nlocal scan: running=%t scheduled=%t", v.Uptime.Round(time.Second), v.Pending, v.Suspended, len(v.Active), v.IndexedFiles, v.RemoteScan.Running, v.RemoteScan.Scheduled, v.LocalScan.Running, v.LocalScan.Scheduled)
}

func formatQueue(value any) string {
	items := value.([]control.QueueItem)
	if len(items) == 0 {
		return "queue is empty"
	}
	result := "ID                               STATE       SIZE         PATH"
	for _, item := range items {
		result += fmt.Sprintf("\n%-32s %-11s %-12d %s", item.ID, item.State, item.Size, item.Source)
	}
	return result
}

func formatCleanup(value any) string {
	switch v := value.(type) {
	case control.CleanupPreview:
		result := fmt.Sprintf("preview: %d files, %d bytes", v.Count, v.TotalSize)
		for _, file := range v.Files {
			result += "\n" + file.Path
		}
		return result + fmt.Sprintf("\nconfirm with: wddl cleanup remote --confirm %s", v.Token)
	case control.CleanupResult:
		return fmt.Sprintf("deleted: %d, skipped: %d, failed: %d", v.Deleted, v.Skipped, v.Failed)
	default:
		return fmt.Sprint(value)
	}
}

func scanJSON(parsed parsedCLI, kind control.ScanKind) bool {
	switch kind {
	case control.ScanRemote:
		return parsed.Options.Scan.Remote.JSON
	case control.ScanLocal:
		return parsed.Options.Scan.Local.JSON
	default:
		return parsed.Options.Scan.All.JSON
	}
}
