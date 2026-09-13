package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadFullConfiguration(t *testing.T) {
	path := writeConfig(t, `
version: 1
webdav:
  server: https://dav.example.test/root
  root: /Sync
download:
  destination: /data/download
  temp: /data/tmp
  workers: 8
  scan_every: 15m
  remove_remote: true
queue:
  file: /state/queue.json
existing_files:
  roots: [/library/a, /library/b]
  scan_every: 12h
logging:
  debug: true
`)

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Version != 1 || got.WebDAV.Server != "https://dav.example.test/root" || got.WebDAV.Root != "/Sync" {
		t.Fatalf("WebDAV config = %#v", got.WebDAV)
	}
	if got.Download.Destination != "/data/download" || got.Download.Temp != "/data/tmp" || got.Download.Workers != 8 || got.Download.ScanEvery.Value() != 15*time.Minute || !got.Download.RemoveRemote {
		t.Fatalf("Download config = %#v", got.Download)
	}
	if got.Queue.File != "/state/queue.json" {
		t.Fatalf("Queue config = %#v", got.Queue)
	}
	if !reflect.DeepEqual(got.ExistingFiles.Roots, []string{"/library/a", "/library/b"}) || got.ExistingFiles.ScanEvery.Value() != 12*time.Hour {
		t.Fatalf("ExistingFiles config = %#v", got.ExistingFiles)
	}
	if !got.Logging.Debug {
		t.Fatal("Logging.Debug = false, want true")
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	got, err := Load(writeConfig(t, "version: 1\nwebdav:\n  server: https://dav.example.test\n"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got.WebDAV.Root != "/" || got.Download.Destination != "./download" || got.Download.Temp != "/tmp/wddl" {
		t.Fatalf("path defaults = %#v, %#v", got.WebDAV, got.Download)
	}
	if got.Download.Workers != 4 || got.Download.ScanEvery.Value() != 10*time.Minute || got.Download.RemoveRemote {
		t.Fatalf("download defaults = %#v", got.Download)
	}
	if got.Queue.File != "./queue.json" || len(got.ExistingFiles.Roots) != 0 || got.ExistingFiles.ScanEvery.Value() != 24*time.Hour || got.Logging.Debug {
		t.Fatalf("remaining defaults = queue %#v existing %#v logging %#v", got.Queue, got.ExistingFiles, got.Logging)
	}
}

func TestLoadRejectsInvalidDocuments(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "empty", content: "", want: "file is empty"},
		{name: "malformed", content: "version: [", want: "decode config"},
		{name: "unknown field", content: validYAML() + "unknown: true\n", want: "field unknown not found"},
		{name: "credential field", content: strings.Replace(validYAML(), "  root: /Sync\n", "  root: /Sync\n  user: secret\n", 1), want: "field user not found"},
		{name: "unknown version", content: strings.Replace(validYAML(), "version: 1", "version: 2", 1), want: "version"},
		{name: "missing version", content: strings.Replace(validYAML(), "version: 1\n", "", 1), want: "version"},
		{name: "invalid duration", content: strings.Replace(validYAML(), "scan_every: 10m", "scan_every: tomorrow", 1), want: "invalid duration"},
		{name: "multiple documents", content: validYAML() + "---\nversion: 1\n", want: "multiple YAML documents"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tt.content))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestValidateRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{name: "empty server", mutate: func(c *Config) { c.WebDAV.Server = "" }, want: "webdav.server"},
		{name: "non-http server", mutate: func(c *Config) { c.WebDAV.Server = "ftp://example.test" }, want: "webdav.server"},
		{name: "relative WebDAV root", mutate: func(c *Config) { c.WebDAV.Root = "Sync" }, want: "webdav.root"},
		{name: "backslash WebDAV root", mutate: func(c *Config) { c.WebDAV.Root = `/Sync\\files` }, want: "webdav.root"},
		{name: "empty destination", mutate: func(c *Config) { c.Download.Destination = " " }, want: "download.destination"},
		{name: "empty temp", mutate: func(c *Config) { c.Download.Temp = "" }, want: "download.temp"},
		{name: "same destination and temp", mutate: func(c *Config) { c.Download.Destination = "./data"; c.Download.Temp = "data" }, want: "must be different"},
		{name: "zero workers", mutate: func(c *Config) { c.Download.Workers = 0 }, want: "download.workers"},
		{name: "zero remote interval", mutate: func(c *Config) { c.Download.ScanEvery = 0 }, want: "download.scan_every"},
		{name: "negative remote interval", mutate: func(c *Config) { c.Download.ScanEvery = Duration(-time.Second) }, want: "download.scan_every"},
		{name: "zero local interval", mutate: func(c *Config) { c.ExistingFiles.ScanEvery = 0 }, want: "existing_files.scan_every"},
		{name: "negative local interval", mutate: func(c *Config) { c.ExistingFiles.ScanEvery = Duration(-time.Second) }, want: "existing_files.scan_every"},
		{name: "empty queue", mutate: func(c *Config) { c.Queue.File = "" }, want: "queue.file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := validConfig()
			tt.mutate(&conf)
			if err := conf.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestValidateNormalizesExistingRoots(t *testing.T) {
	conf := validConfig()
	conf.ExistingFiles.Roots = []string{" /library/a ", "", "/library/b/../b", "/library/a"}
	if err := conf.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	want := []string{"/library/a", "/library/b"}
	if !reflect.DeepEqual(conf.ExistingFiles.Roots, want) {
		t.Fatalf("roots = %#v, want %#v", conf.ExistingFiles.Roots, want)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load() error = %v, want os.ErrNotExist", err)
	}
}

func validConfig() Config {
	conf := Defaults()
	conf.Version = CurrentVersion
	conf.WebDAV.Server = "https://dav.example.test"
	conf.WebDAV.Root = "/Sync"
	return conf
}

func validYAML() string {
	return "version: 1\nwebdav:\n  server: https://dav.example.test\n  root: /Sync\ndownload:\n  scan_every: 10m\nexisting_files:\n  scan_every: 24h\n"
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
