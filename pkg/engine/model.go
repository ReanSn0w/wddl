package engine

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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
}

type Scanner interface {
	Scan(Config, string) ([]File, error)
}

type Downloader interface {
	Download(pch chan<- Progress, file File) error
}

type Queue interface {
	Add(file File) error
	Exists(id string) error
	Len() (int, error)
	List(filter func(f File) error) ([]File, error)
	Chan(ctx context.Context, log lgr.L, filter func(f File) error) <-chan File
	Delete(id string) error
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
