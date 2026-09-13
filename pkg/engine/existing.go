package engine

import (
	"fmt"
	"os"
)

// FindLocalCopy checks the expected destination first and then an optional
// additional-library finder. Every successful result is verified with Stat.
func FindLocalCopy(file File, finder ExistingFileFinder) (string, error) {
	stat, err := os.Stat(file.Dest)
	if err == nil {
		if stat.Mode().IsRegular() && stat.Name() == file.Name && stat.Size() == file.Size {
			return file.Dest, nil
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat destination %q: %w", file.Dest, err)
	}

	if finder == nil {
		return "", ErrLocalFileNotFound
	}

	path, err := finder.Find(file.Name, file.Size)
	if err != nil {
		return "", err
	}
	return path, nil
}
