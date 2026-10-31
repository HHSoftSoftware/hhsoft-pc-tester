//go:build linux

package inputtest

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// Literal UAPI vectors are independent of the production request construction.
// The injected query never invokes ioctl or opens an input device.
func touchpadLinuxReply(t *testing.T, request uintptr, data []byte) (int, error) {
	t.Helper()
	word := 8
	if runtime.GOARCH == "386" {
		word = 4
	}
	bit := func(code int) { data[code/8] |= 1 << uint(code%8) }
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
			t.Fatal("event bitmap request size")
		}
		data[0] = 0x0b
		return word, nil
	case 0x80604521:
		if len(data) != 96 {
			t.Fatal("key bitmap request size")
		}
		bit(0x110)
		bit(0x145)
		bit(0x14a)
		return 96, nil
	case 0x80084522, 0x80084524:
		if len(data) != 8 {
			t.Fatal("relative or misc bitmap request size")
		}
		return word, nil
	case 0x80084523:
		if len(data) != 8 {
			t.Fatal("absolute bitmap request size")
		}
		data[0] = 3
		return 8, nil
	case 0x80084509:
		if len(data) != 8 {
			t.Fatal("property bitmap request size")
		}
		data[0] = 1
		return word, nil
	case 0x80184540, 0x80184541, 0x80184558, 0x8018455c:
		if len(data) != 24 {
			t.Fatal("absolute information request size")
		}
		binary.NativeEndian.PutUint32(data, 100)
		binary.NativeEndian.PutUint32(data[4:], 50)
		binary.NativeEndian.PutUint32(data[8:], 150)
		return 0, nil
	case 0x80604518:
		if len(data) != 96 {
			t.Fatal("key state request size")
		}
		return 96, nil
	default:
		t.Fatal("query escaped the fixed read-only request set")
		return 0, syscall.EINVAL
	}
}

func TestTouchpadLinuxCompiledABIAndRequests(t *testing.T) {
	if !touchpadLinuxABI() {
		t.Fatal("target not covered by the compiled ABI qualification")
	}
	wantSize, wantOffset := uintptr(24), uintptr(16)
	if runtime.GOARCH == "386" {
		wantSize, wantOffset = 16, 8
	}
	if unsafe.Sizeof(inputEvent{}) != wantSize || unsafe.Offsetof(inputEvent{}.Type) != wantOffset ||
		unsafe.Offsetof(inputEvent{}.Code) != wantOffset+2 || unsafe.Offsetof(inputEvent{}.Value) != wantOffset+4 ||
		unsafe.Sizeof(inputEvent{}.Value) != 4 || binary.NativeEndian.Uint32([]byte{1, 0, 0, 0}) != 1 {
		t.Fatal("compiled event layout or endianness differs from literal vectors")
	}
	var requests []uintptr
	query := func(r uintptr, p []byte) (int, error) {
		requests = append(requests, r)
		return touchpadLinuxReply(t, r, p)
	}
	m, err := queryTouchpadMetadata(t.Context(), query)
	want := []uintptr{0x80044501, 0x80084502, 0x80084520, 0x80604521, 0x80084522, 0x80084523, 0x80084509, 0x80084524, 0x80184540, 0x80184541}
	if err != nil || !reflect.DeepEqual(requests, want) || !reflect.DeepEqual(m.capabilities, touchpadTestCapabilities()) || m.identity != (TouchpadIdentity{3, 1, 2, 1}) {
		t.Fatal("literal metadata/request vector mismatch")
	}
	neutral, err := queryTouchpadNeutral(query)
	if err != nil || !neutral || requests[len(requests)-1] != 0x80604518 {
		t.Fatal("terminal getter vector mismatch")
	}
}

func TestTouchpadLinuxEveryQueryReturnIsExact(t *testing.T) {
	for _, row := range []struct {
		name    string
		request uintptr
	}{
		{"version", 0x80044501}, {"identity", 0x80084502}, {"events", 0x80084520}, {"keys", 0x80604521},
		{"relative", 0x80084522}, {"absolute", 0x80084523}, {"properties", 0x80084509}, {"misc", 0x80084524},
		{"x", 0x80184540}, {"y", 0x80184541}, {"pressure", 0x80184558}, {"width", 0x8018455c}, {"neutral", 0x80604518},
	} {
		for _, fault := range []string{"short", "long", "negative", "error"} {
			t.Run(row.name+"_"+fault, func(t *testing.T) {
				seen, after := false, false
				query := func(r uintptr, p []byte) (int, error) {
					if seen {
						after = true
					}
					n, err := touchpadLinuxReply(t, r, p)
					if r == 0x80084523 {
						p[3] |= 0x11
					}
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
							err = errors.New("provider text must not escape")
						}
					}
					return n, err
				}
				var err error
				if row.name == "neutral" {
					_, err = queryTouchpadNeutral(query)
				} else {
					_, err = queryTouchpadMetadata(t.Context(), query)
				}
				if !seen || after || err != errInputIdentity {
					t.Fatal("invalid success count/error accepted or later query issued")
				}
			})
		}
	}
}

func TestTouchpadLinuxClosedCapabilitiesAndSignedAxes(t *testing.T) {
	for _, mode := range []string{"base", "optional", "signed_extrema", "dynamic_only", "version", "event_unknown", "missing_syn", "key_unknown", "missing_finger", "relative", "property_unknown", "direct", "mt", "axis_unknown", "misc_unknown", "misc_without_event", "equal_range", "reversed_range", "negative_fuzz", "negative_flat", "negative_resolution", "fuzz_over_span", "flat_over_span", "threshold_over_span"} {
		t.Run(mode, func(t *testing.T) {
			query := func(r uintptr, p []byte) (int, error) {
				n, err := touchpadLinuxReply(t, r, p)
				switch {
				case r == 0x80044501 && mode == "version":
					p[0] = 2
				case r == 0x80084520:
					if mode == "event_unknown" {
						p[2] |= 1
					}
					if mode == "missing_syn" {
						p[0] &^= 1
					}
					if mode == "optional" {
						p[0] |= 16
					}
				case r == 0x80604521:
					if mode == "key_unknown" {
						p[95] |= 128
					}
					if mode == "missing_finger" {
						p[40] &^= 32
					}
					if mode == "optional" {
						p[34] |= 6
					}
				case r == 0x80084522 && mode == "relative":
					p[0] = 1
				case r == 0x80084509:
					if mode == "property_unknown" {
						p[3] |= 128
					}
					if mode == "direct" {
						p[0] |= 2
					}
					if mode == "optional" {
						p[0] |= 4
					}
				case r == 0x80084523:
					if mode == "mt" {
						p[5] |= 128
					}
					if mode == "axis_unknown" {
						p[0] |= 4
					}
					if mode == "optional" {
						p[3] |= 17
					}
				case r == 0x80084524:
					if mode == "misc_unknown" {
						p[0] = 1
					}
					if mode == "misc_without_event" || mode == "optional" {
						p[0] = 48
					}
				case r == 0x80184540 || r == 0x80184541:
					switch mode {
					case "signed_extrema":
						binary.NativeEndian.PutUint32(p[4:], 0x80000000)
						binary.NativeEndian.PutUint32(p[8:], 0x7fffffff)
					case "dynamic_only":
						binary.NativeEndian.PutUint32(p, 0x80000000)
					case "equal_range":
						binary.NativeEndian.PutUint32(p[8:], 50)
					case "reversed_range":
						binary.NativeEndian.PutUint32(p[8:], 49)
					case "negative_fuzz":
						binary.NativeEndian.PutUint32(p[12:], 0xffffffff)
					case "negative_flat":
						binary.NativeEndian.PutUint32(p[16:], 0xffffffff)
					case "negative_resolution":
						binary.NativeEndian.PutUint32(p[20:], 0xffffffff)
					case "fuzz_over_span":
						binary.NativeEndian.PutUint32(p[12:], 101)
					case "flat_over_span":
						binary.NativeEndian.PutUint32(p[16:], 101)
					case "threshold_over_span":
						binary.NativeEndian.PutUint32(p[12:], 25)
					}
				}
				return n, err
			}
			m, err := queryTouchpadMetadata(t.Context(), query)
			if err != nil {
				t.Fatal("readable unsupported metadata became a hidden device")
			}
			x, y, ok := ValidateTouchpadCapabilities(m.capabilities)
			want := mode == "base" || mode == "optional" || mode == "signed_extrema" || mode == "dynamic_only"
			if ok != want {
				t.Fatal("closed capability admission mismatch")
			}
			if mode == "signed_extrema" && (x != 214748365 || y != 214748365 || m.capabilities.Axes[0].Minimum != -2147483648) {
				t.Fatal("signed32 range was truncated or overflowed")
			}
			if mode == "dynamic_only" && !reflect.DeepEqual(m.capabilities, touchpadTestCapabilities()) {
				t.Fatal("dynamic value entered static identity")
			}
		})
	}
}

func TestTouchpadLinuxQueryCancellationAndBufferOwnership(t *testing.T) {
	for _, mode := range []string{"before", "after_success", "hard_and_cancel", "retained_buffer", "nil_query", "nil_context"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			var retained []byte
			query := func(r uintptr, p []byte) (int, error) {
				calls++
				retained = p
				n, err := touchpadLinuxReply(t, r, p)
				if mode == "after_success" || mode == "hard_and_cancel" {
					cancel()
				}
				if mode == "hard_and_cancel" {
					err = syscall.EIO
				}
				return n, err
			}
			if mode == "before" {
				cancel()
			}
			if mode == "nil_query" {
				query = nil
			}
			var current context.Context = ctx
			if mode == "nil_context" {
				current = nil
			}
			data, err := touchpadQuery(current, query, 0x80044501, 4, 0)
			switch mode {
			case "before":
				if err != context.Canceled || calls != 0 {
					t.Fatal("query after cancellation")
				}
			case "after_success":
				if err != context.Canceled || calls != 1 {
					t.Fatal("post-query cancellation lost")
				}
			case "hard_and_cancel", "nil_query", "nil_context":
				if err != errInputIdentity {
					t.Fatal("hard query/admission failure lost")
				}
			case "retained_buffer":
				retained[0] ^= 255
				if err != nil || binary.NativeEndian.Uint32(data) != 0x010001 {
					t.Fatal("provider retained metadata alias")
				}
			}
		})
	}
}

func TestTouchpadLinuxNeutralChecksEveryReturnedKey(t *testing.T) {
	for _, code := range []int{0, 272, 273, 274, 325, 330, 767} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			ok, err := queryTouchpadNeutral(func(r uintptr, p []byte) (int, error) {
				n, err := touchpadLinuxReply(t, r, p)
				p[code/8] |= 1 << uint(code%8)
				return n, err
			})
			if err != nil || ok {
				t.Fatal("pressed advertised or unknown key counted neutral")
			}
		})
	}
}

func TestTouchpadLinuxReturnedExtentSeparatesCompatPadding(t *testing.T) {
	m, err := queryTouchpadMetadata(t.Context(), func(r uintptr, p []byte) (int, error) {
		n, err := touchpadLinuxReply(t, r, p)
		if r == 0x80084520 || r == 0x80084522 || r == 0x80084509 || r == 0x80084524 {
			p[4] = 1
		}
		return n, err
	})
	if err != nil {
		t.Fatal("literal caller-width vector failed")
	}
	_, _, supported := ValidateTouchpadCapabilities(m.capabilities)
	if supported != (strconv.IntSize == 32) {
		t.Fatal("unreturned compat padding or returned unknown bits acquired authority")
	}
}

func TestTouchpadLinuxRegistryUsesObservedKernelWordWidth(t *testing.T) {
	for _, row := range []struct {
		name, bitmap string
		width        int
	}{
		{"kernel32", "2001000 0", 32}, {"kernel64", "200100000000000", 64},
	} {
		t.Run(row.name, func(t *testing.T) {
			data := "N: Name=\"Anonymous\"\nH: Handlers=kbd event7\nB: KEY=" + row.bitmap + "\n\n"
			got, err := parseFunctionalInputInterfaces(strings.NewReader(data), "keyboard", row.width)
			if err != nil || !reflect.DeepEqual(got, []string{"event7"}) {
				t.Fatal("process width replaced observed registry width")
			}
			got, err = parseTouchpadInterfaces(strings.NewReader(data), row.width)
			if err != nil || len(got) != 0 {
				t.Fatal("keyboard became touchpad candidate")
			}
		})
	}
}

func TestTouchpadLinuxRegistryCandidatesAndRetainedCategories(t *testing.T) {
	for _, width := range []int{32, 64} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			registry := "N: Name=\"Touchpad\"\nH: Handlers=event0\nB: ABS=800000000003\n\n" +
				"N: Name=\"Anonymous\"\nH: Handlers=event1\nB: ABS=3\nB: PROP=80000000\n\n" +
				"N: Name=\"Mouse\"\nH: Handlers=mouse0 event2\nB: ABS=3\n\n" +
				"N: Name=\"Keyboard\"\nH: Handlers=kbd event3\n\n" +
				"N: Name=\"Touchscreen\"\nH: Handlers=event4\nB: ABS=3\nB: PROP=2\n\n"
			got, err := parseTouchpadInterfaces(strings.NewReader(registry), width)
			if err != nil || !reflect.DeepEqual(got, []string{"event0", "event1", "event2"}) {
				t.Fatal("unknown/MT absolute candidate disappeared")
			}
			for _, wanted := range []string{"keyboard", "pointer"} {
				got, err := parseFunctionalInputInterfaces(strings.NewReader(registry), wanted, width)
				expected := []string{"event3"}
				if wanted == "pointer" {
					expected = []string{"event2"}
				}
				if err != nil || !reflect.DeepEqual(got, expected) {
					t.Fatal("existing category result changed")
				}
			}
		})
	}
}

func TestTouchpadLinuxRegistryFailuresDoNotTruncate(t *testing.T) {
	for _, mode := range []string{"width_zero", "width_other", "duplicate", "noncanonical", "missing_handler", "duplicate_field", "overflow", "exact_limit", "read_error", "line_overflow"} {
		t.Run(mode, func(t *testing.T) {
			width := 64
			data := "N: Name=\"Touchpad\"\nH: Handlers=event0\n\n"
			switch mode {
			case "width_zero":
				width = 0
			case "width_other":
				width = 16
			case "duplicate":
				data += data
			case "noncanonical":
				data = strings.Replace(data, "event0", "event00", 1)
			case "missing_handler":
				data = "N: Name=\"Touchpad\"\n\n"
			case "duplicate_field":
				data = "N: Name=\"Touchpad\"\nH: Handlers=event0\nH: Handlers=event1\n\n"
			case "overflow", "exact_limit":
				data = ""
				count := 128
				if mode == "overflow" {
					count++
				}
				for i := 0; i < count; i++ {
					data += fmt.Sprintf("N: Name=\"Touchpad\"\nH: Handlers=event%d\n\n", i)
				}
			case "line_overflow":
				data = strings.Repeat("x", 65537)
			}
			var got []string
			var err error
			if mode == "read_error" {
				got, err = parseTouchpadInterfaces(touchpadRegistryErrorReader{}, width)
			} else {
				got, err = parseTouchpadInterfaces(strings.NewReader(data), width)
			}
			if mode == "exact_limit" {
				if err != nil || len(got) != 128 {
					t.Fatal("exact device bound rejected")
				}
			} else if err == nil || got != nil {
				t.Fatal("registry fault accepted or truncated")
			}
		})
	}
}

type touchpadRegistryErrorReader struct{}

func (touchpadRegistryErrorReader) Read([]byte) (int, error) { return 0, syscall.EIO }
