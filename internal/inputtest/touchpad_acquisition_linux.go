//go:build linux

package inputtest

import (
	"context"
	"io"
	"reflect"
	"strings"
	"syscall"
	"unsafe"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

// The internal provider transfers fresh owners. Reject direct concrete aliases
// at the two cross-role seams without adopting or closing a second reference.
// This is not proof that an arbitrary provider wrapper names a distinct object;
// production uses the existing system factory's separate os.OpenRoot/OpenFile.
func sameTouchpadHandle(a, b any) bool {
	if nilInputValue(a) || nilInputValue(b) {
		return false
	}
	return reflect.TypeOf(a) == reflect.TypeOf(b) && reflect.ValueOf(a).Comparable() && reflect.ValueOf(b).Comparable() && a == b
}

type touchpadRoot struct {
	inputReadinessRoot
	parent inputReadinessRoot
}

func (root *touchpadRoot) OpenRoot(name string) (inputReadinessRoot, error) {
	child, err := root.inputReadinessRoot.OpenRoot(name)
	if sameTouchpadHandle(child, root.inputReadinessRoot) || sameTouchpadHandle(child, root.parent) {
		return nil, errInputIdentity
	}
	if nilInputValue(child) {
		return child, err
	}
	return &touchpadRoot{inputReadinessRoot: child, parent: root.inputReadinessRoot}, err
}

func (root *touchpadRoot) OpenPath(name string, flags int) (inputPathFile, error) {
	path, err := root.inputReadinessRoot.OpenPath(name, flags)
	if sameTouchpadHandle(path, root.inputReadinessRoot) || sameTouchpadHandle(path, root.parent) {
		return nil, errInputIdentity
	}
	return path, err
}

func touchpadOwnedOperations(operations pointerOperations) pointerOperations {
	wrapped := operations
	if operations.input.openRoot != nil {
		wrapped.input.openRoot = func(path string) (inputReadinessRoot, error) {
			root, err := operations.input.openRoot(path)
			if nilInputValue(root) {
				return root, err
			}
			return &touchpadRoot{inputReadinessRoot: root}, err
		}
	}
	if operations.input.openReadable != nil {
		wrapped.input.openReadable = func(path inputPathFile) (inputReadableFile, error) {
			reader, err := operations.input.openReadable(path)
			if sameTouchpadHandle(path, reader) {
				return nil, errInputIdentity
			}
			return reader, err
		}
	}
	return wrapped
}

func parseTouchpadInterfaces(reader io.Reader, wordBits int) ([]string, error) {
	if wordBits != 32 && wordBits != 64 {
		return nil, errInputIdentity
	}
	return parseFunctionalInputInterfaces(reader, "touchpad", wordBits)
}

func discoverTouchpadWithOperations(ctx context.Context, path string, operations pointerOperations) (names []string, err error) {
	operations = touchpadOwnedOperations(operations)
	owner, err := openPointerRoots(ctx, path, false, operations)
	if owner != nil {
		defer func() {
			if closeErr := owner.close(); closeErr != nil {
				names, err = nil, closeErr
			} else if err == nil {
				if stopped := pointerContextError(ctx); stopped != nil {
					names, err = nil, stopped
				}
			}
		}()
	}
	if err != nil {
		return nil, err
	}
	width := inventory.ObservedInputBitmapWordBits()
	if width != 32 && width != 64 {
		return nil, errInputIdentity
	}
	if stopped := pointerContextError(ctx); stopped != nil {
		return nil, stopped
	}
	data, err := acquireInputMetadata(ctx, owner.root, operations.input, "proc/bus/input/devices", maximumInputRegistryBytes, 4096)
	if err != nil {
		return nil, err
	}
	names, err = parseTouchpadInterfaces(strings.NewReader(string(data)), width)
	if err != nil {
		return nil, errInputRead
	}
	return names, nil
}

type touchpadAcquisitionOwner struct {
	roots       *pointerRoots
	observation *inputObservation
	closed      bool
	closeErr    error
}

func (owner *touchpadAcquisitionOwner) Close() error {
	if owner.closed {
		return owner.closeErr
	}
	owner.closed = true // No retry of an uncertain Close.
	failed := false
	if owner.observation != nil {
		failed = closeInputObservation(owner.observation) != nil
		owner.observation = nil
	}
	if owner.roots != nil {
		failed = owner.roots.close() != nil || failed
		owner.roots = nil
	}
	if failed {
		owner.closeErr = errInputClose
	}
	return owner.closeErr
}

func openTouchpadWithOperations(ctx context.Context, path, name string, operations pointerOperations) (result touchpadOpened, err error) {
	if !pointerInterface(name) || !touchpadLinuxABI() {
		return result, errInputIdentity
	}
	operations = touchpadOwnedOperations(operations)
	roots, err := openPointerRoots(ctx, path, true, operations)
	if roots == nil {
		return result, err
	}
	owner := &touchpadAcquisitionOwner{roots: roots}
	result.owner = owner // Transfer even when a subsequent step returns an error.
	defer func() {
		// Only clean absence is an inconclusive unreadable row. Its already
		// acquired roots must close before returning the core's empty sentinel.
		if err == errInputUnavailable {
			err = owner.Close()
			if err == nil {
				err = errInputUnavailable
			}
			result = touchpadOpened{}
		}
	}()
	if err != nil {
		return result, err
	}
	expected, err := inputRegistryNumber(ctx, roots.sys, operations.input, name)
	if err != nil {
		return result, err
	}
	owner.observation, err = openInputObservation(ctx, roots.root, operations.input, "dev/input/"+name, &expected)
	if err != nil {
		return result, err
	}
	query := func(request uintptr, data []byte) (int, error) {
		if owner.closed || owner.observation == nil {
			return 0, errInputIdentity
		}
		return operations.query(owner.observation.reader, request, data)
	}
	snapshot := func(checkContext context.Context) (touchpadMetadata, error) {
		if owner.closed || owner.observation == nil {
			return touchpadMetadata{}, errInputIdentity
		}
		if err := validateInputObservation(checkContext, roots.root, owner.observation); err != nil {
			return touchpadMetadata{}, err
		}
		number, err := inputRegistryNumber(checkContext, roots.sys, operations.input, name)
		if err != nil {
			return touchpadMetadata{}, err
		}
		if number != expected {
			return touchpadMetadata{}, errInputIdentity
		}
		metadata, err := queryTouchpadMetadata(checkContext, query)
		if err != nil {
			return touchpadMetadata{}, err
		}
		if err := validateInputObservation(checkContext, roots.root, owner.observation); err != nil {
			return touchpadMetadata{}, err
		}
		return metadata, nil
	}
	metadata, err := queryTouchpadMetadata(ctx, query)
	if err != nil {
		return result, err
	}
	current, err := snapshot(ctx)
	if err != nil {
		return result, err
	}
	if !sameTouchpadMetadata(metadata, current) {
		return result, errInputIdentity
	}
	result.metadata = metadata
	result.eventSize, result.typeOffset = int(unsafe.Sizeof(inputEvent{})), int(unsafe.Offsetof(inputEvent{}.Type))
	result.snapshot = func() (touchpadMetadata, error) { return snapshot(ctx) }
	result.finalSnapshot = func() (touchpadMetadata, error) { return snapshot(context.WithoutCancel(ctx)) }
	result.neutral = func() (bool, error) { return queryTouchpadNeutral(query) }
	result.read = func(data []byte) (int, error) {
		if owner.closed || owner.observation == nil {
			return 0, errInputStream
		}
		count, err := owner.observation.reader.Read(data)
		if count == 0 && (err == syscall.EAGAIN || err == syscall.EWOULDBLOCK) {
			return 0, errTouchpadIdle
		}
		if count == 0 && err == syscall.EINTR {
			return 0, errTouchpadRetry
		}
		return count, err
	}
	return result, nil
}
