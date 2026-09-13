package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ReanSn0w/wddl/pkg/control"
)

const (
	watchRenderEvery = 250 * time.Millisecond
	watchStatusEvery = 5 * time.Second
)

func runInteractiveWatch(ctx context.Context, client watchClient, renderer interactiveWatchRenderer) (resultErr error) {
	status, err := client.Status(ctx)
	if err != nil {
		return fmt.Errorf("initial watch status: %w", err)
	}
	dashboard := newWatchDashboard(status)
	defer func() {
		if closeErr := renderer.Close(); resultErr == nil && closeErr != nil {
			resultErr = closeErr
		}
	}()
	if err := renderer.Render(dashboard, time.Now()); err != nil {
		return err
	}

	watchCtx, cancel := context.WithCancel(ctx)
	events := make(chan control.Event, 64)
	watchDone := make(chan error, 1)
	watchFinished := false
	go func() {
		watchDone <- client.Watch(watchCtx, func(event control.Event) error {
			select {
			case events <- event:
				return nil
			case <-watchCtx.Done():
				return watchCtx.Err()
			}
		})
	}()
	defer func() {
		cancel()
		if !watchFinished {
			<-watchDone
		}
	}()

	renderTicker := time.NewTicker(watchRenderEvery)
	statusTicker := time.NewTicker(watchStatusEvery)
	defer renderTicker.Stop()
	defer statusTicker.Stop()
	dirty := false

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-watchDone:
			watchFinished = true
			if err == nil || errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return nil
			}
			return err
		case event := <-events:
			dashboard.ApplyEvent(event)
			dirty = true
			if event.Dropped > 0 {
				if fresh, statusErr := client.Status(ctx); statusErr == nil {
					dashboard.ApplyStatus(fresh)
				}
			}
		case <-statusTicker.C:
			if fresh, statusErr := client.Status(ctx); statusErr == nil {
				dashboard.ApplyStatus(fresh)
				dirty = true
			}
		case now := <-renderTicker.C:
			if !dirty {
				continue
			}
			if err := renderer.Render(dashboard, now); err != nil {
				return err
			}
			dirty = false
		}
	}
}
