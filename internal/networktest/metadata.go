package networktest

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"syscall"
)

const (
	maximumNetworkScalarBytes = 256
	maximumNetworkUeventBytes = 4096
)

var (
	errNetworkMetadataRead   = errors.New("cannot acquire bounded network metadata")
	errNetworkMetadataClose  = errors.New("network metadata closure could not be confirmed")
	errNetworkMetadataAbsent = errors.New("network metadata is unavailable")
)

type networkMetadataFile interface {
	Read([]byte) (int, error)
	Stat() (os.FileInfo, error)
	Close() error
}

func nilNetworkValue(value any) bool {
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

// A sole errno is not a joined failure. Path errors must identify the operation
// whose limited absence policy is being applied. Cyclic wrappers fail closed.
func soleNetworkError(err error, operation string, expected ...error) bool {
	for depth := 0; depth < 32 && !nilNetworkValue(err); depth++ {
		if _, multiple := err.(interface{ Unwrap() []error }); multiple {
			return false
		}
		if pathError, ok := err.(*os.PathError); ok {
			if pathError.Op != operation {
				return false
			}
			err = pathError.Err
			continue
		}
		for _, value := range expected {
			if err == value {
				return true
			}
		}
		wrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = wrapped.Unwrap()
	}
	return false
}

// Only bounded buffers are allocated. Stat size hints do not control the read;
// an extra raw byte proves overflow, and only an exact EOF proves completion.
func acquireNetworkMetadata(ctx context.Context, path string, maximum int, optional, offlineValue bool, open func(string) (networkMetadataFile, error)) (data []byte, err error) {
	if nilNetworkValue(ctx) || open == nil || (maximum != maximumNetworkScalarBytes && maximum != maximumNetworkUeventBytes) || offlineValue && (!optional || maximum != maximumNetworkScalarBytes) {
		return nil, errNetworkMetadataRead
	}
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	file, openErr := open(path)
	if nilNetworkValue(file) {
		if optional && soleNetworkError(openErr, "open", os.ErrNotExist, syscall.ENOENT) {
			if stopped := ctx.Err(); stopped != nil {
				return nil, stopped
			}
			return nil, errNetworkMetadataAbsent
		}
		return nil, errNetworkMetadataRead
	}
	defer func() {
		if file.Close() != nil {
			data, err = nil, errNetworkMetadataClose
			return
		}
		if err != nil && err != errNetworkMetadataAbsent {
			data = nil
			return
		}
		if stopped := ctx.Err(); stopped != nil {
			data, err = nil, stopped
		}
	}()
	if openErr != nil {
		return nil, errNetworkMetadataRead
	}
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	info, statErr := file.Stat()
	if statErr != nil || nilNetworkValue(info) || !info.Mode().IsRegular() {
		return nil, errNetworkMetadataRead
	}
	buffer := make([]byte, maximum+1)
	used := 0
	for {
		if stopped := ctx.Err(); stopped != nil {
			return nil, stopped
		}
		request := buffer[used:min(len(buffer), used+32)]
		n, readErr := file.Read(request)
		if n < 0 || n > len(request) || n == 0 && readErr == nil || used+n > maximum {
			return nil, errNetworkMetadataRead
		}
		if readErr != nil && readErr != io.EOF {
			// These three link-state attributes can reject a read while offline.
			// No bytes, prior progress, other role or error chain gains this policy.
			if offlineValue && used == 0 && n == 0 && soleNetworkError(readErr, "read", syscall.EINVAL) {
				return nil, errNetworkMetadataAbsent
			}
			return nil, errNetworkMetadataRead
		}
		used += n
		if stopped := ctx.Err(); stopped != nil {
			return nil, stopped
		}
		if readErr == io.EOF {
			return buffer[:used], nil
		}
	}
}
