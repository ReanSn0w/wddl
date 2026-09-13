package localindex

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ReanSn0w/wddl/pkg/engine"
)

func TestIndexFindUsesExactNameAndSizeRecursively(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "different", "layout", "Movie.mkv")
	writeTestFile(t, want, "video")

	index := New([]string{root})
	count, err := index.Refresh()
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if count != 1 || index.Len() != 1 {
		t.Fatalf("indexed count = %d, Len() = %d, want 1", count, index.Len())
	}

	got, err := index.Find("Movie.mkv", 5)
	if err != nil || got != want {
		t.Fatalf("Find() = %q, %v, want %q, nil", got, err, want)
	}
	if _, err := index.Find("movie.mkv", 5); !errors.Is(err, engine.ErrLocalFileNotFound) {
		t.Fatalf("case-insensitive Find() error = %v, want ErrLocalFileNotFound", err)
	}
	if _, err := index.Find("Movie.mkv", 4); !errors.Is(err, engine.ErrLocalFileNotFound) {
		t.Fatalf("wrong-size Find() error = %v, want ErrLocalFileNotFound", err)
	}
}

func TestIndexKeepsAllMatchingPathsAndDropsOnlyStaleOnes(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "one", "same.mkv")
	second := filepath.Join(root, "two", "same.mkv")
	writeTestFile(t, first, "same")
	writeTestFile(t, second, "same")

	index := New([]string{root})
	if _, err := index.Refresh(); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if err := os.Remove(first); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	got, err := index.Find("same.mkv", 4)
	if err != nil || got != second {
		t.Fatalf("Find() = %q, %v, want %q, nil", got, err, second)
	}
	if index.Len() != 1 {
		t.Fatalf("Len() = %d, want stale path removed and duplicate retained", index.Len())
	}
}

func TestIndexDropsFilesWhoseMetadataChanged(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "movie.mkv")
	writeTestFile(t, file, "old")

	index := New([]string{root})
	if _, err := index.Refresh(); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	writeTestFile(t, file, "new-size")

	if _, err := index.Find("movie.mkv", 3); !errors.Is(err, engine.ErrLocalFileNotFound) {
		t.Fatalf("Find() error = %v, want ErrLocalFileNotFound", err)
	}
	if index.Len() != 0 {
		t.Fatalf("Len() = %d, want 0 after stale metadata removal", index.Len())
	}
}

func TestRefreshAtomicallyReplacesSnapshot(t *testing.T) {
	root := t.TempDir()
	oldFile := filepath.Join(root, "old.mkv")
	newFile := filepath.Join(root, "nested", "new.mkv")
	writeTestFile(t, oldFile, "old")

	index := New([]string{root})
	if _, err := index.Refresh(); err != nil {
		t.Fatalf("first Refresh() error = %v", err)
	}
	if err := os.Remove(oldFile); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	writeTestFile(t, newFile, "new")

	if _, err := index.Refresh(); err != nil {
		t.Fatalf("second Refresh() error = %v", err)
	}
	if _, err := index.Find("old.mkv", 3); !errors.Is(err, engine.ErrLocalFileNotFound) {
		t.Fatalf("old Find() error = %v, want ErrLocalFileNotFound", err)
	}
	if got, err := index.Find("new.mkv", 3); err != nil || got != newFile {
		t.Fatalf("new Find() = %q, %v, want %q, nil", got, err, newFile)
	}
}

func TestRunKeepsPreviousSnapshotAfterRefreshFailure(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "movie.mkv")
	writeTestFile(t, file, "video")

	index := New([]string{root})
	if _, err := index.Refresh(); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	index.walk = func(string, fs.WalkDirFunc) error { return errors.New("disk unavailable") }

	ctx, cancel := context.WithCancel(context.Background())
	log := &testLogger{}
	go index.Run(ctx, log, 5*time.Millisecond)
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(log.String(), "keeping 1 indexed files") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()

	if got, err := index.Find("movie.mkv", 5); err != nil || got != file {
		t.Fatalf("Find() after failed refresh = %q, %v, want %q, nil", got, err, file)
	}
	if !strings.Contains(log.String(), "disk unavailable") {
		t.Fatalf("log = %q, want refresh failure", log.String())
	}
}

func TestIndexConcurrentFindAndRefresh(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "movie.mkv")
	writeTestFile(t, file, "video")
	index := New([]string{root})
	if _, err := index.Refresh(); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = index.Find("movie.mkv", 5)
		}()
		go func() {
			defer wg.Done()
			_, _ = index.Refresh()
		}()
	}
	wg.Wait()
}

type testLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *testLogger) Logf(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *testLogger) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, " ")
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
