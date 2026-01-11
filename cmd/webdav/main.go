package main

import (
	"os"
	"time"

	"git.papkovda.ru/library/gokit/pkg/app"
	"github.com/ReanSn0w/wddl/pkg/engine"
	"github.com/ReanSn0w/wddl/pkg/files"
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

		DBFile      string `long:"db-file" env:"DB_FILE" default:"./wddl.db" description:"database file"`
		Threads     int    `long:"threads" env:"THREADS" default:"4" description:"parallel downloads"`
		Timeout     int    `long:"timeout" env:"TIMEOUT" default:"600" description:"rescan timeout (seconds)"`
		ClearRemote bool   `long:"clear-remote" env:"CLEAR_REMOTE" description:"clear remote files"`

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
			queue, err := queue.New(opts.DBFile)
			if err != nil {
				app.Log().Logf("[ERROR] queue error: %v", err)
				os.Exit(2)
			}

			files := files.New(wd)

			engine := engine.New(app.Log(), config, files, files, queue)
			engine.Start(app.Context())
		}
	}

	app.GS(time.Second * 10)
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
