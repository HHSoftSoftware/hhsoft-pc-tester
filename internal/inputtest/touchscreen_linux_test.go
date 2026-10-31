//go:build linux

package inputtest

import (
	"encoding/binary"
	"errors"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

// These literal request/size/reply vectors do not call ioctl or open a device.
func touchscreenLinuxReply(t *testing.T, request uintptr, data []byte) (int, error) {
	t.Helper()
	word := 8
	if runtime.GOARCH == "386" {
		word = 4
	}
	switch request {
	case 0x80044501:
		if len(data) != 4 {
			t.Fatal("version request size")
		}
		binary.NativeEndian.PutUint32(data, 0x010001)
		return 0, nil
	case 0x80084502:
		if len(data) != 8 {
			t.Fatal("identity request size")
		}
		copy(data, []byte{3, 0, 1, 0, 2, 0, 1, 0})
		return 0, nil
	case 0x80084520:
		if len(data) != 8 {
			t.Fatal("events request size")
		}
		data[0] = 0x0b
		return word, nil
	case 0x80604521:
		if len(data) != 96 {
			t.Fatal("keys request size")
		}
		data[41] = 4 // BTN_TOUCH = 330.
		return 96, nil
	case 0x80084522, 0x80084524:
		if len(data) != 8 {
			t.Fatal("relative or misc request size")
		}
		return word, nil
	case 0x80084523:
		if len(data) != 8 {
			t.Fatal("ABS bitmap request size")
		}
		data[0] = 3
		return 8, nil
	case 0x80084509:
		if len(data) != 8 {
			t.Fatal("property request size")
		}
		data[0] = 2
		return word, nil
	case 0x80184540, 0x80184541:
		if len(data) != 24 {
			t.Fatal("ABS information request size")
		}
		binary.NativeEndian.PutUint32(data, 99)
		binary.NativeEndian.PutUint32(data[4:], 0xffffff9c)
		binary.NativeEndian.PutUint32(data[8:], 100)
		return 0, nil
	case 0x80604518:
		if len(data) != 96 {
			t.Fatal("key getter size")
		}
		return 96, nil
	default:
		t.Fatal("query escaped the fixed read-only request set")
		return -1, errors.New("unreachable query")
	}
}

func TestTouchscreenLinuxLiteralABIAndQueryPhases(t *testing.T) {
	if !touchscreenLinuxABI() {
		t.Fatal("unqualified compiled input ABI")
	}
	size, offset := uintptr(24), uintptr(16)
	if runtime.GOARCH == "386" {
		size, offset = 16, 8
	}
	if unsafe.Sizeof(inputEvent{}) != size || unsafe.Offsetof(inputEvent{}.Type) != offset ||
		unsafe.Offsetof(inputEvent{}.Code) != offset+2 || unsafe.Offsetof(inputEvent{}.Value) != offset+4 ||
		unsafe.Sizeof(inputEvent{}.Value) != 4 || binary.NativeEndian.Uint32([]byte{1, 0, 0, 0}) != 1 {
		t.Fatal("compiled record layout differs from literal UAPI vectors")
	}
	var requests []uintptr
	query := func(r uintptr, p []byte) (int, error) {
		requests = append(requests, r)
		return touchscreenLinuxReply(t, r, p)
	}
	m, err := queryTouchscreenMetadata(t.Context(), query)
	want := []uintptr{0x80044501, 0x80084502, 0x80084520, 0x80604521, 0x80084522, 0x80084523, 0x80084509, 0x80084524, 0x80184540, 0x80184541}
	if err != nil || !reflect.DeepEqual(requests, want) ||
		!reflect.DeepEqual(m.capabilities, touchscreenTestCapabilities()) || m.identity != (TouchscreenIdentity{3, 1, 2, 1}) {
		t.Fatal("literal metadata vector or getter isolation mismatch")
	}
	neutral, err := queryTouchscreenNeutral(query)
	if err != nil || !neutral || len(requests) != 11 || requests[10] != 0x80604518 {
		t.Fatal("neutral query was not the separate key-state getter")
	}
	other, err := queryTouchscreenMetadata(t.Context(), func(r uintptr, p []byte) (int, error) {
		n, err := touchscreenLinuxReply(t, r, p)
		if r == 0x80184540 || r == 0x80184541 {
			binary.NativeEndian.PutUint32(p, 0x80000000)
		}
		return n, err
	})
	if err != nil || !sameTouchscreenMetadata(m, other) {
		t.Fatal("dynamic ioctl coordinate entered static action authority")
	}
}

func TestTouchscreenLinuxEveryQueryCountIsExact(t *testing.T) {
	for _, row := range []struct {
		name    string
		request uintptr
	}{
		{"version", 0x80044501}, {"identity", 0x80084502}, {"events", 0x80084520},
		{"keys", 0x80604521}, {"relative", 0x80084522}, {"absolute", 0x80084523},
		{"properties", 0x80084509}, {"misc", 0x80084524}, {"x", 0x80184540},
		{"y", 0x80184541}, {"neutral", 0x80604518},
	} {
		for _, fault := range []string{"short", "long", "negative", "error"} {
			t.Run(row.name+"_"+fault, func(t *testing.T) {
				seen, after := false, false
				query := func(r uintptr, p []byte) (int, error) {
					if seen {
						after = true
					}
					n, err := touchscreenLinuxReply(t, r, p)
					if r == row.request {
						seen = true
						switch fault {
						case "short":
							n--
						case "long":
							n++
						case "negative":
							n = -1
						case "error":
							err = errors.New("unexported provider detail")
						}
					}
					return n, err
				}
				var err error
				if row.name == "neutral" {
					_, err = queryTouchscreenNeutral(query)
				} else {
					_, err = queryTouchscreenMetadata(t.Context(), query)
				}
				if !seen || after || err != errInputIdentity {
					t.Fatal("query count/error did not fail closed immediately")
				}
			})
		}
	}
}

func TestTouchscreenLinuxUnsupportedBitsRemainVisible(t *testing.T) {
	for _, name := range []string{"pressure", "multitouch", "unknown_axis", "stylus", "pointer", "unknown_event", "unknown_property"} {
		t.Run(name, func(t *testing.T) {
			var queriedAxes []uintptr
			m, err := queryTouchscreenMetadata(t.Context(), func(r uintptr, p []byte) (int, error) {
				n, err := touchscreenLinuxReply(t, r, p)
				if r == 0x80184540 || r == 0x80184541 {
					queriedAxes = append(queriedAxes, r)
				}
				switch {
				case r == 0x80084523 && name == "pressure":
					p[3] |= 1
				case r == 0x80084523 && name == "multitouch":
					p[5] |= 128
				case r == 0x80084523 && name == "unknown_axis":
					p[7] |= 128
				case r == 0x80604521 && name == "stylus":
					p[40] |= 1
				case r == 0x80084509 && name == "pointer":
					p[0] |= 1
				case r == 0x80084520 && name == "unknown_event":
					p[1] |= 128
				case r == 0x80084509 && name == "unknown_property":
					p[3] |= 128
				}
				return n, err
			})
			if err != nil {
				t.Fatal("complete unsupported metadata should remain visible")
			}
			if _, _, ok := ValidateTouchscreenCapabilities(m.capabilities); ok {
				t.Fatal("unknown bits were projected into a supported subset")
			}
			if !reflect.DeepEqual(queriedAxes, []uintptr{0x80184540, 0x80184541}) {
				t.Fatal("unsupported axis was queried as measured configuration")
			}
			if name == "pressure" || name == "multitouch" || name == "unknown_axis" {
				if len(m.capabilities.Axes) != 3 || m.capabilities.Axes[2] != (TouchscreenAxis{Code: map[string]uint16{"pressure": 24, "multitouch": 47, "unknown_axis": 63}[name]}) {
					t.Fatal("unqueried axis did not retain only its unsupported code marker")
				}
			}
		})
	}
}

func TestTouchscreenDiscoveryUsesSharedWidthAwareCandidate(t *testing.T) {
	for _, row := range []struct {
		name, device, abs, prop string
		want                    bool
	}{
		{"direct", "Generic", "3", "2", true},
		{"direct_multiword", "Generic", "3", "0 2", true},
		{"misleading_touchpad", "Fixture Touchpad", "3", "2", true},
		{"named_unknown", "Fixture Touchscreen", "", "", true},
		{"multitouch_visible", "Generic", "800000000003", "2", true},
		{"indirect", "Fixture Touchpad", "3", "1", false},
		{"generic_absolute", "Generic", "3", "", false},
		{"relative_mouse", "Fixture Mouse", "", "", false},
	} {
		t.Run(row.name, func(t *testing.T) {
			for _, width := range []int{32, 64} {
				record := "N: Name=\"" + row.device + "\"\nH: Handlers=event2\nB: ABS=" + row.abs + "\nB: PROP=" + row.prop + "\n"
				names, err := parseTouchscreenInterfaces(strings.NewReader(record), width)
				if err != nil || len(names) != map[bool]int{true: 1, false: 0}[row.want] ||
					row.want && names[0] != "event2" {
					t.Fatal("candidate selection differs from the observed DIRECT predicate")
				}
			}
		})
	}
	for _, width := range []int{0, 16} {
		if _, err := parseTouchscreenInterfaces(strings.NewReader(""), width); err == nil {
			t.Fatal("unknown bitmap width was admitted")
		}
	}
}

func TestTouchscreenDiscoveryRejectsRegistryAmbiguityAndLimit(t *testing.T) {
	base := "N: Name=\"Fixture Touchscreen\"\nH: Handlers=event0\nB: ABS=3\nB: PROP=2\n"
	for _, name := range []string{"duplicate", "missing_interface", "malformed_interface", "duplicate_field", "over_limit"} {
		t.Run(name, func(t *testing.T) {
			raw := base
			switch name {
			case "duplicate":
				raw += "\n" + base
			case "missing_interface":
				raw = strings.ReplaceAll(raw, "event0", "mouse0")
			case "malformed_interface":
				raw = strings.ReplaceAll(raw, "event0", "event00")
			case "duplicate_field":
				raw += "B: PROP=2\n"
			case "over_limit":
				raw = ""
				for i := 0; i < 129; i++ {
					raw += strings.ReplaceAll(base, "event0", "event"+strconv.Itoa(i)) + "\n"
				}
			}
			if _, err := parseTouchscreenInterfaces(strings.NewReader(raw), 64); err == nil {
				t.Fatal("ambiguous or oversized registry admitted")
			}
		})
	}
	raw := ""
	for i := 0; i < 128; i++ {
		raw += strings.ReplaceAll(base, "event0", "event"+strconv.Itoa(i)) + "\n"
	}
	if names, err := parseTouchscreenInterfaces(strings.NewReader(raw), 64); err != nil || len(names) != 128 {
		t.Fatal("exact registry limit rejected")
	}
}
