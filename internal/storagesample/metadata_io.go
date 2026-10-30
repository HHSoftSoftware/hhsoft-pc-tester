package storagesample

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"sort"
)

var errStorageMetadataAcquire = errors.New("storage metadata acquisition failed")
var errStorageMetadataClose = errors.New("storage metadata closure failed")
var errStorageMetadataCanceled = errors.New("storage metadata acquisition canceled")
var errStorageMetadataPlatform = errors.New("storage metadata platform is unsupported")

type storageMetadataPath interface {
	Stat() (os.FileInfo, error)
	Fd() uintptr
	Close() error
}
type storageMetadataFile interface {
	io.Reader
	ReadDir(int) ([]os.DirEntry, error)
	Stat() (os.FileInfo, error)
	Close() error
}
type storageMetadataRoot interface {
	Lstat(string) (os.FileInfo, error)
	Readlink(string) (string, error)
	OpenPath(string) (storageMetadataPath, error)
	OpenReadable(storageMetadataPath) (storageMetadataFile, error)
	Close() error
}
type storageMetadataOperations struct {
	openRoot func(string) (storageMetadataRoot, error)
}

func nilStorageMetadata(value any) bool {
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

func storageMetadataStop(ctx context.Context) error {
	if nilStorageMetadata(ctx) || ctx.Err() != nil {
		return errStorageMetadataCanceled
	}
	return nil
}

// Each non-nil acquired owner is registered immediately, even with an error.
// Cleanup continues through every close failure and never exports provider text.
func closeStorageMetadata(owners []io.Closer, err *error) {
	for i := len(owners) - 1; i >= 0; i-- {
		if owners[i].Close() != nil {
			*err = errors.Join(errStorageMetadataClose, *err)
		}
	}
}

func storageMetadataType(info os.FileInfo, directory bool) bool {
	if nilStorageMetadata(info) {
		return false
	}
	if directory {
		return info.Mode().IsDir() && info.Mode()&os.ModeSymlink == 0
	}
	return info.Mode().IsRegular()
}

func sameStorageMetadata(a, b os.FileInfo, directory bool) bool {
	return storageMetadataType(a, directory) && storageMetadataType(b, directory) &&
		os.SameFile(a, b) && a.Mode() == b.Mode() && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// Acquisition follows the existing safety reader: metadata-only O_PATH first,
// then a readable reopen of that descriptor. No named FIFO/device I/O is needed.
func withStorageMetadata(ctx context.Context, root storageMetadataRoot, path string, directory bool, consume func(storageMetadataFile) error) (err error) {
	if nilStorageMetadata(ctx) || nilStorageMetadata(root) || consume == nil {
		return errStorageMetadataAcquire
	}
	owners := []io.Closer{}
	defer func() {
		closeStorageMetadata(owners, &err)
		if err == nil {
			err = storageMetadataStop(ctx)
		}
	}()
	if err = storageMetadataStop(ctx); err != nil {
		return err
	}
	before, e := root.Lstat(path)
	if e != nil || !storageMetadataType(before, directory) {
		return errStorageMetadataAcquire
	}
	if err = storageMetadataStop(ctx); err != nil {
		return err
	}
	handle, e := root.OpenPath(path)
	if !nilStorageMetadata(handle) {
		owners = append(owners, handle)
	}
	if e != nil || nilStorageMetadata(handle) {
		return errStorageMetadataAcquire
	}
	if err = storageMetadataStop(ctx); err != nil {
		return err
	}
	metadata, e := handle.Stat()
	if e != nil || !sameStorageMetadata(before, metadata, directory) {
		return errStorageMetadataAcquire
	}
	if err = storageMetadataStop(ctx); err != nil {
		return err
	}
	file, e := root.OpenReadable(handle)
	if !nilStorageMetadata(file) {
		owners = append(owners, file)
	}
	if e != nil || nilStorageMetadata(file) {
		return errStorageMetadataAcquire
	}
	if err = storageMetadataStop(ctx); err != nil {
		return err
	}
	opened, e := file.Stat()
	if e != nil || !sameStorageMetadata(metadata, opened, directory) {
		return errStorageMetadataAcquire
	}
	if err = storageMetadataStop(ctx); err != nil {
		return err
	}
	if err = consume(file); err != nil {
		return err
	}
	if err = storageMetadataStop(ctx); err != nil {
		return err
	}
	final, e := file.Stat()
	if e != nil || !sameStorageMetadata(opened, final, directory) {
		return errStorageMetadataAcquire
	}
	if err = storageMetadataStop(ctx); err != nil {
		return err
	}
	after, e := root.Lstat(path)
	if e != nil || !sameStorageMetadata(before, after, directory) {
		return errStorageMetadataAcquire
	}
	return nil
}

func storageMetadataBytes(ctx context.Context, reader io.Reader, limit int) ([]byte, error) {
	if nilStorageMetadata(ctx) || nilStorageMetadata(reader) || limit < 1 || limit > 4<<20 {
		return nil, errStorageMetadataAcquire
	}
	data := make([]byte, 0, min(4096, limit))
	var buffer [4096]byte
	for {
		if err := storageMetadataStop(ctx); err != nil {
			return nil, err
		}
		request := buffer[:min(len(buffer), limit+1-len(data))]
		n, e := reader.Read(request)
		if n < 0 || n > len(request) || n == 0 && e == nil || len(data)+n > limit || e != nil && e != io.EOF {
			return nil, errStorageMetadataAcquire
		}
		data = append(data, request[:n]...)
		if err := storageMetadataStop(ctx); err != nil {
			return nil, err
		}
		if e == io.EOF {
			return data, nil
		}
	}
}

func readStorageMetadata(ctx context.Context, root storageMetadataRoot, path string, limit int) ([]byte, error) {
	var data []byte
	err := withStorageMetadata(ctx, root, path, false, func(file storageMetadataFile) error {
		var e error
		data, e = storageMetadataBytes(ctx, file, limit)
		return e
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Name/type values are copied out of the provider batch before context callbacks.
// Missing EOF, duplicate entries and excessive batches cannot become an empty list.
func storageMetadataNames(ctx context.Context, directory storageMetadataFile, limit int) ([]string, error) {
	if nilStorageMetadata(ctx) || nilStorageMetadata(directory) || limit < 0 || limit > 1024 {
		return nil, errStorageMetadataAcquire
	}
	names := []string{}
	seen := map[string]bool{}
	for {
		if err := storageMetadataStop(ctx); err != nil {
			return nil, err
		}
		request := min(32, limit+1-len(names))
		entries, e := directory.ReadDir(request)
		if len(entries) > request || len(entries) == 0 && e == nil || len(names)+len(entries) > limit || e != nil && e != io.EOF {
			return nil, errStorageMetadataAcquire
		}
		batch := make([]string, len(entries))
		for i, entry := range entries {
			if nilStorageMetadata(entry) || entry.Type() != os.ModeSymlink {
				return nil, errStorageMetadataAcquire
			}
			name := entry.Name()
			if !linuxStorageName(name) || seen[name] {
				return nil, errStorageMetadataAcquire
			}
			seen[name] = true
			batch[i] = name
		}
		names = append(names, batch...)
		if err := storageMetadataStop(ctx); err != nil {
			return nil, err
		}
		if e == io.EOF {
			sort.Strings(names)
			return names, nil
		}
	}
}

func readStorageMetadataNames(ctx context.Context, root storageMetadataRoot, path string, limit int) ([]string, error) {
	var names []string
	err := withStorageMetadata(ctx, root, path, true, func(file storageMetadataFile) error {
		var e error
		names, e = storageMetadataNames(ctx, file, limit)
		return e
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}
