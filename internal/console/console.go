package console

import (
	"context"
	"errors"

	"git.papkovda.ru/library/gokit/pkg/app"
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

func (c *Console) Run(action Action) error {
	switch action {
	case LS:
		conf := LSConfig{}
		err := app.ParseConfiguration(&conf)
		if err != nil {
			return err
		}

		return c.LS(&conf)
	case DL:
		return errors.New("not implemented")
	case UL:
		return errors.New("not implemented")
	case RM:
		return errors.New("not implemented")
	case Sync:
		return errors.New("not implemented")
	default:
		return errors.New("action not implemented")
	}
}
