package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"git.papkovda.ru/library/gokit/pkg/app"
	"github.com/ReanSn0w/wddl/pkg/config"
	"github.com/ReanSn0w/wddl/pkg/engine"
	"github.com/ReanSn0w/wddl/pkg/files"
	"github.com/ReanSn0w/wddl/pkg/localindex"
	"github.com/ReanSn0w/wddl/pkg/queue"
	"github.com/ReanSn0w/wddl/pkg/utils"
	"github.com/go-pkgz/lgr"
	"github.com/studio-b12/gowebdav"
)

var (
	revision = "unknown"
	opts     bootstrapOptions
)

func main() {
	app := app.New("Webdav Downloader", revision, &opts)
	credentials, err := loadCredentials(os.Getenv)
	if err != nil {
		app.Log().Logf("[ERROR] credentials error: %v", err)
		os.Exit(2)
	}
	conf, err := config.Load(opts.ConfigPath)
	if err != nil {
		app.Log().Logf("[ERROR] configuration error: %v", err)
		os.Exit(2)
	}
	log := configureLogger(conf.Logging.Debug)
	log.Logf("[INFO] Application: Webdav Downloader (rev: %v)", revision)

	if err := validateRoots(conf.ExistingFiles.Roots); err != nil {
		log.Logf("[ERROR] existing files configuration error: %v", err)
		os.Exit(2)
	}

	var existingFiles engine.ExistingFileFinder
	if len(conf.ExistingFiles.Roots) > 0 {
		index := localindex.New(conf.ExistingFiles.Roots)
		started := time.Now()
		log.Logf("[INFO] initial local library scan started")
		count, err := index.Refresh()
		if err != nil {
			log.Logf("[ERROR] initial local library scan failed: %v", err)
			os.Exit(2)
		}
		log.Logf("[INFO] initial local library scan completed: %d files in %v", count, time.Since(started).Round(time.Millisecond))
		existingFiles = index
		go index.Run(app.Context(), log, conf.ExistingFiles.ScanEvery.Value())
	}

	{
		engineConfig := toEngineConfig(conf)

		wd := gowebdav.NewClient(conf.WebDAV.Server, credentials.User, credentials.Password)
		err := wd.Connect()
		if err != nil {
			log.Logf("[ERROR] webdav error: %v", err)
			os.Exit(2)
		}

		targetAction := targetAction()
		switch targetAction {
		case ActionClearRemote:
			utils := utils.New(wd, conf.Download.Destination, conf.WebDAV.Root, existingFiles)
			err := utils.ClearRemoteFiles()
			if err != nil {
				log.Logf("[ERROR] clear remote files error: %v", err)
				os.Exit(2)
			}

			os.Exit(0)
		default:
			queue, err := queue.New(conf.Queue.File)
			if err != nil {
				log.Logf("[ERROR] queue error: %v", err)
				os.Exit(2)
			}

			files := files.New(wd)

			engine := engine.New(log, engineConfig, files, files, queue, existingFiles)
			engine.Start(app.Context())
		}
	}

	app.GS(time.Second * 10)
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
