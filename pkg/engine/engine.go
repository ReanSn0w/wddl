package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/go-pkgz/lgr"
)

func New(log lgr.L, conf Config, scanner Scanner, downloader Downloader, queue Queue, existingFiles ExistingFileFinder) *Engine {
	return &Engine{
		log:           log,
		config:        conf,
		queue:         queue,
		scanner:       scanner,
		downloader:    downloader,
		existingFiles: existingFiles,
		fileLocks:     make(map[string]bool),
		active:        make(map[string]ActiveDownload),
		lockMutex:     &sync.Mutex{},
	}
}

type Engine struct {
	log           lgr.L
	config        Config
	queue         Queue
	scanner       Scanner
	downloader    Downloader
	existingFiles ExistingFileFinder
	fileLocks     map[string]bool // Track locked files
	active        map[string]ActiveDownload
	eventSink     func(string, File, any)
	lockMutex     *sync.Mutex // Protect runtime maps
}

func (e *Engine) SetEventSink(sink func(string, File, any)) { e.eventSink = sink }

func (e *Engine) publish(eventType string, file File, data any) {
	if e.eventSink != nil {
		e.eventSink(eventType, file, data)
	}
}

func (e *Engine) Start(ctx context.Context) {
	progressCH := make(chan Progress, e.config.Concurrency)

	// Запуск воркеров для загрузки файлов
	go e.downloadFiles(ctx, progressCH, e.config.Concurrency)

	// Запуск рутины отслеживания прогресса загрузки файлов
	go e.progressPrinter(ctx, progressCH)
}

// ScanNow performs one synchronous remote scan. Long-lived callers should use
// this method as the single entry point for both scheduled and manual scans.
func (e *Engine) ScanNow() error {
	return e.scanNewFilesOnce(e.config.InputPath)
}

func (e *Engine) scanNewFilesOnce(inputPath string) error {
	files, err := e.scanner.Scan(e.config, inputPath)
	if err != nil {
		return err
	}

	e.log.Logf("[DEBUG] scanning completed: %d files found", len(files))
	for _, file := range files {
		localPath, err := e.findLocalFile(file)
		switch {
		case err == nil:
			e.log.Logf("[INFO] file %s already exists locally at %s", file.Name, localPath)
			if e.config.RemoveRemote {
				if err := e.deleteRemoteIfConfirmed(file); err != nil {
					e.log.Logf("[ERROR] failed to delete confirmed remote file %s: %v", file.Name, err)
				}
			}
			continue
		case !errors.Is(err, ErrLocalFileNotFound):
			e.log.Logf("[ERROR] failed to check local copy of %s: %v", file.Name, err)
			continue
		}

		err = e.queue.Exists(file.ID)
		switch err {
		case nil:
			e.log.Logf("[DEBUG] file %s already exists in queue", file.Name)
		case ErrNotFound:
			e.log.Logf("[DEBUG] file %s not found in queue", file.Name)
			if err := e.queue.Add(file); err != nil {
				e.log.Logf("[ERROR] failed to add file %s to queue: %v", file.Name, err)
			} else {
				e.publish("queue.added", file, nil)
			}
		default:
			e.log.Logf("[ERROR] failed to check file %s in queue: %v", file.Name, err)
		}
	}

	return nil
}

// Данный метод запускает воркеры загрузки файлов
func (e *Engine) downloadFiles(ctx context.Context, pc chan<- Progress, limit int) {
	ch := e.queue.Chan(ctx, e.log, nil)

	limiter := make(chan struct{}, limit)

	for {
		select {
		case file, ok := <-ch:
			if !ok {
				return
			}
			// Try to acquire file lock
			if !e.acquireFileLock(file.ID) {
				e.log.Logf("[WARN] file %s is already being downloaded, skipping", file.Name)
				continue
			}

			limiter <- struct{}{}

			go func(f File) {
				defer func() {
					e.endActive(f.ID)
					<-limiter
					e.releaseFileLock(f.ID)
				}()

				err := e.filterTaskFromQueue(f)
				if err != nil {
					return
				}
				e.beginActive(f)
				e.publish("download.started", f, nil)

				e.log.Logf("[DEBUG] starting download of file %s (size: %d bytes)", f.Name, f.Size)

				err = e.downloader.Download(pc, f)
				if err != nil {
					e.log.Logf("[ERROR] failed to download file %s: %v", f.Name, err)
					e.publish("download.failed", f, map[string]string{"error": err.Error()})
				} else {
					e.log.Logf("[INFO] successfully downloaded file %s", f.Name)
					e.publish("download.completed", f, nil)
					err = e.queue.Delete(f.ID)
					if err != nil {
						e.log.Logf("[ERROR] failed to delete file %s from queue: %v", f.Name, err)
					}

					if e.config.RemoveRemote {
						if err := e.deleteRemoteIfConfirmed(f); err != nil {
							e.log.Logf("[ERROR] failed to delete confirmed remote file %s: %v", f.Name, err)
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
			e.updateProgress(progress)
			e.publish("download.progress", File{ID: progress.ID, Name: progress.Name}, progress)
		default:
			time.Sleep(time.Millisecond * 100)
		}
	}
}

func (e *Engine) beginActive(file File) {
	e.lockMutex.Lock()
	e.active[file.ID] = ActiveDownload{ID: file.ID, Name: file.Name, Size: file.Size}
	e.lockMutex.Unlock()
}

func (e *Engine) updateProgress(progress Progress) {
	e.lockMutex.Lock()
	active, ok := e.active[progress.ID]
	if ok {
		active.Percent = progress.Percent
		active.Speed = progress.Speed
		active.Downloaded = int64(float64(active.Size) * progress.Percent / 100)
		e.active[progress.ID] = active
	}
	e.lockMutex.Unlock()
}

func (e *Engine) endActive(id string) {
	e.lockMutex.Lock()
	delete(e.active, id)
	e.lockMutex.Unlock()
}

func (e *Engine) ActiveDownloads() []ActiveDownload {
	e.lockMutex.Lock()
	defer e.lockMutex.Unlock()
	result := make([]ActiveDownload, 0, len(e.active))
	for _, active := range e.active {
		result = append(result, active)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
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

func (e *Engine) filterTaskFromQueue(file File) error {
	localPath, err := e.findLocalFile(file)
	if errors.Is(err, ErrLocalFileNotFound) {
		return nil
	}
	if err != nil {
		e.log.Logf("[ERROR] failed to check local copy of %s before download: %v", file.Name, err)
		return err
	}

	e.log.Logf("[INFO] removing queued file %s because it exists locally at %s", file.Name, localPath)
	if err := e.queue.Delete(file.ID); err != nil {
		e.log.Logf("[ERROR] failed to delete file %s from queue: %v", file.Name, err)
		return err
	}
	if e.config.RemoveRemote {
		if err := e.deleteRemoteIfConfirmed(file); err != nil {
			e.log.Logf("[ERROR] failed to delete confirmed remote file %s: %v", file.Name, err)
		}
	}

	return errors.New("file already exists locally")
}

func (e *Engine) findLocalFile(file File) (string, error) {
	return FindLocalCopy(file, e.existingFiles)
}

func (e *Engine) deleteRemoteIfConfirmed(file File) error {
	if _, err := e.findLocalFile(file); err != nil {
		if errors.Is(err, ErrLocalFileNotFound) {
			return errors.New("local copy is no longer available")
		}
		return fmt.Errorf("revalidate local copy: %w", err)
	}

	if err := e.downloader.Delete(file); err != nil {
		return fmt.Errorf("delete remote file: %w", err)
	}
	return nil
}
