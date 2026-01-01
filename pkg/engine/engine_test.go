package engine_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ReanSn0w/wddl/pkg/engine"
	"github.com/go-pkgz/lgr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock реализации интерфейсов

type mockQueue struct {
	files map[string]*engine.File
}

func (m *mockQueue) List(ctx context.Context, limit int, filter func(engine.File) bool) ([]engine.File, error) {
	var result []engine.File
	count := 0
	for _, f := range m.files {
		if count >= limit {
			break
		}
		if filter(*f) {
			result = append(result, *f)
			count++
		}
	}
	return result, nil
}

func (m *mockQueue) Upsert(ctx context.Context, f engine.File) error {
	m.files[f.ID] = &f
	return nil
}

func (m *mockQueue) Get(ctx context.Context, id string) (*engine.File, error) {
	f, exists := m.files[id]
	if !exists {
		return nil, engine.ErrNotFound
	}
	return f, nil
}

func (m *mockQueue) MarkPartComplete(ctx context.Context, fileID string, partID string) error {
	f, exists := m.files[fileID]
	if !exists {
		return errors.New("file not found")
	}
	for i := range f.Parts {
		if f.Parts[i].ID == partID {
			f.Parts[i].Complete = true
			f.CompleteParts++
		}
	}
	return nil
}

func (m *mockQueue) Delete(ctx context.Context, id string) error {
	delete(m.files, id)
	return nil
}

type mockScanner struct {
	files []engine.File
	err   error
}

func (m *mockScanner) Scan(config engine.Config, path string) ([]engine.File, error) {
	return m.files, m.err
}

type mockDownloader struct {
	err error
}

func (m *mockDownloader) DownloadWithRetry(p engine.Part, maxRetries int) error {
	return m.err
}

type mockCollector struct {
	collectErr error
	checkErr   error
	isComplete bool
}

func (m *mockCollector) Collect(f engine.File) error {
	return m.collectErr
}

func (m *mockCollector) Check(dest string, size int64) (bool, error) {
	return m.isComplete, m.checkErr
}

// Тесты

// func TestNewFile(t *testing.T) {
// 	conf := engine.Config{
// 		InputPath:     "/input",
// 		OutputPath:    "/output",
// 		TempPath:      "/temp",
// 		PartitionSize: 1024,
// 		Concurrency:   4,
// 		ScanEvery:     time.Second,
// 	}

// 	file := engine.NewFile(conf, "/input/test.txt", 3072)

// 	assert.NotEmpty(t, file.ID)
// 	assert.Equal(t, "test.txt", file.Name)
// 	assert.Equal(t, "/input/test.txt", file.Source)
// 	assert.Equal(t, "/output/test.txt", file.Dest)
// 	assert.Equal(t, int64(3072), file.Size)
// 	assert.Equal(t, 3, len(file.Parts))
// 	assert.Equal(t, 0, file.CompleteParts)

// 	// Проверка размеров частей
// 	assert.Equal(t, int64(1024), file.Parts[0].Size)
// 	assert.Equal(t, int64(1024), file.Parts[1].Size)
// 	assert.Equal(t, int64(1024), file.Parts[2].Size)
// }

func TestNewFileWithRemainderPart(t *testing.T) {
	conf := engine.Config{
		InputPath:     "/input",
		OutputPath:    "/output",
		TempPath:      "/temp",
		PartitionSize: 1024,
		Concurrency:   4,
		ScanEvery:     time.Second,
	}

	// Размер не кратен PartitionSize
	file := engine.NewFile(conf, "/input/test.txt", 2500)

	assert.Equal(t, 3, len(file.Parts))
	assert.Equal(t, int64(1024), file.Parts[0].Size)
	assert.Equal(t, int64(1024), file.Parts[1].Size)
	assert.Equal(t, int64(452), file.Parts[2].Size)
}

func TestNewEngine(t *testing.T) {
	log := lgr.New()
	conf := engine.Config{
		InputPath:     "/input",
		OutputPath:    "/output",
		TempPath:      "/temp",
		PartitionSize: 1024,
		Concurrency:   4,
		ScanEvery:     time.Second,
	}

	q := &mockQueue{files: make(map[string]*engine.File)}
	s := &mockScanner{}
	d := &mockDownloader{}
	c := &mockCollector{}

	e := engine.New(log, conf, q, s, d, c)

	assert.NotNil(t, e)
}

// func TestAddToFileQueueNewFile(t *testing.T) {
// 	log := lgr.New()
// 	conf := engine.Config{
// 		InputPath:     "/input",
// 		OutputPath:    "/output",
// 		TempPath:      "/temp",
// 		PartitionSize: 1024,
// 		Concurrency:   4,
// 		ScanEvery:     time.Second,
// 	}

// 	q := &mockQueue{files: make(map[string]*engine.File)}
// 	s := &mockScanner{}
// 	d := &mockDownloader{}
// 	c := &mockCollector{isComplete: false}

// 	e := engine.New(log, conf, q, s, d, c)

// 	// Создаем файл
// 	file := engine.NewFile(conf, "/input/test.txt", 3072)

// 	// Создаем канал и отправляем файл
// 	ch := make(chan engine.File, 1)
// 	ch <- file
// 	close(ch)

// 	ctx, cancel := context.WithCancel(context.Background())
// 	go e.Start(ctx)

// 	time.Sleep(100 * time.Millisecond)
// 	cancel()

// 	// Проверяем, что файл был добавлен в очередь
// 	assert.NotNil(t, q.files[file.ID])
// }

func TestAddToFileQueueAlreadyDownloaded(t *testing.T) {
	log := lgr.New()
	conf := engine.Config{
		InputPath:     "/input",
		OutputPath:    "/output",
		TempPath:      "/temp",
		PartitionSize: 1024,
		Concurrency:   4,
		ScanEvery:     time.Second,
	}

	q := &mockQueue{files: make(map[string]*engine.File)}
	s := &mockScanner{}
	d := &mockDownloader{}
	c := &mockCollector{isComplete: true} // Файл уже загружен

	e := engine.New(log, conf, q, s, d, c)

	file := engine.NewFile(conf, "/input/test.txt", 3072)

	ch := make(chan engine.File, 1)
	ch <- file
	close(ch)

	ctx, cancel := context.WithCancel(context.Background())
	go e.Start(ctx)

	time.Sleep(100 * time.Millisecond)
	cancel()

	// Файл не должен быть добавлен, так как он уже загружен
	assert.Empty(t, q.files)
}

func TestAddToFileQueueAlreadyInQueue(t *testing.T) {
	log := lgr.New()
	conf := engine.Config{
		InputPath:     "/input",
		OutputPath:    "/output",
		TempPath:      "/temp",
		PartitionSize: 1024,
		Concurrency:   4,
		ScanEvery:     time.Second,
	}

	file := engine.NewFile(conf, "/input/test.txt", 3072)

	q := &mockQueue{files: map[string]*engine.File{file.ID: &file}}
	s := &mockScanner{}
	d := &mockDownloader{}
	c := &mockCollector{isComplete: false}

	e := engine.New(log, conf, q, s, d, c)

	ch := make(chan engine.File, 1)
	ch <- file
	close(ch)

	ctx, cancel := context.WithCancel(context.Background())
	go e.Start(ctx)

	time.Sleep(100 * time.Millisecond)
	cancel()

	// Количество файлов не изменилось
	assert.Equal(t, 1, len(q.files))
}

func TestPartGeneration(t *testing.T) {
	conf := engine.Config{
		InputPath:     "/input",
		OutputPath:    "/output",
		TempPath:      "/temp",
		PartitionSize: 1024,
		Concurrency:   4,
		ScanEvery:     time.Second,
	}

	file := engine.NewFile(conf, "/input/test.txt", 3072)

	// Проверяем, что все части имеют корректные ID
	for _, part := range file.Parts {
		assert.NotEmpty(t, part.ID)
		assert.Equal(t, file.ID, part.FileID)
		assert.Equal(t, "/input/test.txt", part.Source)
		assert.Contains(t, part.Dest, file.ID)
		assert.False(t, part.Complete)
	}
}

func TestMarkPartComplete(t *testing.T) {
	q := &mockQueue{files: make(map[string]*engine.File)}
	conf := engine.Config{
		InputPath:     "/input",
		OutputPath:    "/output",
		TempPath:      "/temp",
		PartitionSize: 1024,
		Concurrency:   4,
		ScanEvery:     time.Second,
	}

	file := engine.NewFile(conf, "/input/test.txt", 3072)
	q.Upsert(context.Background(), file)

	// Отмечаем первую часть как загруженную
	err := q.MarkPartComplete(context.Background(), file.ID, file.Parts[0].ID)
	require.NoError(t, err)

	// Проверяем, что часть отмечена
	updatedFile, err := q.Get(context.Background(), file.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, updatedFile.CompleteParts)
	assert.True(t, updatedFile.Parts[0].Complete)
	assert.False(t, updatedFile.Parts[1].Complete)
}

func TestEngineStart(t *testing.T) {
	log := lgr.New()
	conf := engine.Config{
		InputPath:     "/input",
		OutputPath:    "/output",
		TempPath:      "/temp",
		PartitionSize: 1024,
		Concurrency:   2,
		ScanEvery:     time.Millisecond * 500,
	}

	q := &mockQueue{files: make(map[string]*engine.File)}
	s := &mockScanner{files: []engine.File{}}
	d := &mockDownloader{}
	c := &mockCollector{isComplete: false}

	e := engine.New(log, conf, q, s, d, c)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Запускаем engine
	e.Start(ctx)

	// Даем время на инициализацию
	<-ctx.Done()
}

func TestNewFileHashConsistency(t *testing.T) {
	conf := engine.Config{
		InputPath:     "/input",
		OutputPath:    "/output",
		TempPath:      "/temp",
		PartitionSize: 1024,
		Concurrency:   4,
		ScanEvery:     time.Second,
	}

	// Создаем один и тот же файл дважды
	file1 := engine.NewFile(conf, "/input/test.txt", 3072)
	file2 := engine.NewFile(conf, "/input/test.txt", 3072)

	// ID должны быть идентичны
	assert.Equal(t, file1.ID, file2.ID)
	assert.Equal(t, len(file1.Parts), len(file2.Parts))

	for i := range file1.Parts {
		assert.Equal(t, file1.Parts[i].ID, file2.Parts[i].ID)
	}
}

func TestNewFileDifferentSizes(t *testing.T) {
	conf := engine.Config{
		InputPath:     "/input",
		OutputPath:    "/output",
		TempPath:      "/temp",
		PartitionSize: 1024,
		Concurrency:   4,
		ScanEvery:     time.Second,
	}

	file1 := engine.NewFile(conf, "/input/test.txt", 3072)
	file2 := engine.NewFile(conf, "/input/test.txt", 5000)

	// ID должны быть разными
	assert.NotEqual(t, file1.ID, file2.ID)
}
