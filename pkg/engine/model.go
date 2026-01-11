package engine

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-pkgz/lgr"
)

var ErrNotFound = errors.New("file not found")

type Config struct {
	// Путь к директории из которой будут скачиваться файлы
	InputPath string

	// Путь к директории в которую будут скачиваться файлы
	OutputPath string

	// Путь к директории для временных файлов
	TempPath string

	// Количество потоков для скачивания файлов
	Concurrency int

	// Интервал сканирования файлов
	ScanEvery time.Duration

	// Флаг удаления удаленных файлов
	//
	// Полезен в случае, если удаленный Storage следует чистить
	// в автоматическом режиме
	RemoveRemote bool
}

type Scanner interface {
	Scan(Config, string) ([]File, error)
}

type Downloader interface {
	Download(pch chan<- Progress, file File) error
	Delete(file File) error
}

type Queue interface {
	Add(file File) error
	Exists(id string) error
	Len() (int, error)
	Stat() (*Stat, error)
	List(filter func(f File) error) ([]File, error)
	Chan(ctx context.Context, log lgr.L, filter func(f File) error) <-chan File
	Delete(id string) error
}

type Stat struct {
	Files    int
	FullSize int64
}

func (s *Stat) AvgTime(speed int64) time.Duration {
	if s.Files == 0 || speed == 0 {
		return 0
	}

	return time.Duration(s.FullSize/speed) * time.Second
}

func NewFile(conf Config, source string, size int64) File {
	hash := md5.Sum([]byte(source + "_" + fmt.Sprint(size)))
	fileID := fmt.Sprintf("%x", hash)

	return File{
		ID:     fileID,
		Name:   filepath.Base(source),
		Source: source,
		Dest:   conf.OutputPath + strings.TrimPrefix(source, conf.InputPath),
		Temp:   filepath.Join(conf.TempPath, fileID),
		Size:   size,
	}
}

type File struct {
	// Уникальный идентификатор файла
	ID string

	// Имя файла
	Name string

	// Место из которого файл будет загружатся
	Source string

	// Место временного хранения файла
	Temp string

	// Место в которое файл будет загружен
	Dest string

	// Размер файла в байтах
	Size int64
}

type Progress struct {
	// Идентификатор загружаемого файла
	ID string

	// Имя файла
	Name string

	// Процент загрузки файла
	Percent float64

	// Скорость загрузки файла в байтах в секунду
	Speed int64
}

func (p *Progress) String() string {
	return fmt.Sprintf("%s (%.2f%%) %.2f KB/s", p.Name, p.Percent, float64(p.Speed)/1024)
}

func NewSpeedData() *SpeedData {
	return &SpeedData{
		items: make(map[string]struct {
			mark  time.Time
			items []Progress
		}),
	}
}

type SpeedData struct {
	mx    sync.Mutex
	items map[string]struct {
		mark  time.Time
		items []Progress
	}
}

func (sd *SpeedData) MakeChan(input <-chan Progress) <-chan Progress {
	output := make(chan Progress)

	go func() {
		defer close(output)

		for progress := range input {
			sd.mx.Lock()

			item, ok := sd.items[progress.ID]
			if !ok {
				item = struct {
					mark  time.Time
					items []Progress
				}{
					mark:  time.Now(),
					items: []Progress{},
				}
			}

			item.items = append(item.items, progress)
			sd.items[progress.ID] = item

			// Оставляем последние 10 элементов
			if len(item.items) > 10 {
				item.items = item.items[len(item.items)-10:]
			}

			sd.mx.Unlock()

			output <- progress
		}
	}()

	return output
}

func (sd *SpeedData) AvgSpeed() int64 {
	sd.mx.Lock()
	defer sd.mx.Unlock()

	var (
		currentTime = time.Now()

		avgSpeed   int64
		itemsCount int64

		toClean = make([]string, 0)
	)

	const maxAge = time.Minute * 15

	// Count Stage
	for key, item := range sd.items {
		if currentTime.Sub(item.mark) > maxAge {
			toClean = append(toClean, key)
		}

		var (
			itemAvgSpeed   int64 = 0
			itemItemsCount       = 0
		)

		for _, mark := range item.items {
			itemAvgSpeed += mark.Speed
			itemItemsCount++
		}

		avgSpeed += itemAvgSpeed / int64(itemItemsCount)
		itemsCount++
	}

	// Clean Stage
	for _, key := range toClean {
		delete(sd.items, key)
	}

	if itemsCount == 0 {
		return 0
	}

	return avgSpeed / itemsCount
}
