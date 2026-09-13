// Package daemon owns the long-lived downloader components and exposes their
// control plane. Client commands never construct these dependencies directly.
package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ReanSn0w/wddl/pkg/config"
	"github.com/ReanSn0w/wddl/pkg/control"
	"github.com/ReanSn0w/wddl/pkg/engine"
	"github.com/go-pkgz/lgr"
)

type localIndex interface {
	Refresh() (int, error)
	Len() int
	Find(string, int64) (string, error)
}

type queueView interface {
	List(func(engine.File) error) ([]engine.File, error)
	Delete(string) error
	SetState(string, engine.TaskState) (engine.File, error)
}

type logger interface {
	Logf(string, ...interface{})
}

type remoteStore interface {
	Stat(string) (os.FileInfo, error)
	ReadDir(string) ([]os.FileInfo, error)
	Remove(string) error
}

type Daemon struct {
	mu         sync.RWMutex
	config     config.Config
	revision   string
	started    time.Time
	stopping   bool
	engine     *engine.Engine
	queue      queueView
	index      localIndex
	log        logger
	server     *control.SocketServer
	remoteCH   chan struct{}
	localCH    chan struct{}
	allCH      chan struct{}
	remoteMu   sync.Mutex
	localMu    sync.Mutex
	remote     control.ScanState
	local      control.ScanState
	broker     *control.Broker
	remoteFS   remoteStore
	cleanupMu  sync.Mutex
	previews   map[string]cleanupSnapshot
	deleteMu   *sync.Mutex
	reloadCH   chan scheduleReload
	lastReload *control.ReloadResult
}

type scheduleReload struct {
	remote time.Duration
	local  time.Duration
	done   chan struct{}
}

type cleanupSnapshot struct {
	expires time.Time
	files   []cleanupFile
}

type cleanupFile struct {
	file      engine.File
	localPath string
}

func New(conf config.Config, revision string, log logger, downloader *engine.Engine, tasks queueView, index localIndex, remote remoteStore) *Daemon {
	deleteMu := &sync.Mutex{}
	daemon := &Daemon{
		config: conf, revision: revision, log: log, engine: downloader, queue: tasks, index: index,
		remoteCH: make(chan struct{}, 1), localCH: make(chan struct{}, 1), allCH: make(chan struct{}, 1),
		broker:   control.NewBroker(),
		remoteFS: remote,
		previews: make(map[string]cleanupSnapshot),
		deleteMu: deleteMu,
		reloadCH: make(chan scheduleReload),
	}
	downloader.SetEventSink(func(eventType string, file engine.File, data any) {
		if progress, ok := data.(engine.Progress); ok {
			data = control.DownloadProgress{
				ID: progress.ID, Name: progress.Name,
				Percent: progress.Percent, Speed: progress.Speed,
				Downloaded: progress.Downloaded, Size: progress.Size,
			}
		}
		daemon.broker.Publish(control.Event{Type: eventType, ID: file.ID, Message: file.Name, Data: data})
	})
	downloader.SetDeleteMutex(deleteMu)
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
	if err := d.engine.Wait(shutdownCtx); err != nil {
		return fmt.Errorf("wait for active downloads: %w", err)
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
	status.LastReload = d.lastReload
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
	remoteEvery, localEvery := d.config.Download.ScanEvery.Value(), d.config.ExistingFiles.ScanEvery.Value()
	remoteTicker := time.NewTicker(remoteEvery)
	localTicker := time.NewTicker(localEvery)
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
		case reload := <-d.reloadCH:
			remoteTicker.Stop()
			localTicker.Stop()
			remoteEvery, localEvery = reload.remote, reload.local
			remoteTicker, localTicker = time.NewTicker(remoteEvery), time.NewTicker(localEvery)
			rn, ln := time.Now().Add(remoteEvery), time.Now().Add(localEvery)
			d.setNext(control.ScanRemote, rn)
			d.setNext(control.ScanLocal, ln)
			close(reload.done)
		case <-remoteTicker.C:
			d.markScheduled(control.ScanRemote)
			go d.runRemoteScan()
			next := time.Now().Add(remoteEvery)
			d.setNext(control.ScanRemote, next)
		case <-localTicker.C:
			d.markScheduled(control.ScanLocal)
			go d.runLocalScan()
			next := time.Now().Add(localEvery)
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
func (d *Daemon) ResolveRemoteID(remotePath string) (control.RemoteID, error) {
	normalized, err := normalizeRemotePath(d.config.WebDAV.Root, remotePath)
	if err != nil {
		return control.RemoteID{}, err
	}
	info, err := d.remoteFS.Stat(normalized)
	if err != nil {
		if os.IsNotExist(err) {
			return control.RemoteID{}, &control.APIError{Status: http.StatusNotFound, Code: control.CodeNotFound, Message: "remote path was not found"}
		}
		return control.RemoteID{}, fmt.Errorf("stat remote path: %w", err)
	}
	if info.IsDir() {
		return control.RemoteID{}, &control.APIError{Status: http.StatusBadRequest, Code: control.CodeInvalidRequest, Message: "remote path is a directory"}
	}
	file := engine.NewFile(engine.Config{InputPath: d.config.WebDAV.Root, OutputPath: d.config.Download.Destination, TempPath: d.config.Download.Temp}, normalized, info.Size())
	return control.RemoteID{Type: "remote_file", Path: normalized, Name: info.Name(), Size: info.Size(), ID: file.ID}, nil
}
func (d *Daemon) CleanupPreview() (control.CleanupPreview, error) {
	d.cleanupMu.Lock()
	defer d.cleanupMu.Unlock()
	files, err := d.cleanupCandidates(d.config.WebDAV.Root)
	if err != nil {
		return control.CleanupPreview{}, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].file.Source < files[j].file.Source })
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return control.CleanupPreview{}, fmt.Errorf("create cleanup token: %w", err)
	}
	token, expires := hex.EncodeToString(bytes), time.Now().Add(10*time.Minute)
	d.previews[token] = cleanupSnapshot{expires: expires, files: files}
	result := control.CleanupPreview{Token: token, ExpiresAt: expires, Count: len(files)}
	for _, candidate := range files {
		result.Files = append(result.Files, control.CleanupCandidate{Path: candidate.file.Source, LocalPath: candidate.localPath, Size: candidate.file.Size})
		result.TotalSize += candidate.file.Size
	}
	d.broker.Publish(control.Event{Type: "cleanup.preview", Message: fmt.Sprintf("%d remote files confirmed locally", result.Count), Data: result})
	return result, nil
}
func (d *Daemon) CleanupConfirm(token string) (control.CleanupResult, error) {
	d.cleanupMu.Lock()
	defer d.cleanupMu.Unlock()
	snapshot, ok := d.previews[token]
	delete(d.previews, token)
	if !ok || time.Now().After(snapshot.expires) {
		return control.CleanupResult{}, &control.APIError{Status: http.StatusConflict, Code: control.CodeConflict, Message: "cleanup token is invalid or expired"}
	}
	d.deleteMu.Lock()
	defer d.deleteMu.Unlock()
	result := control.CleanupResult{}
	for _, candidate := range snapshot.files {
		localPath, err := engine.FindLocalCopy(candidate.file, d.index)
		if err != nil {
			if errors.Is(err, engine.ErrLocalFileNotFound) {
				result.Skipped++
				continue
			}
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: revalidate local copy: %v", candidate.file.Source, err))
			continue
		}
		if localPath == "" {
			result.Skipped++
			continue
		}
		info, err := d.remoteFS.Stat(candidate.file.Source)
		if err != nil || info.IsDir() || info.Name() != candidate.file.Name || info.Size() != candidate.file.Size {
			result.Skipped++
			continue
		}
		if err := d.remoteFS.Remove(candidate.file.Source); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", candidate.file.Source, err))
			continue
		}
		result.Deleted++
	}
	d.broker.Publish(control.Event{Type: "cleanup.completed", Message: fmt.Sprintf("deleted %d, skipped %d, failed %d", result.Deleted, result.Skipped, result.Failed), Data: result})
	return result, nil
}
func (d *Daemon) Reload(next config.Config) (control.ReloadResult, error) {
	result := control.ReloadResult{At: time.Now()}
	if err := next.Validate(); err != nil {
		result.Message = err.Error()
		d.recordReload(result)
		return result, &control.APIError{Status: http.StatusBadRequest, Code: control.CodeInvalidRequest, Message: err.Error()}
	}
	d.mu.RLock()
	current := d.config
	d.mu.RUnlock()
	result.RestartFields = immutableChanges(current, next)
	if len(result.RestartFields) > 0 {
		result.Message = "configuration contains fields that require a restart"
		d.recordReload(result)
		return result, &control.APIError{Status: http.StatusConflict, Code: control.CodeConflict, Message: result.Message, Matches: result.RestartFields}
	}
	reload := scheduleReload{remote: next.Download.ScanEvery.Value(), local: next.ExistingFiles.ScanEvery.Value(), done: make(chan struct{})}
	timeout := time.NewTimer(current.Control.RequestTimeout.Value())
	defer timeout.Stop()
	select {
	case d.reloadCH <- reload:
	case <-timeout.C:
		return result, &control.APIError{Status: http.StatusServiceUnavailable, Code: control.CodeUnavailable, Message: "scheduler is unavailable"}
	}
	select {
	case <-reload.done:
	case <-timeout.C:
		return result, &control.APIError{Status: http.StatusServiceUnavailable, Code: control.CodeUnavailable, Message: "scheduler reload timed out"}
	}
	configureRuntimeLogger(next.Logging.Debug)
	result.Applied, result.Message = true, "configuration reloaded"
	d.mu.Lock()
	d.config = next
	d.lastReload = &result
	d.mu.Unlock()
	d.broker.Publish(control.Event{Type: "config.reloaded", Message: result.Message, Data: result})
	return result, nil
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

func normalizeRemotePath(root, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, `\`) {
		return "", &control.APIError{Status: http.StatusBadRequest, Code: control.CodeInvalidRequest, Message: "a WebDAV path using '/' separators is required"}
	}
	root = path.Clean(root)
	result := path.Clean(value)
	if !path.IsAbs(value) {
		result = path.Join(root, value)
	}
	if result != root && !strings.HasPrefix(result, strings.TrimSuffix(root, "/")+"/") {
		return "", &control.APIError{Status: http.StatusBadRequest, Code: control.CodeInvalidRequest, Message: "remote path is outside webdav.root"}
	}
	return result, nil
}

func (d *Daemon) cleanupCandidates(dir string) ([]cleanupFile, error) {
	items, err := d.remoteFS.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("scan remote cleanup candidates: %w", err)
	}
	var result []cleanupFile
	for _, info := range items {
		remotePath := path.Join(dir, info.Name())
		if info.IsDir() {
			nested, err := d.cleanupCandidates(remotePath)
			if err != nil {
				return nil, err
			}
			result = append(result, nested...)
			continue
		}
		file := engine.NewFile(engine.Config{InputPath: d.config.WebDAV.Root, OutputPath: d.config.Download.Destination, TempPath: d.config.Download.Temp}, remotePath, info.Size())
		localPath, err := engine.FindLocalCopy(file, d.index)
		if errors.Is(err, engine.ErrLocalFileNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("find local copy for %s: %w", remotePath, err)
		}
		result = append(result, cleanupFile{file: file, localPath: localPath})
	}
	return result, nil
}

func (d *Daemon) recordReload(result control.ReloadResult) {
	d.mu.Lock()
	d.lastReload = &result
	d.mu.Unlock()
}

func immutableChanges(current, next config.Config) []string {
	var fields []string
	if current.Version != next.Version {
		fields = append(fields, "version")
	}
	if current.WebDAV != next.WebDAV {
		fields = append(fields, "webdav")
	}
	if current.Download.Destination != next.Download.Destination {
		fields = append(fields, "download.destination")
	}
	if current.Download.Temp != next.Download.Temp {
		fields = append(fields, "download.temp")
	}
	if current.Download.Workers != next.Download.Workers {
		fields = append(fields, "download.workers")
	}
	if current.Download.RemoveRemote != next.Download.RemoveRemote {
		fields = append(fields, "download.remove_remote")
	}
	if current.Queue != next.Queue {
		fields = append(fields, "queue.file")
	}
	if !reflect.DeepEqual(current.ExistingFiles.Roots, next.ExistingFiles.Roots) {
		fields = append(fields, "existing_files.roots")
	}
	if current.Control != next.Control {
		fields = append(fields, "control")
	}
	return fields
}

func configureRuntimeLogger(debug bool) {
	options := []lgr.Option{lgr.Msec, lgr.LevelBraces}
	if debug {
		options = append(options, lgr.Debug, lgr.CallerFile, lgr.CallerFunc)
	}
	lgr.Setup(options...)
}
