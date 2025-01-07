package main

import (
	"context"
	"os"
	"time"

	"git.papkovda.ru/library/gokit/pkg/app"
	"git.papkovda.ru/tools/webdav/pkg/detector"
	"git.papkovda.ru/tools/webdav/pkg/downloader"
	"git.papkovda.ru/tools/webdav/pkg/queue"
	"github.com/studio-b12/gowebdav"
)

var (
	revision = "unknown"
	opts     = struct {
		app.Debug

		Server   string `short:"s" long:"server" env:"SERVER" default:"https://dav.yandex.ru" description:"webdav server"`
		User     string `short:"u" long:"user" env:"USER" default:"guest" description:"webdav user"`
		Password string `short:"p" long:"password" env:"PASSWORD" description:"webdav password"`

		Input   string `short:"i" long:"input" env:"INPUT" description:"input path"`
		Output  string `short:"o" long:"output" env:"OUTPUT" description:"output path"`
		Threads int    `long:"threads" env:"THREADS" default:"5" description:"parallel downloads"`
		Timeout int    `long:"timeout" env:"TIMEOUT" default:"600" description:"rescan timeout"`
	}{}
)

func main() {
	app := app.New("Webdav Downloader", revision, &opts)

	{
		wd := gowebdav.NewClient(opts.Server, opts.User, opts.Password)
		err := wd.Connect()
		if err != nil {
			app.Log().Logf("[ERROR] webdav error: %v", err)
			os.Exit(2)
		}

		q := queue.New(app.Context())

		detectorLoop := detector.
			New(wd, q).
			WithDestinationPath(opts.Output).
			WithSourcePath(opts.Input).
			Run(time.Second * time.Duration(opts.Timeout))

		app.Add(func(ctx context.Context) {
			detectorLoop.Stop()
		})

		dl := downloader.
			New(wd, q, opts.Threads)

		dl.Start(app.Context())
	}

	app.GS(time.Second * 10)
}
