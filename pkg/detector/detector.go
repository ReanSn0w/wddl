package detector

import (
	"fmt"
	"os"
	"strings"
	"time"

	"git.papkovda.ru/library/gokit/pkg/tool"
	"github.com/go-pkgz/lgr"
	"github.com/studio-b12/gowebdav"
)

func New(client *gowebdav.Client, queue Queue) *Detector {
	return &Detector{
		client:     client,
		queue:      queue,
		sourcePath: "/",
		destPath:   "./",
	}
}

type Detector struct {
	client     *gowebdav.Client
	queue      Queue
	sourcePath string
	destPath   string
}

func (d *Detector) WithSourcePath(path string) *Detector {
	d.sourcePath = path
	return d
}

func (d *Detector) WithDestinationPath(path string) *Detector {
	d.destPath = path
	return d
}

func (d *Detector) Run(duration time.Duration) *tool.Loop {
	loop := tool.NewLoop(d.task())
	loop.Run(duration)
	return loop
}

type File struct {
	Name string
	Path string
	Size int64
	Dest string
}

func (f *File) Location() string {
	return fmt.Sprintf("%v/%v", f.Path, f.Name)
}

type Queue interface {
	Send(...*File)
}

func (d *Detector) task() func() {
	return func() {
		files, err := d.fullscan(d.sourcePath, true)
		if err != nil {
			lgr.Default().Logf("[ERROR] detect file error: %v", err)
			return
		}

		d.queue.Send(files...)
	}
}

func (s *Detector) fullscan(path string, recursive bool) ([]*File, error) {
	list, err := s.client.ReadDir(path)
	if err != nil {
		lgr.Default().Logf("[ERROR] read dir (%v) err: %v", path, err)
		return nil, err
	}

	files := []*File{}

	for _, item := range list {
		if item.IsDir() {
			if recursive {
				list, err := s.fullscan(path+"/"+item.Name(), recursive)
				if err != nil {
					continue
				}

				files = append(files, list...)
			}
		} else {
			file := File{
				Name: item.Name(),
				Path: path,
				Size: item.Size(),
				Dest: s.destPath + strings.TrimPrefix(path+"/"+item.Name(), s.sourcePath),
			}

			if update, err := s.checkFile(&file); update {
				files = append(files, &file)
			} else if err != nil {
				lgr.Default().Logf("[ERROR] check file (%s) err: %v", file.Name, err)
			}
		}
	}

	return files, nil
}

func (d *Detector) checkFile(f *File) (bool, error) {
	info, err := os.Stat(f.Dest)
	if os.IsNotExist(err) {
		lgr.Default().Logf("[ERROR] destination stat err: %v", err)
		return true, nil
	}

	if err != nil {
		return false, err
	}

	if info.Size() == f.Size {
		lgr.Default().Logf("[DEBUG] file (%v) skipped: already exists", f.Dest)
		return false, nil
	}

	return true, nil
}
