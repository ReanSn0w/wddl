package queue

import (
	"context"
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
		writeSnapshot: writeSnapshot,
	}, nil
}

type Queue struct {
	path string

	mx    sync.RWMutex
	items map[string]engine.File

	writeSnapshot snapshotWriter
}

// Add - добавляет файл в очередь
func (q *Queue) Add(file engine.File) error {
	q.mx.Lock()
	defer q.mx.Unlock()

	next := cloneItems(q.items)
	next[file.ID] = file

	committed, err := q.writeSnapshot(q.path, next)
	if committed {
		q.items = next
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
	for _, file := range items {
		if filter != nil {
			if err := filter(file); err != nil {
				continue
			}
		}

		result = append(result, file)
	}

	return result, nil
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

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				items, err := q.List(filter)
				if err != nil {
					log.Logf("[ERROR] listing files: %v", err)
					continue
				}

				for _, item := range items {
					ch <- item
				}
			default:
				time.Sleep(time.Millisecond * 100)
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
