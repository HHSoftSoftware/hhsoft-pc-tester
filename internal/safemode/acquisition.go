package safemode

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"sort"
)

const safetyRegistryLimit = 4096
const safetyReadBatch = 4096
const safetyDirectoryBatch = 32

var errSafetyAcquisition = errors.New("kernel safety metadata could not be acquired")
var errSafetyClose = errors.New("kernel safety metadata closure could not be confirmed")

type safetyPath interface {
	Stat() (os.FileInfo, error)
	Fd() uintptr
	Close() error
}

type safetyFile interface {
	io.Reader
	ReadDir(int) ([]os.DirEntry, error)
	Stat() (os.FileInfo, error)
	Close() error
}

type safetyRoot interface {
	Lstat(string) (os.FileInfo, error)
	OpenPath(string) (safetyPath, error)
	OpenReadable(string, safetyPath) (safetyFile, error)
	Close() error
}

type safetyOperations struct {
	openRoot func(string) (safetyRoot, error)
}

type ownedSafetyRoot struct{ *os.Root }

func systemSafetyOperations() safetyOperations {
	return safetyOperations{openRoot: func(path string) (safetyRoot, error) {
		root, err := os.OpenRoot(path)
		if root == nil {
			return nil, err
		}
		return ownedSafetyRoot{root}, err
	}}
}

func nilSafetyValue(value any) bool {
	if value == nil {
		return true
	}
	switch reflect.ValueOf(value).Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflect.ValueOf(value).IsNil()
	default:
		return false
	}
}

// Each category owns its root independently. An unavailable category cannot
// suppress an independently observed unsafe finding in the other category.
func observeSafetyRoot(ctx context.Context, path string, operations safetyOperations, observe func(context.Context, safetyRoot) (int, error)) (count int, err error) {
	if nilSafetyValue(ctx) || operations.openRoot == nil || observe == nil {
		return 0, errSafetyAcquisition
	}
	if stopped := ctx.Err(); stopped != nil {
		return 0, stopped
	}
	root, openErr := operations.openRoot(path)
	if nilSafetyValue(root) {
		return 0, errSafetyAcquisition
	}
	defer finishSafetyOwner(ctx, root, &err)
	if openErr != nil {
		return 0, errSafetyAcquisition
	}
	if stopped := ctx.Err(); stopped != nil {
		return 0, stopped
	}
	return observe(ctx, root)
}

// The caller installs this immediately after accepting any non-nil handle,
// including a handle returned with an error. Closing never erases unsafe data.
func finishSafetyOwner(ctx context.Context, handle io.Closer, err *error) {
	if handle.Close() != nil {
		*err = errors.Join(errSafetyClose, *err)
	} else if *err == nil {
		*err = ctx.Err()
	}
}

func safetyType(info os.FileInfo, directory bool) bool {
	if nilSafetyValue(info) {
		return false
	}
	if directory {
		return info.Mode().IsDir() && info.Mode()&os.ModeSymlink == 0
	}
	return info.Mode().IsRegular()
}

// Linux first acquires O_PATH metadata without opening a device or FIFO for
// I/O. The readable handle is reopened from that owned descriptor only after
// type proof. os.Root contains intermediate sysfs links; the final component
// must be direct. SameFile binds all three observations to one owned object.
func withSafetyFile(ctx context.Context, root safetyRoot, path string, directory bool, consume func(safetyFile) error) (complete bool, err error) {
	if nilSafetyValue(ctx) || nilSafetyValue(root) || consume == nil {
		return false, errSafetyAcquisition
	}
	if stopped := ctx.Err(); stopped != nil {
		return false, stopped
	}
	before, statErr := root.Lstat(path)
	if statErr != nil || !safetyType(before, directory) {
		return false, errSafetyAcquisition
	}
	if stopped := ctx.Err(); stopped != nil {
		return false, stopped
	}
	handle, openErr := root.OpenPath(path)
	if nilSafetyValue(handle) {
		return false, errSafetyAcquisition
	}
	defer finishSafetyOwner(ctx, handle, &err)
	if openErr != nil {
		return false, errSafetyAcquisition
	}
	if stopped := ctx.Err(); stopped != nil {
		return false, stopped
	}
	metadata, statErr := handle.Stat()
	if statErr != nil || !safetyType(metadata, directory) || !os.SameFile(before, metadata) {
		return false, errSafetyAcquisition
	}
	if stopped := ctx.Err(); stopped != nil {
		return false, stopped
	}
	file, openErr := root.OpenReadable(path, handle)
	if nilSafetyValue(file) {
		return false, errSafetyAcquisition
	}
	defer finishSafetyOwner(ctx, file, &err)
	if openErr != nil {
		return false, errSafetyAcquisition
	}
	if stopped := ctx.Err(); stopped != nil {
		return false, stopped
	}
	opened, statErr := file.Stat()
	if statErr != nil || !safetyType(opened, directory) || !os.SameFile(metadata, opened) {
		return false, errSafetyAcquisition
	}
	if stopped := ctx.Err(); stopped != nil {
		return false, stopped
	}
	if err := consume(file); err != nil {
		return false, err
	}
	if stopped := ctx.Err(); stopped != nil {
		return false, stopped
	}
	final, statErr := file.Stat()
	if statErr != nil || !safetyType(final, directory) || !os.SameFile(opened, final) {
		return false, errSafetyAcquisition
	}
	if stopped := ctx.Err(); stopped != nil {
		return false, stopped
	}
	after, statErr := root.Lstat(path)
	if statErr != nil || !safetyType(after, directory) || !os.SameFile(before, after) {
		return false, errSafetyAcquisition
	}
	return true, nil
}

func readSafetyBytes(ctx context.Context, reader io.Reader, limit int64) ([]byte, error) {
	if nilSafetyValue(ctx) || nilSafetyValue(reader) || limit < 1 || limit > 4<<20 {
		return nil, errSafetyAcquisition
	}
	data := make([]byte, 0, min(safetyReadBatch, int(limit)))
	var buffer [safetyReadBatch]byte
	for {
		if stopped := ctx.Err(); stopped != nil {
			return nil, stopped
		}
		request := buffer[:min(len(buffer), int(limit)+1-len(data))]
		n, readErr := reader.Read(request)
		if n < 0 || n > len(request) || n == 0 && readErr == nil || int64(len(data)+n) > limit || readErr != nil && readErr != io.EOF {
			return nil, errSafetyAcquisition
		}
		data = append(data, request[:n]...)
		if stopped := ctx.Err(); stopped != nil {
			return nil, stopped
		}
		if readErr == io.EOF {
			return data, nil
		}
	}
}

func readMetadata(ctx context.Context, root safetyRoot, path string, limit int64) ([]byte, error) {
	var data []byte
	complete, err := withSafetyFile(ctx, root, path, false, func(file safetyFile) error {
		var err error
		data, err = readSafetyBytes(ctx, file, limit)
		return err
	})
	// Only a completely acquired, identity-bound value may retain unsafe
	// diagnostic content when the remaining failure is closure/cancellation.
	if !complete {
		return nil, err
	}
	return data, err
}

func readSafetyNames(ctx context.Context, directory interface {
	ReadDir(int) ([]os.DirEntry, error)
}) ([]string, error) {
	if nilSafetyValue(ctx) || nilSafetyValue(directory) {
		return nil, errSafetyAcquisition
	}
	names := []string{}
	seen := map[string]bool{}
	for {
		if stopped := ctx.Err(); stopped != nil {
			return nil, stopped
		}
		request := min(safetyDirectoryBatch, safetyRegistryLimit+1-len(names))
		entries, readErr := directory.ReadDir(request)
		if len(entries) > request || len(entries) == 0 && readErr == nil || len(names)+len(entries) > safetyRegistryLimit || readErr != nil && readErr != io.EOF {
			return nil, errSafetyAcquisition
		}
		for _, entry := range entries {
			if stopped := ctx.Err(); stopped != nil {
				return nil, stopped
			}
			if nilSafetyValue(entry) {
				return nil, errSafetyAcquisition
			}
			name := entry.Name()
			if !interfaceName.MatchString(name) || name == "." || name == ".." || seen[name] {
				return nil, errSafetyAcquisition
			}
			seen[name] = true
			names = append(names, name)
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

func registry(ctx context.Context, root safetyRoot, path string) ([]string, error) {
	var names []string
	_, err := withSafetyFile(ctx, root, path, true, func(file safetyFile) error {
		var err error
		names, err = readSafetyNames(ctx, file)
		return err
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}
