package usbtest

import (
	"context"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
)

const (
	maximumUSBDevices         = 512
	maximumUSBRegistryEntries = maximumUSBDevices * 4
	usbRegistryBatch          = 64
)

var (
	errUSBRegistryRead  = errors.New("cannot read bounded USB device registry")
	errUSBRegistryClose = errors.New("USB device registry closure could not be confirmed")
)

type usbRegistryDirectory interface {
	Stat() (os.FileInfo, error)
	ReadDir(int) ([]os.DirEntry, error)
	Close() error
}

// ReadDir is always called with a positive bounded request. The final one-entry
// probe distinguishes the existing limit from a truncated registry. Only
// validated, cached names survive; no per-entry Stat follows directory links.
func readUSBRegistry(ctx context.Context, path string, open func(string) (usbRegistryDirectory, error)) (names []string, err error) {
	if nilUSBValue(ctx) || open == nil {
		return nil, errUSBRegistryRead
	}
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	directory, openErr := open(path)
	if nilUSBValue(directory) {
		return nil, errUSBRegistryRead
	}
	defer func() {
		if directory.Close() != nil {
			names, err = nil, errUSBRegistryClose
			return
		}
		if err != nil {
			names = nil
			return
		}
		if stopped := ctx.Err(); stopped != nil {
			names, err = nil, stopped
		}
	}()
	if openErr != nil {
		return nil, errUSBRegistryRead
	}
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	info, statErr := directory.Stat()
	if statErr != nil || nilUSBValue(info) || !info.IsDir() {
		return nil, errUSBRegistryRead
	}
	names = make([]string, 0, maximumUSBRegistryEntries)
	seen := make(map[string]bool)
	for {
		if stopped := ctx.Err(); stopped != nil {
			return nil, stopped
		}
		request := min(usbRegistryBatch, maximumUSBRegistryEntries-len(names)+1)
		batch, readErr := directory.ReadDir(request)
		if len(batch) > request || len(batch)+len(names) > maximumUSBRegistryEntries ||
			len(batch) == 0 && readErr == nil || readErr != nil && readErr != io.EOF {
			return nil, errUSBRegistryRead
		}
		for _, entry := range batch {
			if nilUSBValue(entry) {
				return nil, errUSBRegistryRead
			}
			name := entry.Name()
			if name == "" || name == "." || name == ".." || len(name) > 255 || strings.ContainsAny(name, "/\\\x00") || seen[name] {
				return nil, errUSBRegistryRead
			}
			seen[name] = true
			names = append(names, name)
			if stopped := ctx.Err(); stopped != nil {
				return nil, stopped
			}
		}
		if stopped := ctx.Err(); stopped != nil {
			return nil, stopped
		}
		if readErr == io.EOF {
			sort.Strings(names)
			return names, nil
		}
	}
}
