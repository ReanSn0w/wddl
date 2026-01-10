package queue_test

import (
	"context"
	"testing"
	"time"

	"github.com/ReanSn0w/wddl/pkg/engine"
	"github.com/ReanSn0w/wddl/pkg/queue"
	"github.com/go-pkgz/lgr"
)

func TestNew(t *testing.T) {
	// Создаем временный файл для тестирования
	tmpFile := t.TempDir() + "/test.db"

	tests := []struct {
		name      string
		path      string
		wantError bool
	}{
		{
			name:      "Successful creation",
			path:      tmpFile,
			wantError: false,
		},
		{
			name:      "Invalid path",
			path:      "/invalid/nonexistent/path/db.db",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := queue.New(tt.path)
			if (err != nil) != tt.wantError {
				t.Errorf("New() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if !tt.wantError && q == nil {
				t.Error("New() returned nil queue")
			}
		})
	}
}

func TestAdd(t *testing.T) {
	tmpFile := t.TempDir() + "/test.db"
	q, err := queue.New(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}

	tests := []struct {
		name      string
		file      engine.File
		wantError bool
	}{
		{
			name: "Add valid file",
			file: engine.File{
				ID:   "file1",
				Size: 1024,
			},
			wantError: false,
		},
		{
			name: "Add another file",
			file: engine.File{
				ID:   "file2",
				Size: 2048,
			},
			wantError: false,
		},
		{
			name: "Add file with same ID",
			file: engine.File{
				ID:   "file1",
				Size: 512,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := q.Add(tt.file)
			if (err != nil) != tt.wantError {
				t.Errorf("Add() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestExists(t *testing.T) {
	tmpFile := t.TempDir() + "/test.db"
	q, err := queue.New(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}

	file := engine.File{ID: "test_file", Size: 1024}
	err = q.Add(file)
	if err != nil {
		t.Fatalf("Failed to add file: %v", err)
	}

	tests := []struct {
		name      string
		id        string
		wantError bool
	}{
		{
			name:      "File exists",
			id:        "test_file",
			wantError: false,
		},
		{
			name:      "File does not exist",
			id:        "nonexistent",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := q.Exists(tt.id)
			if (err != nil) != tt.wantError {
				t.Errorf("Exists() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestLen(t *testing.T) {
	tmpFile := t.TempDir() + "/test.db"
	q, err := queue.New(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}

	// Пустая очередь
	len, err := q.Len()
	if err != nil {
		t.Errorf("Len() error = %v", err)
	}
	if len != 0 {
		t.Errorf("Len() = %d, want 0", len)
	}

	// Добавляем файлы
	files := []engine.File{
		{ID: "file1", Size: 1024},
		{ID: "file2", Size: 2048},
		{ID: "file3", Size: 512},
	}

	for _, file := range files {
		err = q.Add(file)
		if err != nil {
			t.Fatalf("Failed to add file: %v", err)
		}
	}

	// Проверяем длину
	len, err = q.Len()
	if err != nil {
		t.Errorf("Len() error = %v", err)
	}
	if len != 3 {
		t.Errorf("Len() = %d, want 3", len)
	}
}

func TestStat(t *testing.T) {
	tmpFile := t.TempDir() + "/test.db"
	q, err := queue.New(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}

	files := []engine.File{
		{ID: "file1", Size: 1024},
		{ID: "file2", Size: 2048},
		{ID: "file3", Size: 512},
	}

	for _, file := range files {
		err = q.Add(file)
		if err != nil {
			t.Fatalf("Failed to add file: %v", err)
		}
	}

	stat, err := q.Stat()
	if err != nil {
		t.Errorf("Stat() error = %v", err)
	}

	if stat.Files != 3 {
		t.Errorf("Stat() Files = %d, want 3", stat.Files)
	}

	expectedSize := int64(1024 + 2048 + 512)
	if stat.FullSize != expectedSize {
		t.Errorf("Stat() FullSize = %d, want %d", stat.FullSize, expectedSize)
	}
}

func TestList(t *testing.T) {
	tmpFile := t.TempDir() + "/test.db"
	q, err := queue.New(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}

	files := []engine.File{
		{ID: "file1", Size: 1024},
		{ID: "file2", Size: 2048},
		{ID: "file3", Size: 512},
	}

	for _, file := range files {
		err = q.Add(file)
		if err != nil {
			t.Fatalf("Failed to add file: %v", err)
		}
	}

	t.Run("List all files", func(t *testing.T) {
		result, err := q.List(nil)
		if err != nil {
			t.Errorf("List() error = %v", err)
		}
		if len(result) != 3 {
			t.Errorf("List() returned %d files, want 3", len(result))
		}
	})

	t.Run("List with filter", func(t *testing.T) {
		filter := func(f engine.File) error {
			if f.Size < 1000 {
				return engine.ErrNotFound
			}
			return nil
		}

		result, err := q.List(filter)
		if err != nil {
			t.Errorf("List() error = %v", err)
		}
		if len(result) != 2 {
			t.Errorf("List() returned %d files, want 2", len(result))
		}
	})
}

func TestDelete(t *testing.T) {
	tmpFile := t.TempDir() + "/test.db"
	q, err := queue.New(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}

	file := engine.File{ID: "test_file", Size: 1024}
	err = q.Add(file)
	if err != nil {
		t.Fatalf("Failed to add file: %v", err)
	}

	// Проверяем, что файл существует
	err = q.Exists("test_file")
	if err != nil {
		t.Errorf("File should exist before deletion")
	}

	// Удаляем
	err = q.Delete("test_file")
	if err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	// Проверяем, что файл удален
	err = q.Exists("test_file")
	if err == nil {
		t.Errorf("File should not exist after deletion")
	}

	// Удаляем несуществующий файл (не должно быть ошибки)
	err = q.Delete("nonexistent")
	if err != nil {
		t.Errorf("Delete() error = %v, want nil", err)
	}
}

func TestChan(t *testing.T) {
	tmpFile := t.TempDir() + "/test.db"
	q, err := queue.New(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}

	files := []engine.File{
		{ID: "file1", Size: 1024},
		{ID: "file2", Size: 2048},
	}

	// Добавляем файлы до подписки
	for _, file := range files {
		err = q.Add(file)
		if err != nil {
			t.Fatalf("Failed to add file: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logger := lgr.New()

	results := make([]engine.File, 0)
	resultsCh := q.Chan(ctx, logger, nil)

	// Собираем результаты в течение 5 секунд
	timeout := time.NewTimer(5 * time.Second)

	for {
		select {
		case file, ok := <-resultsCh:
			if !ok {
				return
			}
			results = append(results, file)
			if len(results) >= 2 {
				cancel()
			}
		case <-timeout.C:
			if len(results) < 2 {
				t.Logf("Chan() returned %d files in 5 seconds, expected at least 2", len(results))
			}

			return
		}
	}
}

func TestIntegration(t *testing.T) {
	tmpFile := t.TempDir() + "/test.db"
	q, err := queue.New(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}

	// Добавляем файлы
	files := []engine.File{
		{ID: "file1", Size: 1024},
		{ID: "file2", Size: 2048},
		{ID: "file3", Size: 512},
	}

	for _, file := range files {
		err = q.Add(file)
		if err != nil {
			t.Fatalf("Failed to add file: %v", err)
		}
	}

	// Проверяем статистику
	stat, err := q.Stat()
	if err != nil {
		t.Errorf("Stat() error = %v", err)
	}
	if stat.Files != 3 {
		t.Errorf("Expected 3 files, got %d", stat.Files)
	}

	// Удаляем один файл
	err = q.Delete("file2")
	if err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	// Проверяем, что осталось 2 файла
	l, err := q.Len()
	if err != nil {
		t.Errorf("Len() error = %v", err)
	}
	if l != 2 {
		t.Errorf("Expected 2 files after deletion, got %d", l)
	}

	// Проверяем список
	list, err := q.List(nil)
	if err != nil {
		t.Errorf("List() error = %v", err)
	}
	if len(list) != 2 {
		t.Errorf("Expected 2 files in list, got %d", len(list))
	}
}
