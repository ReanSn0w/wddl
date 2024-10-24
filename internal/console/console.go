package console

import (
	"context"

	"github.com/go-pkgz/lgr"
	"github.com/studio-b12/gowebdav"
)

func New(ctx context.Context, log lgr.L, cl *gowebdav.Client) *Console {
	return &Console{
		log:    log,
		ctx:    ctx,
		client: cl,
	}
}

type Console struct {
	log    lgr.L
	ctx    context.Context
	client *gowebdav.Client
}
