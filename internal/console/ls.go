package console

import (
	"fmt"
	"io/fs"
	"sort"
)

type LSConfig struct {
	Path string
}

func (c *Console) LS(conf *LSConfig) error {
	items, err := c.client.ReadDir(conf.Path)
	if err != nil {
		return err
	}

	c.print(items...)
	return nil
}

func (c *Console) print(items ...fs.FileInfo) {
	sort.Slice(items, func(i, j int) bool {
		ival := 0
		if items[i].IsDir() {
			ival += 1
		}

		jval := 0
		if items[j].IsDir() {
			jval += 1
		}

		return ival > jval
	})

	for i := range items {
		if items[i].IsDir() {
			c.printDirectory(items[i])
		} else {
			c.printFile(items[i])
		}
	}
}

func (c *Console) printDirectory(dir fs.FileInfo) {
	fmt.Printf("[D] %v (%v)", dir.Name(), dir.Mode())
}

func (c *Console) printFile(item fs.FileInfo) {
	fmt.Printf("[F] %v (%v | %v)", item.Name(), item.Size(), item.Mode())
}
