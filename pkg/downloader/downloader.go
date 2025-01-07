package downloader

import (
	"context"
	"io"

	"git.papkovda.ru/library/gokit/pkg/tool"
	"git.papkovda.ru/tools/webdav/pkg/queue"
	"github.com/go-pkgz/lgr"
	"github.com/studio-b12/gowebdav"
)

func New(client *gowebdav.Client, queue Queue, parallel int) *Downloader {
	return &Downloader{
		limiter: tool.NewRoutineLimiter(parallel),
		client:  client,
		queue:   queue,
	}
}

type Queue interface {
	Stream() <-chan *queue.Task
	Done(id string, err error)
}

type Downloader struct {
	limiter *tool.RoutineLimiter
	client  *gowebdav.Client
	queue   Queue
}

func (d *Downloader) Start(ctx context.Context) {
	go d.start(ctx, d.queue.Stream())
}

func (d *Downloader) start(ctx context.Context, ch <-chan *queue.Task) {
	done := ctx.Done()

	for {
		select {
		case <-done:
			lgr.Default().Logf("[INFO] downloader stopped: context ends.")
			break
		case item := <-ch:
			d.limiter.Run(func() {
				d.downloadItem(ctx, item)
			})
		}
	}
}

func (d *Downloader) downloadItem(ctx context.Context, task *queue.Task) {
	lgr.Default().Logf("[DEBUG] task started. id: %v", task.ID())
	defer func() {
		lgr.Default().Logf("[INFO] task stopped. id: %v", task.ID())
	}()

	err := d.downloadFile(task)
	if err != nil {
		lgr.Default().Logf("[ERROR] download file by task %v err: %v", task.ID(), err)
	}

	d.queue.Done(task.ID(), err)
}

func (d *Downloader) downloadFile(task *queue.Task) error {
	file, err := task.Open()
	if err != nil {
		lgr.Default().Logf("[DEBUG] open file for task %v error: %v", task.ID(), err)
		return err
	}

	defer file.Close()

	location := task.File.Location()

	stream, err := d.client.ReadStream(location)
	if err != nil {
		return err
	}

	defer stream.Close()

	_, err = io.Copy(file, stream)
	return err
}
