package app

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-pkgz/lgr"
)

// NewGracefulShutdown создает сруктуру для плавного отключения приложения
func NewGracefulShutdown(log lgr.L) *GracefulShutdown {
	return &GracefulShutdown{
		funcs: make([]func(ctx context.Context), 0),
		log:   log,
	}
}

// GracefulShutdown структура для плавного отключения приложения
//
// Содержит стек функций которые необходимо выполнить перед завершением приложения
// и методы для их добавления и выполнения
type GracefulShutdown struct {
	funcs []func(ctx context.Context)
	log   lgr.L
}

// Add добавляет функции которые необходимо выполнить
// перед завершением приложения в стек
func (gs *GracefulShutdown) Add(fn func(ctx context.Context)) {
	gs.funcs = append(gs.funcs, fn)
}

// Wait подписывается на системные уведомления об отклбчении
// и производит запуск функции Shutdown после получения сигналов
// syscall.SIGTERM, syscall.SIGINT
func (gs *GracefulShutdown) Wait(ctx context.Context, timeout time.Duration) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	select {
	case <-ctx.Done():
		gs.log.Logf("[INFO] cancel context")
	case registredSignal := <-quit:
		gs.log.Logf("[INFO] system signal is: %s", registredSignal.String())
	}

	ctx, done := context.WithTimeout(context.Background(), timeout)
	gs.shutdown(ctx, done)
}

func (st *GracefulShutdown) shutdown(ctx context.Context, done func()) {
	go func() {
		for _, fn := range st.funcs {
			if fn != nil {
				fn(ctx)
			}
		}

		done()
	}()

	<-ctx.Done()
}
