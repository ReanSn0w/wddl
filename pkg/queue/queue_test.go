package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ReanSn0w/wddl/pkg/engine"
	"github.com/go-pkgz/lgr"
)

func TestNewMissingFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")

	q, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	length, err := q.Len()
	if err != nil {
		t.Fatalf("Len() error = %v", err)
	}
	if length != 0 {
		t.Fatalf("Len() = %d, want 0", length)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("queue file should not be created before a mutation, stat error = %v", err)
	}
}

func TestNewRejectsInvalidParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "queue.json")

	if _, err := New(path); err == nil {
		t.Fatal("New() error = nil, want invalid parent error")
	}
}

func TestNewLoadsValidQueue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	want := testFile("file-1", 4096)
	writeQueueFile(t, path, persistedQueue{
		Version: formatVersion,
		Files: []persistedFile{{
			ID:     want.ID,
			Name:   want.Name,
			Source: want.Source,
			Temp:   want.Temp,
			Dest:   want.Dest,
			Size:   want.Size,
		}},
	})

	q, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	items, err := q.List(nil)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 || items[0] != want {
		t.Fatalf("List() = %#v, want %#v", items, []engine.File{want})
	}
}

func TestNewRejectsInvalidQueueFilesWithoutOverwriting(t *testing.T) {
	validFile := testFile("file-1", 1)
	tests := []struct {
		name    string
		content string
	}{
		{name: "empty", content: ""},
		{name: "malformed", content: "{"},
		{name: "unknown version", content: `{"version":2,"files":[]}`},
		{name: "missing files", content: `{"version":1}`},
		{name: "unknown field", content: `{"version":1,"files":[],"extra":true}`},
		{
			name: "duplicate id",
			content: mustJSON(t, persistedQueue{
				Version: formatVersion,
				Files: []persistedFile{
					persistedFromEngine(validFile),
					persistedFromEngine(validFile),
				},
			}),
		},
		{
			name: "missing required item data",
			content: mustJSON(t, persistedQueue{
				Version: formatVersion,
				Files:   []persistedFile{{ID: "file-1"}},
			}),
		},
		{name: "multiple values", content: `{"version":1,"files":[]} {}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "queue.json")
			original := []byte(tt.content)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			if _, err := New(path); err == nil {
				t.Fatal("New() error = nil, want invalid queue error")
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			if string(got) != string(original) {
				t.Fatalf("invalid queue was overwritten: got %q, want %q", got, original)
			}
		})
	}
}

func TestAddPersistsAndReloadsDeterministically(t *testing.T) {
	q, path := newTestQueue(t)
	second := testFile("file-2", 2048)
	first := testFile("file-1", 1024)
	replacement := first
	replacement.Size = 4096

	for _, file := range []engine.File{second, first, replacement} {
		if err := q.Add(file); err != nil {
			t.Fatalf("Add(%q) error = %v", file.ID, err)
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("queue mode = %o, want 600", info.Mode().Perm())
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Index(string(raw), `"id": "file-1"`) > strings.Index(string(raw), `"id": "file-2"`) {
		t.Fatalf("queue entries are not sorted by id:\n%s", raw)
	}

	reloaded, err := New(path)
	if err != nil {
		t.Fatalf("New(reload) error = %v", err)
	}
	items, err := reloaded.List(nil)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 2 || items[0] != replacement || items[1] != second {
		t.Fatalf("reloaded items = %#v, want [%#v %#v]", items, replacement, second)
	}
}

func TestAddRejectsInvalidFile(t *testing.T) {
	q, path := newTestQueue(t)

	if err := q.Add(engine.File{ID: "file-1"}); err == nil {
		t.Fatal("Add() error = nil, want validation error")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid Add() created queue file, stat error = %v", err)
	}
}

func TestDeletePersistsAndMissingIDIsNoOp(t *testing.T) {
	q, path := newTestQueue(t)
	first := testFile("file-1", 1024)
	second := testFile("file-2", 2048)
	if err := q.Add(first); err != nil {
		t.Fatalf("Add(first) error = %v", err)
	}
	if err := q.Add(second); err != nil {
		t.Fatalf("Add(second) error = %v", err)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(before) error = %v", err)
	}
	if err := q.Delete("missing"); err != nil {
		t.Fatalf("Delete(missing) error = %v", err)
	}
	afterMissing, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(after missing) error = %v", err)
	}
	if string(afterMissing) != string(before) {
		t.Fatal("Delete(missing) rewrote queue file")
	}

	if err := q.Delete(first.ID); err != nil {
		t.Fatalf("Delete(first) error = %v", err)
	}
	reloaded, err := New(path)
	if err != nil {
		t.Fatalf("New(reload) error = %v", err)
	}
	if err := reloaded.Exists(first.ID); !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("Exists(deleted) error = %v, want ErrNotFound", err)
	}
	if err := reloaded.Exists(second.ID); err != nil {
		t.Fatalf("Exists(second) error = %v", err)
	}
}

func TestReadMethodsAndFilter(t *testing.T) {
	q, _ := newTestQueue(t)
	files := []engine.File{
		testFile("file-1", 512),
		testFile("file-2", 1024),
		testFile("file-3", 2048),
	}
	for _, file := range files {
		if err := q.Add(file); err != nil {
			t.Fatalf("Add(%q) error = %v", file.ID, err)
		}
	}

	length, err := q.Len()
	if err != nil || length != len(files) {
		t.Fatalf("Len() = (%d, %v), want (%d, nil)", length, err, len(files))
	}
	stat, err := q.Stat()
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if stat.Files != 3 || stat.FullSize != 3584 {
		t.Fatalf("Stat() = %#v, want 3 files and 3584 bytes", stat)
	}

	filtered, err := q.List(func(file engine.File) error {
		if file.Size < 1024 {
			return engine.ErrNotFound
		}
		return nil
	})
	if err != nil {
		t.Fatalf("List(filter) error = %v", err)
	}
	if len(filtered) != 2 || filtered[0].ID != "file-2" || filtered[1].ID != "file-3" {
		t.Fatalf("List(filter) = %#v", filtered)
	}
}

func TestListFilterRunsOutsideLock(t *testing.T) {
	q, _ := newTestQueue(t)
	if err := q.Add(testFile("file-1", 1)); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := q.List(func(engine.File) error {
			return q.Add(testFile("file-2", 2))
		})
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("List() filter deadlocked while mutating queue")
	}
}

func TestWriteFailureKeepsLastSuccessfulStateAndCleansTemp(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "queue.json")
	q, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	first := testFile("file-1", 1)
	if err := q.Add(first); err != nil {
		t.Fatalf("Add(first) error = %v", err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	blockingTarget := filepath.Join(root, "target-directory")
	if err := os.Mkdir(blockingTarget, 0755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	q.path = blockingTarget

	if err := q.Add(testFile("file-2", 2)); err == nil {
		t.Fatal("Add(second) error = nil, want replace error")
	}
	if err := q.Exists("file-2"); !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("failed Add() changed memory, Exists() error = %v", err)
	}

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(current) error = %v", err)
	}
	if string(current) != string(original) {
		t.Fatal("failed Add() changed last successful queue file")
	}
	temps, err := filepath.Glob(filepath.Join(root, ".wddl-queue-*.tmp"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(temps) != 0 {
		t.Fatalf("temporary files left after failure: %v", temps)
	}
}

func TestInjectedWriteFailureDoesNotPublishState(t *testing.T) {
	q, _ := newTestQueue(t)
	q.writeSnapshot = func(string, map[string]engine.File) (bool, error) {
		return false, errors.New("injected failure")
	}

	if err := q.Add(testFile("file-1", 1)); err == nil {
		t.Fatal("Add() error = nil, want injected failure")
	}
	if err := q.Exists("file-1"); !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("failed Add() changed memory, Exists() error = %v", err)
	}
}

func TestChanEmitsExistingItemImmediately(t *testing.T) {
	q, _ := newTestQueue(t)
	want := testFile("file-1", 1)
	if err := q.Add(want); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	items := q.Chan(ctx, lgr.New(), nil)

	select {
	case got := <-items:
		if got != want {
			t.Fatalf("Chan() item = %#v, want %#v", got, want)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Chan() did not emit existing item immediately")
	}
}

func TestChanEmitsNewItemOnNotification(t *testing.T) {
	q, _ := newTestQueue(t)
	q.retryEvery = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	items := q.Chan(ctx, lgr.New(), nil)
	want := testFile("file-1", 1)

	if err := q.Add(want); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	select {
	case got := <-items:
		if got != want {
			t.Fatalf("Chan() item = %#v, want %#v", got, want)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Chan() did not emit newly added item")
	}
}

func TestChanRetriesRemainingItem(t *testing.T) {
	q, _ := newTestQueue(t)
	q.retryEvery = 20 * time.Millisecond
	want := testFile("file-1", 1)
	if err := q.Add(want); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	select {
	case <-q.changed:
	default:
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	items := q.Chan(ctx, lgr.New(), nil)

	for attempt := 0; attempt < 2; attempt++ {
		select {
		case got := <-items:
			if got != want {
				t.Fatalf("Chan() item = %#v, want %#v", got, want)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("Chan() did not emit attempt %d", attempt+1)
		}
	}
}

func TestChanAppliesFilter(t *testing.T) {
	q, _ := newTestQueue(t)
	if err := q.Add(testFile("small", 1)); err != nil {
		t.Fatalf("Add(small) error = %v", err)
	}
	want := testFile("large", 2)
	if err := q.Add(want); err != nil {
		t.Fatalf("Add(large) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	items := q.Chan(ctx, lgr.New(), func(file engine.File) error {
		if file.Size < 2 {
			return engine.ErrNotFound
		}
		return nil
	})

	select {
	case got := <-items:
		if got != want {
			t.Fatalf("Chan() item = %#v, want %#v", got, want)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Chan() did not emit filtered item")
	}
}

func TestChanStopsWhileSendIsBlocked(t *testing.T) {
	q, _ := newTestQueue(t)
	if err := q.Add(testFile("file-1", 1)); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	items := q.Chan(ctx, lgr.New(), nil)
	cancel()

	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case _, open := <-items:
			if !open {
				return
			}
		case <-deadline:
			t.Fatal("Chan() did not stop after cancellation")
		}
	}
}

func TestBlockedConsumerDoesNotBlockAdd(t *testing.T) {
	q, _ := newTestQueue(t)
	if err := q.Add(testFile("file-1", 1)); err != nil {
		t.Fatalf("Add(first) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = q.Chan(ctx, lgr.New(), nil)

	done := make(chan error, 1)
	go func() {
		done <- q.Add(testFile("file-2", 2))
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Add(second) error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Add() blocked on queue consumer")
	}
}

func TestConcurrentAccess(t *testing.T) {
	q, _ := newTestQueue(t)
	q.writeSnapshot = func(string, map[string]engine.File) (bool, error) {
		return true, nil
	}

	const workers = 8
	const iterations = 50
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				id := fmt.Sprintf("file-%d-%d", worker, iteration)
				file := testFile(id, int64(iteration))
				if err := q.Add(file); err != nil {
					t.Errorf("Add(%q) error = %v", id, err)
					return
				}
				_, _ = q.Len()
				_, _ = q.Stat()
				_, _ = q.List(nil)
				_ = q.Exists(id)
				if iteration%2 == 0 {
					if err := q.Delete(id); err != nil {
						t.Errorf("Delete(%q) error = %v", id, err)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}

func TestSetStatePersistsAndSuspendedTaskIsNotEmitted(t *testing.T) {
	q, path := newTestQueue(t)
	file := testFile("paused", 42)
	if err := q.Add(file); err != nil {
		t.Fatal(err)
	}
	updated, err := q.SetState(file.ID, engine.TaskSuspended)
	if err != nil || updated.State != engine.TaskSuspended {
		t.Fatalf("SetState() = %#v, %v", updated, err)
	}

	reloaded, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	items, err := reloaded.List(nil)
	if err != nil || len(items) != 1 || items[0].State != engine.TaskSuspended {
		t.Fatalf("reloaded = %#v, %v", items, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if item, ok := <-reloaded.Chan(ctx, lgr.New(), nil); ok {
		t.Fatalf("suspended item emitted: %#v", item)
	}
}

func newTestQueue(t *testing.T) (*Queue, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "queue.json")
	q, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return q, path
}

func testFile(id string, size int64) engine.File {
	return engine.File{
		ID:     id,
		Name:   id + ".mkv",
		Source: "/remote/" + id + ".mkv",
		Temp:   "/tmp/" + id,
		Dest:   "/downloads/" + id + ".mkv",
		Size:   size,
		State:  engine.TaskReady,
	}
}

func persistedFromEngine(file engine.File) persistedFile {
	return persistedFile{
		ID:     file.ID,
		Name:   file.Name,
		Source: file.Source,
		Temp:   file.Temp,
		Dest:   file.Dest,
		Size:   file.Size,
		State:  string(file.State),
	}
}

func writeQueueFile(t *testing.T, path string, state persistedQueue) {
	t.Helper()

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	return string(data)
}
