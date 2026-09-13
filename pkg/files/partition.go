package files

import (
	"context"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/ReanSn0w/wddl/pkg/engine"
)

type PartitionWriteCloser struct {
	Context      context.Context
	ProgressChan chan<- engine.Progress
	File         *engine.File

	Path         string
	CurrentIndex int

	writedBytes       int64
	downloaded        int64
	currentPart       *os.File
	now               func() time.Time
	progressEvery     time.Duration
	lastProgressAt    time.Time
	lastProgressBytes int64
	lastSpeed         int64
}

func newPartitionWriteCloser(
	ctx context.Context,
	progress chan<- engine.Progress,
	file *engine.File,
	path string,
	currentIndex int,
	downloaded int64,
	now func() time.Time,
	progressEvery time.Duration,
) *PartitionWriteCloser {
	if now == nil {
		now = time.Now
	}
	if progressEvery <= 0 {
		progressEvery = time.Second
	}
	mark := now()
	return &PartitionWriteCloser{
		Context: ctx, ProgressChan: progress, File: file, Path: path,
		CurrentIndex: currentIndex, downloaded: downloaded, now: now,
		progressEvery: progressEvery, lastProgressAt: mark, lastProgressBytes: downloaded,
	}
}

func (p *PartitionWriteCloser) CurrentPart() *os.File {
	return p.currentPart
}

func (p *PartitionWriteCloser) WritedBytes() int64 {
	return p.writedBytes
}

func (p *PartitionWriteCloser) Write(data []byte) (int, error) {
	totalWritten := 0
	dataToWrite := data

	for len(dataToWrite) > 0 {
		// Создаем новую часть если нужна
		if p.currentPart == nil {
			if err := p.makePartition(); err != nil {
				return totalWritten, err
			}
		}

		// Сколько байт можем записать в текущую часть
		remaining := partitionSize - p.writedBytes
		toWrite := int64(len(dataToWrite))

		if toWrite > remaining {
			toWrite = remaining
		}

		// Записываем порцию данных
		n, err := p.currentPart.Write(dataToWrite[:toWrite])
		if err != nil {
			return totalWritten + n, err
		}

		p.writedBytes += int64(n)
		p.downloaded += int64(n)
		totalWritten += n
		dataToWrite = dataToWrite[toWrite:]
		if err := p.PublishProgress(false); err != nil {
			return totalWritten, err
		}

		// Если текущая часть полная, закрываем её и синхронизируем
		if p.writedBytes == partitionSize {
			// Sync to ensure data is written to disk
			if err := p.currentPart.Sync(); err != nil {
				p.currentPart.Close()
				return totalWritten, err
			}
			if err := p.currentPart.Close(); err != nil {
				return totalWritten, err
			}
			p.currentPart = nil
		}
	}

	return totalWritten, nil
}

func (p *PartitionWriteCloser) Close() error {
	if p.currentPart == nil {
		return nil
	}
	part := p.currentPart
	p.currentPart = nil

	// Sync to ensure data is written to disk before closing
	if err := part.Sync(); err != nil {
		part.Close()
		return err
	}

	err := part.Close()
	if err != nil {
		return err
	}

	return nil
}

func (p *PartitionWriteCloser) PublishProgress(force bool) error {
	now := p.now()
	elapsed := now.Sub(p.lastProgressAt)
	if !force && elapsed < p.progressEvery {
		return nil
	}

	speed := p.lastSpeed
	if delta := p.downloaded - p.lastProgressBytes; delta > 0 && elapsed > 0 {
		speed = int64(float64(delta) / elapsed.Seconds())
	}
	progress := newProgress(*p.File, p.downloaded, speed)
	if err := publishProgress(p.Context, p.ProgressChan, progress, force); err != nil {
		return err
	}
	p.lastProgressAt = now
	p.lastProgressBytes = p.downloaded
	p.lastSpeed = speed
	return nil
}

func (p *PartitionWriteCloser) makePartition() (err error) {
	p.CurrentIndex++
	p.writedBytes = 0

	p.currentPart, err = os.Create(fmt.Sprintf("%s/%d.part", p.Path, p.CurrentIndex))
	return err
}

func newProgress(file engine.File, downloaded, speed int64) engine.Progress {
	if downloaded < 0 {
		downloaded = 0
	}
	if downloaded > file.Size {
		downloaded = file.Size
	}
	percent := 100.0
	if file.Size > 0 {
		percent = float64(downloaded) / float64(file.Size) * 100
	}
	if math.IsNaN(percent) || math.IsInf(percent, 0) {
		percent = 0
	}
	percent = math.Max(0, math.Min(100, percent))
	if speed < 0 {
		speed = 0
	}
	return engine.Progress{
		ID: file.ID, Name: file.Name, Percent: percent, Speed: speed,
		Downloaded: downloaded, Size: file.Size,
	}
}

func publishProgress(ctx context.Context, output chan<- engine.Progress, progress engine.Progress, required bool) error {
	if output == nil {
		return nil
	}
	if !required {
		select {
		case output <- progress:
		default:
		}
		return nil
	}
	select {
	case output <- progress:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
