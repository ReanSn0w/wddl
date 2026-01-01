package files_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ReanSn0w/wddl/pkg/engine"
	"github.com/ReanSn0w/wddl/pkg/files"
	"github.com/stretchr/testify/assert"
)

// Mock для интерфейса Webdav
type MockWebdav struct {
	readDirFunc         func(path string) ([]os.FileInfo, error)
	readStreamRangeFunc func(path string, offset int64, length int64) (io.ReadCloser, error)
}

func (m *MockWebdav) ReadDir(path string) ([]os.FileInfo, error) {
	return m.readDirFunc(path)
}

func (m *MockWebdav) ReadStreamRange(path string, offset int64, length int64) (io.ReadCloser, error) {
	return m.readStreamRangeFunc(path, offset, length)
}

// Mock для os.FileInfo
type MockFileInfo struct {
	name  string
	size  int64
	isDir bool
}

func (m *MockFileInfo) Name() string       { return m.name }
func (m *MockFileInfo) Size() int64        { return m.size }
func (m *MockFileInfo) Mode() os.FileMode  { return 0644 }
func (m *MockFileInfo) ModTime() time.Time { return time.Now() }
func (m *MockFileInfo) IsDir() bool        { return m.isDir }
func (m *MockFileInfo) Sys() interface{}   { return nil }

// Helper для создания конфига с валидными значениями
func createTestConfig() engine.Config {
	tmpDir := os.TempDir()
	return engine.Config{
		InputPath:     tmpDir,
		OutputPath:    tmpDir,
		TempPath:      tmpDir,
		PartitionSize: 1024 * 1024, // 1MB
		Concurrency:   4,
		ScanEvery:     10 * time.Second,
	}
}

// TestScan_SimpleFiles проверяет сканирование простых файлов
func TestScan_SimpleFiles(t *testing.T) {
	mock := &MockWebdav{
		readDirFunc: func(path string) ([]os.FileInfo, error) {
			if path == "/root" {
				return []os.FileInfo{
					&MockFileInfo{name: "file1.txt", size: 100, isDir: false},
					&MockFileInfo{name: "file2.txt", size: 200, isDir: false},
				}, nil
			}
			return nil, errors.New("not found")
		},
	}

	f := files.New(mock)
	conf := createTestConfig()
	files, err := f.Scan(conf, "/root")

	if err != nil {
		t.Fatalf("ожидали nil, получили %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("ожидали 2 файла, получили %d", len(files))
	}
}

// TestScan_RecursiveDirectory проверяет рекурсивное сканирование
func TestScan_RecursiveDirectory(t *testing.T) {
	mock := &MockWebdav{
		readDirFunc: func(path string) ([]os.FileInfo, error) {
			if path == "/root" {
				return []os.FileInfo{
					&MockFileInfo{name: "subdir", size: 0, isDir: true},
				}, nil
			}
			if path == "/root/subdir" {
				return []os.FileInfo{
					&MockFileInfo{name: "file.txt", size: 100, isDir: false},
				}, nil
			}
			return nil, errors.New("not found")
		},
	}

	f := files.New(mock)
	conf := createTestConfig()
	files, err := f.Scan(conf, "/root")

	if err != nil {
		t.Fatalf("ожидали nil, получили %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("ожидали 1 файл, получили %d", len(files))
	}
}

// TestScan_EmptyDirectory проверяет сканирование пустой директории
func TestScan_EmptyDirectory(t *testing.T) {
	mock := &MockWebdav{
		readDirFunc: func(path string) ([]os.FileInfo, error) {
			return []os.FileInfo{}, nil
		},
	}

	f := files.New(mock)
	conf := createTestConfig()
	files, err := f.Scan(conf, "/root")

	if err != nil {
		t.Fatalf("ожидали nil, получили %v", err)
	}

	if len(files) != 0 {
		t.Fatalf("ожидали 0 файлов, получили %d", len(files))
	}
}

// TestScan_Error проверяет обработку ошибок
func TestScan_Error(t *testing.T) {
	mock := &MockWebdav{
		readDirFunc: func(path string) ([]os.FileInfo, error) {
			return nil, errors.New("permission denied")
		},
	}

	f := files.New(mock)
	conf := createTestConfig()
	_, err := f.Scan(conf, "/root")

	if err == nil {
		t.Fatal("ожидали ошибку, получили nil")
	}
}

// TestScan_RecursiveError проверяет ошибку при рекурсивном сканировании
func TestScan_RecursiveError(t *testing.T) {
	callCount := 0
	mock := &MockWebdav{
		readDirFunc: func(path string) ([]os.FileInfo, error) {
			callCount++
			if path == "/root" {
				return []os.FileInfo{
					&MockFileInfo{name: "subdir", size: 0, isDir: true},
				}, nil
			}
			// Ошибка при сканировании подпапки
			return nil, errors.New("cannot read subdir")
		},
	}

	f := files.New(mock)
	conf := createTestConfig()
	_, err := f.Scan(conf, "/root")

	if err == nil {
		t.Fatal("ожидали ошибку при рекурсивном сканировании")
	}
}

// TestDownload проверяет загрузку части файла
func TestDownload(t *testing.T) {
	tmpDir := t.TempDir()
	destFile := filepath.Join(tmpDir, "downloaded.bin")

	testData := "Hello, World!"

	mock := &MockWebdav{
		readStreamRangeFunc: func(path string, offset int64, length int64) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(testData)), nil
		},
	}

	f := files.New(mock)
	part := engine.Part{
		ID:     "part1",
		Source: "/remote/file.bin",
		Dest:   destFile,
		Offset: 0,
		Size:   int64(len(testData)),
	}

	err := f.Download(part)
	if err != nil {
		t.Fatalf("ожидали nil, получили %v", err)
	}

	// Проверяем содержимое файла
	content, err := os.ReadFile(destFile)
	if err != nil {
		t.Fatalf("не удалось прочитать файл: %v", err)
	}

	if string(content) != testData {
		t.Fatalf("ожидали '%s', получили '%s'", testData, string(content))
	}
}

// TestDownload_WithOffset проверяет загрузку с смещением
func TestDownload_WithOffset(t *testing.T) {
	tmpDir := t.TempDir()
	destFile := filepath.Join(tmpDir, "downloaded.bin")

	testData := "World!"

	mock := &MockWebdav{
		readStreamRangeFunc: func(path string, offset int64, length int64) (io.ReadCloser, error) {
			if offset != 6 || length != 6 {
				t.Errorf("неверные параметры: offset=%d, length=%d", offset, length)
			}
			return io.NopCloser(strings.NewReader(testData)), nil
		},
	}

	f := files.New(mock)
	part := engine.Part{
		ID:     "part1",
		Source: "/remote/file.bin",
		Dest:   destFile,
		Offset: 6,
		Size:   6,
	}

	err := f.Download(part)
	if err != nil {
		t.Fatalf("ожидали nil, получили %v", err)
	}

	content, _ := os.ReadFile(destFile)
	if string(content) != testData {
		t.Fatalf("ожидали '%s', получили '%s'", testData, string(content))
	}
}

// TestDownload_StreamError проверяет обработку ошибок потока
func TestDownload_StreamError(t *testing.T) {
	tmpDir := t.TempDir()
	destFile := filepath.Join(tmpDir, "downloaded.bin")

	mock := &MockWebdav{
		readStreamRangeFunc: func(path string, offset int64, length int64) (io.ReadCloser, error) {
			return nil, errors.New("stream error")
		},
	}

	f := files.New(mock)
	part := engine.Part{
		ID:     "part1",
		Source: "/remote/file.bin",
		Dest:   destFile,
		Offset: 0,
		Size:   100,
	}

	err := f.Download(part)
	if err == nil {
		t.Fatal("ожидали ошибку, получили nil")
	}
}

// TestDownload_CreateFileError проверяет ошибку создания файла
func TestDownload_CreateFileError(t *testing.T) {
	mock := &MockWebdav{
		readStreamRangeFunc: func(path string, offset int64, length int64) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("data")), nil
		},
	}

	f := files.New(mock)
	part := engine.Part{
		ID:     "part1",
		Source: "/remote/file.bin",
		Dest:   "/invalid/path/that/does/not/exist/file.bin",
		Offset: 0,
		Size:   4,
	}

	err := f.Download(part)
	if err == nil {
		t.Fatal("ожидали ошибку при создании файла")
	}
}

// TestCollect проверяет сборку файла из частей
func TestCollect(t *testing.T) {
	tmpDir := t.TempDir()

	// Создаем части файла
	part1Path := filepath.Join(tmpDir, "part1")
	part2Path := filepath.Join(tmpDir, "part2")
	destPath := filepath.Join(tmpDir, "result")

	os.WriteFile(part1Path, []byte("Hello "), 0644)
	os.WriteFile(part2Path, []byte("World!"), 0644)

	mock := &MockWebdav{}

	f := files.New(mock)
	file := engine.File{
		Dest: destPath,
		Parts: []engine.Part{
			{ID: "part1", Dest: part1Path, Complete: true},
			{ID: "part2", Dest: part2Path, Complete: true},
		},
	}

	err := f.Collect(file)
	if err != nil {
		t.Fatalf("ожидали nil, получили %v", err)
	}

	// Проверяем содержимое
	content, _ := os.ReadFile(destPath)
	expected := "Hello World!"
	if string(content) != expected {
		t.Fatalf("ожидали '%s', получили '%s'", expected, string(content))
	}
}

// TestCollect_SinglePart проверяет сборку из одной части
func TestCollect_SinglePart(t *testing.T) {
	tmpDir := t.TempDir()

	partPath := filepath.Join(tmpDir, "part1")
	destPath := filepath.Join(tmpDir, "result")

	testData := "Single part"
	os.WriteFile(partPath, []byte(testData), 0644)

	mock := &MockWebdav{}
	f := files.New(mock)

	file := engine.File{
		Dest: destPath,
		Parts: []engine.Part{
			{ID: "part1", Dest: partPath, Complete: true},
		},
	}

	err := f.Collect(file)
	if err != nil {
		t.Fatalf("ожидали nil, получили %v", err)
	}

	content, _ := os.ReadFile(destPath)
	if string(content) != testData {
		t.Fatalf("ожидали '%s', получили '%s'", testData, string(content))
	}
}

// TestCollect_IncompleteFile проверяет обработку неполного файла
func TestCollect_IncompleteFile(t *testing.T) {
	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "result")

	mock := &MockWebdav{}

	f := files.New(mock)
	file := engine.File{
		Dest: destPath,
		Parts: []engine.Part{
			{ID: "part1", Complete: false},
		},
	}

	err := f.Collect(file)
	if err == nil {
		t.Fatal("ожидали ошибку для неполного файла")
	}

	if !strings.Contains(err.Error(), "не завершена") {
		t.Fatalf("неверное сообщение об ошибке: %v", err)
	}
}

// TestCollect_MissingPart проверяет обработку отсутствующей части
// func TestCollect_MissingPart(t *testing.T) {
// 	tmpDir := t.TempDir()
// 	destPath := filepath.Join(tmpDir, "result")

// 	mock := &MockWebdav{}

// 	f := files.New(mock)
// 	file := engine.File{
// 		Dest: destPath,
// 		Parts: []engine.Part{
// 			{ID: "part1", Dest: "/nonexistent/part", Complete: true},
// 		},
// 	}

// 	err := f.Collect(file)
// 	if err == nil {
// 		t.Fatal("ожидали ошибку для отсутствующей части")
// 	}
// }

// TestCollect_InvalidDestPath проверяет ошибку при создании результирующего файла
func TestCollect_InvalidDestPath(t *testing.T) {
	tmpDir := t.TempDir()
	partPath := filepath.Join(tmpDir, "part1")
	os.WriteFile(partPath, []byte("data"), 0644)

	mock := &MockWebdav{}

	f := files.New(mock)
	file := engine.File{
		Dest: "/invalid/path/result",
		Parts: []engine.Part{
			{ID: "part1", Dest: partPath, Complete: true},
		},
	}

	err := f.Collect(file)
	if err == nil {
		t.Fatal("ожидали ошибку при создании результирующего файла")
	}
}

// TestCheck_FileExists проверяет существование файла с правильным размером
func TestCheck_FileExists(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testData := "Hello"

	os.WriteFile(testFile, []byte(testData), 0644)

	mock := &MockWebdav{}
	f := files.New(mock)

	exists, err := f.Check(testFile, int64(len(testData)))
	if err != nil {
		t.Fatalf("ожидали nil, получили %v", err)
	}

	if !exists {
		t.Fatal("ожидали true, получили false")
	}
}

// TestCheck_FileSizeMismatch проверяет несовпадение размера
func TestCheck_FileSizeMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	os.WriteFile(testFile, []byte("Hello"), 0644)

	mock := &MockWebdav{}
	f := files.New(mock)

	exists, err := f.Check(testFile, 1000)
	if err != nil {
		t.Fatalf("ожидали nil, получили %v", err)
	}

	if exists {
		t.Fatal("ожидали false (размер не совпадает)")
	}
}

// TestCheck_FileNotExists проверяет несуществующий файл
func TestCheck_FileNotExists(t *testing.T) {
	mock := &MockWebdav{}
	f := files.New(mock)

	_, err := f.Check("/nonexistent/file", 100)
	assert.NoError(t, err)
}

// TestCheck_ZeroSize проверяет файл с нулевым размером
func TestCheck_ZeroSize(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "empty.txt")

	os.WriteFile(testFile, []byte(""), 0644)

	mock := &MockWebdav{}
	f := files.New(mock)

	exists, err := f.Check(testFile, 0)
	if err != nil {
		t.Fatalf("ожидали nil, получили %v", err)
	}

	if !exists {
		t.Fatal("ожидали true для файла с нулевым размером")
	}
}
