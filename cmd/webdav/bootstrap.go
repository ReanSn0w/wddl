package main

import (
	"errors"
	"strings"

	"github.com/ReanSn0w/wddl/pkg/config"
	"github.com/ReanSn0w/wddl/pkg/engine"
	"github.com/umputun/go-flags"
)

type outputOptions struct {
	JSON bool `long:"json" description:"print machine-readable JSON"`
}

type watchCommand struct {
	outputOptions
	Plain bool `long:"plain" description:"print one text line per event without terminal control sequences"`
}

type emptyCommand struct{}

type idCommand struct {
	outputOptions
	Args struct {
		RemotePath string `positional-arg-name:"remote-path" required:"yes"`
	} `positional-args:"yes"`
}

type itemCommand struct {
	outputOptions
	Args struct {
		ID string `positional-arg-name:"id" required:"yes"`
	} `positional-args:"yes"`
}

type cleanupRemoteCommand struct {
	outputOptions
	Confirm string `long:"confirm" value-name:"token" description:"confirm a previous preview with its one-time token"`
}

type cliOptions struct {
	ConfigPath string `long:"config" env:"WDDL_CONFIG" default:"./config.yaml" description:"path to YAML configuration"`

	Run    emptyCommand  `command:"run" description:"run the downloader daemon"`
	Status outputOptions `command:"status" description:"show a snapshot of daemon state"`
	Watch  watchCommand  `command:"watch" description:"show live daemon activity until interrupted"`
	Scan   struct {
		Remote outputOptions `command:"remote" description:"schedule a remote WebDAV scan"`
		Local  outputOptions `command:"local" description:"schedule a local library scan"`
		All    outputOptions `command:"all" description:"schedule local then remote scans"`
	} `command:"scan" description:"schedule scanning before its periodic deadline"`
	Queue struct {
		List   outputOptions `command:"list" description:"list queued download tasks"`
		Remove itemCommand   `command:"remove" description:"remove a non-active task without deleting data"`
		Retry  itemCommand   `command:"retry" description:"resume a suspended task"`
	} `command:"queue" description:"inspect and change the persistent task queue"`
	Download struct {
		Cancel itemCommand `command:"cancel" description:"cancel an active download and preserve completed parts"`
	} `command:"download" description:"control active downloads"`
	ID      idCommand `command:"id" description:"resolve a remote WebDAV path to its stable task ID"`
	Cleanup struct {
		Remote cleanupRemoteCommand `command:"remote" description:"preview or confirm safe remote deletion"`
	} `command:"cleanup" description:"perform explicitly confirmed maintenance"`
	Config struct {
		Validate outputOptions `command:"validate" description:"validate YAML locally without daemon credentials"`
		Reload   outputOptions `command:"reload" description:"reload supported settings in the running daemon"`
	} `command:"config" description:"validate or reload configuration"`
	Version outputOptions `command:"version" description:"print build and platform information"`
}

type parsedCLI struct {
	Options cliOptions
	Command string
}

func parseCLI(args []string) (parsedCLI, error) {
	var opts cliOptions
	parser := flags.NewParser(&opts, flags.Default)
	parser.Name = "wddl"
	parser.LongDescription = "Download-only WebDAV daemon controlled over a local Unix socket.\n\nExamples:\n  wddl run\n  wddl status --json\n  wddl watch\n  wddl watch --plain\n  wddl scan all\n  wddl cleanup remote"
	if _, err := parser.ParseArgs(args); err != nil {
		return parsedCLI{}, err
	}

	var names []string
	for command := parser.Command.Active; command != nil; command = command.Active {
		names = append(names, command.Name)
	}
	if len(names) == 0 {
		return parsedCLI{}, &flags.Error{Type: flags.ErrCommandRequired, Message: "a command is required"}
	}
	command := strings.Join(names, " ")
	if command == "watch" && opts.Watch.JSON && opts.Watch.Plain {
		return parsedCLI{}, errors.New("watch: --json and --plain cannot be used together")
	}
	return parsedCLI{Options: opts, Command: command}, nil
}

type credentials struct {
	User     string
	Password string
}

func loadCredentials(getenv func(string) string) (credentials, error) {
	result := credentials{User: getenv("WEBDAV_USER"), Password: getenv("WEBDAV_PASSWORD")}
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
		InputPath: conf.WebDAV.Root, OutputPath: conf.Download.Destination,
		TempPath: conf.Download.Temp, Concurrency: conf.Download.Workers,
		ScanEvery: conf.Download.ScanEvery.Value(), RemoveRemote: conf.Download.RemoveRemote,
	}
}
