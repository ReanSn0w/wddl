package main

import "testing"

func TestSelectWatchOutputMode(t *testing.T) {
	tests := []struct {
		name     string
		options  watchCommand
		terminal bool
		termName string
		want     watchOutputMode
	}{
		{name: "json wins outside terminal", options: watchCommand{outputOptions: outputOptions{JSON: true}}, want: watchOutputJSON},
		{name: "plain forced in terminal", options: watchCommand{Plain: true}, terminal: true, termName: "xterm", want: watchOutputPlain},
		{name: "interactive terminal", terminal: true, termName: "xterm-256color", want: watchOutputInteractive},
		{name: "dumb terminal", terminal: true, termName: "dumb", want: watchOutputPlain},
		{name: "pipe", termName: "xterm", want: watchOutputPlain},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := selectWatchOutputMode(tt.options, tt.terminal, tt.termName); got != tt.want {
				t.Fatalf("selectWatchOutputMode() = %v, want %v", got, tt.want)
			}
		})
	}
}
