//go:build linux

package inputtest

import (
	"context"
	"encoding/binary"
	"errors"
)

func runKeyboardPlatform(ctx context.Context, options KeyboardOptions) KeyboardResult {
	return runKeyboardWith(ctx, options, keyboardBackend{discover: discoverKeyboardInterfaces, open: openKeyboardInterface})
}

func discoverKeyboardInterfaces(ctx context.Context, root string) ([]string, error) {
	return discoverKeyboardWithOperations(ctx, root, systemPointerOperations())
}

type keyboardMetadata struct {
	identity     KeyboardIdentity
	capabilities KeyboardCapabilities
	events, keys []byte
}

func keyboardRequest(query func(uintptr, []byte) (int, error), number byte, data []byte) (int, error) {
	return query(uintptr(0x80000000|uint32(len(data))<<16|uint32('E')<<8|uint32(number)), data)
}

func queryKeyboardMetadata(query func(uintptr, []byte) (int, error)) (keyboardMetadata, error) {
	var result keyboardMetadata
	result.capabilities.FixedKeys = []string{}
	version, identity := make([]byte, 4), make([]byte, 8)
	if count, err := keyboardRequest(query, 0x01, version); err != nil || count != 0 || binary.NativeEndian.Uint32(version) != 0x010001 {
		return result, errors.New("unsupported input interface")
	}
	if count, err := keyboardRequest(query, 0x02, identity); err != nil || count != 0 {
		return result, errors.New("input identity unavailable")
	}
	result.identity = KeyboardIdentity{binary.NativeEndian.Uint16(identity), binary.NativeEndian.Uint16(identity[2:]), binary.NativeEndian.Uint16(identity[4:]), binary.NativeEndian.Uint16(identity[6:])}
	result.events, result.keys = make([]byte, 8), make([]byte, 96)
	if count, err := keyboardRequest(query, 0x20, result.events); err != nil || count < 1 || count > len(result.events) || result.events[0]&1 == 0 {
		return result, errors.New("input event capabilities unavailable")
	}
	result.capabilities.EventKey = result.events[0]&2 != 0
	if result.capabilities.EventKey {
		if count, err := keyboardRequest(query, 0x21, result.keys); err != nil || count < 14 || count > len(result.keys) {
			return result, errors.New("input key capabilities unavailable")
		}
		names := KeyboardActions()
		for index, code := range keyboardCodes {
			if result.keys[code/8]&(1<<uint(code%8)) != 0 {
				result.capabilities.FixedKeys = append(result.capabilities.FixedKeys, names[index])
			}
		}
	}
	return result, nil
}

func queryKeyboardNeutral(query func(uintptr, []byte) (int, error)) (bool, error) {
	keys := make([]byte, 96)
	if count, err := keyboardRequest(query, 0x18, keys); err != nil || count < 14 || count > len(keys) {
		return false, errors.New("input key state unavailable")
	}
	for _, code := range keyboardCodes {
		if keys[code/8]&(1<<uint(code%8)) != 0 {
			return false, nil
		}
	}
	return true, nil
}

func openKeyboardInterface(ctx context.Context, root, name string) (keyboardOpened, error) {
	return openKeyboardWithOperations(ctx, root, name, systemPointerOperations())
}
