package control

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSocketLifecycle(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "run", "wddl.sock")
	server, err := Listen(path, http.NewServeMux())
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o660 {
		t.Fatalf("socket permissions = %o, want 660", got)
	}
	if _, err := Listen(path, http.NewServeMux()); err == nil || !strings.Contains(err.Error(), "already listening") {
		t.Fatalf("second Listen() error = %v", err)
	}
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("socket remains after shutdown: %v", err)
	}
}

func TestListenReplacesOnlyStaleSocket(t *testing.T) {
	root := shortTempDir(t)
	path := filepath.Join(root, "stale.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	server, err := Listen(path, http.NewServeMux())
	if err != nil {
		t.Fatalf("Listen(stale) error = %v", err)
	}
	_ = server.Shutdown(context.Background())

	regular := filepath.Join(root, "regular")
	if err := os.WriteFile(regular, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(regular, http.NewServeMux()); err == nil {
		t.Fatal("Listen(regular file) error = nil")
	}
	if got, err := os.ReadFile(regular); err != nil || string(got) != "keep" {
		t.Fatalf("regular file changed: %q, %v", got, err)
	}
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "wddl-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
