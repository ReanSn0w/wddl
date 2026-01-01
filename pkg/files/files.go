package files

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/ReanSn0w/wddl/pkg/engine"
	"github.com/go-pkgz/lgr"
)

type Webdav interface {
	ReadDir(path string) ([]os.FileInfo, error)
	ReadStreamRange(path string, offset int64, length int64) (io.ReadCloser, error)
}

func New(client Webdav) *Files {
	return &Files{
		client: client,
	}
}

type Files struct {
	client Webdav
}

func (f *Files) Scan(conf engine.Config, inputDir string) ([]engine.File, error) {
	files, err := f.client.ReadDir(inputDir)
	if err != nil {
		return nil, err
	}

	var result []engine.File
	for _, file := range files {
		if file.IsDir() {
			sub, err := f.Scan(conf, inputDir+"/"+file.Name())
			if err != nil {
				return nil, err
			}

			result = append(result, sub...)
		} else {
			result = append(result, engine.NewFile(conf, inputDir+"/"+file.Name(), file.Size()))
		}
	}

	return result, nil
}

func (d *Files) DownloadWithRetry(part engine.Part, maxRetries int) error {
	var lastErr error

	for attempt := range maxRetries {
		err := d.Download(part)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt < maxRetries-1 {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			lgr.Default().Logf("[WARN] загрузка части %s неудачна (попытка %d/%d), повтор через %v: %v",
				part.ID, attempt+1, maxRetries, backoff, err)
			time.Sleep(backoff)
		}
	}

	if lastErr != nil {
		return fmt.Errorf("не удалось скачать %s после %d попыток: %w", part.ID, maxRetries, lastErr)
	}

	return nil
}

func (f *Files) Download(part engine.Part) error {
	if err := os.MkdirAll(filepath.Dir(part.Dest), 0755); err != nil {
		return fmt.Errorf("create dest directory: %w", err)
	}

	file, err := os.Create(part.Dest)
	if err != nil {
		lgr.Default().Logf("[DEBUG] open file for task %v error: %v", part.ID, err)
		return err
	}

	defer file.Close()

	stream, err := f.client.ReadStreamRange(part.Source, part.Offset, part.Size)
	if err != nil {
		return err
	}

	defer stream.Close()

	_, err = io.Copy(file, stream)
	if err != nil {
		os.Remove(part.Dest)
	}

	return err
}

func (f *Files) Collect(file engine.File) error {
	destTmp := file.Dest + ".tmp"

	if err := os.MkdirAll(filepath.Dir(destTmp), 0755); err != nil {
		return fmt.Errorf("create dest directory: %w", err)
	}

	result, err := os.Create(destTmp)
	if err != nil {
		return err
	}

	defer result.Close()

	for _, part := range file.Parts {
		if !part.Complete {
			err = fmt.Errorf("загрузка части %s не завершена", part.ID)
			break
		}

		partFile, err := os.Open(part.Dest)
		if err != nil {
			err = fmt.Errorf("открытие части %s не удалось: %v", part.ID, err)
			break
		}

		_, err = io.Copy(result, partFile)
		partFile.Close()
		if err != nil {
			err = fmt.Errorf("копирование части %s не удалось: %v", part.ID, err)
			break
		}
	}

	if err == nil {
		for _, part := range file.Parts {
			if removeErr := os.Remove(part.Dest); removeErr != nil && !os.IsNotExist(removeErr) {
				lgr.Default().Logf("[WARN] не удалось удалить часть %s: %v", part.Dest, removeErr)
			}
		}

		if renameErr := os.Rename(destTmp, file.Dest); renameErr != nil {
			return fmt.Errorf("ошибка при переименовании временного файла в конечный: %w", renameErr)
		}
	} else {
		if removeErr := os.Remove(destTmp); removeErr != nil {
			lgr.Default().Logf("[WARN] не удалось удалить временный файл %s: %v", destTmp, removeErr)
		}
	}

	return err
}

func (f *Files) Check(name string, size int64) (bool, error) {
	fi, err := os.Stat(name)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if fi.Size() == size {
		return true, nil
	}

	return false, nil
}
