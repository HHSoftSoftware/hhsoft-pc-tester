//go:build linux

package inputtest

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"unsafe"
)

// Pointer acquisition uses the same owned O_PATH and bounded metadata pipeline
// as input readiness. Only the pinned readable event descriptor receives the
// existing read-only metadata ioctl allowlist.
type pointerOperations struct {
	input inputReadinessOperations
	query func(inputReadableFile, uintptr, []byte) (int, error)
}

func systemPointerOperations() pointerOperations {
	return pointerOperations{input: systemInputReadinessOperations(), query: func(file inputReadableFile, request uintptr, data []byte) (int, error) {
		descriptor, ok := file.(interface{ Fd() uintptr })
		if !ok || nilInputValue(descriptor) || len(data) == 0 {
			return 0, errInputAcquire
		}
		count, _, errno := syscall.Syscall(syscall.SYS_IOCTL, descriptor.Fd(), request, uintptr(unsafe.Pointer(&data[0])))
		if errno != 0 {
			return 0, errno
		}
		return int(count), nil
	}}
}

type pointerRoots struct{ root, sys inputReadinessRoot }

func (owner *pointerRoots) close() error {
	failed := false
	if !nilInputValue(owner.sys) {
		failed = owner.sys.Close() != nil
		owner.sys = nil
	}
	if !nilInputValue(owner.root) {
		failed = owner.root.Close() != nil || failed
		owner.root = nil
	}
	if failed {
		return errInputClose
	}
	return nil
}

func openPointerRoots(ctx context.Context, path string, withSys bool, operations pointerOperations) (*pointerRoots, error) {
	if nilInputValue(ctx) || operations.input.openRoot == nil || operations.input.openReadable == nil || operations.query == nil || !filepath.IsAbs(path) || len(path) > 4096 || strings.ContainsRune(path, 0) {
		return nil, errInputAcquire
	}
	if err := pointerContextError(ctx); err != nil {
		return nil, err
	}
	root, err := operations.input.openRoot(path)
	if nilInputValue(root) {
		return nil, errInputAcquire
	}
	owner := &pointerRoots{root: root}
	// Return acquired owners even on error; the caller closes them once.
	if err != nil {
		return owner, errInputAcquire
	}
	if err := pointerContextError(ctx); err != nil {
		return owner, err
	}
	if !withSys {
		return owner, nil
	}
	metadata, err := root.Lstat("sys")
	if err != nil || nilInputValue(metadata) || !metadata.IsDir() || metadata.Mode()&os.ModeSymlink != 0 {
		return owner, errInputIdentity
	}
	if err := pointerContextError(ctx); err != nil {
		return owner, err
	}
	owner.sys, err = root.OpenRoot("sys")
	if nilInputValue(owner.sys) || err != nil {
		return owner, errInputAcquire
	}
	if err := pointerContextError(ctx); err != nil {
		return owner, err
	}
	return owner, nil
}

func discoverPointerWithOperations(ctx context.Context, path string, operations pointerOperations) (names []string, err error) {
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
	data, err := acquireInputMetadata(ctx, owner.root, operations.input, "proc/bus/input/devices", maximumInputRegistryBytes, 4096)
	if err != nil {
		return nil, err
	}
	names, err = parsePointerInterfaces(strings.NewReader(string(data)))
	if err != nil {
		return nil, errInputRead
	}
	return names, nil
}

func openPointerWithOperations(ctx context.Context, path, name string, operations pointerOperations) (result pointerOpened, err error) {
	if !pointerInterface(name) {
		return result, errInputIdentity
	}
	owner, err := openPointerRoots(ctx, path, true, operations)
	var observation *inputObservation
	accepted := false
	closeOwned := func() error {
		failed := false
		if observation != nil {
			failed = closeInputObservation(observation) != nil
			observation = nil
		}
		if owner != nil {
			failed = owner.close() != nil || failed
		}
		if failed {
			return errInputClose
		}
		return nil
	}
	defer func() {
		if !accepted {
			if closeErr := closeOwned(); closeErr != nil {
				result, err = pointerOpened{}, closeErr
			}
		}
	}()
	if err != nil {
		return result, err
	}
	expected, err := inputRegistryNumber(ctx, owner.sys, operations.input, name)
	if err != nil {
		return result, err
	}
	observation, err = openInputObservation(ctx, owner.root, operations.input, "dev/input/"+name, &expected)
	if err != nil {
		return result, err
	}
	query := func(request uintptr, data []byte) (int, error) {
		return operations.query(observation.reader, request, data)
	}
	identity, capabilities, err := queryPointerMetadata(query)
	if err != nil {
		return result, errInputIdentity
	}
	validate := func() error {
		if observation == nil {
			return errInputIdentity
		}
		if err := validateInputObservation(ctx, owner.root, observation); err != nil {
			return err
		}
		number, err := inputRegistryNumber(ctx, owner.sys, operations.input, name)
		if err != nil {
			return err
		}
		if number != expected {
			return errInputIdentity
		}
		currentIdentity, currentCapabilities, err := queryPointerMetadata(query)
		if err != nil || currentIdentity != identity || !reflect.DeepEqual(currentCapabilities, capabilities) {
			return errInputIdentity
		}
		return pointerContextError(ctx)
	}
	if err := validate(); err != nil {
		return result, err
	}
	result = pointerOpened{identity: identity, capabilities: capabilities,
		eventSize: int(unsafe.Sizeof(inputEvent{})), typeOffset: int(unsafe.Offsetof(inputEvent{}.Type)),
		validate: validate, close: closeOwned,
		read: func(data []byte) (int, error) {
			if observation == nil {
				return 0, errInputStream
			}
			count, err := observation.reader.Read(data)
			// Do not let errors.Is erase a joined hard error or a partial read.
			if count == 0 && (err == syscall.EAGAIN || err == syscall.EWOULDBLOCK) {
				return 0, errPointerIdle
			}
			if count == 0 && err == syscall.EINTR {
				return 0, errPointerRetry
			}
			return count, err
		}}
	accepted = true
	return result, nil
}
