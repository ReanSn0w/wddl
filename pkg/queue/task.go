package queue

import (
	"io"
	"os"
	"sync/atomic"

	"git.papkovda.ru/tools/webdav/pkg/detector"
)

func newTask(file *detector.File) *Task {
	return &Task{
		File: file,
	}
}

type Task struct {
	File   *detector.File
	writer *progressWriter
}

func (t *Task) ID() string {
	return t.File.Name
}

func (i *Task) Open() (io.WriteCloser, error) {
	if i.writer != nil {
		_ = i.writer.Close()
		i.writer = nil
	}

	file, err := os.OpenFile(i.File.Dest, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return nil, err
	}

	i.writer = &progressWriter{file: file, progress: &atomic.Int64{}}
	return i.writer, nil
}

func (i *Task) Progress() int64 {
	if i.writer == nil {
		return 0
	}

	return i.writer.progress.Load()
}

type progressWriter struct {
	file     *os.File
	progress *atomic.Int64
}

func (p *progressWriter) Write(data []byte) (int, error) {
	n, err := p.file.Write(data)
	if err == nil {
		p.progress.Add(int64(n))
	}
	return n, err
}

func (p *progressWriter) Close() error {
	return p.file.Close()
}
