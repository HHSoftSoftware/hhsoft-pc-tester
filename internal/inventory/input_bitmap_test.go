package inventory

import (
	"errors"
	"strconv"
	"testing"
)

func TestInputBitmapWidthRejectsUnknownOrOverriddenKernelObservation(t *testing.T) {
	for _, item := range []struct {
		name        string
		personality uintptr
		queryError  bool
		unameError  bool
		want        int
	}{
		{name: "normal Linux", want: 64},
		{name: "Linux with unrelated flag", personality: 0x40000, want: 64},
		{name: "query unavailable", queryError: true},
		{name: "Linux32 personality", personality: 8},
		{name: "Linux32 with flags", personality: 0x40008},
		{name: "other personality", personality: 1},
		{name: "uname failed", unameError: true},
	} {
		t.Run(item.name, func(t *testing.T) {
			queries, unames := 0, 0
			got := observedInputBitmapWordBits(func() (uintptr, error) {
				queries++
				if item.queryError {
					return 0, errors.New("fixture query failure")
				}
				return item.personality, nil
			}, func() (string, error) {
				unames++
				if item.unameError {
					return "", errors.New("fixture uname failure")
				}
				return "x86_64", nil
			})
			wantUname := 1
			if item.queryError || item.personality&0xff != 0 {
				wantUname = 0
			}
			if got != item.want || queries != 1 || unames != wantUname {
				t.Fatalf("width/query/uname = %d/%d/%d, want %d/1/%d", got, queries, unames, item.want, wantUname)
			}
		})
	}
}

func TestInputBitmapKernelMachineWhitelist(t *testing.T) {
	for machine, want := range map[string]int{
		"x86_64": 64, "aarch64": 64,
		"i386": 32, "i486": 32, "i586": 32, "i686": 32,
		"": 0, "amd64": 0, "arm64": 0, "unknown": 0,
		"X86_64": 0, "x86_64 ": 0, "x86_64_extra": 0,
	} {
		if got := inputBitmapWordBitsForMachine(machine); got != want {
			t.Fatalf("kernel bitmap width for %q = %d, want %d", machine, got, want)
		}
	}
}

func TestUnknownKernelWidthPreservesOnlyUnambiguousPropertyHints(t *testing.T) {
	for _, item := range []struct {
		name, properties, want string
	}{
		{"direct", "2", "touchscreen"},
		{"buttonpad", "4", "touchpad"},
		{"semi-mt", "8", "touchpad"},
		{"topbuttonpad", "10", "touchpad"},
		{"whitespace", "  2\n", "touchscreen"},
		{"pointer-only", "1", "input"},
		{"empty", "", "input"},
		{"malformed", "zz2", "input"},
		{"overflow", "100000002", "input"},
		{"ambiguous-word-count", "1 2", "input"},
	} {
		t.Run(item.name, func(t *testing.T) {
			if got := ClassifyInputWithBitmapWidth("FixturePanel", "event0", "", "", "3", item.properties, 0); got != item.want {
				t.Fatalf("category = %q, want %q", got, item.want)
			}
		})
	}
	if got := ClassifyInputWithBitmapWidth("Generic Input", "kbd event0", "40010000", "", "", "2", 0); got != "input" {
		t.Fatalf("unknown-width key map established keyboard category: %q", got)
	}
	if got := ClassifyInputWithBitmapWidth("Generic Input", "kbd event0", "40010000", "", "", "2", 32); got != "keyboard" {
		t.Fatalf("observed-width key map did not establish keyboard category: %q", got)
	}
}

func TestInputBitmapKernelWidthIsObservedForProcAndSysfs(t *testing.T) {
	for _, source := range []string{"proc", "sysfs"} {
		for _, width := range []int{0, 32, 64} {
			t.Run(source+"/"+strconv.Itoa(width), func(t *testing.T) {
				root := t.TempDir()
				if source == "proc" {
					writeFixture(t, root, map[string]string{"proc/bus/input/devices": "I: Bus=0003 Vendor=1234 Product=5678 Version=0100 bitmap_word_bits=999\nN: Name=\"Fixture Mouse\"\nH: Handlers=mouse0 event0\nB: EV=7\nB: REL=3\nB: KEY=70000 0 0 0 0\n"})
				} else {
					writeFixture(t, root, map[string]string{
						"sys/class/input/event0/device/name":             "Fixture Mouse\n",
						"sys/class/input/event0/device/capabilities/ev":  "7\n",
						"sys/class/input/event0/device/capabilities/rel": "3\n",
						"sys/class/input/event0/device/capabilities/key": "70000 0 0 0 0\n",
					})
				}
				collector := NewLinuxCollector(root)
				observations := 0
				collector.inputBitmapWordBits = func() int { observations++; return width }
				components := collector.newCollection(t.Context()).collectInput()
				if len(components) != 1 || observations != 1 {
					t.Fatalf("input inventory/observation counts = %d/%d", len(components), observations)
				}
				value, found := components[0].Attribute("bitmap_word_bits")
				if width == 0 && found || width != 0 && (!found || value != strconv.Itoa(width)) {
					t.Fatalf("kernel bitmap width = %q/%v for %d", value, found, width)
				}
				if category, _ := components[0].Attribute("category"); category != "pointer" {
					t.Fatal("kernel-width observation changed pointer applicability")
				}
			})
		}
	}
}
