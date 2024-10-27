package tool

import (
	"context"
	"sync"
	"time"

	"github.com/go-pkgz/lgr"
)

const (
	MainToken TRKey = "main_token"
)

type TRKey string

func NewTokenRefresher(log lgr.L, name string) *TokenRefresher {
	return &TokenRefresher{
		log:  log,
		data: make(map[TRKey]interface{}),
	}
}

type (
	TokenRefresherFunc func(TokenRefresherGetSet) error

	TokenRefresher struct {
		mx   sync.RWMutex
		log  lgr.L
		name string
		data map[TRKey]interface{}
	}

	Token interface {
		Main() (interface{}, bool)
	}

	TokenRefresherGetSet interface {
		Get(key TRKey) (interface{}, bool)
		Set(key TRKey, val interface{})
	}
)

func (tr *TokenRefresher) Main() (interface{}, bool) {
	return tr.Get(MainToken)
}

func (tr *TokenRefresher) Get(key TRKey) (interface{}, bool) {
	tr.mx.RLock()
	defer tr.mx.RUnlock()
	val, ok := tr.data[key]
	return val, ok
}

func (tr *TokenRefresher) Set(key TRKey, val interface{}) {
	tr.mx.Lock()
	defer tr.mx.Unlock()
	tr.data[key] = val
}

func (tr *TokenRefresher) With(key TRKey, value interface{}) *TokenRefresher {
	tr.mx.Lock()
	defer tr.mx.Unlock()
	tr.data[key] = value
	return tr
}

func (tr *TokenRefresher) MustStart(ctx context.Context, timerDuration time.Duration, fn TokenRefresherFunc) Token {
	t, err := tr.Start(ctx, timerDuration, fn)
	if err != nil {
		panic(err)
	}

	return t
}

func (tr *TokenRefresher) Start(ctx context.Context, timerDuration time.Duration, fn TokenRefresherFunc) (Token, error) {
	err := fn(tr)

	if err == nil {
		go func() {
			var i time.Duration = 0
			doneCtx := ctx.Done()
			t := time.NewTimer(timerDuration)

			for {
				select {
				case <-doneCtx:
					tr.log.Logf("[INFO] (%v) token refresher context ended", tr.name)
					return
				case <-t.C:
					tr.log.Logf("[DEBUG] (%v) token refresh started", tr.name)
					err := fn(tr)

					if err != nil {
						i += 1
						tr.log.Logf("[ERROR] (%v) token refresh error (restart after: %v seconds): %v", tr.name, i*3, err)
						t.Reset(time.Second * i)
					} else {
						i = 0
						tr.log.Logf("[DEBUG] (%v) token refresh complete (restart after: %v)", tr.name, timerDuration)
						t.Reset(timerDuration)
					}
				}
			}
		}()

		return tr, nil
	} else {
		return nil, err
	}
}
