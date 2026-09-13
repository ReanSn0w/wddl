// Package daemon owns the long-lived downloader components and exposes their
// control plane. Client commands never construct these dependencies directly.
package daemon

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ReanSn0w/wddl/pkg/config"
	"github.com/ReanSn0w/wddl/pkg/control"
	"github.com/ReanSn0w/wddl/pkg/engine"
)

type localIndex interface {
	Refresh() (int, error)
	Len() int
}

type queueView interface {
	List(func(engine.File) error) ([]engine.File, error)
}

type logger interface {
	Logf(string, ...interface{})
}

type Daemon struct {
	mu       sync.RWMutex
	config   config.Config
	revision string
	started  time.Time
	stopping bool
	engine   *engine.Engine
	queue    queueView
	index    localIndex
	log      logger
	server   *control.SocketServer
}

func New(conf config.Config, revision string, log logger, downloader *engine.Engine, tasks queueView, index localIndex) *Daemon {
	return &Daemon{config: conf, revision: revision, log: log, engine: downloader, queue: tasks, index: index}
}

func (d *Daemon) Run(ctx context.Context) error {
	if len(d.config.ExistingFiles.Roots) > 0 {
		started := time.Now()
		d.log.Logf("[INFO] initial local library scan started")
		count, err := d.index.Refresh()
		if err != nil {
			return fmt.Errorf("initial local library scan: %w", err)
		}
		d.log.Logf("[INFO] initial local library scan completed: %d files in %v", count, time.Since(started).Round(time.Millisecond))
	}

	server, err := control.Listen(d.config.Control.Socket, control.NewHandler(d))
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.started = time.Now()
	d.server = server
	d.mu.Unlock()

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve() }()
	d.engine.Start(ctx)
	d.log.Logf("[INFO] control socket listening at %s", d.config.Control.Socket)

	select {
	case err := <-serveErr:
		return fmt.Errorf("control server: %w", err)
	case <-ctx.Done():
	}

	d.mu.Lock()
	d.stopping = true
	d.mu.Unlock()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), d.config.Control.ShutdownTimeout.Value())
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown control server: %w", err)
	}
	return nil
}

func (d *Daemon) Status() control.Status {
	d.mu.RLock()
	started, stopping := d.started, d.stopping
	d.mu.RUnlock()
	status := control.Status{SchemaVersion: 1, Revision: d.revision, StartedAt: started, ShuttingDown: stopping}
	if !started.IsZero() {
		status.Uptime = time.Since(started)
	}
	if d.index != nil {
		status.IndexedFiles = d.index.Len()
	}
	if d.queue != nil {
		items, _ := d.queue.List(nil)
		status.Pending = len(items)
	}
	return status
}

func unavailable(operation string) error {
	return &control.APIError{Status: http.StatusNotImplemented, Code: control.CodeConflict, Message: operation + " is not initialized"}
}

func (d *Daemon) Subscribe(context.Context) (<-chan control.Event, func()) {
	ch := make(chan control.Event)
	close(ch)
	return ch, func() {}
}
func (d *Daemon) TriggerScan(control.ScanKind) (control.ScanAccepted, error) {
	return control.ScanAccepted{}, unavailable("scan scheduler")
}
func (d *Daemon) QueueList() ([]control.QueueItem, error) { return nil, unavailable("queue control") }
func (d *Daemon) QueueRemove(string) (control.QueueItem, error) {
	return control.QueueItem{}, unavailable("queue control")
}
func (d *Daemon) QueueRetry(string) (control.QueueItem, error) {
	return control.QueueItem{}, unavailable("queue control")
}
func (d *Daemon) CancelDownload(string) (control.QueueItem, error) {
	return control.QueueItem{}, unavailable("download cancellation")
}
func (d *Daemon) ResolveRemoteID(string) (control.RemoteID, error) {
	return control.RemoteID{}, unavailable("remote ID lookup")
}
func (d *Daemon) CleanupPreview() (control.CleanupPreview, error) {
	return control.CleanupPreview{}, unavailable("remote cleanup")
}
func (d *Daemon) CleanupConfirm(string) (control.CleanupResult, error) {
	return control.CleanupResult{}, unavailable("remote cleanup")
}
func (d *Daemon) Reload(config.Config) (control.ReloadResult, error) {
	return control.ReloadResult{}, unavailable("configuration reload")
}

var _ control.Service = (*Daemon)(nil)
