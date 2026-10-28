package audio

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"
)

const (
	maximumRegistryEntries = 4096
	maximumAttributeBytes  = 64
	audioAcquisitionBatch  = 32
)

var (
	errAudioClose         = errors.New("audio metadata closure could not be confirmed")
	errAudioRead          = errors.New("ALSA attribute is unreadable or exceeds the supported limit")
	errAudioRegistryOpen  = errors.New("cannot read ALSA sound registry")
	errAudioRegistryRead  = errors.New("cannot enumerate ALSA sound registry")
	errAudioRegistryLimit = errors.New("ALSA sound registry exceeds the supported limit")
)

type audioDirectory interface {
	ReadDir(int) ([]os.DirEntry, error)
	Close() error
}

func nilAudioValue(value any) bool {
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

// Only a sole error from the expected operation is absence. A returned handle,
// metadata, multi-error, wrong operation or unbounded wrapper is not absence.
func soleAudioAbsence(err error, operation string) bool {
	for depth := 0; depth < 32 && !nilAudioValue(err); depth++ {
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
		if err == os.ErrNotExist || err == syscall.ENOENT {
			return true
		}
		wrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = wrapped.Unwrap()
	}
	return false
}

func acquireAudioRegistry(ctx context.Context, open func() (audioDirectory, error)) (names []string, err error) {
	if nilAudioValue(ctx) || open == nil {
		return nil, errAudioRegistryOpen
	}
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	directory, openErr := open()
	if nilAudioValue(directory) {
		if soleAudioAbsence(openErr, "openat") {
			if stopped := ctx.Err(); stopped != nil {
				return nil, stopped
			}
			return nil, nil
		}
		return nil, errAudioRegistryOpen
	}
	defer func() {
		if directory.Close() != nil {
			names, err = nil, errAudioClose
		} else if err != nil {
			names = nil
		} else if stopped := ctx.Err(); stopped != nil {
			names, err = nil, stopped
		}
	}()
	if openErr != nil {
		return nil, errAudioRegistryOpen
	}
	seen := make(map[string]bool)
	for {
		if stopped := ctx.Err(); stopped != nil {
			return nil, stopped
		}
		requested := min(audioAcquisitionBatch, maximumRegistryEntries+1-len(names))
		entries, readErr := directory.ReadDir(requested)
		if len(entries) > requested || len(entries) == 0 && readErr == nil {
			return nil, errAudioRegistryRead
		}
		if readErr != nil && readErr != io.EOF {
			return nil, errAudioRegistryRead
		}
		if len(names)+len(entries) > maximumRegistryEntries {
			return nil, errAudioRegistryLimit
		}
		for _, entry := range entries {
			if stopped := ctx.Err(); stopped != nil {
				return nil, stopped
			}
			if nilAudioValue(entry) {
				return nil, errAudioRegistryRead
			}
			name := entry.Name()
			if len(name) == 0 || len(name) > 255 || name == "." || name == ".." ||
				strings.ContainsAny(name, "/\\\x00") || !utf8.ValidString(name) || seen[name] {
				return nil, errAudioRegistryRead
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

// The caller owns both descriptor lifetimes. A separate exact EOF proves that
// a full64-byte value is complete; the65th byte is an overflow witness.
func readAudioBytes(ctx context.Context, reader io.Reader) ([]byte, error) {
	if nilAudioValue(ctx) || nilAudioValue(reader) {
		return nil, errAudioRead
	}
	var buffer [maximumAttributeBytes + 1]byte
	used := 0
	for {
		if stopped := ctx.Err(); stopped != nil {
			return nil, stopped
		}
		request := buffer[used:min(len(buffer), used+audioAcquisitionBatch)]
		n, readErr := reader.Read(request)
		if n < 0 || n > len(request) || n == 0 && readErr == nil || used+n > maximumAttributeBytes {
			return nil, errAudioRead
		}
		if readErr != nil && readErr != io.EOF {
			return nil, errAudioRead
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

func closeAudioAttribute(ctx context.Context, handle io.Closer, data *[]byte, err *error) {
	if handle.Close() != nil {
		*data, *err = nil, errAudioClose
	} else if *err != nil {
		*data = nil
	} else if stopped := ctx.Err(); stopped != nil {
		*data, *err = nil, stopped
	}
}
