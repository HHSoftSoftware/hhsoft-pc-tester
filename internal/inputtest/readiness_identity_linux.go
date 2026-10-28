//go:build linux

package inputtest

import (
	"context"
	"os"
	"strings"
	"syscall"
)

type inputFileIdentity struct {
	device, inode, rdev uint64
	mode                os.FileMode
}

func inputIdentity(info os.FileInfo, character bool) (inputFileIdentity, bool) {
	if nilInputValue(info) {
		return inputFileIdentity{}, false
	}
	mode := info.Mode()
	if mode&os.ModeSymlink != 0 || character && mode&os.ModeType != os.ModeDevice|os.ModeCharDevice || !character && !mode.IsRegular() {
		return inputFileIdentity{}, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return inputFileIdentity{}, false
	}
	return inputFileIdentity{uint64(stat.Dev), uint64(stat.Ino), uint64(stat.Rdev), mode}, true
}

func (identity inputFileIdentity) number() inputDeviceNumber {
	value := identity.rdev
	return inputDeviceNumber{uint32(value>>8&0xfff | value>>32&0xfffff000), uint32(value&0xff | value>>12&0xffffff00)}
}

type inputObservation struct {
	path      inputPathFile
	reader    inputReadableFile
	identity  inputFileIdentity
	name      string
	character bool
}

func closeInputObservation(observation *inputObservation) error {
	var failed bool
	if !nilInputValue(observation.reader) {
		failed = observation.reader.Close() != nil
		observation.reader = nil
	}
	if !nilInputValue(observation.path) {
		failed = observation.path.Close() != nil || failed
		observation.path = nil
	}
	if failed {
		return errInputClose
	}
	return nil
}

func openInputObservation(ctx context.Context, root inputReadinessRoot, operations inputReadinessOperations, name string, expected *inputDeviceNumber) (result *inputObservation, err error) {
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	if len(name) == 0 || len(name) > 4096 || strings.HasPrefix(name, "/") || strings.ContainsRune(name, 0) {
		return nil, errInputAcquire
	}
	character := expected != nil
	before, statErr := root.Lstat(name)
	if statErr != nil {
		if before == nil && character && inputAvailabilityError(statErr, "statat", true) {
			if stopped := ctx.Err(); stopped != nil {
				return nil, stopped
			}
			return nil, errInputUnavailable
		}
		return nil, errInputAcquire
	}
	identity, valid := inputIdentity(before, character)
	if !valid || character && identity.number() != *expected {
		return nil, errInputIdentity
	}
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	path, openErr := root.OpenPath(name, inputOpenPath|syscall.O_NOFOLLOW|syscall.O_CLOEXEC)
	if nilInputValue(path) {
		if path == nil && character && inputAvailabilityError(openErr, "openat", true) {
			if stopped := ctx.Err(); stopped != nil {
				return nil, stopped
			}
			return nil, errInputUnavailable
		}
		return nil, errInputAcquire
	}
	owned := &inputObservation{path: path, identity: identity, name: name, character: character}
	defer func() {
		if result != nil {
			return
		}
		if closeErr := closeInputObservation(owned); closeErr != nil {
			err = closeErr
		}
	}()
	if openErr != nil {
		return nil, errInputAcquire
	}
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	opened, statErr := path.Stat()
	actual, valid := inputIdentity(opened, character)
	if statErr != nil || !valid || actual != identity {
		return nil, errInputIdentity
	}
	// The metadata-only handle is verified before a read-only open can invoke
	// any device driver. The second open names only this pinned descriptor.
	if err := validateInputObservation(ctx, root, owned); err != nil {
		return nil, err
	}
	reader, readOpenErr := operations.openReadable(path)
	owned.reader = reader
	if nilInputValue(reader) {
		if reader == nil && character && inputAvailabilityError(readOpenErr, "open", false) {
			if stopped := ctx.Err(); stopped != nil {
				return nil, stopped
			}
			return nil, errInputUnavailable
		}
		return nil, errInputAcquire
	}
	if readOpenErr != nil {
		return nil, errInputAcquire
	}
	if err := validateInputObservation(ctx, root, owned); err != nil {
		return nil, err
	}
	return owned, nil
}

func validateInputObservation(ctx context.Context, root inputReadinessRoot, observation *inputObservation) error {
	if stopped := ctx.Err(); stopped != nil {
		return stopped
	}
	metadata, err := observation.path.Stat()
	identity, valid := inputIdentity(metadata, observation.character)
	if err != nil || !valid || identity != observation.identity {
		return errInputIdentity
	}
	if stopped := ctx.Err(); stopped != nil {
		return stopped
	}
	if !nilInputValue(observation.reader) {
		metadata, err = observation.reader.Stat()
		identity, valid = inputIdentity(metadata, observation.character)
		if err != nil || !valid || identity != observation.identity {
			return errInputIdentity
		}
		if stopped := ctx.Err(); stopped != nil {
			return stopped
		}
	}
	metadata, err = root.Lstat(observation.name)
	identity, valid = inputIdentity(metadata, observation.character)
	if err != nil || !valid || identity != observation.identity {
		return errInputIdentity
	}
	return ctx.Err()
}

func acquireInputMetadata(ctx context.Context, root inputReadinessRoot, operations inputReadinessOperations, name string, limit, batch int) (data []byte, err error) {
	observation, err := openInputObservation(ctx, root, operations, name, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := closeInputObservation(observation); closeErr != nil {
			data, err = nil, closeErr
		} else if err != nil {
			data = nil
		} else if stopped := ctx.Err(); stopped != nil {
			data, err = nil, stopped
		}
	}()
	data, err = readInputBytes(ctx, observation.reader, limit, batch)
	if err != nil {
		return nil, err
	}
	if err := validateInputObservation(ctx, root, observation); err != nil {
		return nil, err
	}
	return data, nil
}

func inputRegistryNumber(ctx context.Context, sys inputReadinessRoot, operations inputReadinessOperations, name string) (inputDeviceNumber, error) {
	if !eventInterfacePattern.MatchString(name) || len(name) > 255 {
		return inputDeviceNumber{}, errInputIdentity
	}
	data, err := acquireInputMetadata(ctx, sys, operations, "class/input/"+name+"/dev", maximumInputIdentityBytes, 32)
	if err != nil {
		return inputDeviceNumber{}, err
	}
	number, valid := parseInputDeviceNumber(data)
	if !valid || !inputEvdevNumber(number) {
		return inputDeviceNumber{}, errInputIdentity
	}
	return number, nil
}

func finishInputEvent(ctx context.Context, root, sys inputReadinessRoot, operations inputReadinessOperations, observation *inputObservation, name string) (err error) {
	defer func() {
		if closeErr := closeInputObservation(observation); closeErr != nil {
			err = closeErr
		} else if err == nil {
			err = ctx.Err()
		}
	}()
	if err := validateInputObservation(ctx, root, observation); err != nil {
		return err
	}
	number, err := inputRegistryNumber(ctx, sys, operations, name)
	if err != nil {
		return err
	}
	if number != observation.identity.number() {
		return errInputIdentity
	}
	return validateInputObservation(ctx, root, observation)
}
