package console

import (
	"io"
	"os"
)

type DLConfig struct {
	File   string `short:"f" long:"file" description:"file location"`
	Output string `short:"o" long:"output" description:"output location"`
}

func (c *Console) DL(path, output string) error {
	f, err := os.Create(output)
	if err != nil {
		return err
	}

	stream, err := c.client.ReadStream(path)
	if err != nil {
		return err
	}

	_, err = io.Copy(f, stream)
	return err
}
