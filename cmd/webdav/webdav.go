package main

import (
	"context"
	"os"
	"time"

	"git.papkovda.ru/library/gokit/pkg/app"
	"git.papkovda.ru/tools/webdav/internal/sync"
	"github.com/studio-b12/gowebdav"
)

var (
	revision = "unknown"
	opts     = struct {
		app.Debug

		Server   string `short:"s" long:"server" env:"SERVER" default:"https://dav.yandex.ru" default:"webdav server"`
		User     string `short:"u" long:"user" env:"USER" default:"guest" description:"webdav user"`
		Password string `short:"p" long:"password" env:"PASSWORD" description:"webdav password"`

		Input  string `short:"i" long:"input" description:"input path"`
		Output string `short:"o" long:"output" description:"output path"`
		Sync   bool   `long:"sync" description:"sync mode will delete local files when it deleted on remote webdav storage"`
	}{}
)

func main() {
	app := app.New("Webdav Downloader", revision, &opts)

	wd := gowebdav.NewClient(opts.Server, opts.User, opts.Password)
	err := wd.Connect()
	if err != nil {
		app.Log().Logf("[ERROR] webdav error: %v", err)
		os.Exit(2)
	}

	s := sync.New(app.Log(), wd, opts.Input, opts.Output)
	s.Start(app.Context(), time.Minute*3)
	app.Add(func(ctx context.Context) {
		s.Stop()
	})

	app.GS(time.Second * 10)
}
