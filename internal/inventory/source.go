package inventory

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type PathSource interface {
	ReadFile(string) ([]byte, error)
	ReadDir(string) ([]fs.DirEntry, error)
}

type RootPathSource struct {
	root string
}

func NewRootPathSource(root string) *RootPathSource {
	return &RootPathSource{root: filepath.Clean(root)}
}

func (source *RootPathSource) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(source.resolve(name))
}

func (source *RootPathSource) ReadDir(name string) ([]fs.DirEntry, error) {
	return os.ReadDir(source.resolve(name))
}

func (source *RootPathSource) resolve(name string) string {
	cleaned := path.Clean("/" + strings.TrimSpace(name))
	relative := strings.TrimPrefix(cleaned, "/")
	return filepath.Join(source.root, filepath.FromSlash(relative))
}
