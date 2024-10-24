package main

import (
	"os"

	"git.papkovda.ru/library/gokit/pkg/app"
	"git.papkovda.ru/tools/webdav/internal/console"
	"github.com/studio-b12/gowebdav"
)

var (
	revision = "unknown"
	opts     = struct {
		app.Debug

		Server   string         `short:"s" long:"server" default:"dav.yandex.ru" default:"webdav server"`
		User     string         `short:"u" long:"user" default:"guest" description:"webdav user"`
		Password string         `short:"p" long:"password" description:"webdav password"`
		Action   console.Action `short:"a" long:"action" default:"sync" description:"app action"`
	}{}
)

func main() {
	app := app.New("Webdav Client", revision, &opts)

	wd := gowebdav.NewClient(opts.Server, opts.User, opts.Password)
	err := wd.Connect()
	if err != nil {
		app.Log().Logf("[ERROR] webdav error: %v", err)
		os.Exit(2)
	}

	con := console.New(app.Context(), app.Log(), wd)
	err = con.Run(opts.Action)
	if err != nil {
		app.Log().Logf("[ERROR] operation failed: %v", err)
		os.Exit(2)
	}
}
