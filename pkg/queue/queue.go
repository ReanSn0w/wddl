package queue

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/ReanSn0w/wddl/pkg/engine"
	"github.com/go-pkgz/lgr"
)

func New(path string) (*Queue, error) {
	if err := checkParentWritable(path); err != nil {
		return nil, err
	}

	items, err := load(path)
	if err != nil {
		return nil, err
	}

	return &Queue{
		path:          path,
		items:         items,
		changed:       make(chan struct{}, 1),
		writeSnapshot: writeSnapshot,
	}, nil
}

type Queue struct {
	path string

	mx    sync.RWMutex
	items map[string]engine.File

	changed       chan struct{}
	writeSnapshot snapshotWriter
}

// Add - добавляет файл в очередь
func (q *Queue) Add(file engine.File) error {
	q.mx.Lock()
	next := cloneItems(q.items)
	next[file.ID] = file

	committed, err := q.writeSnapshot(q.path, next)
	if committed {
		q.items = next
	}
	q.mx.Unlock()

	if committed {
		q.notifyChanged()
	}
	if err != nil {
		return err
	}
	return nil
}

// Exists - проверяет наличие файла в очереди
// в случае его отсутствия возвращает ошибку
func (q *Queue) Exists(id string) error {
	q.mx.RLock()
	defer q.mx.RUnlock()

	if _, exists := q.items[id]; !exists {
		return engine.ErrNotFound
	}

	return nil
}

// Len - возвращает количество файлов в очереди
// в случае их отсутствия возвращает (0, nil)
func (q *Queue) Len() (int, error) {
	q.mx.RLock()
	defer q.mx.RUnlock()

	return len(q.items), nil
}

// Stat - возвращает статистику состояния очереди
func (q *Queue) Stat() (*engine.Stat, error) {
	items := q.snapshot()
	stat := &engine.Stat{Files: len(items)}
	for _, file := range items {
		stat.FullSize += file.Size
	}

	return stat, nil
}

// List - возвращает список файлов из очереди
// в случае случае их отсутсвия возвращает (nil, nil)
func (q *Queue) List(filter func(f engine.File) error) ([]engine.File, error) {
	items := q.snapshot()
	result := make([]engine.File, 0, len(items))
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		file := items[id]
		if filter != nil {
			if err := filter(file); err != nil {
				continue
			}
		}

		result = append(result, file)
	}

	return result, nil
}

func (q *Queue) notifyChanged() {
	select {
	case q.changed <- struct{}{}:
	default:
	}
}

func (q *Queue) snapshot() map[string]engine.File {
	q.mx.RLock()
	defer q.mx.RUnlock()

	return cloneItems(q.items)
}

// Chan - возвращает канал с файлами из очереди
// в случае их присутствия в очереди в противном случае породит go рутину
// которая будет периодически опрашивать очередь на наличие новых файлов
func (q *Queue) Chan(ctx context.Context, log lgr.L, filter func(f engine.File) error) <-chan engine.File {
	ch := make(chan engine.File)

	go func() {
		defer close(ch)

		ticker := time.NewTicker(time.Second * 3)
		defer ticker.Stop()

		emit := func() bool {
			items, err := q.List(filter)
			if err != nil {
				log.Logf("[ERROR] listing files: %v", err)
				return true
			}

			for _, item := range items {
				select {
				case ch <- item:
				case <-ctx.Done():
					return false
				}
			}

			return true
		}

		if !emit() {
			return
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !emit() {
					return
				}
			case <-q.changed:
				if !emit() {
					return
				}
			}
		}
	}()

	return ch
}

// Delete - удаляет файл из очереди
// в случае его присутствия в очереди
func (q *Queue) Delete(id string) error {
	q.mx.Lock()
	defer q.mx.Unlock()

	if _, exists := q.items[id]; !exists {
		return nil
	}

	next := cloneItems(q.items)
	delete(next, id)

	committed, err := q.writeSnapshot(q.path, next)
	if committed {
		q.items = next
	}
	if err != nil {
		return err
	}
	return nil
}
