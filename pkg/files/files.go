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

const (
	maxRetries    = 3
	partitionSize = 64 << 20 // 64 MB
)

type Webdav interface {
	ReadDir(path string) ([]os.FileInfo, error)
	ReadStreamRange(path string, offset int64, length int64) (io.ReadCloser, error)
	Remove(path string) error
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

	lgr.Default().Logf("[DEBUG] download delay before starting (3 seconds)")
	time.Sleep(time.Second * 3)

	for attempt := range maxRetries {
		lgr.Default().Logf("[DEBUG] download attempt %d/%d for file %s", attempt+1, maxRetries, file.Name)
		err := d.download(pch, file)
		if err == nil {
			lgr.Default().Logf("[INFO] download completed successfully for file %s", file.Name)
			return nil
		}
		lastErr = err
		if attempt < maxRetries-1 {
			backoff := time.Duration(math.Pow(2, float64(attempt+1))) * time.Second
			lgr.Default().Logf("[WARN] download attempt %d/%d failed for %s, retry in %v: %v",
				attempt+1, maxRetries, file.ID, backoff, err)
			time.Sleep(backoff)
		}
	}

	lgr.Default().Logf("[ERROR] download failed for %s after %d attempts: %v", file.ID, maxRetries, lastErr)
	return fmt.Errorf("failed to download %s after %d attempts: %w", file.ID, maxRetries, lastErr)
}

func (d *Files) Delete(file engine.File) error {
	return d.client.Remove(file.Source)
}

func (f *Files) download(pch chan<- engine.Progress, file engine.File) error {
	lgr.Default().Logf("[DEBUG] creating temp directory for file %s", file.Name)
	err := os.MkdirAll(file.Temp, 0755)
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	lgr.Default().Logf("[DEBUG] checking download status for file %s", file.Name)
	stat, err := f.currentStat(file)
	if err != nil {
		return fmt.Errorf("failed to get current stat: %w", err)
	}

	lgr.Default().Logf("[DEBUG] file %s progress: %d/%d partitions (%.2f%%)",
		file.Name, stat.Done, stat.Count, stat.CompletePercent())

	if !stat.IsComplete() {
		lgr.Default().Logf("[DEBUG] starting download stream for file %s from byte %d", file.Name, stat.SkipBytes)
		datastream, err := f.client.ReadStreamRange(file.Source, stat.SkipBytes, file.Size-stat.SkipBytes)
		if err != nil {
			return fmt.Errorf("failed to create read stream: %w", err)
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
			return fmt.Errorf("failed to copy download data: %w", err)
		}

		lgr.Default().Logf("[DEBUG] download stream completed for file %s", file.Name)
	} else {
		lgr.Default().Logf("[DEBUG] file %s is already fully downloaded, skipping stream", file.Name)
	}

	lgr.Default().Logf("[DEBUG] completing file %s (merging partitions and moving to destination)", file.Name)
	return f.completeFile(file)
}

func (f *Files) validatePartitions(file engine.File, stat *Stat) error {
	// Verify all expected partitions exist and have correct sizes
	for i := int64(1); i <= stat.Count; i++ {
		partFile := fmt.Sprintf("%s/%d.part", file.Temp, i)
		info, err := os.Stat(partFile)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("partition %d missing: %w", i, err)
			}
			return fmt.Errorf("failed to stat partition %d: %w", i, err)
		}

		expectedSize := int64(partitionSize)
		if i == stat.Count && stat.LastPartitionSize != 0 {
			expectedSize = stat.LastPartitionSize
		}

		if info.Size() != expectedSize {
			return fmt.Errorf("partition %d has incorrect size: expected %d, got %d", i, expectedSize, info.Size())
		}
	}

	return nil
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

	// Count complete partitions sequentially from the beginning
	for i := int64(1); i <= stat.Count; i++ {
		partFile := fmt.Sprintf("%s/%d.part", file.Temp, i)
		info, err := os.Stat(partFile)
		if err != nil {
			if os.IsNotExist(err) {
				// Partition doesn't exist, stop counting
				break
			}
			return nil, err
		}

		// Validate partition size
		expectedSize := int64(partitionSize)
		if i == stat.Count && stat.LastPartitionSize != 0 {
			expectedSize = stat.LastPartitionSize
		}

		if info.Size() != expectedSize {
			// Partition has incorrect size, stop counting
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
	// First, get current stat and validate all partitions exist
	stat, err := f.currentStat(file)
	if err != nil {
		return fmt.Errorf("failed to get partition stat: %w", err)
	}

	if !stat.IsComplete() {
		return fmt.Errorf("download incomplete: %d/%d partitions", stat.Done, stat.Count)
	}

	// Validate all expected partitions exist and have correct sizes
	if err := f.validatePartitions(file, stat); err != nil {
		return fmt.Errorf("partition validation failed: %w", err)
	}

	// Create a temporary merge file first (in temp directory, then move)
	tempMergeFile := file.Temp + "/" + file.Name + ".merging"
	resultFile, err := os.Create(tempMergeFile)
	if err != nil {
		return fmt.Errorf("failed to create merge file: %w", err)
	}

	// Merge all partitions in order
	for i := int64(1); i <= stat.Count; i++ {
		partFile := fmt.Sprintf("%s/%d.part", file.Temp, i)
		pf, err := os.Open(partFile)
		if err != nil {
			resultFile.Close()
			os.Remove(tempMergeFile)
			return fmt.Errorf("failed to open partition %d: %w", i, err)
		}

		_, err = io.Copy(resultFile, pf)
		pf.Close()
		if err != nil {
			resultFile.Close()
			os.Remove(tempMergeFile)
			return fmt.Errorf("failed to copy partition %d: %w", i, err)
		}
	}

	// Sync to ensure all data is written to disk before closing
	if err := resultFile.Sync(); err != nil {
		resultFile.Close()
		os.Remove(tempMergeFile)
		return fmt.Errorf("failed to sync merged file: %w", err)
	}

	if err := resultFile.Close(); err != nil {
		os.Remove(tempMergeFile)
		return fmt.Errorf("failed to close merged file: %w", err)
	}

	// Verify merged file size matches expected size
	info, err := os.Stat(tempMergeFile)
	if err != nil {
		os.Remove(tempMergeFile)
		return fmt.Errorf("failed to stat merged file: %w", err)
	}

	if info.Size() != file.Size {
		os.Remove(tempMergeFile)
		return fmt.Errorf("merged file size mismatch: expected %d, got %d", file.Size, info.Size())
	}

	// Move to final destination
	err = os.MkdirAll(filepath.Dir(file.Dest), 0755)
	if err != nil {
		os.Remove(tempMergeFile)
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	err = f.moveFile(tempMergeFile, file.Dest)
	if err != nil {
		os.Remove(tempMergeFile)
		return fmt.Errorf("failed to move file to destination: %w", err)
	}

	// Clean up temporary directory
	err = os.RemoveAll(file.Temp)
	if err != nil {
		return fmt.Errorf("failed to remove temp directory: %w", err)
	}

	return nil
}

func (f *Files) moveFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		dstFile.Close()
		os.Remove(dst)
		return fmt.Errorf("failed to copy file data: %w", err)
	}

	// Sync to ensure data is written to disk before closing
	if err := dstFile.Sync(); err != nil {
		dstFile.Close()
		os.Remove(dst)
		return fmt.Errorf("failed to sync destination file: %w", err)
	}

	if err := dstFile.Close(); err != nil {
		os.Remove(dst)
		return fmt.Errorf("failed to close destination file: %w", err)
	}

	// Only remove source after successful destination write
	if err := os.Remove(src); err != nil {
		return fmt.Errorf("failed to remove source file: %w", err)
	}

	return nil
}
