package tool

import (
	"time"

	"github.com/go-pkgz/lgr"
)

// NewRetry Создает новую структуру для повторения задачи
// в случае ошибки
func NewRetry(log lgr.L, max int, delay time.Duration) *Retry {
	return &Retry{
		log:   log,
		max:   max,
		delay: delay,
	}
}

type Retry struct {
	log   lgr.L
	max   int
	delay time.Duration
}

// Do выполняет задачу и в случае ошибки повторяет ее
// max раз с задержкой delay
func (r *Retry) Do(task func() error) error {
	var (
		taskID = NewID()
		err    error
	)

	r.log.Logf("[DEBUG] task %v started", taskID)

	for i := 0; i < r.max; i++ {
		if i > 0 {
			r.log.Logf("[DEBUG] task %v retry %v", taskID, i)
		}

		err = task()
		if err == nil {
			r.log.Logf("[DEBUG] task %v finished", taskID)
			break
		}

		retryDelay := r.delay * time.Duration(i)
		r.log.Logf("[ERROR] task %v failled. retrying after %v seconds. error: %v", taskID, retryDelay.Seconds(), err)
		time.Sleep(retryDelay)
	}

	return err
}
