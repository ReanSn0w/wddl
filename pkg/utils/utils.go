package utils

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/studio-b12/gowebdav"
)

func New(client *gowebdav.Client, target, source string) *Cleaner {
	return &Cleaner{
		Target: target,
		Source: source,
		wd:     client,
	}
}

type Cleaner struct {
	Target string
	Source string

	wd *gowebdav.Client
}

func (c *Cleaner) ClearRemoteFiles() error {
	files, err := c.scanRemoteFiles(c.Source)
	if err != nil {
		return err
	}

	files, err = c.filterAlreadyDownloaded(files)
	if err != nil {
		return err
	}

	err = c.deleteRemoteFiles(files)
	if err != nil {
		return err
	}

	return nil
}

type scannedFile struct {
	CleanPath string
	Size      int64
}

func (c *Cleaner) deleteRemoteFiles(files []scannedFile) error {
	for _, file := range files {
		err := c.wd.Remove(filepath.Join(c.Source, file.CleanPath))
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Cleaner) filterAlreadyDownloaded(files []scannedFile) ([]scannedFile, error) {
	var (
		result []scannedFile
	)

	for _, file := range files {
		fileInfo, err := os.Stat(filepath.Join(c.Target, file.CleanPath))
		if err != nil {
			continue
		}

		if fileInfo.Size() == file.Size {
			result = append(result, file)
		}
	}

	return result, nil
}

func (c *Cleaner) scanRemoteFiles(dir string) ([]scannedFile, error) {
	var (
		result []scannedFile
	)

	items, err := c.wd.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, item := range items {
		switch item.IsDir() {
		case true:
			subItems, err := c.scanRemoteFiles(filepath.Join(dir, item.Name()))
			if err != nil {
				return nil, err
			}

			result = append(result, subItems...)
		case false:
			result = append(result, scannedFile{
				CleanPath: strings.TrimPrefix(filepath.Join(dir, item.Name()), c.Source),
				Size:      item.Size(),
			})
		}
	}

	return result, nil
}
