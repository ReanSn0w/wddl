package queue

import (
	"sort"

	"github.com/ReanSn0w/wddl/pkg/engine"
)

const formatVersion = 1

type persistedQueue struct {
	Version int             `json:"version"`
	Files   []persistedFile `json:"files"`
}

type persistedFile struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Source string `json:"source"`
	Temp   string `json:"temp"`
	Dest   string `json:"dest"`
	Size   int64  `json:"size"`
}

func newPersistedQueue(items map[string]engine.File) persistedQueue {
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}

	sort.Strings(ids)

	files := make([]persistedFile, 0, len(ids))
	for _, id := range ids {
		file := items[id]
		files = append(files, persistedFile{
			ID:     file.ID,
			Name:   file.Name,
			Source: file.Source,
			Temp:   file.Temp,
			Dest:   file.Dest,
			Size:   file.Size,
		})
	}

	return persistedQueue{
		Version: formatVersion,
		Files:   files,
	}
}

func (f persistedFile) engineFile() engine.File {
	return engine.File{
		ID:     f.ID,
		Name:   f.Name,
		Source: f.Source,
		Temp:   f.Temp,
		Dest:   f.Dest,
		Size:   f.Size,
	}
}

func cloneItems(items map[string]engine.File) map[string]engine.File {
	result := make(map[string]engine.File, len(items))
	for id, file := range items {
		result[id] = file
	}

	return result
}
