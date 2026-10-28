//go:build linux

package inputtest

import (
	"context"
	"reflect"
	"strings"
	"syscall"
	"unsafe"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

// The owned input primitives prove node identity with O_PATH before a readable
// open. Keyboard queries retain their existing numeric read-only allowlist.
func discoverKeyboardWithOperations(ctx context.Context, path string, operations pointerOperations) (names []string, err error) {
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
	data, err := acquireInputMetadata(ctx, owner.root, operations.input, "proc/bus/input/devices", maximumInputRegistryBytes, 4096)
	if err != nil {
		return nil, err
	}
	names, err = parseFunctionalInputInterfaces(strings.NewReader(string(data)), "keyboard", width)
	if err != nil {
		return nil, errInputRead
	}
	return names, nil
}

func openKeyboardWithOperations(ctx context.Context, path, name string, operations pointerOperations) (result keyboardOpened, err error) {
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
				result, err = keyboardOpened{}, closeErr
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
	observed, err := queryKeyboardMetadata(query)
	if err != nil {
		return result, errInputIdentity
	}
	validate := func(checkContext context.Context) error {
		if observation == nil {
			return errInputIdentity
		}
		if err := validateInputObservation(checkContext, owner.root, observation); err != nil {
			return err
		}
		number, err := inputRegistryNumber(checkContext, owner.sys, operations.input, name)
		if err != nil {
			return err
		}
		if number != expected {
			return errInputIdentity
		}
		latest, err := queryKeyboardMetadata(query)
		if err != nil || !reflect.DeepEqual(latest, observed) {
			return errInputIdentity
		}
		return pointerContextError(checkContext)
	}
	if err := validate(ctx); err != nil {
		return result, err
	}
	result = keyboardOpened{identity: observed.identity, capabilities: observed.capabilities,
		eventSize: int(unsafe.Sizeof(inputEvent{})), typeOffset: int(unsafe.Offsetof(inputEvent{}.Type)),
		validate: func() error { return validate(ctx) },
		// Only the final owned completion recheck uses this callback. It never
		// opens another event endpoint, waits, or credits additional key actions.
		// Selected sysfs metadata is reopened with bounded reads and checked
		// closure under the already-owned root solely for this closing proof.
		finalValidate: func() error { return validate(context.WithoutCancel(ctx)) },
		neutral:       func() (bool, error) { return queryKeyboardNeutral(query) },
		close:         closeOwned,
		read: func(data []byte) (int, error) {
			if observation == nil {
				return 0, errInputStream
			}
			count, err := observation.reader.Read(data)
			if count == 0 && (err == syscall.EAGAIN || err == syscall.EWOULDBLOCK) {
				return 0, errKeyboardIdle
			}
			if count == 0 && err == syscall.EINTR {
				return 0, errKeyboardRetry
			}
			return count, err
		}}
	accepted = true
	return result, nil
}
