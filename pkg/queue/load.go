package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ReanSn0w/wddl/pkg/engine"
)

func load(path string) (map[string]engine.File, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]engine.File), nil
	}
	if err != nil {
		return nil, fmt.Errorf("open queue file: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()

	var state persistedQueue
	if err := decoder.Decode(&state); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("queue file is empty")
		}
		return nil, fmt.Errorf("decode queue file: %w", err)
	}

	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("queue file contains multiple JSON values")
		}
		return nil, fmt.Errorf("decode trailing queue data: %w", err)
	}

	if state.Version != formatVersion {
		return nil, fmt.Errorf("unsupported queue format version %d", state.Version)
	}
	if state.Files == nil {
		return nil, errors.New("queue file is missing files list")
	}

	items := make(map[string]engine.File, len(state.Files))
	for index, persisted := range state.Files {
		item := persisted.engineFile()
		if err := validateFile(item); err != nil {
			return nil, fmt.Errorf("validate queue file item %d: %w", index, err)
		}
		if _, exists := items[item.ID]; exists {
			return nil, fmt.Errorf("queue file contains duplicate id %q", item.ID)
		}

		items[item.ID] = item
	}

	return items, nil
}

func validateFile(file engine.File) error {
	switch {
	case file.ID == "":
		return errors.New("id is required")
	case file.Name == "":
		return errors.New("name is required")
	case file.Source == "":
		return errors.New("source is required")
	case file.Temp == "":
		return errors.New("temp is required")
	case file.Dest == "":
		return errors.New("dest is required")
	case file.Size < 0:
		return errors.New("size must not be negative")
	case file.State != engine.TaskReady && file.State != engine.TaskSuspended:
		return fmt.Errorf("unknown state %q", file.State)
	default:
		return nil
	}
}

func checkParentWritable(path string) error {
	dir := filepath.Dir(filepath.Clean(path))
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat queue directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("queue parent %q is not a directory", dir)
	}

	probe, err := os.CreateTemp(dir, ".wddl-queue-check-*")
	if err != nil {
		return fmt.Errorf("queue directory is not writable: %w", err)
	}
	probePath := probe.Name()

	if err := probe.Close(); err != nil {
		_ = os.Remove(probePath)
		return fmt.Errorf("close queue directory probe: %w", err)
	}
	if err := os.Remove(probePath); err != nil {
		return fmt.Errorf("remove queue directory probe: %w", err)
	}

	return nil
}
