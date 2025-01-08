package files

import (
	"fmt"
	"os"
	"time"

	"github.com/ReanSn0w/wddl/pkg/engine"
)

type PartitionWriteCloser struct {
	ProgressChan chan<- engine.Progress
	File         *engine.File

	Path         string
	CurrentIndex int

	writedBytes   int64
	currentPart   *os.File
	lastSplitTime time.Time
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
		totalWritten += n
		dataToWrite = dataToWrite[toWrite:]

		// Если текущая часть полная, закрываем её
		if p.writedBytes == partitionSize {
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

	err := p.currentPart.Close()
	if err != nil {
		return err
	}

	return nil
}

func (p *PartitionWriteCloser) makeProgress() engine.Progress {
	downloaded := (p.CurrentIndex - 1) * partitionSize
	fullSize := p.File.Size

	duration := time.Since(p.lastSplitTime)
	seconds := duration / time.Second

	return engine.Progress{
		ID:      p.File.ID,
		Name:    p.File.Name,
		Percent: float64(downloaded) / float64(fullSize) * 100,
		Speed:   partitionSize / int64(seconds),
	}
}

func (p *PartitionWriteCloser) makePartition() (err error) {
	p.CurrentIndex++
	p.writedBytes = 0

	if !p.lastSplitTime.IsZero() {
		p.ProgressChan <- p.makeProgress()
	}

	p.lastSplitTime = time.Now()

	p.currentPart, err = os.Create(fmt.Sprintf("%s/%d.part", p.Path, p.CurrentIndex))
	return err
}
