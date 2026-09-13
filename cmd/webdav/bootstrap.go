package main

import (
	"errors"

	"github.com/ReanSn0w/wddl/pkg/config"
	"github.com/ReanSn0w/wddl/pkg/engine"
)

type bootstrapOptions struct {
	ConfigPath string `long:"config" env:"WDDL_CONFIG" default:"./config.yaml" description:"path to YAML configuration"`

	Util struct {
		ClearRemote bool `long:"clear-remote" env:"CLEAR_REMOTE" description:"clear remote files"`
	} `group:"Утилиты" namespace:"util" env-namespace:"UTIL"`
}

type credentials struct {
	User     string
	Password string
}

func loadCredentials(getenv func(string) string) (credentials, error) {
	result := credentials{
		User:     getenv("WEBDAV_USER"),
		Password: getenv("WEBDAV_PASSWORD"),
	}
	if result.User == "" {
		return credentials{}, errors.New("WEBDAV_USER is required")
	}
	if result.Password == "" {
		return credentials{}, errors.New("WEBDAV_PASSWORD is required")
	}
	return result, nil
}

func toEngineConfig(conf config.Config) engine.Config {
	return engine.Config{
		InputPath:    conf.WebDAV.Root,
		OutputPath:   conf.Download.Destination,
		TempPath:     conf.Download.Temp,
		Concurrency:  conf.Download.Workers,
		ScanEvery:    conf.Download.ScanEvery.Value(),
		RemoveRemote: conf.Download.RemoveRemote,
	}
}
