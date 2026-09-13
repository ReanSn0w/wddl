package control

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type SocketServer struct {
	path     string
	listener *net.UnixListener
	http     *http.Server
}

func Listen(socketPath string, handler http.Handler) (*SocketServer, error) {
	if !filepath.IsAbs(socketPath) {
		return nil, errors.New("socket path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o750); err != nil {
		return nil, fmt.Errorf("create socket directory: %w", err)
	}
	if err := prepareSocket(socketPath); err != nil {
		return nil, err
	}

	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("listen on unix socket: %w", err)
	}
	if err := os.Chmod(socketPath, 0o660); err != nil {
		listener.Close()
		_ = os.Remove(socketPath)
		return nil, fmt.Errorf("set socket permissions: %w", err)
	}

	return &SocketServer{
		path:     socketPath,
		listener: listener,
		http: &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       35 * time.Second,
			WriteTimeout:      35 * time.Second,
			IdleTimeout:       60 * time.Second,
		},
	}, nil
}

func prepareSocket(socketPath string) error {
	info, err := os.Lstat(socketPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect socket path: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("socket path %q exists and is not a socket", socketPath)
	}

	conn, dialErr := net.DialTimeout("unix", socketPath, 250*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		return fmt.Errorf("another daemon is already listening on %q", socketPath)
	}
	if err := os.Remove(socketPath); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}
	return nil
}

func (s *SocketServer) Serve() error {
	err := s.http.Serve(s.listener)
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func (s *SocketServer) Shutdown(ctx context.Context) error {
	// Closing the listener first prevents new commands from entering while
	// in-flight handlers receive their graceful shutdown window.
	_ = s.listener.Close()
	err := s.http.Shutdown(ctx)
	removeErr := os.Remove(s.path)
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		removeErr = fmt.Errorf("remove socket: %w", removeErr)
	} else {
		removeErr = nil
	}
	return errors.Join(err, removeErr)
}
