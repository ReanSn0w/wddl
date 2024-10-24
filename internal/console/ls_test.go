package console_test

import (
	"testing"

	"git.papkovda.ru/tools/webdav/internal/console"
)

func TestConsole_LS(t *testing.T) {
	cases := []struct {
		Name string
		Path string
	}{
		{
			Name: "root dir",
			Path: "/",
		},
	}

	for _, c := range cases {
		err := consoleApp.LS(&console.LSConfig{
			Path: c.Path,
		})

		if err != nil {
			t.Errorf("console ls case %v failed with err: %v", c.Name, err)
		}
	}

	t.Error("done")
}
