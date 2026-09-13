package utils

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ReanSn0w/wddl/pkg/engine"
	"github.com/ReanSn0w/wddl/pkg/localindex"
)

func TestCleanerUsesAdditionalLibraryAndRevalidatesBeforeDelete(t *testing.T) {
	root := t.TempDir()
	localPath := filepath.Join(root, "unrelated", "movie.mkv")
	writeLocalFile(t, localPath, "video")
	index := localindex.New([]string{root})
	if _, err := index.Refresh(); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	webdav := &fakeWebdav{directories: map[string][]os.FileInfo{
		"/remote":        {testFileInfo{name: "nested", dir: true}},
		"/remote/nested": {testFileInfo{name: "movie.mkv", size: 5}},
	}}
	cleaner := New(webdav, filepath.Join(t.TempDir(), "output"), "/remote", index)

	if err := cleaner.ClearRemoteFiles(); err != nil {
		t.Fatalf("ClearRemoteFiles() error = %v", err)
	}
	if len(webdav.removed) != 1 || webdav.removed[0] != "/remote/nested/movie.mkv" {
		t.Fatalf("removed = %#v, want remote movie", webdav.removed)
	}
}

func TestCleanerSkipsDeleteWhenIndexedFileDisappears(t *testing.T) {
	root := t.TempDir()
	localPath := filepath.Join(root, "movie.mkv")
	writeLocalFile(t, localPath, "video")
	index := localindex.New([]string{root})
	if _, err := index.Refresh(); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	webdav := &fakeWebdav{}
	cleaner := New(webdav, t.TempDir(), "/remote", index)
	candidates, err := cleaner.filterAlreadyDownloaded([]scannedFile{{CleanPath: "/movie.mkv", Name: "movie.mkv", Size: 5}})
	if err != nil || len(candidates) != 1 {
		t.Fatalf("filterAlreadyDownloaded() = %#v, %v, want one candidate", candidates, err)
	}
	if err := os.Remove(localPath); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if err := cleaner.deleteRemoteFiles(candidates); err != nil {
		t.Fatalf("deleteRemoteFiles() error = %v", err)
	}
	if len(webdav.removed) != 0 {
		t.Fatalf("removed = %#v, want none for stale index", webdav.removed)
	}
}

func TestCleanerPreservesDestinationOnlyBehaviorWithoutIndex(t *testing.T) {
	output := t.TempDir()
	writeLocalFile(t, filepath.Join(output, "nested", "movie.mkv"), "video")
	webdav := &fakeWebdav{directories: map[string][]os.FileInfo{
		"/remote":        {testFileInfo{name: "nested", dir: true}},
		"/remote/nested": {testFileInfo{name: "movie.mkv", size: 5}},
	}}

	if err := New(webdav, output, "/remote", nil).ClearRemoteFiles(); err != nil {
		t.Fatalf("ClearRemoteFiles() error = %v", err)
	}
	if len(webdav.removed) != 1 {
		t.Fatalf("removed = %#v, want destination match removed", webdav.removed)
	}
}

func TestCleanerDoesNotDeleteSizeMismatch(t *testing.T) {
	output := t.TempDir()
	writeLocalFile(t, filepath.Join(output, "movie.mkv"), "different")
	webdav := &fakeWebdav{directories: map[string][]os.FileInfo{
		"/remote": {testFileInfo{name: "movie.mkv", size: 5}},
	}}

	if err := New(webdav, output, "/remote", nil).ClearRemoteFiles(); err != nil {
		t.Fatalf("ClearRemoteFiles() error = %v", err)
	}
	if len(webdav.removed) != 0 {
		t.Fatalf("removed = %#v, want none", webdav.removed)
	}
}

type fakeWebdav struct {
	directories map[string][]os.FileInfo
	removed     []string
	removeErr   error
}

func (w *fakeWebdav) ReadDir(path string) ([]os.FileInfo, error) {
	if w.directories == nil {
		return nil, errors.New("unexpected ReadDir")
	}
	return w.directories[path], nil
}

func (w *fakeWebdav) Remove(path string) error {
	w.removed = append(w.removed, path)
	return w.removeErr
}

type testFileInfo struct {
	name string
	size int64
	dir  bool
}

func (i testFileInfo) Name() string       { return i.name }
func (i testFileInfo) Size() int64        { return i.size }
func (i testFileInfo) Mode() os.FileMode  { return 0 }
func (i testFileInfo) ModTime() time.Time { return time.Time{} }
func (i testFileInfo) IsDir() bool        { return i.dir }
func (i testFileInfo) Sys() interface{}   { return nil }

func writeLocalFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

var _ engine.ExistingFileFinder = (*localindex.Index)(nil)
