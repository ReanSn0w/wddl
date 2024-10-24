package main

import (
	"errors"
	"os"

	"git.papkovda.ru/library/gokit/pkg/app"
	"git.papkovda.ru/tools/webdav/internal/console"
	"github.com/studio-b12/gowebdav"
)

var (
	revision = "unknown"
	opts     = struct {
		app.Debug

		Server   string `short:"s" long:"server" env:"SERVER" default:"https://dav.yandex.ru" default:"webdav server"`
		User     string `short:"u" long:"user" env:"USER" default:"guest" description:"webdav user"`
		Password string `long:"password" env:"PASSWORD" description:"webdav password"`

		Path   string `short:"p" long:"path" description:"directory path"`
		Output string `short:"o" long:"output" description:"result output"`
	}{}
)

func main() {
	app := app.New("Webdav Client", revision, &opts)
	action := ""
	if len(os.Args) > 1 {
		action = os.Args[1]
	}

	wd := gowebdav.NewClient(opts.Server, opts.User, opts.Password)
	err := wd.Connect()
	if err != nil {
		app.Log().Logf("[ERROR] webdav error: %v", err)
		os.Exit(2)
	}

	con := console.New(app.Context(), app.Log(), wd)

	{
		switch action {
		case "ls":
			err = con.LS(opts.Path)
		case "dl":
			err = con.DL(opts.Path, opts.Output)
		default:
			err = errors.New("unknown case")
		}
	}

	if err != nil {
		app.Log().Logf("[ERROR] operation failed: %v", err)
		os.Exit(2)
	}
}
