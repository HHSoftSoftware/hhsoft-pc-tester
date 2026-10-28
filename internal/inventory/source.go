package inventory

import (
	"context"
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
	return source.readDirectory(context.Background(), name)
}

func (source *RootPathSource) resolve(name string) string {
	cleaned := path.Clean("/" + strings.TrimSpace(name))
	relative := strings.TrimPrefix(cleaned, "/")
	return filepath.Join(source.root, filepath.FromSlash(relative))
}

func (source *RootPathSource) readDirectory(ctx context.Context, name string) ([]fs.DirEntry, error) {
	return readInventoryDirectory(ctx, source.resolve(name), openInventoryDirectory)
}

func (source *RootPathSource) readScalar(ctx context.Context, name string) ([]byte, bool, error) {
	if source == nil {
		return nil, false, ErrNoPathSource
	}
	return readInventoryScalar(ctx, source.resolve(name), openInventoryScalar)
}
