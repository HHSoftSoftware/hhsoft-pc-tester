package networktest

import (
	"context"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
	"unicode"
)

const maximumNetworkInterfaces = 128
const networkRegistryBatch = 32

var (
	errNetworkRegistryRead  = errors.New("cannot read bounded network interface registry")
	errNetworkRegistryClose = errors.New("network interface registry closure could not be confirmed")
)

type networkRegistryDirectory interface {
	Stat() (os.FileInfo, error)
	ReadDir(int) ([]os.DirEntry, error)
	Close() error
}

func validInterfaceName(name string) bool {
	return name != "" && len(name) <= 64 && name != "." && name != ".." && !strings.ContainsAny(name, `/\`) && strings.IndexFunc(name, unicode.IsControl) == -1
}

// A positive request bounds each registry allocation before the interface cap.
// Names are observed once and cached; normal sysfs device links are not walked
// through entry.Info. The final one-entry probe refuses silent truncation.
func readNetworkRegistry(ctx context.Context, path string, open func(string) (networkRegistryDirectory, error)) (names []string, err error) {
	if nilNetworkValue(ctx) || open == nil {
		return nil, errNetworkRegistryRead
	}
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	directory, openErr := open(path)
	if nilNetworkValue(directory) {
		return nil, errNetworkRegistryRead
	}
	defer func() {
		if directory.Close() != nil {
			names, err = nil, errNetworkRegistryClose
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
		return nil, errNetworkRegistryRead
	}
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	info, statErr := directory.Stat()
	if statErr != nil || nilNetworkValue(info) || !info.IsDir() {
		return nil, errNetworkRegistryRead
	}
	names = make([]string, 0, maximumNetworkInterfaces)
	seen := make(map[string]bool)
	for {
		if stopped := ctx.Err(); stopped != nil {
			return nil, stopped
		}
		request := min(networkRegistryBatch, maximumNetworkInterfaces-len(names)+1)
		batch, readErr := directory.ReadDir(request)
		if len(batch) > request || len(names)+len(batch) > maximumNetworkInterfaces || len(batch) == 0 && readErr == nil || readErr != nil && readErr != io.EOF {
			return nil, errNetworkRegistryRead
		}
		for _, entry := range batch {
			if nilNetworkValue(entry) {
				return nil, errNetworkRegistryRead
			}
			name := entry.Name()
			if !validInterfaceName(name) || seen[name] {
				return nil, errNetworkRegistryRead
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
