package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"sort"
)

const maximumBlockRegistryEntries = 256

var (
	errBlockRegistryRead  = errors.New("cannot read block device registry")
	errBlockRegistryLimit = errors.New("block device registry exceeds the supported limit")
	errBlockRegistryClose = errors.New("block device registry closure could not be confirmed")
)

type blockRegistryDirectory interface {
	ReadDir(int) ([]os.DirEntry, error)
	Close() error
}

func readBlockRegistry(ctx context.Context, path string, open func(string) (blockRegistryDirectory, error)) (entries []os.DirEntry, err error) {
	if ctx == nil || open == nil {
		return nil, errBlockRegistryRead
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	directory, openErr := open(path)
	if directory == nil {
		return nil, errBlockRegistryRead
	}
	defer func() {
		if directory.Close() != nil {
			entries, err = nil, errBlockRegistryClose
			return
		}
		if err != nil {
			entries = nil
			return
		}
		if stopped := ctx.Err(); stopped != nil {
			entries, err = nil, stopped
			return
		}
		// Match os.ReadDir's order before the caller processes identities or
		// retains a successful prefix preceding a later metadata error.
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		if stopped := ctx.Err(); stopped != nil {
			entries, err = nil, stopped
		}
	}()
	if openErr != nil {
		return nil, errBlockRegistryRead
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		requested := maximumBlockRegistryEntries + 1 - len(entries)
		batch, readErr := directory.ReadDir(requested)
		if len(batch) > requested || readErr != nil && readErr != io.EOF {
			return nil, errBlockRegistryRead
		}
		if len(batch) > maximumBlockRegistryEntries-len(entries) {
			return nil, errBlockRegistryLimit
		}
		if len(batch) == 0 && readErr == nil {
			return nil, errBlockRegistryRead
		}
		for _, entry := range batch {
			if entry == nil {
				return nil, errBlockRegistryRead
			}
		}
		// An observation failure from this return takes priority over a
		// simultaneous cancellation. A short successful batch is not EOF.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entries = append(entries, batch...)
		if readErr == io.EOF {
			return entries, nil
		}
	}
}
