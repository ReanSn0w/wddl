package tool

import (
	"context"
	"time"
)

// NewLoop функция создает новую структуру для решения
// повторяющихся задач
func NewLoop(task func()) *Loop {
	return &Loop{
		timer: time.NewTimer(0),
		task:  task,
	}
}

// Loop Структура для работы с циклической задачей
type Loop struct {
	timer  *time.Timer
	task   func()
	cancel func()
}

// Запуск задачи единоразово здесь и сейчас
func (r *Loop) Once() {
	r.task()
}

// Останавливает цикл выполнения задачи
func (r *Loop) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
}

// Запускает цикл выполнения задачи
// в случае если задача уже запущена
func (r *Loop) Run(d time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())

	r.cancel = cancel
	r.timer.Reset(d)

	go func() {
		for {
			select {
			case <-r.timer.C:
				r.task()
				r.timer.Reset(d)
			case <-ctx.Done():
				r.cancel = nil
				break
			}
		}
	}()
}
