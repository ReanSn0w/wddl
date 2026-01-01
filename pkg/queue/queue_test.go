package queue_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ReanSn0w/wddl/pkg/engine"
	"github.com/ReanSn0w/wddl/pkg/queue"
)

// Helper функция для создания временной БД
func setupTestQueue(t *testing.T) (*queue.Queue, string) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	q, err := queue.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}

	return q, dbPath
}

func TestNew(t *testing.T) {
	q, dbPath := setupTestQueue(t)
	defer os.RemoveAll(filepath.Dir(dbPath))

	if q == nil {
		t.Error("Expected queue to be created, got nil")
	}

	if q.Client() == nil {
		t.Error("Expected q.q to be initialized")
	}
}

func TestClient(t *testing.T) {
	q, dbPath := setupTestQueue(t)
	defer os.RemoveAll(filepath.Dir(dbPath))

	client := q.Client()
	if client == nil {
		t.Error("Expected client to not be nil")
	}
}

func TestUpsert(t *testing.T) {
	q, dbPath := setupTestQueue(t)
	defer os.RemoveAll(filepath.Dir(dbPath))

	f := engine.File{
		ID:    "test-id",
		Name:  "test.txt",
		Size:  100,
		Parts: []engine.Part{{ID: "part1", Complete: false}},
	}

	ctx := context.Background()
	err := q.Upsert(ctx, f)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
}

func TestGet(t *testing.T) {
	q, dbPath := setupTestQueue(t)
	defer os.RemoveAll(filepath.Dir(dbPath))

	ctx := context.Background()

	// Вставляем файл
	original := engine.File{
		ID:    "test-id-1",
		Name:  "test.txt",
		Size:  100,
		Parts: []engine.Part{{ID: "part1", Complete: false}},
	}

	err := q.Upsert(ctx, original)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Получаем файл
	retrieved, err := q.Get(ctx, "test-id-1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.ID != original.ID {
		t.Errorf("Expected ID %s, got %s", original.ID, retrieved.ID)
	}

	if retrieved.Name != original.Name {
		t.Errorf("Expected Name %s, got %s", original.Name, retrieved.Name)
	}
}

func TestGetNotFound(t *testing.T) {
	q, dbPath := setupTestQueue(t)
	defer os.RemoveAll(filepath.Dir(dbPath))

	ctx := context.Background()
	_, err := q.Get(ctx, "non-existent-id")

	if err == nil {
		t.Error("Expected error for non-existent file")
	}

	if err != engine.ErrNotFound {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

func TestList(t *testing.T) {
	q, dbPath := setupTestQueue(t)
	defer os.RemoveAll(filepath.Dir(dbPath))

	ctx := context.Background()

	// Вставляем несколько файлов
	files := []engine.File{
		{ID: "id-1", Name: "file1.txt", Size: 100},
		{ID: "id-2", Name: "file2.txt", Size: 200},
		{ID: "id-3", Name: "file3.txt", Size: 300},
	}

	for _, f := range files {
		err := q.Upsert(ctx, f)
		if err != nil {
			t.Fatalf("Upsert failed: %v", err)
		}
	}

	// Получаем все файлы
	result, err := q.List(ctx, 10, func(f engine.File) bool {
		return true
	})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(result) != 3 {
		t.Errorf("Expected 3 files, got %d", len(result))
	}
}

func TestListWithLimit(t *testing.T) {
	q, dbPath := setupTestQueue(t)
	defer os.RemoveAll(filepath.Dir(dbPath))

	ctx := context.Background()

	// Вставляем файлы
	for i := 1; i <= 5; i++ {
		f := engine.File{
			ID:   fmt.Sprintf("id-%d", i),
			Name: fmt.Sprintf("id-%d.txt", i),
			Size: int64(i * 100),
		}
		err := q.Upsert(ctx, f)
		if err != nil {
			t.Fatalf("Upsert failed: %v", err)
		}
	}

	// Получаем с лимитом
	result, err := q.List(ctx, 2, func(f engine.File) bool {
		return true
	})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 files with limit, got %d", len(result))
	}
}

func TestListWithFilter(t *testing.T) {
	q, dbPath := setupTestQueue(t)
	defer os.RemoveAll(filepath.Dir(dbPath))

	ctx := context.Background()

	// Вставляем файлы
	files := []engine.File{
		{ID: "id-1", Name: "small.txt", Size: 50},
		{ID: "id-2", Name: "medium.txt", Size: 500},
		{ID: "id-3", Name: "large.txt", Size: 1000},
	}

	for _, f := range files {
		err := q.Upsert(ctx, f)
		if err != nil {
			t.Fatalf("Upsert failed: %v", err)
		}
	}

	// Фильтруем файлы больше 100 байт
	result, err := q.List(ctx, 10, func(f engine.File) bool {
		return f.Size > 100
	})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 files after filter, got %d", len(result))
	}
}

func TestMarkPartComplete(t *testing.T) {
	q, dbPath := setupTestQueue(t)
	defer os.RemoveAll(filepath.Dir(dbPath))

	ctx := context.Background()

	// Вставляем файл с частями
	f := engine.File{
		ID:   "test-id",
		Name: "test.txt",
		Size: 300,
		Parts: []engine.Part{
			{ID: "part1", Complete: false},
			{ID: "part2", Complete: false},
			{ID: "part3", Complete: false},
		},
		CompleteParts: 0,
	}

	err := q.Upsert(ctx, f)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Отмечаем часть как завершенную
	err = q.MarkPartComplete(ctx, "test-id", "part2")
	if err != nil {
		t.Fatalf("MarkPartComplete failed: %v", err)
	}

	// Проверяем результат
	updated, err := q.Get(ctx, "test-id")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if updated.CompleteParts != 1 {
		t.Errorf("Expected 1 complete part, got %d", updated.CompleteParts)
	}

	if !updated.Parts[1].Complete {
		t.Error("Expected part2 to be marked as complete")
	}

	if updated.Parts[0].Complete || updated.Parts[2].Complete {
		t.Error("Other parts should not be marked as complete")
	}
}

func TestMarkPartCompleteNotFound(t *testing.T) {
	q, dbPath := setupTestQueue(t)
	defer os.RemoveAll(filepath.Dir(dbPath))

	ctx := context.Background()

	err := q.MarkPartComplete(ctx, "non-existent-id", "part1")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}

	if err != engine.ErrNotFound {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

func TestDelete(t *testing.T) {
	q, dbPath := setupTestQueue(t)
	defer os.RemoveAll(filepath.Dir(dbPath))

	ctx := context.Background()

	// Вставляем файл
	f := engine.File{
		ID:   "test-id",
		Name: "test.txt",
		Size: 100,
	}

	err := q.Upsert(ctx, f)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Удаляем
	err = q.Delete(ctx, "test-id")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Проверяем, что файл удален
	_, err = q.Get(ctx, "test-id")
	if err != engine.ErrNotFound {
		t.Error("Expected file to be deleted")
	}
}

func TestUpsertUpdate(t *testing.T) {
	q, dbPath := setupTestQueue(t)
	defer os.RemoveAll(filepath.Dir(dbPath))

	ctx := context.Background()

	// Вставляем файл
	f1 := engine.File{
		ID:   "test-id",
		Name: "test1.txt",
		Size: 100,
	}

	err := q.Upsert(ctx, f1)
	if err != nil {
		t.Fatalf("First upsert failed: %v", err)
	}

	// Обновляем
	f2 := engine.File{
		ID:   "test-id",
		Name: "test2.txt",
		Size: 200,
	}

	err = q.Upsert(ctx, f2)
	if err != nil {
		t.Fatalf("Second upsert failed: %v", err)
	}

	// Проверяем обновление
	retrieved, err := q.Get(ctx, "test-id")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.Name != "test2.txt" {
		t.Errorf("Expected name test2.txt, got %s", retrieved.Name)
	}

	if retrieved.Size != 200 {
		t.Errorf("Expected size 200, got %d", retrieved.Size)
	}
}
