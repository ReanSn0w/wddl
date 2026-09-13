package queue

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ReanSn0w/wddl/pkg/engine"
)

type snapshotWriter func(path string, items map[string]engine.File) (committed bool, err error)

func writeSnapshot(path string, items map[string]engine.File) (committed bool, err error) {
	dir := filepath.Dir(filepath.Clean(path))
	temp, err := os.CreateTemp(dir, ".wddl-queue-*.tmp")
	if err != nil {
		return false, fmt.Errorf("create temporary queue file: %w", err)
	}

	tempPath := temp.Name()
	keepTemp := true
	defer func() {
		if keepTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if err := temp.Chmod(0600); err != nil {
		_ = temp.Close()
		return false, fmt.Errorf("set temporary queue file permissions: %w", err)
	}

	encoder := json.NewEncoder(temp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(newPersistedQueue(items)); err != nil {
		_ = temp.Close()
		return false, fmt.Errorf("encode queue snapshot: %w", err)
	}

	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return false, fmt.Errorf("sync temporary queue file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return false, fmt.Errorf("close temporary queue file: %w", err)
	}

	if err := os.Rename(tempPath, path); err != nil {
		return false, fmt.Errorf("replace queue file: %w", err)
	}
	keepTemp = false

	directory, err := os.Open(dir)
	if err != nil {
		return true, fmt.Errorf("open queue directory for sync: %w", err)
	}
	defer directory.Close()

	if err := directory.Sync(); err != nil {
		return true, fmt.Errorf("sync queue directory: %w", err)
	}

	return true, nil
}
