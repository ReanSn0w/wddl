package sync

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"git.papkovda.ru/library/gokit/pkg/tool"
	"github.com/go-pkgz/lgr"
	"github.com/studio-b12/gowebdav"
)

func New(log lgr.L, client *gowebdav.Client, inputPath, outputPath string) *Sync {
	return &Sync{
		log:        log,
		client:     client,
		inputPath:  inputPath,
		outputPath: outputPath,
		rl:         tool.NewRoutineLimiter(5),
	}
}

type Sync struct {
	log    lgr.L
	client *gowebdav.Client

	inputPath  string
	outputPath string

	rl   *tool.RoutineLimiter
	loop *tool.Loop
}

func (s *Sync) Start(ctx context.Context, interval time.Duration) error {
	if s.loop != nil {
		return errors.New("loop already running")
	}

	s.loop = tool.NewLoop(s.checkNewFiles)
	// s.loop.Once()

	go func() {
		<-ctx.Done()
		s.loop.Stop()
	}()

	s.loop.Run(interval)
	return nil
}

func (s *Sync) Stop() {
	s.loop.Stop()
}

func (s *Sync) checkNewFiles() {
	files, err := s.readDir(s.inputPath, true)
	if err != nil {
		s.log.Logf("[ERROR] check files err: %v", err)
		return
	}

	wg := sync.WaitGroup{}
	wg.Add(len(files))

	for i, file := range files {
		s.log.Logf("[INFO] sync %v / %v files", i+1, len(files))

		s.rl.Run(func() {
			defer wg.Done()

			err := s.downloadFile(file)
			if err != nil {
				s.log.Logf("[ERROR] download file err: %v", err)
				return
			}

			s.log.Logf("[INFO] sync file: $v) %v complete", i+1, file.path)
		})
	}

	wg.Wait()
	s.log.Logf("[INFO] sync done")
}

type fileForSync struct {
	path string
	size int64
}

func (s *Sync) readDir(path string, recursive bool) ([]fileForSync, error) {
	list, err := s.client.ReadDir(path)
	if err != nil {
		lgr.Default().Logf("[ERROR] read dir (%v) err: %v", path, err)
		return nil, err
	}

	files := []fileForSync{}

	for _, item := range list {
		if item.IsDir() {
			if recursive {
				list, err := s.readDir(path+"/"+item.Name(), recursive)
				if err != nil {
					continue
				}

				files = append(files, list...)
			}
		} else {
			files = append(files, fileForSync{
				path: path + "/" + item.Name(),
				size: item.Size(),
			})
		}
	}

	return files, nil
}

func (s *Sync) downloadFile(f fileForSync) error {
	err := s.checkDirectory(s.outputPath, f.path)
	if err != nil {
		return err
	}

	err = s.checkDownload(f)
	if err == nil {
		return nil
	}

	return s.prepareFileDownlaod(f.path)
}

func (s *Sync) checkDirectory(current string, path string) error {
	pathParts := strings.Split(path[1:], "/")
	if len(pathParts) == 1 {
		return nil
	}

	currentPath := current + "/" + pathParts[0]

	dir, err := os.Stat(currentPath)
	if os.IsNotExist(err) {
		err = os.Mkdir(currentPath, 0666)
		if err != nil {
			return err
		}

		dir, err = os.Stat(currentPath)
	}

	if err != nil {
		return err
	}

	if !dir.IsDir() {
		return errors.New("is not directory")
	}

	return s.checkDirectory(path+"/"+pathParts[0], strings.Join(pathParts[1:], "/"))
}

func (s *Sync) checkDownload(file fileForSync) error {
	f, err := os.Stat(s.outputPath + file.path)
	if err != nil {
		return err
	}

	if f.Size() != file.size {
		return nil
	}

	return errors.New("file already exists")
}

func (s *Sync) prepareFileDownlaod(path string) error {
	localFile, err := os.Create(s.outputPath + strings.TrimPrefix(path, s.inputPath))
	if err != nil {
		return err
	}

	defer localFile.Close()

	stream, err := s.client.ReadStream(path)
	if err != nil {
		return err
	}

	defer stream.Close()

	_, err = io.Copy(localFile, stream)
	return err
}
