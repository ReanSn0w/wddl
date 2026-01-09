package files

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ReanSn0w/wddl/pkg/engine"
	"github.com/go-pkgz/lgr"
)

const (
	maxRetries    = 3
	partitionSize = 64 << 20 // 64 MB
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

func (d *Files) Download(pch chan<- engine.Progress, file engine.File) error {
	var lastErr error

	time.Sleep(time.Second * 3)

	for attempt := range maxRetries {
		err := d.download(pch, file)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt < maxRetries-1 {
			backoff := time.Duration(math.Pow(2, float64(attempt+1))) * time.Second
			lgr.Default().Logf("[WARN] download part %s failed (attempt %d/%d), retry in %v: %v",
				file.ID, attempt+1, maxRetries, backoff, err)
			time.Sleep(backoff)
		}
	}

	if lastErr != nil {
		return fmt.Errorf("failed to download %s after %d attempts: %w", file.ID, maxRetries, lastErr)
	}

	return nil
}

func (f *Files) download(pch chan<- engine.Progress, file engine.File) error {
	err := os.MkdirAll(file.Temp, 0755)
	if err != nil {
		return err
	}

	stat, err := f.currentStat(file)
	if err != nil {
		return err
	}

	if !stat.IsComplete() {
		datastream, err := f.client.ReadStreamRange(file.Source, stat.SkipBytes, file.Size-stat.SkipBytes)
		if err != nil {
			return err
		}

		defer datastream.Close()

		pwc := &PartitionWriteCloser{
			ProgressChan: pch,
			File:         &file,
			Path:         file.Temp,
			CurrentIndex: int(stat.Done),
		}

		defer pwc.Close()

		_, err = io.Copy(pwc, datastream)
		if err != nil {
			return err
		}
	}

	return f.completeFile(file)
}

func (f *Files) currentStat(file engine.File) (*Stat, error) {
	stat := &Stat{
		ID:       file.ID,
		FileName: file.Name,
	}

	stat.Count = file.Size / partitionSize
	if lastSize := file.Size % partitionSize; lastSize != 0 {
		stat.Count++
		stat.LastPartitionSize = lastSize
	}

	entries, err := os.ReadDir(file.Temp)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".part") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return nil, err
		}

		if info.Size() != partitionSize && info.Size() != stat.LastPartitionSize {
			break
		}

		stat.Done++
	}

	if !stat.IsComplete() {
		stat.SkipBytes = stat.Done * partitionSize
	}

	return stat, nil
}

func (f *Files) completeFile(file engine.File) error {
	var (
		resultFile *os.File
		entries    []os.DirEntry
		err        error
	)

	entries, err = os.ReadDir(file.Temp)
	if err != nil {
		return err
	}

	sort.Slice(entries, func(i, j int) bool {
		firstNumber, err1 := strconv.Atoi(strings.TrimSuffix(entries[i].Name(), ".part"))
		secondNumber, err2 := strconv.Atoi(strings.TrimSuffix(entries[j].Name(), ".part"))

		if err1 != nil || err2 != nil {
			panic(fmt.Sprintf(
				"Invalid file name format: %s=%v , %s=%v",
				entries[i].Name(), err1, entries[j].Name(), err2))
		}

		return firstNumber < secondNumber
	})

	resultFile, err = os.Create(file.Temp + "/" + file.Name)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		pf, err := os.Open(file.Temp + "/" + entry.Name())
		if err != nil {
			return err
		}

		_, err = io.Copy(resultFile, pf)
		if err != nil {
			return err
		}

		err = pf.Close()
		if err != nil {
			return err
		}
	}

	err = resultFile.Close()
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(file.Dest), 0755)
	if err != nil {
		return err
	}

	err = os.Rename(file.Temp+"/"+file.Name, file.Dest)
	if err != nil {
		return err
	}

	err = os.RemoveAll(file.Temp)
	if err != nil {
		return err
	}

	return nil
}
