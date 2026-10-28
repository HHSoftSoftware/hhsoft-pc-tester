package inputtest

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"syscall"
)

const (
	maximumInputRegistryBytes     = 1 << 20
	maximumInputIdentityBytes     = 128
	maximumInputRegistryLines     = 32768
	maximumInputRegistryRecords   = 2048
	maximumInputRegistryLineBytes = 65536
)

var (
	errInputAcquire     = errors.New("cannot safely acquire input readiness metadata")
	errInputRead        = errors.New("input metadata is unreadable or exceeds the supported limit")
	errInputIdentity    = errors.New("input descriptor or selected registry identity changed")
	errInputClose       = errors.New("input descriptor closure could not be confirmed")
	errInputUnavailable = errors.New("input event interface is absent or inaccessible")
	errInputStream      = errors.New("input event stream is incomplete or unreadable")
)

type inputDeviceNumber struct{ major, minor uint32 }

func nilInputValue(value any) bool {
	if value == nil {
		return true
	}
	switch reflect.ValueOf(value).Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflect.ValueOf(value).IsNil()
	}
	return false
}

// No size hint controls allocation. The extra byte is an overflow witness;
// exact EOF (including a separate EOF after a full value) proves completion.
func readInputBytes(ctx context.Context, source io.Reader, limit, batch int) ([]byte, error) {
	if nilInputValue(ctx) || nilInputValue(source) || limit < 1 || limit > maximumInputRegistryBytes || batch < 1 || batch > 4096 {
		return nil, errInputRead
	}
	data := make([]byte, limit+1)
	used := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		request := data[used:min(len(data), used+batch)]
		n, err := source.Read(request)
		if n < 0 || n > len(request) || n == 0 && err == nil || used+n > limit {
			return nil, errInputRead
		}
		if err != nil && err != io.EOF {
			return nil, errInputRead
		}
		used += n
		if stopped := ctx.Err(); stopped != nil {
			return nil, stopped
		}
		if err == io.EOF {
			return data[:used], nil
		}
	}
}

func parseInputDeviceNumber(data []byte) (inputDeviceNumber, bool) {
	parts := strings.Split(strings.TrimSpace(string(data)), ":")
	if len(parts) != 2 {
		return inputDeviceNumber{}, false
	}
	major, majorErr := strconv.ParseUint(parts[0], 10, 32)
	minor, minorErr := strconv.ParseUint(parts[1], 10, 32)
	if majorErr != nil || minorErr != nil || strconv.FormatUint(major, 10) != parts[0] || strconv.FormatUint(minor, 10) != parts[1] {
		return inputDeviceNumber{}, false
	}
	return inputDeviceNumber{uint32(major), uint32(minor)}, true
}

// evdev owns the legacy input minors 64..95 and dynamically allocated minors
// starting at 256. This is a type/range check, not an event-index formula.
func inputEvdevNumber(number inputDeviceNumber) bool {
	return number.major == 13 && number.minor <= 1<<20-1 &&
		(number.minor >= 64 && number.minor <= 95 || number.minor >= 256)
}

// Only an operation-matched, sole errno without an acquired handle can be
// ordinary unavailability. Joined/masked errors and unbounded wrappers cannot.
func inputAvailabilityError(err error, operation string, allowMissing bool) bool {
	for depth := 0; depth < 32 && !nilInputValue(err); depth++ {
		if _, multiple := err.(interface{ Unwrap() []error }); multiple {
			return false
		}
		if pathErr, ok := err.(*os.PathError); ok {
			if pathErr.Op != operation {
				return false
			}
			err = pathErr.Err
			continue
		}
		if err == syscall.EACCES || err == syscall.EPERM || allowMissing && (err == syscall.ENOENT || err == os.ErrNotExist) {
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

func recordInputFailure(result *Result, err error) {
	result.Status, result.Detail = StatusError, err.Error()
	if err == context.Canceled || err == context.DeadlineExceeded {
		result.Status, result.Detail = StatusAborted, "cancelled"
	}
}

func cancelInputResult(ctx context.Context, result *Result) bool {
	if ctx.Err() == nil {
		return false
	}
	if result.Status != StatusError {
		result.Status, result.Detail = StatusAborted, "cancelled"
	}
	return true
}
