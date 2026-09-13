package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/ReanSn0w/wddl/pkg/control"
)

type watchClient interface {
	Status(context.Context) (control.Status, error)
	Watch(context.Context, func(control.Event) error) error
}

func runPlainWatch(ctx context.Context, client watchClient, out io.Writer) error {
	return client.Watch(ctx, func(event control.Event) error {
		if _, err := fmt.Fprintln(out, formatPlainWatchEvent(event)); err != nil {
			return err
		}
		if event.Dropped == 0 {
			return nil
		}

		status, err := client.Status(ctx)
		if err != nil {
			_, writeErr := fmt.Fprintf(out, "%s %-20s %s\n", time.Now().Format(time.RFC3339), "status.resync.failed", err)
			return writeErr
		}
		_, err = fmt.Fprintf(out, "%s %-20s queue: %d ready, %d suspended, %d active; index: %d files\n",
			time.Now().Format(time.RFC3339), "status.resynced", status.Pending, status.Suspended, len(status.Active), status.IndexedFiles)
		return err
	})
}

func formatPlainWatchEvent(event control.Event) string {
	message := event.Message
	if progress, ok := event.Data.(control.DownloadProgress); ok && event.Type == "download.progress" {
		name := progress.Name
		if name == "" {
			name = event.Message
		}
		message = fmt.Sprintf("%s  %.1f%%  %s/%s  %s",
			name, progress.Percent, formatBytes(progress.Downloaded), formatBytes(progress.Size), formatRate(progress.Speed))
		if eta, ok := progressETA(progress); ok {
			message += "  ETA " + formatWatchDuration(eta)
		}
	}
	if event.Dropped > 0 {
		message += fmt.Sprintf(" (dropped %d; resyncing status)", event.Dropped)
	}
	return fmt.Sprintf("%s %-20s %s", event.Time.Format(time.RFC3339), event.Type, message)
}

func progressETA(progress control.DownloadProgress) (time.Duration, bool) {
	if progress.Speed <= 0 || progress.Size <= 0 || progress.Downloaded >= progress.Size {
		return 0, false
	}
	seconds := (progress.Size - progress.Downloaded) / progress.Speed
	if seconds < 1 {
		seconds = 1
	}
	return time.Duration(seconds) * time.Second, true
}

func formatBytes(value int64) string {
	if value < 0 {
		return "--"
	}
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	units := [...]string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	size := float64(value)
	unit := -1
	for size >= 1024 && unit < len(units)-1 {
		size /= 1024
		unit++
	}
	return fmt.Sprintf("%.1f %s", size, units[unit])
}

func formatRate(bytesPerSecond int64) string {
	if bytesPerSecond <= 0 {
		return "--"
	}
	return formatBytes(bytesPerSecond) + "/s"
}

func formatWatchDuration(value time.Duration) string {
	if value <= 0 {
		return "--"
	}
	seconds := int64(math.Ceil(value.Seconds()))
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	seconds %= 60
	if minutes < 60 {
		return fmt.Sprintf("%dm%02ds", minutes, seconds)
	}
	hours := minutes / 60
	minutes %= 60
	if hours < 24 {
		return fmt.Sprintf("%dh%02dm", hours, minutes)
	}
	days := hours / 24
	hours %= 24
	return fmt.Sprintf("%dd%02dh", days, hours)
}
