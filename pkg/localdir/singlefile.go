package localdir

import (
	"io/fs"
	"os"
)

type SingleFileFS struct {
	filePath string
}

func (sfs SingleFileFS) Open(name string) (fs.File, error) {
	if name != "." {
		return nil, fs.ErrNotExist
	}
	return os.Open(sfs.filePath)
}

func (sfs SingleFileFS) ReadFile(name string) ([]byte, error) {
	if name != "." {
		return nil, fs.ErrNotExist
	}
	return os.ReadFile(sfs.filePath)
}

func NewSingleFileFS(filePath string) *SingleFileFS {
	return &SingleFileFS{filePath: filePath}
}
