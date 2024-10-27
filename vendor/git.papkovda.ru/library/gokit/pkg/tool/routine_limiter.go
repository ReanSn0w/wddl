package tool

import "sync"

// NewRoutineLimiter Создает новую структуру для ограничения
// количества одновременно запущенных горутин
func NewRoutineLimiter(max int) *RoutineLimiter {
	return &RoutineLimiter{
		ch: make(chan struct{}, max),
	}
}

type RoutineLimiter struct {
	wg sync.WaitGroup
	ch chan struct{}
}

// Run запускает задачу в горутине
// в случае если доступных слотов нет,
// то ожидает освобождения слота
// перед запуском задачи
func (r *RoutineLimiter) Run(task func()) {
	r.wg.Add(1)

	go func() {
		r.ch <- struct{}{}
		task()
		<-r.ch
		r.wg.Done()
	}()
}

// Wait метод позволяет дождатся завершения
// всех go-рутин
func (r *RoutineLimiter) Wait() {
	r.wg.Wait()
}
