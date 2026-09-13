// Package daemon owns the long-lived downloader components and exposes their
// control plane. Client commands never construct these dependencies directly.
package daemon

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
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
	Delete(string) error
	SetState(string, engine.TaskState) (engine.File, error)
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
	remoteCH chan struct{}
	localCH  chan struct{}
	allCH    chan struct{}
	remoteMu sync.Mutex
	localMu  sync.Mutex
	remote   control.ScanState
	local    control.ScanState
	broker   *control.Broker
}

func New(conf config.Config, revision string, log logger, downloader *engine.Engine, tasks queueView, index localIndex) *Daemon {
	daemon := &Daemon{
		config: conf, revision: revision, log: log, engine: downloader, queue: tasks, index: index,
		remoteCH: make(chan struct{}, 1), localCH: make(chan struct{}, 1), allCH: make(chan struct{}, 1),
		broker: control.NewBroker(),
	}
	downloader.SetEventSink(func(eventType string, file engine.File, data any) {
		daemon.broker.Publish(control.Event{Type: eventType, ID: file.ID, Message: file.Name, Data: data})
	})
	return daemon
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
	// The first remote scan follows the initial local snapshot and happens
	// before workers can consume newly discovered tasks.
	d.runRemoteScan()

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
	go d.runScheduler(ctx)
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
		for _, item := range items {
			if item.State == engine.TaskSuspended {
				status.Suspended++
			} else {
				status.Pending++
			}
		}
	}
	for _, active := range d.engine.ActiveDownloads() {
		item := control.ActiveDownload{
			ID: active.ID, Name: active.Name, Size: active.Size, Downloaded: active.Downloaded,
			Percent: active.Percent, Speed: active.Speed,
		}
		if active.Speed > 0 && active.Downloaded < active.Size {
			item.ETA = time.Duration((active.Size-active.Downloaded)/active.Speed) * time.Second
		}
		status.Active = append(status.Active, item)
	}
	status.Pending -= len(status.Active)
	if status.Pending < 0 {
		status.Pending = 0
	}
	d.mu.RLock()
	status.RemoteScan = d.remote
	status.LocalScan = d.local
	status.Errors = map[string]string{}
	if d.remote.LastError != "" {
		status.Errors["remote_scan"] = d.remote.LastError
	}
	if d.local.LastError != "" {
		status.Errors["local_scan"] = d.local.LastError
	}
	d.mu.RUnlock()
	return status
}

func unavailable(operation string) error {
	return &control.APIError{Status: http.StatusNotImplemented, Code: control.CodeConflict, Message: operation + " is not initialized"}
}

func (d *Daemon) Subscribe(ctx context.Context) (<-chan control.Event, func()) {
	return d.broker.Subscribe(ctx)
}
func (d *Daemon) TriggerScan(kind control.ScanKind) (control.ScanAccepted, error) {
	if err := control.ValidateScanKind(kind); err != nil {
		return control.ScanAccepted{}, err
	}
	d.mu.Lock()
	already := false
	switch kind {
	case control.ScanRemote:
		already = d.remote.Running || d.remote.Scheduled
		if !already {
			d.remote.Scheduled = true
		}
	case control.ScanLocal:
		already = d.local.Running || d.local.Scheduled
		if !already {
			d.local.Scheduled = true
		}
	case control.ScanAll:
		already = d.local.Running || d.local.Scheduled || d.remote.Running || d.remote.Scheduled
		if !already {
			d.local.Scheduled, d.remote.Scheduled = true, true
		}
	}
	d.mu.Unlock()
	if !already {
		var ch chan struct{}
		switch kind {
		case control.ScanRemote:
			ch = d.remoteCH
		case control.ScanLocal:
			ch = d.localCH
		default:
			ch = d.allCH
		}
		select {
		case ch <- struct{}{}:
		default:
			already = true
		}
	}
	message := "scan scheduled"
	if already {
		message = "scan already scheduled or running"
	}
	return control.ScanAccepted{Kind: kind, Scheduled: !already, Message: message}, nil
}

func (d *Daemon) runScheduler(ctx context.Context) {
	remoteTicker := time.NewTicker(d.config.Download.ScanEvery.Value())
	localTicker := time.NewTicker(d.config.ExistingFiles.ScanEvery.Value())
	defer remoteTicker.Stop()
	defer localTicker.Stop()
	d.mu.Lock()
	rn, ln := time.Now().Add(d.config.Download.ScanEvery.Value()), time.Now().Add(d.config.ExistingFiles.ScanEvery.Value())
	d.remote.NextRun, d.local.NextRun = &rn, &ln
	d.mu.Unlock()
	for {
		select {
		case <-ctx.Done():
			return
		case <-remoteTicker.C:
			d.markScheduled(control.ScanRemote)
			go d.runRemoteScan()
			next := time.Now().Add(d.config.Download.ScanEvery.Value())
			d.setNext(control.ScanRemote, next)
		case <-localTicker.C:
			d.markScheduled(control.ScanLocal)
			go d.runLocalScan()
			next := time.Now().Add(d.config.ExistingFiles.ScanEvery.Value())
			d.setNext(control.ScanLocal, next)
		case <-d.remoteCH:
			go d.runRemoteScan()
		case <-d.localCH:
			go d.runLocalScan()
		case <-d.allCH:
			go func() { d.runLocalScan(); d.runRemoteScan() }()
		}
	}
}

func (d *Daemon) runRemoteScan() {
	if !d.remoteMu.TryLock() {
		return
	}
	defer d.remoteMu.Unlock()
	d.scanStarted(control.ScanRemote)
	err := d.engine.ScanNow()
	d.scanFinished(control.ScanRemote, err)
}

func (d *Daemon) runLocalScan() {
	if !d.localMu.TryLock() {
		return
	}
	defer d.localMu.Unlock()
	d.scanStarted(control.ScanLocal)
	_, err := d.index.Refresh()
	d.scanFinished(control.ScanLocal, err)
}

func (d *Daemon) markScheduled(kind control.ScanKind) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if kind == control.ScanRemote {
		d.remote.Scheduled = true
	} else {
		d.local.Scheduled = true
	}
}

func (d *Daemon) setNext(kind control.ScanKind, next time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if kind == control.ScanRemote {
		d.remote.NextRun = &next
	} else {
		d.local.NextRun = &next
	}
}

func (d *Daemon) scanStarted(kind control.ScanKind) {
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	state := &d.local
	if kind == control.ScanRemote {
		state = &d.remote
	}
	state.Scheduled, state.Running, state.LastStart = false, true, &now
	d.broker.Publish(control.Event{Type: "scan.started", Message: string(kind) + " scan started", Data: map[string]any{"kind": kind}})
}

func (d *Daemon) scanFinished(kind control.ScanKind, err error) {
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	state := &d.local
	if kind == control.ScanRemote {
		state = &d.remote
	}
	state.Running, state.LastEnd, state.LastError = false, &now, ""
	if err != nil {
		state.LastError = err.Error()
		d.log.Logf("[ERROR] %s scan failed: %v", kind, err)
	} else {
		d.log.Logf("[INFO] %s scan completed", kind)
	}
	event := control.Event{Type: "scan.completed", Message: string(kind) + " scan completed", Data: map[string]any{"kind": kind}}
	if err != nil {
		event.Type = "scan.failed"
		event.Message = string(kind) + " scan failed"
		event.Data = map[string]any{"kind": kind, "error": err.Error()}
	}
	d.broker.Publish(event)
}
func (d *Daemon) QueueList() ([]control.QueueItem, error) {
	files, err := d.queue.List(nil)
	if err != nil {
		return nil, err
	}
	result := make([]control.QueueItem, 0, len(files))
	for _, file := range files {
		result = append(result, queueItem(file))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}
func (d *Daemon) QueueRemove(prefix string) (control.QueueItem, error) {
	file, err := d.resolveTask(prefix)
	if err != nil {
		return control.QueueItem{}, err
	}
	for _, active := range d.engine.ActiveDownloads() {
		if active.ID == file.ID {
			return control.QueueItem{}, &control.APIError{Status: http.StatusConflict, Code: control.CodeConflict, Message: "task is active; cancel the download first"}
		}
	}
	if err := d.queue.Delete(file.ID); err != nil {
		return control.QueueItem{}, err
	}
	d.broker.Publish(control.Event{Type: "queue.removed", ID: file.ID, Message: "task removed; a remote scan may add it again"})
	return queueItem(file), nil
}
func (d *Daemon) QueueRetry(prefix string) (control.QueueItem, error) {
	file, err := d.resolveTask(prefix)
	if err != nil {
		return control.QueueItem{}, err
	}
	if file.State != engine.TaskSuspended {
		return control.QueueItem{}, &control.APIError{Status: http.StatusConflict, Code: control.CodeConflict, Message: "task is not suspended"}
	}
	file, err = d.queue.SetState(file.ID, engine.TaskReady)
	if err != nil {
		return control.QueueItem{}, err
	}
	d.broker.Publish(control.Event{Type: "download.resumed", ID: file.ID, Message: file.Name})
	return queueItem(file), nil
}
func (d *Daemon) CancelDownload(prefix string) (control.QueueItem, error) {
	file, err := d.resolveTask(prefix)
	if err != nil {
		return control.QueueItem{}, err
	}
	if !d.engine.CancelDownload(file.ID) {
		return control.QueueItem{}, &control.APIError{Status: http.StatusConflict, Code: control.CodeConflict, Message: "task is not actively downloading"}
	}
	file, err = d.queue.SetState(file.ID, engine.TaskSuspended)
	if err != nil {
		return control.QueueItem{}, err
	}
	return queueItem(file), nil
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

func (d *Daemon) resolveTask(prefix string) (engine.File, error) {
	prefix = strings.TrimSpace(prefix)
	files, err := d.queue.List(nil)
	if err != nil {
		return engine.File{}, err
	}
	var matches []engine.File
	var ids []string
	for _, file := range files {
		if strings.HasPrefix(file.ID, prefix) {
			matches = append(matches, file)
			ids = append(ids, file.ID)
		}
	}
	if len(matches) != 1 {
		return engine.File{}, control.IDPrefixError(prefix, ids)
	}
	return matches[0], nil
}

func queueItem(file engine.File) control.QueueItem {
	return control.QueueItem{ID: file.ID, Name: file.Name, Source: file.Source, Dest: file.Dest, Size: file.Size, State: string(file.State)}
}
