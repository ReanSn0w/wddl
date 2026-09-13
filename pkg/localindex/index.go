// Package localindex maintains a metadata-only index of existing local files.
package localindex

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ReanSn0w/wddl/pkg/engine"
)

type logger interface {
	Logf(format string, args ...interface{})
}

type fileKey struct {
	name string
	size int64
}

// Index stores immutable snapshots built from one or more library roots.
// Refresh builds a complete replacement before publishing it.
type Index struct {
	mu        sync.RWMutex
	roots     []string
	files     map[fileKey][]string
	fileCount int
	walk      func(string, fs.WalkDirFunc) error
}

// New creates an empty index for roots. Call Refresh before relying on it.
func New(roots []string) *Index {
	return &Index{
		roots: append([]string(nil), roots...),
		files: make(map[fileKey][]string),
		walk:  filepath.WalkDir,
	}
}

// Refresh recursively indexes regular files without opening their contents.
// A failed traversal leaves the previously published snapshot untouched.
func (i *Index) Refresh() (int, error) {
	next := make(map[fileKey][]string)
	count := 0

	for _, root := range i.roots {
		err := i.walk(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}

			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}

			key := fileKey{name: info.Name(), size: info.Size()}
			next[key] = append(next[key], path)
			count++
			return nil
		})
		if err != nil {
			return 0, fmt.Errorf("scan library root %q: %w", root, err)
		}
	}

	i.mu.Lock()
	i.files = next
	i.fileCount = count
	i.mu.Unlock()

	return count, nil
}

// Len returns the number of regular files in the active snapshot.
func (i *Index) Len() int {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.fileCount
}

// Run periodically replaces the active snapshot until ctx is cancelled.
func (i *Index) Run(ctx context.Context, log logger, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			started := time.Now()
			log.Logf("[INFO] local library scan started")
			count, err := i.Refresh()
			if err != nil {
				log.Logf("[ERROR] local library scan failed; keeping %d indexed files: %v", i.Len(), err)
				continue
			}
			log.Logf("[INFO] local library scan completed: %d files in %v", count, time.Since(started).Round(time.Millisecond))
		}
	}
}

// Find returns a path that still has the exact case-sensitive base name and
// byte size. Stale paths are removed from the active snapshot as discovered.
func (i *Index) Find(name string, size int64) (string, error) {
	key := fileKey{name: name, size: size}

	i.mu.RLock()
	paths := append([]string(nil), i.files[key]...)
	i.mu.RUnlock()

	if len(paths) == 0 {
		return "", engine.ErrLocalFileNotFound
	}

	stale := make(map[string]struct{})
	var firstErr error
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			stale[path] = struct{}{}
			if !errors.Is(err, os.ErrNotExist) && firstErr == nil {
				firstErr = fmt.Errorf("stat indexed file %q: %w", path, err)
			}
			continue
		}

		if !info.Mode().IsRegular() || info.Name() != name || info.Size() != size {
			stale[path] = struct{}{}
			continue
		}

		i.removeStale(key, stale)
		return path, nil
	}

	i.removeStale(key, stale)
	if firstErr != nil {
		return "", firstErr
	}
	return "", engine.ErrLocalFileNotFound
}

func (i *Index) removeStale(key fileKey, stale map[string]struct{}) {
	if len(stale) == 0 {
		return
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	current := i.files[key]
	kept := current[:0]
	for _, path := range current {
		if _, remove := stale[path]; !remove {
			kept = append(kept, path)
		}
	}

	i.fileCount -= len(current) - len(kept)
	if len(kept) == 0 {
		delete(i.files, key)
		return
	}
	i.files[key] = kept
}
