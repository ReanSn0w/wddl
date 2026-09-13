package main

import "strings"

type watchOutputMode uint8

const (
	watchOutputPlain watchOutputMode = iota
	watchOutputJSON
	watchOutputInteractive
)

func selectWatchOutputMode(options watchCommand, terminal bool, termName string) watchOutputMode {
	switch {
	case options.JSON:
		return watchOutputJSON
	case options.Plain:
		return watchOutputPlain
	case terminal && !strings.EqualFold(termName, "dumb"):
		return watchOutputInteractive
	default:
		return watchOutputPlain
	}
}
