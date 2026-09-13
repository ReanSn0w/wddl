package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ReanSn0w/wddl/pkg/control"
)

func TestPlainWatchFormatsProgressWithoutANSIAndResyncsDroppedEvents(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	client := &fakeWatchClient{
		status: control.Status{Pending: 2, Suspended: 1, Active: []control.ActiveDownload{{ID: "active"}}, IndexedFiles: 1500},
		events: []control.Event{{
			Time: now, Type: "download.progress", ID: "id", Message: "movie.mkv", Dropped: 2,
			Data: control.DownloadProgress{ID: "id", Name: "movie.mkv", Downloaded: 512, Size: 1024, Percent: 50, Speed: 128},
		}},
	}
	var output bytes.Buffer
	if err := runPlainWatch(context.Background(), client, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{"movie.mkv", "50.0%", "512 B/1.0 KiB", "128 B/s", "ETA 4s", "dropped 2", "status.resynced"} {
		if !strings.Contains(got, want) {
			t.Errorf("output misses %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("plain output contains ANSI: %q", got)
	}
	if client.statusCalls != 1 {
		t.Fatalf("Status calls = %d, want 1", client.statusCalls)
	}
}

func TestJSONWatchRemainsNDJSONWithoutANSI(t *testing.T) {
	client := &fakeWatchClient{events: []control.Event{{
		Type: "download.progress", ID: "id", Message: "movie.mkv",
		Data: control.DownloadProgress{ID: "id", Name: "movie.mkv", Downloaded: 1, Size: 2, Percent: 50, Speed: 1},
	}}}
	var output bytes.Buffer
	if err := runJSONWatch(context.Background(), client, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Count(got, "\n") != 1 || !strings.Contains(got, `"type":"download.progress"`) || !strings.Contains(got, `"Downloaded":1`) {
		t.Fatalf("JSON output = %q", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("JSON output contains ANSI: %q", got)
	}
}

func TestDashboardRendersMultipleDownloadsAndFitsWidth(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	next := now.Add(90 * time.Minute)
	dashboard := newWatchDashboard(control.Status{
		Pending: 3, Suspended: 1, IndexedFiles: 1542, RemoteScan: control.ScanState{NextRun: &next}, LocalScan: control.ScanState{Running: true},
		Active: []control.ActiveDownload{
			{ID: "b", Name: "Очень длинное имя второго видеофайла.mkv", Size: 200, Downloaded: 50, Percent: 25},
			{ID: "a", Name: "movie.mkv", Size: 100, Downloaded: 50, Percent: 50, Speed: 20},
		},
	})
	dashboard.ApplyEvent(control.Event{Time: now, Type: "download.failed", ID: "old", Message: "old.mkv", Data: map[string]any{"error": "network error"}})
	lines := dashboard.Lines(100, now)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"2 active, 3 waiting, 1 suspended", "movie.mkv", "50.0%", "ETA --", "Remote scan: next in 1h30m", "Local scan: running", "network error"} {
		if !strings.Contains(joined, want) {
			t.Errorf("dashboard misses %q:\n%s", want, joined)
		}
	}
	for _, line := range lines {
		if utf8.RuneCountInString(line) > 100 {
			t.Errorf("line is wider than terminal: %q", line)
		}
		if !utf8.ValidString(line) {
			t.Errorf("line is invalid UTF-8: %q", line)
		}
	}
}

func TestANSIRendererRestoresCursorWithoutAlternateScreen(t *testing.T) {
	var output bytes.Buffer
	renderer := newANSIWatchRenderer(&output, func() int { return 60 })
	dashboard := newWatchDashboard(control.Status{})
	if err := renderer.Render(dashboard, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := renderer.Render(dashboard, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := renderer.Close(); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "\x1b[?25l") || !strings.Contains(got, "\x1b[?25h\n") || !strings.Contains(got, "\x1b[2K") {
		t.Fatalf("renderer output misses cursor lifecycle: %q", got)
	}
	if strings.Contains(got, "\x1b[?1049") {
		t.Fatalf("renderer used alternate screen: %q", got)
	}
}

func TestInteractiveWatchStopsClientAndClosesRendererOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &blockingWatchClient{started: make(chan struct{})}
	renderer := &recordingWatchRenderer{}
	done := make(chan error, 1)
	go func() { done <- runInteractiveWatch(ctx, client, renderer) }()

	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("watch stream did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runInteractiveWatch() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("interactive watch did not stop")
	}
	if !renderer.closed || renderer.renders == 0 {
		t.Fatalf("renderer = %#v", renderer)
	}
}

func TestInteractiveWatchResyncsAfterDroppedEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &resyncWatchClient{resynced: make(chan struct{})}
	renderer := &recordingWatchRenderer{}
	done := make(chan error, 1)
	go func() { done <- runInteractiveWatch(ctx, client, renderer) }()

	select {
	case <-client.resynced:
	case <-time.After(time.Second):
		t.Fatal("status was not refreshed after dropped event")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("runInteractiveWatch() error = %v", err)
	}
}

func TestInteractiveWatchClosesRendererWhenSocketBreaks(t *testing.T) {
	renderer := &recordingWatchRenderer{}
	want := errors.New("socket closed")
	err := runInteractiveWatch(context.Background(), &errorWatchClient{err: want}, renderer)
	if !errors.Is(err, want) {
		t.Fatalf("runInteractiveWatch() error = %v, want %v", err, want)
	}
	if !renderer.closed {
		t.Fatal("renderer was not closed")
	}
}

type fakeWatchClient struct {
	status      control.Status
	events      []control.Event
	statusCalls int
}

func (c *fakeWatchClient) Status(context.Context) (control.Status, error) {
	c.statusCalls++
	return c.status, nil
}

func (c *fakeWatchClient) Watch(_ context.Context, consume func(control.Event) error) error {
	for _, event := range c.events {
		if err := consume(event); err != nil {
			return err
		}
	}
	return nil
}

type blockingWatchClient struct {
	started chan struct{}
	once    sync.Once
}

type resyncWatchClient struct {
	mu       sync.Mutex
	calls    int
	resynced chan struct{}
	once     sync.Once
}

func (c *resyncWatchClient) Status(context.Context) (control.Status, error) {
	c.mu.Lock()
	c.calls++
	calls := c.calls
	c.mu.Unlock()
	if calls == 2 {
		c.once.Do(func() { close(c.resynced) })
	}
	return control.Status{}, nil
}

func (c *resyncWatchClient) Watch(ctx context.Context, consume func(control.Event) error) error {
	if err := consume(control.Event{Type: "download.progress", Dropped: 1}); err != nil {
		return err
	}
	<-ctx.Done()
	return ctx.Err()
}

type errorWatchClient struct{ err error }

func (c *errorWatchClient) Status(context.Context) (control.Status, error) {
	return control.Status{}, nil
}

func (c *errorWatchClient) Watch(context.Context, func(control.Event) error) error {
	return c.err
}

func (c *blockingWatchClient) Status(context.Context) (control.Status, error) {
	return control.Status{}, nil
}

func (c *blockingWatchClient) Watch(ctx context.Context, _ func(control.Event) error) error {
	c.once.Do(func() { close(c.started) })
	<-ctx.Done()
	return ctx.Err()
}

type recordingWatchRenderer struct {
	renders int
	closed  bool
}

func (r *recordingWatchRenderer) Render(*watchDashboard, time.Time) error {
	r.renders++
	return nil
}

func (r *recordingWatchRenderer) Close() error {
	r.closed = true
	return nil
}
