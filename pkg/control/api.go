package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ReanSn0w/wddl/pkg/config"
)

const maxRequestBody = 1 << 20

type Service interface {
	Status() Status
	Subscribe(context.Context) (<-chan Event, func())
	TriggerScan(ScanKind) (ScanAccepted, error)
	QueueList() ([]QueueItem, error)
	QueueRemove(string) (QueueItem, error)
	QueueRetry(string) (QueueItem, error)
	CancelDownload(string) (QueueItem, error)
	ResolveRemoteID(string) (RemoteID, error)
	CleanupPreview() (CleanupPreview, error)
	CleanupConfirm(string) (CleanupResult, error)
	Reload(config.Config) (ReloadResult, error)
}

type APIError struct {
	Status  int
	Code    ErrorCode
	Message string
	Matches []string
}

func (e *APIError) Error() string { return e.Message }

func NewHandler(service Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, _ *http.Request) {
		writeSuccess(w, http.StatusOK, service.Status())
	})
	mux.HandleFunc("GET /v1/watch", func(w http.ResponseWriter, r *http.Request) {
		watch(w, r, service)
	})
	mux.HandleFunc("POST /v1/scans/{kind}", func(w http.ResponseWriter, r *http.Request) {
		result, err := service.TriggerScan(ScanKind(r.PathValue("kind")))
		writeResult(w, http.StatusAccepted, result, err)
	})
	mux.HandleFunc("GET /v1/queue", func(w http.ResponseWriter, _ *http.Request) {
		result, err := service.QueueList()
		writeResult(w, http.StatusOK, result, err)
	})
	mux.HandleFunc("DELETE /v1/queue/{id}", func(w http.ResponseWriter, r *http.Request) {
		result, err := service.QueueRemove(r.PathValue("id"))
		writeResult(w, http.StatusOK, result, err)
	})
	mux.HandleFunc("POST /v1/queue/{id}/retry", func(w http.ResponseWriter, r *http.Request) {
		result, err := service.QueueRetry(r.PathValue("id"))
		writeResult(w, http.StatusOK, result, err)
	})
	mux.HandleFunc("POST /v1/downloads/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		result, err := service.CancelDownload(r.PathValue("id"))
		writeResult(w, http.StatusOK, result, err)
	})
	mux.HandleFunc("GET /v1/id", func(w http.ResponseWriter, r *http.Request) {
		result, err := service.ResolveRemoteID(r.URL.Query().Get("path"))
		writeResult(w, http.StatusOK, result, err)
	})
	mux.HandleFunc("POST /v1/cleanup/remote", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Confirm string `json:"confirm"`
		}
		if err := decodeBody(w, r, &request); err != nil {
			writeError(w, err)
			return
		}
		if request.Confirm == "" {
			result, err := service.CleanupPreview()
			writeResult(w, http.StatusOK, result, err)
			return
		}
		result, err := service.CleanupConfirm(request.Confirm)
		status := http.StatusOK
		if result.Failed > 0 {
			status = http.StatusMultiStatus
		}
		writeResult(w, status, result, err)
	})
	mux.HandleFunc("POST /v1/config/reload", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Config config.Config `json:"config"`
		}
		if err := decodeBody(w, r, &request); err != nil {
			writeError(w, err)
			return
		}
		result, err := service.Reload(request.Config)
		writeResult(w, http.StatusOK, result, err)
	})
	return mux
}

func decodeBody(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return &APIError{Status: http.StatusBadRequest, Code: CodeInvalidRequest, Message: fmt.Sprintf("invalid request body: %v", err)}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return &APIError{Status: http.StatusBadRequest, Code: CodeInvalidRequest, Message: "request body must contain one JSON value"}
	}
	return nil
}

func watch(w http.ResponseWriter, r *http.Request, service Service) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, &APIError{Status: http.StatusInternalServerError, Code: CodeInternal, Message: "streaming is unavailable"})
		return
	}
	events, unsubscribe := service.Subscribe(r.Context())
	defer unsubscribe()
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	encoder := json.NewEncoder(w)
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := encoder.Encode(event); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeResult(w http.ResponseWriter, status int, data any, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	writeSuccess(w, status, data)
}

func writeSuccess(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, Envelope{Version: APIVersion, Data: data})
}

func writeError(w http.ResponseWriter, err error) {
	apiErr := &APIError{Status: http.StatusInternalServerError, Code: CodeInternal, Message: "internal daemon error"}
	if !errors.As(err, &apiErr) {
		apiErr = &APIError{Status: http.StatusInternalServerError, Code: CodeInternal, Message: err.Error()}
	}
	writeJSON(w, apiErr.Status, Envelope{Version: APIVersion, Error: &Error{Code: apiErr.Code, Message: apiErr.Message, Matches: apiErr.Matches}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func ValidateScanKind(kind ScanKind) error {
	switch kind {
	case ScanRemote, ScanLocal, ScanAll:
		return nil
	default:
		return &APIError{Status: http.StatusBadRequest, Code: CodeInvalidRequest, Message: "scan kind must be remote, local, or all"}
	}
}

func IDPrefixError(prefix string, matches []string) error {
	if strings.TrimSpace(prefix) == "" {
		return &APIError{Status: http.StatusBadRequest, Code: CodeInvalidRequest, Message: "task ID is required"}
	}
	if len(matches) == 0 {
		return &APIError{Status: http.StatusNotFound, Code: CodeNotFound, Message: "task ID was not found"}
	}
	return &APIError{Status: http.StatusConflict, Code: CodeConflict, Message: "task ID prefix is ambiguous", Matches: matches}
}
