package queue

import (
	"context"
	"sync"
	"time"

	"git.papkovda.ru/tools/webdav/pkg/detector"
	"git.papkovda.ru/tools/webdav/pkg/utils"
	"github.com/go-pkgz/lgr"
)

func New(ctx context.Context) *Queue {
	q := &Queue{
		storage:    map[string]*Task{},
		downstream: make(chan *Task),
	}

	go q.lifecycle(ctx)
	return q
}

type Queue struct {
	mx         sync.RWMutex
	downstream chan *Task
	storage    map[string]*Task
}

func (q *Queue) Send(files ...*detector.File) {
	q.mx.Lock()
	defer q.mx.Unlock()

	tasks := []*Task{}
	for _, f := range files {
		if _, ok := q.storage[f.Name]; ok {
			continue
		}

		task := newTask(f)
		tasks = append(tasks, task)
		q.storage[task.ID()] = task
	}

	go func() {
		for _, t := range tasks {
			q.downstream <- t
		}
	}()
}

func (q *Queue) Stream() <-chan *Task {
	return q.downstream
}

func (q *Queue) Done(id string, err error) {
	q.mx.Lock()
	defer q.mx.Unlock()

	if err != nil {
		lgr.Default().Logf("[ERROR] task %v error: %v", id, err)
	} else {
		lgr.Default().Logf("[INFO] task %v complete", id)
	}

	delete(q.storage, id)
}

func (q *Queue) lifecycle(ctx context.Context) {
	timer := time.NewTimer(time.Minute)
	done := ctx.Done()

	for {
		select {
		case <-done:
			return
		case <-timer.C:
			q.printProgress()
			timer.Reset(time.Minute)
		}
	}
}

func (q *Queue) printProgress() {
	q.mx.RLock()
	defer q.mx.RUnlock()

	var downloadingFiles int64

	items := []utils.FileProgress{}

	for _, t := range q.storage {
		if progress := t.Progress(); progress > 0 {
			downloadingFiles += 1
			items = append(
				items,
				utils.NewProgres(t.ID(),
					float64(progress)/(float64(t.File.Size)/100)))
		}
	}

	if downloadingFiles == 0 {
		lgr.Default().Logf("[DEBUG] no tasks: waiting")
		return
	}

	lgr.Default().Logf(
		"[INFO] %v",
		utils.MakeProgressMessage(int(downloadingFiles), len(q.storage), items...))
}
