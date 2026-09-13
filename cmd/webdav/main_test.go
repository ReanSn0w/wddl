package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ReanSn0w/wddl/pkg/config"
	"github.com/umputun/go-flags"
)

func TestConfigPathPriority(t *testing.T) {
	tests := []struct {
		name string
		env  string
		args []string
		want string
	}{
		{name: "default", want: "./config.yaml"},
		{name: "environment", env: "/env/config.yaml", want: "/env/config.yaml"},
		{name: "CLI over environment", env: "/env/config.yaml", args: []string{"--config", "/cli/config.yaml"}, want: "/cli/config.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env == "" {
				old, existed := os.LookupEnv("WDDL_CONFIG")
				if err := os.Unsetenv("WDDL_CONFIG"); err != nil {
					t.Fatalf("Unsetenv() error = %v", err)
				}
				t.Cleanup(func() {
					if existed {
						_ = os.Setenv("WDDL_CONFIG", old)
					}
				})
			} else {
				t.Setenv("WDDL_CONFIG", tt.env)
			}
			var got bootstrapOptions
			parser := flags.NewParser(&got, flags.None)
			if _, err := parser.ParseArgs(tt.args); err != nil {
				t.Fatalf("ParseArgs() error = %v", err)
			}
			if got.ConfigPath != tt.want {
				t.Fatalf("ConfigPath = %q, want %q", got.ConfigPath, tt.want)
			}
		})
	}
}

func TestLoadCredentials(t *testing.T) {
	values := map[string]string{"WEBDAV_USER": "user", "WEBDAV_PASSWORD": "password"}
	getenv := func(key string) string { return values[key] }
	got, err := loadCredentials(getenv)
	if err != nil || got.User != "user" || got.Password != "password" {
		t.Fatalf("loadCredentials() = %#v, %v", got, err)
	}

	delete(values, "WEBDAV_USER")
	if _, err := loadCredentials(getenv); err == nil {
		t.Fatal("missing WEBDAV_USER error = nil")
	}
	values["WEBDAV_USER"] = "user"
	delete(values, "WEBDAV_PASSWORD")
	if _, err := loadCredentials(getenv); err == nil {
		t.Fatal("missing WEBDAV_PASSWORD error = nil")
	}
}

func TestToEngineConfig(t *testing.T) {
	conf := config.Config{
		WebDAV: config.WebDAV{Root: "/Sync"},
		Download: config.Download{
			Destination:  "/download",
			Temp:         "/temp",
			Workers:      7,
			ScanEvery:    config.Duration(42 * time.Minute),
			RemoveRemote: true,
		},
	}
	got := toEngineConfig(conf)
	if got.InputPath != "/Sync" || got.OutputPath != "/download" || got.TempPath != "/temp" || got.Concurrency != 7 || got.ScanEvery != 42*time.Minute || !got.RemoveRemote {
		t.Fatalf("toEngineConfig() = %#v", got)
	}
}

func TestValidateRoots(t *testing.T) {
	root := t.TempDir()
	if err := validateRoots([]string{root}); err != nil {
		t.Fatalf("validateRoots(valid) error = %v", err)
	}

	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := validateRoots([]string{file}); err == nil {
		t.Fatal("validateRoots(file) error = nil, want not-a-directory error")
	}
	if err := validateRoots([]string{filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("validateRoots(missing) error = nil, want error")
	}
}
