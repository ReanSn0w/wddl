package control

import (
	"context"
	"testing"
	"time"
)

func TestBrokerDoesNotBlockOnSlowSubscriber(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	broker := NewBroker()
	events, unsubscribe := broker.Subscribe(ctx)
	defer unsubscribe()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			broker.Publish(Event{Type: "progress"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on slow subscriber")
	}

	for len(events) > 0 {
		<-events
	}
	broker.Publish(Event{Type: "next"})
	select {
	case event := <-events:
		if event.Dropped == 0 {
			t.Fatal("dropped event count = 0")
		}
	case <-time.After(time.Second):
		t.Fatal("next event was not delivered")
	}
}
