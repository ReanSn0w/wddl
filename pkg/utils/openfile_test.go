package utils_test

import (
	"os"
	"testing"

	"git.papkovda.ru/tools/webdav/pkg/utils"
)

func Test_CreateFile(t *testing.T) {
	cases := []struct {
		Path string
	}{
		{Path: "./file.txt"},
		{Path: "./dir/file.txt"},
		{Path: "./dir/dir/file.txt"},
	}

	defer func() {
		t.Log("Clear test data")
		_ = os.RemoveAll("./dir")
		_ = os.Remove("./file.txt")
	}()

	for i, c := range cases {
		file, err := utils.CreateFile(c.Path)
		if err != nil {
			t.Errorf("case %v create file err: %v", i, err)
		}

		err = file.Close()
		if err != nil {
			t.Errorf("case %v close file err: %v", i, err)
		}

		file, err = utils.OpenFile(c.Path)
		if err != nil {
			t.Errorf("case %v open file err: %v", i, err)
		}

		err = file.Close()
		if err != nil {
			t.Errorf("case %v close file err: %v", i, err)
		}
	}
}
