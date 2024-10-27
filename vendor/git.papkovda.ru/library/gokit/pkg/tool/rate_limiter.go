package tool

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrRateLimited = errors.New("rate limited")
)

func NewRateLimiter(duration time.Duration) *RateLimiter {
	return &RateLimiter{
		duration: duration,
		values:   make(map[string]time.Time),
	}
}

type RateLimiter struct {
	duration time.Duration
	values   map[string]time.Time
	mutex    sync.Mutex
}

func (r *RateLimiter) Do(key string, fn func() error) error {
	if err := r.check(key); err != nil {
		return err
	}

	return fn()
}

func (r *RateLimiter) check(key string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	t := r.values[key]
	if t.After(time.Now()) {
		return ErrRateLimited
	}

	r.values[key] = time.Now().Add(r.duration)
	return nil
}
