// Package config loads and validates wddl's versioned YAML configuration.
package config

import "time"

const CurrentVersion = 1

type Duration time.Duration

type Config struct {
	Version       int           `yaml:"version"`
	WebDAV        WebDAV        `yaml:"webdav"`
	Download      Download      `yaml:"download"`
	Queue         Queue         `yaml:"queue"`
	ExistingFiles ExistingFiles `yaml:"existing_files"`
	Logging       Logging       `yaml:"logging"`
}

type WebDAV struct {
	Server string `yaml:"server"`
	Root   string `yaml:"root"`
}

type Download struct {
	Destination  string   `yaml:"destination"`
	Temp         string   `yaml:"temp"`
	Workers      int      `yaml:"workers"`
	ScanEvery    Duration `yaml:"scan_every"`
	RemoveRemote bool     `yaml:"remove_remote"`
}

type Queue struct {
	File string `yaml:"file"`
}

type ExistingFiles struct {
	Roots     []string `yaml:"roots"`
	ScanEvery Duration `yaml:"scan_every"`
}

type Logging struct {
	Debug bool `yaml:"debug"`
}

func (d Duration) Value() time.Duration {
	return time.Duration(d)
}
