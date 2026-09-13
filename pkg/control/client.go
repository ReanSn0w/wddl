package control

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/ReanSn0w/wddl/pkg/config"
)

type Client struct {
	http *http.Client
}

func NewClient(socketPath string, timeout time.Duration) *Client {
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, "unix", socketPath)
	}}
	return &Client{http: &http.Client{Transport: transport, Timeout: timeout}}
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	var result Status
	err := c.do(ctx, http.MethodGet, "/v1/status", nil, &result)
	return result, err
}

func (c *Client) Scan(ctx context.Context, kind ScanKind) (ScanAccepted, error) {
	var result ScanAccepted
	err := c.do(ctx, http.MethodPost, "/v1/scans/"+string(kind), struct{}{}, &result)
	return result, err
}

func (c *Client) Queue(ctx context.Context) ([]QueueItem, error) {
	var result []QueueItem
	err := c.do(ctx, http.MethodGet, "/v1/queue", nil, &result)
	return result, err
}

func (c *Client) QueueRemove(ctx context.Context, id string) (QueueItem, error) {
	var result QueueItem
	err := c.do(ctx, http.MethodDelete, "/v1/queue/"+url.PathEscape(id), nil, &result)
	return result, err
}

func (c *Client) QueueRetry(ctx context.Context, id string) (QueueItem, error) {
	var result QueueItem
	err := c.do(ctx, http.MethodPost, "/v1/queue/"+url.PathEscape(id)+"/retry", struct{}{}, &result)
	return result, err
}

func (c *Client) Cancel(ctx context.Context, id string) (QueueItem, error) {
	var result QueueItem
	err := c.do(ctx, http.MethodPost, "/v1/downloads/"+url.PathEscape(id)+"/cancel", struct{}{}, &result)
	return result, err
}

func (c *Client) RemoteID(ctx context.Context, path string) (RemoteID, error) {
	var result RemoteID
	err := c.do(ctx, http.MethodGet, "/v1/id?path="+url.QueryEscape(path), nil, &result)
	return result, err
}

func (c *Client) Cleanup(ctx context.Context, confirm string) (any, error) {
	if confirm == "" {
		var result CleanupPreview
		err := c.do(ctx, http.MethodPost, "/v1/cleanup/remote", map[string]string{"confirm": ""}, &result)
		return result, err
	}
	var result CleanupResult
	err := c.do(ctx, http.MethodPost, "/v1/cleanup/remote", map[string]string{"confirm": confirm}, &result)
	return result, err
}

func (c *Client) Reload(ctx context.Context, conf config.Config) (ReloadResult, error) {
	var result ReloadResult
	err := c.do(ctx, http.MethodPost, "/v1/config/reload", map[string]any{"config": conf}, &result)
	return result, err
}

func (c *Client) Watch(ctx context.Context, consume func(Event) error) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://wddl/v1/watch", nil)
	if err != nil {
		return err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return decodeAPIError(response)
	}
	scanner := bufio.NewScanner(response.Body)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, maxRequestBody)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("decode watch event: %w", err)
		}
		if err := consume(event); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (c *Client) do(ctx context.Context, method, path string, body, target any) error {
	var input io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://wddl"+path, input)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return &APIError{Status: http.StatusServiceUnavailable, Code: CodeUnavailable, Message: fmt.Sprintf("connect to daemon: %v", err)}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeAPIError(response)
	}
	var raw struct {
		Version string          `json:"version"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&raw); err != nil {
		return fmt.Errorf("decode daemon response: %w", err)
	}
	if raw.Version != APIVersion {
		return fmt.Errorf("unsupported daemon API version %q", raw.Version)
	}
	if target != nil {
		if err := json.Unmarshal(raw.Data, target); err != nil {
			return fmt.Errorf("decode daemon result: %w", err)
		}
	}
	return nil
}

func decodeAPIError(response *http.Response) error {
	var envelope Envelope
	if err := json.NewDecoder(io.LimitReader(response.Body, maxRequestBody)).Decode(&envelope); err != nil || envelope.Error == nil {
		return &APIError{Status: response.StatusCode, Code: CodeInternal, Message: response.Status}
	}
	return &APIError{Status: response.StatusCode, Code: envelope.Error.Code, Message: envelope.Error.Message, Matches: envelope.Error.Matches}
}
