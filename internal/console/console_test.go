package console_test

import (
	"context"
	"os"

	"git.papkovda.ru/tools/webdav/internal/console"
	"github.com/go-pkgz/lgr"
	"github.com/studio-b12/gowebdav"
)

var (
	WebdavServer   = os.Getenv("WEBDAV_SERVER")
	WebdavUser     = os.Getenv("WEBDAV_USER")
	WebdavPassword = os.Getenv("WEBDAV_PASSWORD")

	webdavClient *gowebdav.Client
	consoleApp   *console.Console
)

func init() {
	webdavClient = gowebdav.NewClient(WebdavServer, WebdavUser, WebdavPassword)
	err := webdavClient.Connect()
	if err != nil {
		panic(err)
	}

	consoleApp = console.New(context.Background(), lgr.Default(), webdavClient)
}
