package utils

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ReanSn0w/wddl/pkg/engine"
)

type Webdav interface {
	ReadDir(path string) ([]os.FileInfo, error)
	Remove(path string) error
}

func New(client Webdav, target, source string, existingFiles engine.ExistingFileFinder) *Cleaner {
	return &Cleaner{
		Target:        target,
		Source:        source,
		wd:            client,
		existingFiles: existingFiles,
	}
}

type Cleaner struct {
	Target string
	Source string

	wd            Webdav
	existingFiles engine.ExistingFileFinder
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

	return c.deleteRemoteFiles(files)
}

type scannedFile struct {
	CleanPath string
	Name      string
	Size      int64
}

func (c *Cleaner) deleteRemoteFiles(files []scannedFile) error {
	for _, file := range files {
		_, err := c.findLocalCopy(file)
		if errors.Is(err, engine.ErrLocalFileNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("revalidate local copy for %q: %w", file.CleanPath, err)
		}

		if err := c.wd.Remove(path.Join(c.Source, file.CleanPath)); err != nil {
			return fmt.Errorf("delete remote file %q: %w", file.CleanPath, err)
		}
	}

	return nil
}

func (c *Cleaner) filterAlreadyDownloaded(files []scannedFile) ([]scannedFile, error) {
	result := make([]scannedFile, 0, len(files))

	for _, file := range files {
		_, err := c.findLocalCopy(file)
		if errors.Is(err, engine.ErrLocalFileNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("find local copy for %q: %w", file.CleanPath, err)
		}
		result = append(result, file)
	}

	return result, nil
}

func (c *Cleaner) scanRemoteFiles(dir string) ([]scannedFile, error) {
	var result []scannedFile

	items, err := c.wd.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, item := range items {
		if item.IsDir() {
			subItems, err := c.scanRemoteFiles(path.Join(dir, item.Name()))
			if err != nil {
				return nil, err
			}

			result = append(result, subItems...)
			continue
		}

		result = append(result, scannedFile{
			CleanPath: strings.TrimPrefix(path.Join(dir, item.Name()), c.Source),
			Name:      item.Name(),
			Size:      item.Size(),
		})
	}

	return result, nil
}

func (c *Cleaner) findLocalCopy(file scannedFile) (string, error) {
	cleanPath := strings.TrimPrefix(file.CleanPath, "/")
	return engine.FindLocalCopy(engine.File{
		Name: file.Name,
		Dest: filepath.Join(c.Target, filepath.FromSlash(cleanPath)),
		Size: file.Size,
	}, c.existingFiles)
}
