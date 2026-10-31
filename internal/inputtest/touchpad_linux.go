//go:build linux

package inputtest

import (
	"context"
	"encoding/binary"
	"runtime"
	"strconv"
	"unsafe"
)

// These layouts and generic read-only requests are limited to the named Go
// targets. Compiling the vectors does not qualify a deployed kernel or device.
func touchpadLinuxABI() bool {
	if binary.NativeEndian.Uint16([]byte{1, 0}) != 1 {
		return false
	}
	size, offset := unsafe.Sizeof(inputEvent{}), unsafe.Offsetof(inputEvent{}.Type)
	if unsafe.Offsetof(inputEvent{}.Code) != offset+2 || unsafe.Offsetof(inputEvent{}.Value) != offset+4 {
		return false
	}
	switch runtime.GOARCH {
	case "amd64", "arm64":
		return strconv.IntSize == 64 && size == 24 && offset == 16
	case "386":
		return strconv.IntSize == 32 && size == 16 && offset == 8
	default:
		return false
	}
}

func runTouchpadPlatform(ctx context.Context, options TouchpadOptions) TouchpadResult {
	if !touchpadLinuxABI() {
		return unsupportedTouchpadPlatform(ctx, options)
	}
	return runTouchpadWith(ctx, options, touchpadBackend{discover: discoverTouchpadInterfaces, open: openTouchpadInterface})
}

func discoverTouchpadInterfaces(ctx context.Context, root string) ([]string, error) {
	return discoverTouchpadWithOperations(ctx, root, systemPointerOperations())
}

func openTouchpadInterface(ctx context.Context, root, name string) (touchpadOpened, error) {
	return openTouchpadWithOperations(ctx, root, name, systemPointerOperations())
}

// A query failure is an observed identity/protocol failure even when the same
// callback cancels the context. Copy its buffer before consulting any callback.
func touchpadQuery(ctx context.Context, query func(uintptr, []byte) (int, error), request uintptr, size, want int) ([]byte, error) {
	if nilInputValue(ctx) || query == nil || size < 1 || size > 96 {
		return nil, errInputIdentity
	}
	if stopped := pointerContextError(ctx); stopped != nil {
		return nil, stopped
	}
	buffer := make([]byte, size)
	count, err := query(request, buffer)
	if err != nil || count != want {
		return nil, errInputIdentity
	}
	owned := append([]byte{}, buffer...)
	if stopped := pointerContextError(ctx); stopped != nil {
		return nil, stopped
	}
	return owned, nil
}

func touchpadBitmap(data []byte, extent int) []uint16 {
	result := []uint16{}
	for index, value := range data[:extent] {
		for bit := uint(0); bit < 8; bit++ {
			if value&(1<<bit) != 0 {
				result = append(result, uint16(index*8)+uint16(bit))
			}
		}
	}
	return result
}

func queryTouchpadMetadata(ctx context.Context, query func(uintptr, []byte) (int, error)) (touchpadMetadata, error) {
	var result touchpadMetadata
	if !touchpadLinuxABI() {
		return result, errInputIdentity
	}
	version, err := touchpadQuery(ctx, query, 0x80044501, 4, 0)
	if err != nil {
		return result, err
	}
	result.capabilities.ProtocolVersion = int32(binary.NativeEndian.Uint32(version))
	id, err := touchpadQuery(ctx, query, 0x80084502, 8, 0)
	if err != nil {
		return result, err
	}
	result.identity = TouchpadIdentity{binary.NativeEndian.Uint16(id), binary.NativeEndian.Uint16(id[2:]), binary.NativeEndian.Uint16(id[4:]), binary.NativeEndian.Uint16(id[6:])}
	if result.capabilities.ProtocolVersion != 0x010001 {
		return result, nil // A readable unsupported descriptor cannot gain credit.
	}
	wordBytes := strconv.IntSize / 8 // ioctl caller ABI, not proc bitmap width.
	var absolute []uint16
	for _, row := range []struct {
		request        uintptr
		size, returned int
		destination    *[]uint16
	}{
		{0x80084520, 8, wordBytes, &result.capabilities.Events},
		{0x80604521, 96, 96, &result.capabilities.Keys},
		{0x80084522, 8, wordBytes, &result.capabilities.Relative},
		{0x80084523, 8, 8, &absolute},
		{0x80084509, 8, wordBytes, &result.capabilities.Properties},
		{0x80084524, 8, wordBytes, &result.capabilities.Misc},
	} {
		data, err := touchpadQuery(ctx, query, row.request, row.size, row.returned)
		if err != nil {
			return touchpadMetadata{}, err
		}
		// The whole authenticated returned extent is decoded, including unknown
		// bits. Unreturned padding never becomes capability evidence.
		*row.destination = touchpadBitmap(data, row.returned)
	}
	result.capabilities.Axes = []TouchpadAxis{}
	for _, code := range absolute {
		axis := TouchpadAxis{Code: code}
		if code == 0 || code == 1 || code == 24 || code == 28 {
			data, err := touchpadQuery(ctx, query, 0x80184540+uintptr(code), 24, 0)
			if err != nil {
				return touchpadMetadata{}, err
			}
			// data[0:4] is dynamic value, deliberately not evidence or identity.
			axis.Minimum = int32(binary.NativeEndian.Uint32(data[4:]))
			axis.Maximum = int32(binary.NativeEndian.Uint32(data[8:]))
			axis.Fuzz = int32(binary.NativeEndian.Uint32(data[12:]))
			axis.Flat = int32(binary.NativeEndian.Uint32(data[16:]))
			axis.Resolution = int32(binary.NativeEndian.Uint32(data[20:]))
		}
		// Unsupported axes are retained with their code; they are never queried
		// or silently removed to manufacture a supported subset.
		result.capabilities.Axes = append(result.capabilities.Axes, axis)
	}
	return result, nil
}

// This same-client getter may compact EV_KEY records. Only the core's baseline
// and noncrediting terminal phases may call it. Static snapshots never do.
func queryTouchpadNeutral(query func(uintptr, []byte) (int, error)) (bool, error) {
	data, err := touchpadQuery(context.Background(), query, 0x80604518, 96, 96)
	if err != nil {
		return false, err
	}
	for _, value := range data {
		if value != 0 {
			return false, nil
		}
	}
	return true, nil
}
