package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-pkgz/lgr"
)

func TestScanSkipsFileFoundAtDifferentLocalPath(t *testing.T) {
	file, finder := localMatch(t)
	queue := newFakeQueue()
	downloader := &fakeDownloader{}
	engine := New(lgr.New(), Config{}, fakeScanner{files: []File{file}}, downloader, queue, finder)

	if err := engine.scanNewFilesOnce("/"); err != nil {
		t.Fatalf("scanNewFilesOnce() error = %v", err)
	}
	if queue.addCount != 0 {
		t.Fatalf("queue Add count = %d, want 0", queue.addCount)
	}
	if downloader.deleteCount != 0 {
		t.Fatalf("remote Delete count = %d, want 0", downloader.deleteCount)
	}
}

func TestScanDeletesConfirmedRemoteOnlyWhenEnabled(t *testing.T) {
	file, finder := localMatch(t)
	queue := newFakeQueue()
	downloader := &fakeDownloader{}
	engine := New(lgr.New(), Config{RemoveRemote: true}, fakeScanner{files: []File{file}}, downloader, queue, finder)

	if err := engine.scanNewFilesOnce("/"); err != nil {
		t.Fatalf("scanNewFilesOnce() error = %v", err)
	}
	if downloader.deleteCount != 1 {
		t.Fatalf("remote Delete count = %d, want 1", downloader.deleteCount)
	}
}

func TestScanDoesNotDeleteWhenRevalidationIsStale(t *testing.T) {
	file := File{ID: "id", Name: "movie.mkv", Source: "/movie.mkv", Dest: filepath.Join(t.TempDir(), "missing", "movie.mkv"), Size: 5}
	finder := &sequenceFinder{results: []finderResult{
		{path: "/library/movie.mkv"},
		{err: ErrLocalFileNotFound},
	}}
	downloader := &fakeDownloader{}
	engine := New(lgr.New(), Config{RemoveRemote: true}, fakeScanner{files: []File{file}}, downloader, newFakeQueue(), finder)

	if err := engine.scanNewFilesOnce("/"); err != nil {
		t.Fatalf("scanNewFilesOnce() error = %v", err)
	}
	if downloader.deleteCount != 0 {
		t.Fatalf("remote Delete count = %d, want 0", downloader.deleteCount)
	}
}

func TestScanRetriesRemoteDeleteOnNextScan(t *testing.T) {
	file, finder := localMatch(t)
	downloader := &fakeDownloader{deleteErr: errors.New("temporary WebDAV error")}
	engine := New(lgr.New(), Config{RemoveRemote: true}, fakeScanner{files: []File{file}}, downloader, newFakeQueue(), finder)

	if err := engine.scanNewFilesOnce("/"); err != nil {
		t.Fatalf("first scanNewFilesOnce() error = %v", err)
	}
	if err := engine.scanNewFilesOnce("/"); err != nil {
		t.Fatalf("second scanNewFilesOnce() error = %v", err)
	}
	if downloader.deleteCount != 2 {
		t.Fatalf("remote Delete count = %d, want one attempt per scan", downloader.deleteCount)
	}
}

func TestScanQueuesFileWhenIndexedCopyDisappeared(t *testing.T) {
	file := File{ID: "id", Name: "movie.mkv", Source: "/movie.mkv", Dest: filepath.Join(t.TempDir(), "output", "movie.mkv"), Size: 5}
	queue := newFakeQueue()
	engine := New(lgr.New(), Config{}, fakeScanner{files: []File{file}}, &fakeDownloader{}, queue, &sequenceFinder{results: []finderResult{{err: ErrLocalFileNotFound}}})

	if err := engine.scanNewFilesOnce("/"); err != nil {
		t.Fatalf("scanNewFilesOnce() error = %v", err)
	}
	if queue.addCount != 1 {
		t.Fatalf("queue Add count = %d, want 1", queue.addCount)
	}
}

func TestDownloadWorkerRemovesExistingTaskWithoutDownloading(t *testing.T) {
	file, finder := localMatch(t)
	queue := newFakeQueue()
	queue.files[file.ID] = file
	queue.items = make(chan File, 1)
	queue.items <- file
	close(queue.items)
	downloader := &fakeDownloader{}
	engine := New(lgr.New(), Config{}, fakeScanner{}, downloader, queue, finder)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go engine.downloadFiles(ctx, make(chan Progress, 1), 1)

	select {
	case <-queue.deleted:
	case <-time.After(time.Second):
		t.Fatal("queued local copy was not removed")
	}
	if downloader.downloadCount != 0 {
		t.Fatalf("Download count = %d, want 0", downloader.downloadCount)
	}
}

func TestFindLocalCopyUsesExpectedDestinationWithoutFinder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "movie.mkv")
	if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	file := File{Name: "movie.mkv", Dest: path, Size: 5}

	got, err := FindLocalCopy(file, nil)
	if err != nil || got != path {
		t.Fatalf("FindLocalCopy() = %q, %v, want %q, nil", got, err, path)
	}
}

func localMatch(t *testing.T) (File, ExistingFileFinder) {
	t.Helper()
	dir := t.TempDir()
	localPath := filepath.Join(dir, "other-layout", "movie.mkv")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(localPath, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return File{
		ID:     "id",
		Name:   "movie.mkv",
		Source: "/remote/movie.mkv",
		Dest:   filepath.Join(dir, "expected", "movie.mkv"),
		Size:   5,
	}, &sequenceFinder{results: []finderResult{{path: localPath}, {path: localPath}, {path: localPath}}}
}

type fakeScanner struct {
	files []File
	err   error
}

func (s fakeScanner) Scan(Config, string) ([]File, error) { return s.files, s.err }

type fakeDownloader struct {
	mu            sync.Mutex
	downloadCount int
	deleteCount   int
	deleteErr     error
}

func (d *fakeDownloader) Download(context.Context, chan<- Progress, File) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.downloadCount++
	return nil
}

func (d *fakeDownloader) Delete(File) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deleteCount++
	return d.deleteErr
}

type finderResult struct {
	path string
	err  error
}

type sequenceFinder struct {
	mu      sync.Mutex
	results []finderResult
	index   int
}

func (f *sequenceFinder) Find(string, int64) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.results) == 0 {
		return "", ErrLocalFileNotFound
	}
	index := f.index
	if index >= len(f.results) {
		index = len(f.results) - 1
	}
	f.index++
	return f.results[index].path, f.results[index].err
}

type fakeQueue struct {
	mu          sync.Mutex
	files       map[string]File
	items       chan File
	addCount    int
	deleteCount int
	deleted     chan struct{}
}

func newFakeQueue() *fakeQueue {
	return &fakeQueue{files: make(map[string]File), deleted: make(chan struct{}, 1)}
}

func (q *fakeQueue) Add(file File) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.files[file.ID] = file
	q.addCount++
	return nil
}

func (q *fakeQueue) Exists(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, ok := q.files[id]; !ok {
		return ErrNotFound
	}
	return nil
}

func (q *fakeQueue) Len() (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.files), nil
}

func (q *fakeQueue) Stat() (*Stat, error) { return &Stat{}, nil }

func (q *fakeQueue) List(func(File) error) ([]File, error) { return nil, nil }

func (q *fakeQueue) Chan(context.Context, lgr.L, func(File) error) <-chan File {
	if q.items != nil {
		return q.items
	}
	ch := make(chan File)
	close(ch)
	return ch
}

func (q *fakeQueue) Delete(id string) error {
	q.mu.Lock()
	delete(q.files, id)
	q.deleteCount++
	q.mu.Unlock()
	select {
	case q.deleted <- struct{}{}:
	default:
	}
	return nil
}

func (q *fakeQueue) SetState(id string, state TaskState) (File, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	file, ok := q.files[id]
	if !ok {
		return File{}, ErrNotFound
	}
	file.State = state
	q.files[id] = file
	return file, nil
}
