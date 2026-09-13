package config

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

func (c *Config) Validate() error {
	if c.Version != CurrentVersion {
		return fmt.Errorf("version: unsupported value %d (want %d)", c.Version, CurrentVersion)
	}

	c.WebDAV.Server = strings.TrimSpace(c.WebDAV.Server)
	server, err := url.Parse(c.WebDAV.Server)
	if err != nil || server.Host == "" || (server.Scheme != "http" && server.Scheme != "https") {
		return fmt.Errorf("webdav.server: must be a non-empty HTTP(S) URL")
	}
	if server.User != nil {
		return fmt.Errorf("webdav.server: credentials in URL are not allowed")
	}

	c.WebDAV.Root = strings.TrimSpace(c.WebDAV.Root)
	if c.WebDAV.Root == "" || !path.IsAbs(c.WebDAV.Root) || strings.Contains(c.WebDAV.Root, `\`) {
		return fmt.Errorf("webdav.root: must be an absolute path using '/' separators")
	}
	c.WebDAV.Root = path.Clean(c.WebDAV.Root)

	if err := cleanLocalPath("download.destination", &c.Download.Destination); err != nil {
		return err
	}
	if err := cleanLocalPath("download.temp", &c.Download.Temp); err != nil {
		return err
	}
	if c.Download.Destination == c.Download.Temp {
		return fmt.Errorf("download.destination and download.temp: must be different paths")
	}
	if c.Download.Workers <= 0 {
		return fmt.Errorf("download.workers: must be positive")
	}
	if c.Download.ScanEvery.Value() <= 0 {
		return fmt.Errorf("download.scan_every: must be positive")
	}

	if err := cleanLocalPath("queue.file", &c.Queue.File); err != nil {
		return err
	}
	if c.ExistingFiles.ScanEvery.Value() <= 0 {
		return fmt.Errorf("existing_files.scan_every: must be positive")
	}
	c.ExistingFiles.Roots = normalizeLocalPaths(c.ExistingFiles.Roots)

	return nil
}

func cleanLocalPath(field string, value *string) error {
	*value = strings.TrimSpace(*value)
	if *value == "" {
		return fmt.Errorf("%s: must not be empty", field)
	}
	*value = filepath.Clean(*value)
	return nil
}

func normalizeLocalPaths(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		value = filepath.Clean(value)
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
