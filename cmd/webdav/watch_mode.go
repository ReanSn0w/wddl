package main

import (
	"io"
	"strings"

	"golang.org/x/term"
)

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

func watchTerminal(out io.Writer) (bool, func() int) {
	file, ok := out.(interface{ Fd() uintptr })
	if !ok {
		return false, func() int { return 0 }
	}
	fd := int(file.Fd())
	return term.IsTerminal(fd), func() int {
		width, _, err := term.GetSize(fd)
		if err != nil {
			return 0
		}
		return width
	}
}
