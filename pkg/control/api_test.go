package control

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ReanSn0w/wddl/pkg/config"
)

type stubService struct{ status Status }

func (s *stubService) Status() Status { return s.status }
func (s *stubService) Subscribe(ctx context.Context) (<-chan Event, func()) {
	ch := make(chan Event)
	return ch, func() { close(ch) }
}
func (s *stubService) TriggerScan(kind ScanKind) (ScanAccepted, error) {
	if err := ValidateScanKind(kind); err != nil {
		return ScanAccepted{}, err
	}
	return ScanAccepted{Kind: kind, Scheduled: true}, nil
}
func (s *stubService) QueueList() ([]QueueItem, error) {
	return []QueueItem{{ID: "abc", State: "ready"}}, nil
}
func (s *stubService) QueueRemove(string) (QueueItem, error)        { return QueueItem{}, nil }
func (s *stubService) QueueRetry(string) (QueueItem, error)         { return QueueItem{}, nil }
func (s *stubService) CancelDownload(string) (QueueItem, error)     { return QueueItem{}, nil }
func (s *stubService) ResolveRemoteID(string) (RemoteID, error)     { return RemoteID{}, nil }
func (s *stubService) CleanupPreview() (CleanupPreview, error)      { return CleanupPreview{}, nil }
func (s *stubService) CleanupConfirm(string) (CleanupResult, error) { return CleanupResult{}, nil }
func (s *stubService) Reload(config.Config) (ReloadResult, error) {
	return ReloadResult{Applied: true}, nil
}

func TestAPIEnvelopeAndValidationError(t *testing.T) {
	handler := NewHandler(&stubService{status: Status{SchemaVersion: 1, Revision: "test"}})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"version":"v1"`) || !strings.Contains(response.Body.String(), `"revision":"test"`) {
		t.Fatalf("status response = %d %s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/scans/wrong", nil))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_request"`) {
		t.Fatalf("error response = %d %s", response.Code, response.Body.String())
	}
}

func TestAPILimitsRequestBody(t *testing.T) {
	handler := NewHandler(&stubService{})
	body := strings.NewReader(`{"confirm":"` + strings.Repeat("x", maxRequestBody) + `"}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/cleanup/remote", body))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestClientServerOverUnixSocket(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "control.sock")
	server, err := Listen(path, NewHandler(&stubService{status: Status{SchemaVersion: 1, Revision: "unix"}}))
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve() }()
	client := NewClient(path, time.Second)
	status, err := client.Status(context.Background())
	if err != nil || status.Revision != "unix" {
		t.Fatalf("Status() = %#v, %v", status, err)
	}
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-serveDone; err != nil {
		t.Fatal(err)
	}
}
