package control

import (
	"encoding/json"
	"testing"
)

func TestDownloadProgressEventKeepsV1WireFields(t *testing.T) {
	wire := []byte(`{"type":"download.progress","id":"abc","message":"movie.mkv","data":{"ID":"abc","Name":"movie.mkv","Percent":12.5,"Speed":4096,"Downloaded":128,"Size":1024}}`)

	var event Event
	if err := json.Unmarshal(wire, &event); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	progress, ok := event.Data.(DownloadProgress)
	if !ok {
		t.Fatalf("Data type = %T, want DownloadProgress", event.Data)
	}
	if progress.ID != "abc" || progress.Name != "movie.mkv" || progress.Percent != 12.5 || progress.Speed != 4096 || progress.Downloaded != 128 || progress.Size != 1024 {
		t.Fatalf("progress = %#v", progress)
	}

	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("decode encoded event: %v", err)
	}
	data, ok := got["data"].(map[string]any)
	if !ok {
		t.Fatalf("encoded data = %#v", got["data"])
	}
	for _, key := range []string{"ID", "Name", "Percent", "Speed", "Downloaded", "Size"} {
		if _, ok := data[key]; !ok {
			t.Errorf("encoded progress misses %q: %#v", key, data)
		}
	}
}

func TestOtherEventPayloadRemainsGeneric(t *testing.T) {
	var event Event
	if err := json.Unmarshal([]byte(`{"type":"scan.completed","message":"done","data":{"kind":"remote"}}`), &event); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	payload, ok := event.Data.(map[string]any)
	if !ok || payload["kind"] != "remote" {
		t.Fatalf("Data = %#v", event.Data)
	}
}
