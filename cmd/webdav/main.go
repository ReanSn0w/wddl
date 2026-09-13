package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ReanSn0w/wddl/pkg/config"
	"github.com/ReanSn0w/wddl/pkg/control"
	"github.com/ReanSn0w/wddl/pkg/daemon"
	"github.com/ReanSn0w/wddl/pkg/engine"
	"github.com/ReanSn0w/wddl/pkg/files"
	"github.com/ReanSn0w/wddl/pkg/localindex"
	"github.com/ReanSn0w/wddl/pkg/queue"
	"github.com/go-pkgz/lgr"
	"github.com/studio-b12/gowebdav"
	"github.com/umputun/go-flags"
)

var revision = "unknown"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

func run(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	parsed, err := parseCLI(args)
	if err != nil {
		var flagErr *flags.Error
		if errors.As(err, &flagErr) && flagErr.Type == flags.ErrHelp {
			fmt.Fprint(stdout, flagErr.Message)
			return 0
		}
		fmt.Fprintf(stderr, "wddl: %v\n", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := executeCommand(ctx, parsed, stdout, getenv); err != nil {
		fmt.Fprintf(stderr, "wddl: %v\n", err)
		var apiErr *control.APIError
		if errors.As(err, &apiErr) && apiErr.Code == control.CodePartial {
			return 3
		}
		return 1
	}
	return 0
}

func runDaemon(ctx context.Context, configPath string, getenv func(string) string) error {
	credentials, err := loadCredentials(getenv)
	if err != nil {
		return fmt.Errorf("credentials: %w", err)
	}
	conf, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	log := configureLogger(conf.Logging.Debug)
	log.Logf("[INFO] Application: Webdav Downloader (rev: %v)", revision)

	if err := validateRoots(conf.ExistingFiles.Roots); err != nil {
		return fmt.Errorf("existing files configuration: %w", err)
	}
	if err := validateWritableDirs(conf.Download.Destination, conf.Download.Temp, filepath.Dir(conf.Queue.File)); err != nil {
		return fmt.Errorf("writable storage configuration: %w", err)
	}
	index := localindex.New(conf.ExistingFiles.Roots)
	var existingFiles engine.ExistingFileFinder
	if len(conf.ExistingFiles.Roots) > 0 {
		existingFiles = index
	}

	wd := gowebdav.NewClient(conf.WebDAV.Server, credentials.User, credentials.Password)
	if err := wd.Connect(); err != nil {
		return fmt.Errorf("webdav: %w", err)
	}
	tasks, err := queue.New(conf.Queue.File)
	if err != nil {
		return fmt.Errorf("queue: %w", err)
	}
	storage := files.New(wd)
	downloader := engine.New(log, toEngineConfig(conf), storage, storage, tasks, existingFiles)
	service := daemon.New(conf, revision, log, downloader, tasks, index, wd)
	return service.Run(ctx)
}

func configureLogger(debug bool) lgr.L {
	options := []lgr.Option{lgr.Msec, lgr.LevelBraces}
	if debug {
		options = append(options, lgr.Debug, lgr.CallerFile, lgr.CallerFunc)
	}
	lgr.Setup(options...)
	return lgr.Default()
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

func validateWritableDirs(paths ...string) error {
	for _, target := range paths {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return fmt.Errorf("create %q: %w", target, err)
		}
		probe, err := os.CreateTemp(target, ".wddl-write-check-*")
		if err != nil {
			return fmt.Errorf("directory %q is not writable: %w", target, err)
		}
		name := probe.Name()
		if err := probe.Close(); err != nil {
			_ = os.Remove(name)
			return fmt.Errorf("close write probe in %q: %w", target, err)
		}
		if err := os.Remove(name); err != nil {
			return fmt.Errorf("remove write probe in %q: %w", target, err)
		}
	}
	return nil
}
