// Package control defines the public contract shared by the wddl daemon and
// its command-line clients. The wire format is versioned independently from
// the persistent queue format.
package control

import (
	"encoding/json"
	"time"
)

const APIVersion = "v1"

type ErrorCode string

const (
	CodeInvalidRequest ErrorCode = "invalid_request"
	CodeNotFound       ErrorCode = "not_found"
	CodeConflict       ErrorCode = "conflict"
	CodeUnavailable    ErrorCode = "daemon_unavailable"
	CodeInternal       ErrorCode = "internal_error"
	CodePartial        ErrorCode = "partial_failure"
)

type Error struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Matches []string  `json:"matches,omitempty"`
}

type Envelope struct {
	Version string `json:"version"`
	Data    any    `json:"data,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

type ScanKind string

const (
	ScanRemote ScanKind = "remote"
	ScanLocal  ScanKind = "local"
	ScanAll    ScanKind = "all"
)

type ScanState struct {
	Running   bool       `json:"running"`
	Scheduled bool       `json:"scheduled"`
	LastStart *time.Time `json:"last_start,omitempty"`
	LastEnd   *time.Time `json:"last_end,omitempty"`
	NextRun   *time.Time `json:"next_run,omitempty"`
	LastError string     `json:"last_error,omitempty"`
}

type ScanAccepted struct {
	Kind      ScanKind `json:"kind"`
	Scheduled bool     `json:"scheduled"`
	Message   string   `json:"message"`
}

type QueueItem struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Source string `json:"source"`
	Dest   string `json:"dest"`
	Size   int64  `json:"size"`
	State  string `json:"state"`
}

type ActiveDownload struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Size       int64         `json:"size"`
	Downloaded int64         `json:"downloaded"`
	Percent    float64       `json:"percent"`
	Speed      int64         `json:"speed_bytes_per_second"`
	ETA        time.Duration `json:"eta_nanoseconds,omitempty"`
}

type Status struct {
	SchemaVersion int               `json:"schema_version"`
	Revision      string            `json:"revision"`
	StartedAt     time.Time         `json:"started_at"`
	Uptime        time.Duration     `json:"uptime_nanoseconds"`
	ShuttingDown  bool              `json:"shutting_down"`
	Pending       int               `json:"pending"`
	Suspended     int               `json:"suspended"`
	Active        []ActiveDownload  `json:"active"`
	RemoteScan    ScanState         `json:"remote_scan"`
	LocalScan     ScanState         `json:"local_scan"`
	IndexedFiles  int               `json:"indexed_files"`
	Errors        map[string]string `json:"errors,omitempty"`
	LastReload    *ReloadResult     `json:"last_reload,omitempty"`
}

type Event struct {
	Time    time.Time `json:"time"`
	Type    string    `json:"type"`
	ID      string    `json:"id,omitempty"`
	Message string    `json:"message"`
	Data    any       `json:"data,omitempty"`
	Dropped uint64    `json:"dropped,omitempty"`
}

// DownloadProgress is the stable payload of a download.progress event. The
// capitalized JSON field names preserve the v1 wire format that was originally
// produced by engine.Progress without explicit JSON tags.
type DownloadProgress struct {
	ID         string  `json:"ID"`
	Name       string  `json:"Name"`
	Percent    float64 `json:"Percent"`
	Speed      int64   `json:"Speed"`
	Downloaded int64   `json:"Downloaded,omitempty"`
	Size       int64   `json:"Size,omitempty"`
}

// UnmarshalJSON keeps arbitrary event payloads generic while making the
// download.progress payload directly useful to human renderers.
func (e *Event) UnmarshalJSON(data []byte) error {
	var wire struct {
		Time    time.Time       `json:"time"`
		Type    string          `json:"type"`
		ID      string          `json:"id,omitempty"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data,omitempty"`
		Dropped uint64          `json:"dropped,omitempty"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

	e.Time = wire.Time
	e.Type = wire.Type
	e.ID = wire.ID
	e.Message = wire.Message
	e.Dropped = wire.Dropped
	e.Data = nil
	if len(wire.Data) == 0 || string(wire.Data) == "null" {
		return nil
	}

	if wire.Type == "download.progress" {
		var progress DownloadProgress
		if err := json.Unmarshal(wire.Data, &progress); err != nil {
			return err
		}
		e.Data = progress
		return nil
	}

	return json.Unmarshal(wire.Data, &e.Data)
}

type RemoteID struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	ID   string `json:"id"`
}

type CleanupCandidate struct {
	Path      string `json:"path"`
	LocalPath string `json:"local_path"`
	Size      int64  `json:"size"`
}

type CleanupPreview struct {
	Token     string             `json:"token"`
	ExpiresAt time.Time          `json:"expires_at"`
	Files     []CleanupCandidate `json:"files"`
	Count     int                `json:"count"`
	TotalSize int64              `json:"total_size"`
}

type CleanupResult struct {
	Deleted int      `json:"deleted"`
	Skipped int      `json:"skipped"`
	Failed  int      `json:"failed"`
	Errors  []string `json:"errors,omitempty"`
}

type ReloadResult struct {
	At            time.Time `json:"at"`
	Applied       bool      `json:"applied"`
	RestartFields []string  `json:"restart_fields,omitempty"`
	Message       string    `json:"message"`
}

type Version struct {
	Revision  string `json:"revision"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
}
