package app

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/go-pkgz/lgr"
	"golang.org/x/term"
)

func New(title, revision string, opts any) *Application {
	log, err := LoadConfiguration(title, revision, opts)
	if err != nil {
		os.Exit(2)
	}

	ctx, cancel := context.WithCancelCause(context.Background())

	return &Application{
		gs:     NewGracefulShutdown(lgr.Default()),
		log:    log,
		ctx:    ctx,
		cancel: cancel,
	}
}

type Application struct {
	ctx    context.Context
	cancel context.CancelCauseFunc

	log lgr.L
	gs  *GracefulShutdown
}

// Add - Добавляет функцию, которая будет выполнена перед завершением приложения в очередь
func (a *Application) Add(fn func(ctx context.Context)) {
	a.gs.Add(fn)
}

// GS - Обявляет точку для ожидания завершения приложения
func (a *Application) GS(timeout time.Duration) {
	a.gs.Wait(a.ctx, timeout)
}

// Log - возвращает логгер
func (a *Application) Log() lgr.L {
	return a.log
}

// Context - возвращает глобальный контекст
func (a *Application) Context() context.Context {
	return a.ctx
}

// Cancel - возвращает функцию для завершения приложения
func (a *Application) Cancel() context.CancelFunc {
	return func() {
		a.cancel(errors.New("context cancelled"))
	}
}

// CancelCause - возвращает функцию для завершения приложения
// с возможностью передать ошибку в качестве причины завершения
func (a *Application) CancelCause() context.CancelCauseFunc {
	return a.cancel
}

// AnyKeyToExit - реализует выход из приложения после нажатия любой клавиши пользователем
//
// https://stackoverflow.com/questions/15159118/read-a-character-from-standard-input-in-go-without-pressing-enter
func (a *Application) EnableAnyKeyToExit() {
	go func() {
		a.log.Logf("[INFO] press any key to exit")
		oldState, _ := term.MakeRaw(int(os.Stdin.Fd()))
		defer term.Restore(int(os.Stdin.Fd()), oldState)
		b := make([]byte, 1)
		os.Stdin.Read(b)
		a.Cancel()
	}()
}
