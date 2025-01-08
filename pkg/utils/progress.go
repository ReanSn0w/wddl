package utils

import (
	"bytes"
	"fmt"
	"sort"
)

func NewProgres(id string, progress float64) FileProgress {
	return FileProgress{
		ID:       id,
		Progress: progress,
	}
}

type FileProgress struct {
	ID       string
	Progress float64
}

func MakeProgressMessage(workFiles, maxFiles int, files ...FileProgress) string {
	buf := bytes.Buffer{}

	buf.WriteString(
		fmt.Sprintf("Downloading: %v / %v", workFiles, maxFiles),
	)

	sort.Slice(files, func(i, j int) bool {
		return files[i].Progress > files[j].Progress
	})

	for i, file := range files {
		buf.WriteString(
			fmt.Sprintf("\n%v) %s (%.2f%%)", i+1, file.ID, file.Progress),
		)
	}

	return buf.String()
}
