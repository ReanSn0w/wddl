package control

import (
	"context"
	"sync"
	"time"
)

type subscription struct {
	ch      chan Event
	dropped uint64
}

// Broker fan-outs runtime events without ever blocking the downloader. A slow
// subscriber receives the number of events skipped with its next event.
type Broker struct {
	mu     sync.Mutex
	nextID uint64
	subs   map[uint64]*subscription
}

func NewBroker() *Broker { return &Broker{subs: make(map[uint64]*subscription)} }

func (b *Broker) Publish(event Event) {
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, sub := range b.subs {
		copy := event
		copy.Dropped = sub.dropped
		select {
		case sub.ch <- copy:
			sub.dropped = 0
		default:
			sub.dropped++
		}
	}
}

func (b *Broker) Subscribe(ctx context.Context) (<-chan Event, func()) {
	b.mu.Lock()
	id := b.nextID
	b.nextID++
	sub := &subscription{ch: make(chan Event, 64)}
	b.subs[id] = sub
	b.mu.Unlock()
	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, id)
			close(sub.ch)
			b.mu.Unlock()
		})
	}
	go func() {
		<-ctx.Done()
		unsubscribe()
	}()
	return sub.ch, unsubscribe
}
