package files

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ReanSn0w/wddl/pkg/engine"
)

func TestPartitionWriterReportsExactRateLimitedProgress(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	progress := make(chan engine.Progress, 4)
	file := engine.File{ID: "id", Name: "small.bin", Size: 10}
	writer := newPartitionWriteCloser(context.Background(), progress, &file, t.TempDir(), 0, 0, clock, time.Second)
	t.Cleanup(func() { _ = writer.Close() })

	if _, err := writer.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-progress:
		t.Fatalf("unexpected early progress: %#v", event)
	default:
	}

	now = now.Add(time.Second)
	if _, err := writer.Write([]byte("def")); err != nil {
		t.Fatal(err)
	}
	event := <-progress
	if event.Downloaded != 6 || event.Size != 10 || event.Percent != 60 || event.Speed != 6 {
		t.Fatalf("progress = %#v", event)
	}
}

func TestPartitionWriterFastForcedProgressDoesNotDivideByZero(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	progress := make(chan engine.Progress, 2)
	file := engine.File{ID: "id", Name: "fast.bin", Size: 4}
	writer := newPartitionWriteCloser(context.Background(), progress, &file, t.TempDir(), 0, 0, func() time.Time { return now }, time.Second)
	t.Cleanup(func() { _ = writer.Close() })

	if _, err := writer.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := writer.PublishProgress(true); err != nil {
		t.Fatal(err)
	}
	event := <-progress
	if event.Downloaded != 4 || event.Percent != 100 || event.Speed != 0 {
		t.Fatalf("progress = %#v", event)
	}
}

func TestNewProgressClampsInvalidValues(t *testing.T) {
	zero := newProgress(engine.File{ID: "zero", Size: 0}, 0, -1)
	if zero.Percent != 100 || zero.Downloaded != 0 || zero.Speed != 0 {
		t.Fatalf("zero progress = %#v", zero)
	}
	overflow := newProgress(engine.File{ID: "id", Size: 10}, 20, 1)
	if overflow.Percent != 100 || overflow.Downloaded != 10 {
		t.Fatalf("overflow progress = %#v", overflow)
	}
}

func TestRequiredProgressStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := publishProgress(ctx, make(chan engine.Progress), engine.Progress{}, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("publishProgress() error = %v, want context.Canceled", err)
	}
}

func TestCurrentStatCountsResumeAndFinalParts(t *testing.T) {
	tests := []struct {
		name     string
		size     int64
		parts    []int64
		wantDone int64
		wantSkip int64
		complete bool
	}{
		{name: "resume before partial final part", size: 2*partitionSize + 7, parts: []int64{partitionSize, partitionSize, 3}, wantDone: 2, wantSkip: 2 * partitionSize},
		{name: "complete with final partial part", size: partitionSize + 7, parts: []int64{partitionSize, 7}, wantDone: 2, complete: true},
		{name: "complete exact multiple", size: 2 * partitionSize, parts: []int64{partitionSize, partitionSize}, wantDone: 2, complete: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			temp := t.TempDir()
			for index, size := range tt.parts {
				path := filepath.Join(temp, string(rune('1'+index))+".part")
				part, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := part.Truncate(size); err != nil {
					part.Close()
					t.Fatal(err)
				}
				if err := part.Close(); err != nil {
					t.Fatal(err)
				}
			}
			file := engine.File{ID: "id", Name: "video.bin", Temp: temp, Size: tt.size}
			stat, err := (&Files{}).currentStat(file)
			if err != nil {
				t.Fatal(err)
			}
			if stat.Done != tt.wantDone || stat.SkipBytes != tt.wantSkip || stat.IsComplete() != tt.complete {
				t.Fatalf("stat = %#v", stat)
			}
		})
	}
}

func TestDownloadSmallFilePublishesInitialAndFinalProgress(t *testing.T) {
	content := []byte("small video")
	client := &memoryWebdav{content: content}
	storage := New(client)
	storage.progressEvery = time.Hour
	temp := t.TempDir()
	file := engine.File{
		ID: "small", Name: "small.bin", Source: "/small.bin",
		Temp: filepath.Join(temp, "parts"), Dest: filepath.Join(temp, "result", "small.bin"), Size: int64(len(content)),
	}
	progress := make(chan engine.Progress, 4)
	if err := storage.download(context.Background(), progress, file); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(file.Dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("downloaded content = %q", got)
	}
	initial, final := <-progress, <-progress
	if initial.Downloaded != 0 || initial.Percent != 0 {
		t.Fatalf("initial progress = %#v", initial)
	}
	if final.Downloaded != int64(len(content)) || final.Percent != 100 {
		t.Fatalf("final progress = %#v", final)
	}
}

func TestDownloadRetriesWithoutRealWaiting(t *testing.T) {
	content := []byte("retry video")
	client := &retryWebdav{content: content, failures: 1}
	storage := New(client)
	var sleeps []time.Duration
	storage.sleep = func(_ context.Context, duration time.Duration) error {
		sleeps = append(sleeps, duration)
		return nil
	}
	storage.progressEvery = time.Hour
	temp := t.TempDir()
	file := engine.File{
		ID: "retry", Name: "retry.bin", Source: "/retry.bin",
		Temp: filepath.Join(temp, "parts"), Dest: filepath.Join(temp, "result", "retry.bin"), Size: int64(len(content)),
	}
	if err := storage.Download(context.Background(), make(chan engine.Progress, 8), file); err != nil {
		t.Fatal(err)
	}
	if client.calls != 2 {
		t.Fatalf("ReadStreamRange calls = %d, want 2", client.calls)
	}
	if len(sleeps) != 2 || sleeps[0] != 3*time.Second || sleeps[1] != 2*time.Second {
		t.Fatalf("sleep durations = %v", sleeps)
	}
}

type memoryWebdav struct{ content []byte }

func (m *memoryWebdav) ReadDir(string) ([]os.FileInfo, error) { return nil, nil }
func (m *memoryWebdav) Remove(string) error                   { return nil }
func (m *memoryWebdav) ReadStreamRange(_ string, offset, length int64) (io.ReadCloser, error) {
	end := offset + length
	if end > int64(len(m.content)) {
		end = int64(len(m.content))
	}
	return io.NopCloser(bytes.NewReader(m.content[offset:end])), nil
}

type retryWebdav struct {
	content  []byte
	failures int
	calls    int
}

func (r *retryWebdav) ReadDir(string) ([]os.FileInfo, error) { return nil, nil }
func (r *retryWebdav) Remove(string) error                   { return nil }
func (r *retryWebdav) ReadStreamRange(_ string, offset, length int64) (io.ReadCloser, error) {
	r.calls++
	if r.calls <= r.failures {
		return nil, errors.New("temporary range error")
	}
	end := offset + length
	if end > int64(len(r.content)) {
		end = int64(len(r.content))
	}
	return io.NopCloser(bytes.NewReader(r.content[offset:end])), nil
}
