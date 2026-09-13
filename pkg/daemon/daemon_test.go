package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ReanSn0w/wddl/pkg/config"
	"github.com/ReanSn0w/wddl/pkg/control"
	"github.com/ReanSn0w/wddl/pkg/engine"
	"github.com/ReanSn0w/wddl/pkg/queue"
	"github.com/go-pkgz/lgr"
)

type testScanner struct{}

func (testScanner) Scan(engine.Config, string) ([]engine.File, error) { return nil, nil }

type testDownloader struct{}

func (testDownloader) Download(context.Context, chan<- engine.Progress, engine.File) error {
	return nil
}
func (testDownloader) Delete(engine.File) error { return nil }

type testIndex struct{ path string }

func (i *testIndex) Refresh() (int, error) { return 1, nil }
func (i *testIndex) Len() int {
	if i.path == "" {
		return 0
	}
	return 1
}
func (i *testIndex) Find(name string, size int64) (string, error) {
	if i.path == "" {
		return "", engine.ErrLocalFileNotFound
	}
	info, err := os.Stat(i.path)
	if err != nil || info.Name() != name || info.Size() != size {
		return "", engine.ErrLocalFileNotFound
	}
	return i.path, nil
}

type testRemote struct {
	files   map[string]os.FileInfo
	removed []string
}

func (r *testRemote) Stat(path string) (os.FileInfo, error) {
	info, ok := r.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return info, nil
}
func (r *testRemote) ReadDir(path string) ([]os.FileInfo, error) {
	if path != "/Sync" {
		return nil, os.ErrNotExist
	}
	var result []os.FileInfo
	for remotePath, info := range r.files {
		if filepath.Dir(remotePath) == "/Sync" {
			result = append(result, info)
		}
	}
	return result, nil
}
func (r *testRemote) Remove(path string) error {
	if _, ok := r.files[path]; !ok {
		return os.ErrNotExist
	}
	delete(r.files, path)
	r.removed = append(r.removed, path)
	return nil
}

type testInfo struct {
	name string
	size int64
	dir  bool
}

func (i testInfo) Name() string { return i.name }
func (i testInfo) Size() int64  { return i.size }
func (i testInfo) Mode() os.FileMode {
	if i.dir {
		return os.ModeDir | 0o755
	}
	return 0o644
}
func (i testInfo) ModTime() time.Time { return time.Time{} }
func (i testInfo) IsDir() bool        { return i.dir }
func (i testInfo) Sys() any           { return nil }

func newTestDaemon(t *testing.T, remote *testRemote, index *testIndex) (*Daemon, *queue.Queue) {
	t.Helper()
	conf := config.Defaults()
	conf.Version = 1
	conf.WebDAV.Server = "https://example.test"
	conf.WebDAV.Root = "/Sync"
	conf.Download.Destination = t.TempDir()
	conf.Download.Temp = t.TempDir()
	conf.Queue.File = filepath.Join(t.TempDir(), "queue.json")
	q, err := queue.New(conf.Queue.File)
	if err != nil {
		t.Fatal(err)
	}
	e := engine.New(lgr.New(), engine.Config{InputPath: "/Sync", OutputPath: conf.Download.Destination, TempPath: conf.Download.Temp, Concurrency: 1}, testScanner{}, testDownloader{}, q, index)
	return New(conf, "test", lgr.New(), e, q, index, remote), q
}

func TestQueuePrefixResolutionAndRetry(t *testing.T) {
	d, q := newTestDaemon(t, &testRemote{files: map[string]os.FileInfo{}}, &testIndex{})
	for _, file := range []engine.File{{ID: "abc111", Name: "one", Source: "/Sync/one", Temp: "/tmp/one", Dest: "/data/one", Size: 1, State: engine.TaskReady}, {ID: "abc222", Name: "two", Source: "/Sync/two", Temp: "/tmp/two", Dest: "/data/two", Size: 2, State: engine.TaskSuspended}} {
		if err := q.Add(file); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.QueueRemove("abc"); err == nil {
		t.Fatal("ambiguous prefix accepted")
	}
	item, err := d.QueueRetry("abc2")
	if err != nil || item.State != string(engine.TaskReady) {
		t.Fatalf("QueueRetry() = %#v, %v", item, err)
	}
	item, err = d.QueueRemove("abc1")
	if err != nil || item.ID != "abc111" {
		t.Fatalf("QueueRemove() = %#v, %v", item, err)
	}
}

func TestResolveRemoteID(t *testing.T) {
	remote := &testRemote{files: map[string]os.FileInfo{"/Sync/movie.mkv": testInfo{name: "movie.mkv", size: 99}}}
	d, _ := newTestDaemon(t, remote, &testIndex{})
	got, err := d.ResolveRemoteID("movie.mkv")
	if err != nil {
		t.Fatal(err)
	}
	want := engine.NewFile(engine.Config{InputPath: "/Sync", OutputPath: d.config.Download.Destination, TempPath: d.config.Download.Temp}, "/Sync/movie.mkv", 99).ID
	if got.ID != want || got.Path != "/Sync/movie.mkv" {
		t.Fatalf("ResolveRemoteID() = %#v", got)
	}
	if _, err := d.ResolveRemoteID("/outside/movie.mkv"); err == nil {
		t.Fatal("outside path accepted")
	}
}

func TestCleanupTokenIsRevalidatedAndSingleUse(t *testing.T) {
	localDir := t.TempDir()
	local := filepath.Join(localDir, "movie.mkv")
	if err := os.WriteFile(local, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	remote := &testRemote{files: map[string]os.FileInfo{"/Sync/movie.mkv": testInfo{name: "movie.mkv", size: 5}}}
	d, _ := newTestDaemon(t, remote, &testIndex{path: local})
	preview, err := d.CleanupPreview()
	if err != nil || preview.Count != 1 {
		t.Fatalf("CleanupPreview() = %#v, %v", preview, err)
	}
	result, err := d.CleanupConfirm(preview.Token)
	if err != nil || result.Deleted != 1 {
		t.Fatalf("CleanupConfirm() = %#v, %v", result, err)
	}
	if _, err := d.CleanupConfirm(preview.Token); err == nil {
		t.Fatal("cleanup token reused")
	}
}

func TestImmutableReloadChangesAreReported(t *testing.T) {
	current := config.Defaults()
	next := current
	next.Download.Workers++
	fields := immutableChanges(current, next)
	if len(fields) != 1 || fields[0] != "download.workers" {
		t.Fatalf("immutableChanges() = %v", fields)
	}
	next = current
	next.Download.ScanEvery = config.Duration(time.Hour)
	if fields := immutableChanges(current, next); len(fields) != 0 {
		t.Fatalf("mutable fields = %v", fields)
	}
}

func TestExpiredCleanupTokenRejected(t *testing.T) {
	d, _ := newTestDaemon(t, &testRemote{files: map[string]os.FileInfo{}}, &testIndex{})
	d.previews["expired"] = cleanupSnapshot{expires: time.Now().Add(-time.Second)}
	_, err := d.CleanupConfirm("expired")
	var apiErr *control.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != control.CodeConflict {
		t.Fatalf("error = %v", err)
	}
}
