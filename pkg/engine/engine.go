package engine

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/go-pkgz/lgr"
)

func New(log lgr.L, conf Config, scanner Scanner, downloader Downloader, queue Queue) *Engine {
	return &Engine{
		log:        log,
		config:     conf,
		queue:      queue,
		scanner:    scanner,
		downloader: downloader,
		fileLocks:  make(map[string]bool),
		lockMutex:  &sync.Mutex{},
	}
}

type Engine struct {
	log        lgr.L
	config     Config
	queue      Queue
	scanner    Scanner
	downloader Downloader
	fileLocks  map[string]bool // Track locked files
	lockMutex  *sync.Mutex     // Protect fileLocks map
}

func (e *Engine) Start(ctx context.Context) {
	progressCH := make(chan Progress, e.config.Concurrency)

	// Запуск рутины для добавления новых файлов в очередь загрузки
	go e.scanNewFiles(ctx, e.config.ScanEvery, e.config.InputPath)

	// Запуск воркеров для загрузки файлов
	go e.downloadFiles(ctx, progressCH, e.config.Concurrency)

	// Запуск рутины отслеживания прогресса загрузки файлов
	go e.progressPrinter(ctx, progressCH)
}

// Данный метод переодически запускает сканирование новых файлов в удаленном хранилище
func (e *Engine) scanNewFiles(ctx context.Context, duration time.Duration, inputPath string) {
	ticker := time.NewTicker(duration)
	e.log.Logf("[DEBUG] scan loop started")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.log.Logf("[DEBUG] scan started")

			files, err := e.scanner.Scan(e.config, inputPath)
			if err != nil {
				e.log.Logf("[ERROR] failed to scan files: %v", err)
				continue
			}

			e.log.Logf("[DEBUG] scanning completed: %d files found", len(files))

			for _, file := range files {
				stat, err := os.Stat(file.Dest)
				if err != nil && !os.IsNotExist(err) {
					e.log.Logf("[ERROR] failed to stat file %s: %v", file.Name, err)
					continue
				}

				if stat != nil {
					if stat.Size() == file.Size {
						continue
					}
				}

				err = e.queue.Exists(file.ID)
				switch err {
				case nil:
					e.log.Logf("[DEBUG] file %s already exists in queue", file.Name)
				case ErrNotFound:
					e.log.Logf("[DEBUG] file %s not found in queue", file.Name)
					e.queue.Add(file)
				default:
					e.log.Logf("[ERROR] failed to check file %s in queue: %v", file.Name, err)
				}
			}
		default:
			time.Sleep(time.Millisecond * 100)
		}
	}
}

// Данный метод запускает воркеры загрузки файлов
func (e *Engine) downloadFiles(ctx context.Context, pc chan<- Progress, limit int) {
	ch := e.queue.Chan(ctx, e.log, func(f File) error {
		stat, err := os.Stat(f.Dest)
		if os.IsNotExist(err) {
			return nil
		}

		if f.Size != stat.Size() {
			return nil
		}

		return err
	})

	limiter := make(chan struct{}, limit)

	for {
		select {
		case file := <-ch:
			// Try to acquire file lock
			if !e.acquireFileLock(file.ID) {
				e.log.Logf("[WARN] file %s is already being downloaded, skipping", file.Name)
				continue
			}

			limiter <- struct{}{}

			go func(f File) {
				defer func() {
					<-limiter
					e.releaseFileLock(f.ID)
				}()

				err := e.filterTaskFromQueue(f.ID, f.Name, f.Dest, f.Size)
				if err != nil {
					return
				}

				e.log.Logf("[DEBUG] starting download of file %s (size: %d bytes)", f.Name, f.Size)

				err = e.downloader.Download(pc, f)
				if err != nil {
					e.log.Logf("[ERROR] failed to download file %s: %v", f.Name, err)
				} else {
					e.log.Logf("[INFO] successfully downloaded file %s", f.Name)
					err = e.queue.Delete(f.ID)
					if err != nil {
						e.log.Logf("[ERROR] failed to delete file %s from queue: %v", f.Name, err)
					}

					if e.config.RemoveRemote {
						err = e.downloader.Delete(f)
						if err != nil {
							e.log.Logf("[ERROR] failed to delete remote file %s from downloader: %v", f.Name, err)
						}
					}
				}
			}(file)
		case <-ctx.Done():
			return
		default:
			time.Sleep(time.Millisecond * 100)
		}
	}
}

// Данный метод запускает процесс отслеживания прогресса загрузки файлов
func (e *Engine) progressPrinter(ctx context.Context, items <-chan Progress) {
	ticker := time.NewTicker(time.Minute * 15)

	speedCounter := NewSpeedData()
	items = speedCounter.MakeChan(items)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			avgSpeed := speedCounter.AvgSpeed()

			stat, err := e.queue.Stat()
			if err != nil {
				e.log.Logf("[ERROR] failed to get queue length: %v", err)
				continue
			}

			avgTime := stat.AvgTime(avgSpeed)

			if stat.Files > 0 {
				e.log.Logf(
					"[INFO] avg speed %.2f KB/s ; estimate %v ; in queue %d files",
					float64(avgSpeed)/1024, avgTime, stat.Files)
			}
		case progress := <-items:
			e.log.Logf("[INFO] %s", progress.String())
		default:
			time.Sleep(time.Millisecond * 100)
		}
	}
}

func (e *Engine) acquireFileLock(fileID string) bool {
	e.lockMutex.Lock()
	defer e.lockMutex.Unlock()

	if _, locked := e.fileLocks[fileID]; locked {
		return false // File is already being downloaded
	}

	e.fileLocks[fileID] = true
	return true
}

func (e *Engine) releaseFileLock(fileID string) {
	e.lockMutex.Lock()
	defer e.lockMutex.Unlock()

	delete(e.fileLocks, fileID)
}

func (e *Engine) filterTaskFromQueue(fileid, filename, filepath string, filesize int64) error {
	stat, err := os.Stat(filepath)
	if err == nil {
		if stat.Size() == filesize {
			e.log.Logf("[WARN] filter task from queue: %s", filename)
			err = e.queue.Delete(fileid)
			if err != nil {
				e.log.Logf("[ERROR] failed to delete file %s from queue: %v", filename, err)
			}

			return errors.New("file is already downloaded")
		}
	}

	return nil
}
