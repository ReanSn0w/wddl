// Package config loads and validates wddl's versioned YAML configuration.
package config

import "time"

const CurrentVersion = 1

type Duration time.Duration

type Config struct {
	Version       int           `yaml:"version" json:"version"`
	WebDAV        WebDAV        `yaml:"webdav" json:"webdav"`
	Download      Download      `yaml:"download" json:"download"`
	Queue         Queue         `yaml:"queue" json:"queue"`
	ExistingFiles ExistingFiles `yaml:"existing_files" json:"existing_files"`
	Logging       Logging       `yaml:"logging" json:"logging"`
	Control       Control       `yaml:"control" json:"control"`
}

type WebDAV struct {
	Server string `yaml:"server" json:"server"`
	Root   string `yaml:"root" json:"root"`
}

type Download struct {
	Destination  string   `yaml:"destination" json:"destination"`
	Temp         string   `yaml:"temp" json:"temp"`
	Workers      int      `yaml:"workers" json:"workers"`
	ScanEvery    Duration `yaml:"scan_every" json:"scan_every"`
	RemoveRemote bool     `yaml:"remove_remote" json:"remove_remote"`
}

type Queue struct {
	File string `yaml:"file" json:"file"`
}

type ExistingFiles struct {
	Roots     []string `yaml:"roots" json:"roots"`
	ScanEvery Duration `yaml:"scan_every" json:"scan_every"`
}

type Logging struct {
	Debug bool `yaml:"debug" json:"debug"`
}

type Control struct {
	Socket          string   `yaml:"socket" json:"socket"`
	RequestTimeout  Duration `yaml:"request_timeout" json:"request_timeout"`
	ShutdownTimeout Duration `yaml:"shutdown_timeout" json:"shutdown_timeout"`
}

func (d Duration) Value() time.Duration {
	return time.Duration(d)
}
