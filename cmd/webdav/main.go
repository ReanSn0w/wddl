package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"git.papkovda.ru/library/gokit/pkg/app"
	"github.com/ReanSn0w/wddl/pkg/engine"
	"github.com/ReanSn0w/wddl/pkg/files"
	"github.com/ReanSn0w/wddl/pkg/localindex"
	"github.com/ReanSn0w/wddl/pkg/queue"
	"github.com/ReanSn0w/wddl/pkg/utils"
	"github.com/studio-b12/gowebdav"
)

var (
	revision = "unknown"
	opts     = struct {
		app.Debug

		Input  string `short:"i" long:"input" env:"INPUT" default:"/" description:"input path"`
		Temp   string `short:"t" long:"temp" env:"TEMP" default:"/tmp/wddl" description:"temporary path"`
		Output string `short:"o" long:"output" env:"OUTPUT" default:"./download" description:"output path"`

		QueueFile   string `long:"queue-file" env:"QUEUE_FILE" default:"./queue.json" description:"queue file"`
		Threads     int    `long:"threads" env:"THREADS" default:"4" description:"parallel downloads"`
		Timeout     int    `long:"timeout" env:"TIMEOUT" default:"600" description:"rescan timeout (seconds)"`
		ClearRemote bool   `long:"clear-remote" env:"CLEAR_REMOTE" description:"clear remote files"`

		ExistingRoots     []string      `long:"existing-root" env:"EXISTING_FILES_ROOTS" env-delim:"," description:"additional local library root (repeatable)"`
		ExistingScanEvery time.Duration `long:"existing-files-scan-every" env:"EXISTING_FILES_SCAN_EVERY" default:"24h" description:"additional local library rescan interval"`

		WebDav struct {
			Server   string `long:"server" env:"SERVER" default:"https://dav.yandex.ru" description:"webdav server"`
			User     string `long:"user" env:"USER" default:"guest" description:"webdav user"`
			Password string `long:"password" env:"PASSWORD" description:"webdav password"`
		} `group:"WebDav Сервер" namespace:"webdav" env-namespace:"WEBDAV"`

		Util struct {
			ClearRemote bool `long:"clear-remote" env:"CLEAR_REMOTE" description:"clear remote files"`
		} `group:"Утилиты" namespace:"util" env-namespace:"UTIL"`
	}{}
)

func main() {
	app := app.New("Webdav Downloader", revision, &opts)
	opts.ExistingRoots = normalizeRoots(opts.ExistingRoots)
	if opts.ExistingScanEvery <= 0 {
		app.Log().Logf("[ERROR] existing files scan interval must be positive")
		os.Exit(2)
	}
	if err := validateRoots(opts.ExistingRoots); err != nil {
		app.Log().Logf("[ERROR] existing files configuration error: %v", err)
		os.Exit(2)
	}

	var existingFiles engine.ExistingFileFinder
	if len(opts.ExistingRoots) > 0 {
		index := localindex.New(opts.ExistingRoots)
		started := time.Now()
		app.Log().Logf("[INFO] initial local library scan started")
		count, err := index.Refresh()
		if err != nil {
			app.Log().Logf("[ERROR] initial local library scan failed: %v", err)
			os.Exit(2)
		}
		app.Log().Logf("[INFO] initial local library scan completed: %d files in %v", count, time.Since(started).Round(time.Millisecond))
		existingFiles = index
		go index.Run(app.Context(), app.Log(), opts.ExistingScanEvery)
	}

	{
		config := engine.Config{
			InputPath:    opts.Input,
			OutputPath:   opts.Output,
			TempPath:     opts.Temp,
			Concurrency:  opts.Threads,
			ScanEvery:    time.Second * time.Duration(opts.Timeout),
			RemoveRemote: opts.ClearRemote,
		}

		wd := gowebdav.NewClient(opts.WebDav.Server, opts.WebDav.User, opts.WebDav.Password)
		err := wd.Connect()
		if err != nil {
			app.Log().Logf("[ERROR] webdav error: %v", err)
			os.Exit(2)
		}

		targetAction := targetAction()
		switch targetAction {
		case ActionClearRemote:
			utils := utils.New(wd, opts.Output, opts.Input)
			err := utils.ClearRemoteFiles()
			if err != nil {
				app.Log().Logf("[ERROR] clear remote files error: %v", err)
				os.Exit(2)
			}

			os.Exit(0)
		default:
			queue, err := queue.New(opts.QueueFile)
			if err != nil {
				app.Log().Logf("[ERROR] queue error: %v", err)
				os.Exit(2)
			}

			files := files.New(wd)

			engine := engine.New(app.Log(), config, files, files, queue, existingFiles)
			engine.Start(app.Context())
		}
	}

	app.GS(time.Second * 10)
}

func normalizeRoots(roots []string) []string {
	result := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}

		root = filepath.Clean(root)
		if _, ok := seen[root]; ok {
			continue
		}

		seen[root] = struct{}{}
		result = append(result, root)
	}

	return result
}

func validateRoots(roots []string) error {
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			return fmt.Errorf("library root %q: %w", root, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("library root %q is not a directory", root)
		}

		dir, err := os.Open(root)
		if err != nil {
			return fmt.Errorf("open library root %q: %w", root, err)
		}
		_, readErr := dir.Readdirnames(1)
		closeErr := dir.Close()
		if readErr != nil && readErr != io.EOF {
			return fmt.Errorf("read library root %q: %w", root, readErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close library root %q: %w", root, closeErr)
		}
	}

	return nil
}

type Action string

const (
	ActionNone        Action = "none"
	ActionClearRemote Action = "clear-remote"
)

func targetAction() Action {
	if opts.Util.ClearRemote {
		return ActionClearRemote
	}

	return ActionNone
}
