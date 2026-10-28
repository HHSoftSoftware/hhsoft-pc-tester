//go:build linux

package inputtest

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"sort"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func runPointerPlatform(ctx context.Context, options PointerOptions) PointerResult {
	return runPointerWith(ctx, options, pointerBackend{discover: discoverPointerInterfaces, open: openPointerInterface})
}

func discoverPointerInterfaces(ctx context.Context, root string) ([]string, error) {
	return discoverPointerWithOperations(ctx, root, systemPointerOperations())
}

func parsePointerInterfaces(reader io.Reader) ([]string, error) {
	return parseFunctionalInputInterfaces(reader, "pointer", 0)
}

func parseFunctionalInputInterfaces(reader io.Reader, wanted string, wordBits int) ([]string, error) {
	var result []string
	seen := map[string]bool{}
	fields := map[string]string{}
	flush := func() error {
		category := inventory.ClassifyInput(fields["name"], fields["handlers"], fields["KEY"], fields["REL"], fields["ABS"], fields["PROP"])
		if wanted == "keyboard" {
			category = inventory.ClassifyInputWithBitmapWidth(fields["name"], fields["handlers"], fields["KEY"], fields["REL"], fields["ABS"], fields["PROP"], wordBits)
		}
		hasEvent := false
		for _, handler := range strings.Fields(fields["handlers"]) {
			if !strings.HasPrefix(handler, "event") {
				continue
			}
			if !pointerInterface(handler) || seen[handler] {
				return errors.New("invalid or duplicate input event interface")
			}
			seen[handler] = true
			hasEvent = true
			if category == wanted {
				result = append(result, handler)
				if len(result) > maximumPointerDevices {
					return errors.New("pointer registry exceeds device limit")
				}
			}
		}
		if category == wanted && !hasEvent {
			return errors.New("pointer registry record has no event interface")
		}
		fields = map[string]string{}
		return nil
	}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		var key, value string
		switch {
		case strings.HasPrefix(line, "N: Name="):
			key, value = "name", strings.Trim(strings.TrimPrefix(line, "N: Name="), "\"")
		case strings.HasPrefix(line, "H: Handlers="):
			key, value = "handlers", strings.TrimPrefix(line, "H: Handlers=")
		case strings.HasPrefix(line, "B: "):
			key, value, _ = strings.Cut(strings.TrimPrefix(line, "B: "), "=")
		}
		if key != "" {
			if _, exists := fields[key]; exists {
				return nil, errors.New("duplicate input registry field")
			}
			fields[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	sort.Strings(result)
	return result, nil
}

func openPointerInterface(ctx context.Context, root, name string) (pointerOpened, error) {
	return openPointerWithOperations(ctx, root, name, systemPointerOperations())
}

// Read-only Linux input UAPI requests from include/uapi/linux/input.h. No
// grab, clock, key-map, or other device-setting ioctl is issued.
func queryPointerMetadata(query func(uintptr, []byte) (int, error)) (PointerIdentity, PointerCapabilities, error) {
	var identity PointerIdentity
	capabilities := PointerCapabilities{Buttons: []string{}, WheelAxes: []PointerWheelCapability{}}
	request := func(number byte, bytes []byte) (int, error) {
		return query(uintptr(0x80000000|uint32(len(bytes))<<16|uint32('E')<<8|uint32(number)), bytes)
	}
	version := make([]byte, 4)
	if count, err := request(0x01, version); err != nil || count != 0 || binary.NativeEndian.Uint32(version) != 0x010001 {
		return identity, capabilities, errors.New("descriptor is not a supported evdev interface")
	}
	id := make([]byte, 8)
	if count, err := request(0x02, id); err != nil || count != 0 {
		return identity, capabilities, errors.New("cannot establish evdev numeric identity")
	}
	identity = PointerIdentity{binary.NativeEndian.Uint16(id), binary.NativeEndian.Uint16(id[2:]), binary.NativeEndian.Uint16(id[4:]), binary.NativeEndian.Uint16(id[6:])}
	events, keys, relative := make([]byte, 8), make([]byte, 96), make([]byte, 8)
	if count, err := request(0x20, events); err != nil || count < 1 || count > len(events) {
		return identity, capabilities, errors.New("cannot establish evdev event capabilities")
	}
	capabilities.Key, capabilities.Relative, capabilities.Absolute = events[0]&2 != 0, events[0]&4 != 0, events[0]&8 != 0
	if capabilities.Key {
		if count, err := request(0x21, keys); err != nil || count < 35 || count > len(keys) {
			return identity, capabilities, errors.New("cannot establish evdev primary buttons")
		}
		for index, name := range pointerPrimaryButtons {
			code := 0x110 + index
			if keys[code/8]&(1<<uint(code%8)) != 0 {
				capabilities.Buttons = append(capabilities.Buttons, name)
			}
		}
	}
	if capabilities.Relative {
		if count, err := request(0x22, relative); err != nil || count < 2 || count > len(relative) {
			return identity, capabilities, errors.New("cannot establish evdev relative axes")
		}
		capabilities.RelativeX, capabilities.RelativeY = relative[0]&1 != 0, relative[0]&2 != 0
		for _, wheel := range []PointerWheelCapability{
			{Axis: "vertical", Legacy: relative[1]&1 != 0, HighResolution: relative[1]&8 != 0},
			{Axis: "horizontal", Legacy: relative[0]&64 != 0, HighResolution: relative[1]&16 != 0},
		} {
			if wheel.Legacy || wheel.HighResolution {
				capabilities.WheelAxes = append(capabilities.WheelAxes, wheel)
			}
		}
	}
	return identity, capabilities, nil
}
