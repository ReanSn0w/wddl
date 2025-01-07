package utils

import (
	"errors"
	"os"
	"strings"

	"github.com/go-pkgz/lgr"
)

func OpenFile(filepath string) (*os.File, error) {
	err := checkFilePath(filepath)
	if err != nil {
		return nil, err
	}

	return os.Open(filepath)
}

func CreateFile(filepath string) (*os.File, error) {
	err := checkFilePath(filepath)
	if err != nil {
		return nil, err
	}

	return os.Create(filepath)
}

func checkFilePath(filepath string) error {
	fileDir := dropLastPathPart(filepath)
	_, err := os.Stat(fileDir)
	if os.IsNotExist(err) {
		err = createDirectoryByPath(fileDir)
		if os.IsExist(err) {
			// Данное исключение исправляет проблему,
			// когда несколько потоков пытаются одну и ту же
			// папку, от этого в лог падает несколько сообщений
			err = nil
		}
		if err != nil {
			return err
		}
	}

	if err != nil {
		return err
	}

	return nil
}

func createDirectoryByPath(directoryPath string) error {
	info, err := os.Stat(directoryPath)
	if os.IsNotExist(err) {
		lowerDir := dropLastPathPart(directoryPath)
		err = createDirectoryByPath(lowerDir)
		if err == nil {
			err = os.Mkdir(directoryPath, 0777)
			if err != nil {
				return err
			}

			info, err = os.Stat(directoryPath)
		}
	}

	if err != nil {
		lgr.Default().Logf("[TRACE] error is: %v", err)
		return err
	}

	if !info.IsDir() {
		return errors.New("path is not directory")
	}

	return nil
}

func dropLastPathPart(path string) string {
	pathParts := strings.Split(path, "/")
	if len(pathParts) == 1 {
		return "."
	}

	if len(pathParts) == 2 && pathParts[1] == "" {
		return "."
	}

	pathParts = pathParts[:len(pathParts)-1]
	return strings.Join(pathParts, "/")
}
