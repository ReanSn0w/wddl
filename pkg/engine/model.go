package engine

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

var ErrNotFound = errors.New("file not found")

type Queue interface {
	List(context.Context, int, func(File) bool) ([]File, error)
	Upsert(context.Context, File) error
	Get(context.Context, string) (*File, error)
	MarkPartComplete(context.Context, string, string) error
	Delete(context.Context, string) error
}

type Scanner interface {
	Scan(Config, string) ([]File, error)
}

type Downloader interface {
	DownloadWithRetry(part Part, maxRetries int) error
}

type Collector interface {
	Collect(File) error
	Check(string, int64) (bool, error)
}

func NewFile(conf Config, source string, size int64) File {
	hash := md5.Sum([]byte(source + "_" + fmt.Sprint(size)))
	fileID := fmt.Sprintf("%x", hash)

	partsCount := int(size / conf.PartitionSize)
	if size%conf.PartitionSize != 0 {
		partsCount++
	}

	parts := make([]Part, partsCount)
	for i := 0; i < partsCount; i++ {
		parts[i].ID = fmt.Sprintf("%s_%d", fileID, i)
		parts[i].FileID = fileID
		parts[i].Source = source
		parts[i].Dest = fmt.Sprintf("%s/%s/%s.part", conf.TempPath, fileID, parts[i].ID)
		parts[i].Size = conf.PartitionSize
		if i == partsCount-1 {
			parts[i].Size = size % conf.PartitionSize
		}
	}

	return File{
		ID:            fileID,
		Name:          filepath.Base(source),
		Source:        source,
		Dest:          conf.OutputPath + strings.TrimPrefix(source, conf.InputPath),
		Size:          size,
		CompleteParts: 0,
		Parts:         parts,
	}
}

type File struct {
	// Уникальный идентификатор файла
	ID string

	// Имя файла
	Name string

	// Место из которого файл будет загружатся
	Source string

	// Место в которое файл будет загружен
	Dest string

	// Размер файла в байтах
	Size int64

	// Количество скачанных частей файла
	CompleteParts int

	// Список частей файла
	Parts []Part
}

type Part struct {
	// Уникальный идентификатор части файла
	ID string

	// Идентификатор файла
	FileID string

	// Место из которого часть файла будет загружатся
	Source string

	// Место в которое часть файла будет загружатся
	Dest string

	// Флаг, указывающий на то, что часть файла загружена
	Complete bool

	// Отступ от начала файла
	Offset int64

	// Размер части файла в байтах
	Size int64
}
