package inventory

import (
	"context"
	"io/fs"
	"strings"
	"testing"
)

// Each representation is built from literal registry roles, independently of
// the collector's attribute table. No live device or kernel query is used.
func touchpadInventoryFixture(mode string, values map[string]string) PathSource {
	files := map[string]string{}
	if mode == "proc" {
		record := "I: Bus=0003 Vendor=1234 Product=5678 Version=0100 " + values["identity_extra"] + "\n"
		record += "N: Name=\"" + values["name"] + "\"\n"
		handlers := "event0"
		if value, ok := values["handlers"]; ok {
			handlers = value
		}
		record += "H: Handlers=" + handlers + "\n"
		for _, role := range []struct{ field, label string }{
			{"ev", "EV"}, {"key", "KEY"}, {"rel", "REL"},
			{"abs", "ABS"}, {"prop", "PROP"}, {"msc", "MSC"},
		} {
			if value, ok := values[role.field]; ok {
				record += "B: " + role.label + "=" + value + "\n"
			}
		}
		files["proc/bus/input/devices"] = record
	} else {
		for _, role := range []struct{ field, relative string }{
			{"name", "name"}, {"ev", "capabilities/ev"},
			{"key", "capabilities/key"}, {"rel", "capabilities/rel"},
			{"abs", "capabilities/abs"}, {"prop", "properties"},
			{"msc", "capabilities/msc"},
		} {
			if value, ok := values[role.field]; ok {
				files["sys/class/input/event0/device/"+role.relative] = value
			}
		}
	}
	return collectionSource{
		file: func(name string) ([]byte, error) {
			if value, ok := files[name]; ok {
				return []byte(value), nil
			}
			return nil, fs.ErrNotExist
		},
		directory: func(name string) ([]fs.DirEntry, error) {
			if mode == "sysfs" && name == "sys/class/input" {
				return enumerationEntries("event0"), nil
			}
			return nil, fs.ErrNotExist
		},
	}
}

func touchpadInventoryCollect(t *testing.T, source PathSource, width int) Component {
	t.Helper()
	collector := NewLinuxCollectorWithSource(source)
	observations := 0
	collector.inputBitmapWordBits = func() int { observations++; return width }
	snapshot, err := collector.Collect(t.Context())
	if err != nil || snapshot.SchemaVersion != 1 || len(snapshot.Components) != 1 || observations != 1 {
		t.Fatalf("incomplete inventory: error=%v schema=%d components=%d width calls=%d", err, snapshot.SchemaVersion, len(snapshot.Components), observations)
	}
	if snapshot.Components[0].Type != ComponentInput {
		t.Fatal("unexpected component type")
	}
	return snapshot.Components[0]
}

func TestInventoryTouchpadCandidateCategories(t *testing.T) {
	for _, fixture := range []struct {
		name, device, absolute, relative, properties, category, candidate string
	}{
		{"named_touchpad", "Fixture Touchpad", "", "", "", "touchpad", "true"},
		{"named_trackpad", "Fixture Trackpad", "", "", "", "touchpad", "true"},
		{"buttonpad", "Generic", "3", "", "4", "touchpad", "true"},
		{"semi_mt_visible", "Generic", "3", "", "8", "touchpad", "true"},
		{"top_buttonpad", "Generic", "3", "", "10", "touchpad", "true"},
		{"direct_surface", "Generic", "3", "", "2", "touchscreen", "false"},
		{"named_touchscreen", "Fixture Touchscreen", "3", "", "4", "touchscreen", "false"},
		{"generic_absolute", "Generic", "3", "", "", "input", "true"},
		{"zero_absolute_word", "Generic", "0", "", "", "input", "true"},
		{"unrecognized_absolute", "Generic", "not-hex", "", "", "input", "true"},
		{"mouse_absolute", "Fixture Mouse", "3", "", "", "pointer", "true"},
		{"mouse_relative_only", "Fixture Mouse", "", "3", "", "pointer", "false"},
		{"generic_relative_absolute", "Generic", "3", "3", "", "pointer", "true"},
		{"generic_relative_only", "Generic", "", "3", "", "pointer", "false"},
		{"keyboard_absolute", "Fixture Keyboard", "3", "", "", "keyboard", "false"},
		{"gamepad_absolute", "Fixture Gamepad", "3", "", "", "controller", "false"},
		{"tablet_absolute", "Fixture Tablet", "3", "", "", "tablet", "false"},
		{"generic_without_absolute", "Generic", "", "", "", "input", "false"},
		{"property_without_absolute", "Generic", "", "", "4", "input", "false"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			for _, mode := range []string{"proc", "sysfs"} {
				for _, width := range []int{0, 32, 64} {
					component := touchpadInventoryCollect(t, touchpadInventoryFixture(mode, map[string]string{
						"name": fixture.device, "abs": fixture.absolute,
						"rel": fixture.relative, "prop": fixture.properties,
					}), width)
					category, foundCategory := component.Attribute("category")
					candidate, foundCandidate := component.Attribute("touchpad_candidate")
					if !foundCategory || !foundCandidate || category != fixture.category || candidate != fixture.candidate {
						t.Fatalf("%s/%d category=%q candidate=%q", mode, width, category, candidate)
					}
					if _, found := component.Attribute("msc_capabilities"); found {
						t.Fatal("unobserved MSC became an empty bitmap")
					}
				}
			}
		})
	}
}

func TestInventoryTouchpadCandidateObservedWidth(t *testing.T) {
	for _, fixture := range []struct {
		name, keys, category, candidate string
		width                           int
	}{
		{"kernel32_keyboard", "2001000 0", "keyboard", "false", 32},
		{"kernel64_keyboard", "200100000000000", "keyboard", "false", 64},
		{"unknown_kernel32_text", "2001000 0", "input", "true", 0},
		{"unknown_kernel64_text", "200100000000000", "input", "true", 0},
		{"unsupported_width", "2001000 0", "input", "true", 16},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			component := touchpadInventoryCollect(t, touchpadInventoryFixture("proc", map[string]string{
				"name": "Generic HID", "handlers": "kbd event0", "key": fixture.keys, "abs": "3",
				"identity_extra": "bitmap_word_bits=64 touchpad_candidate=forged category=touchpad",
			}), fixture.width)
			category, _ := component.Attribute("category")
			candidate, _ := component.Attribute("touchpad_candidate")
			if category != fixture.category || candidate != fixture.candidate {
				t.Fatal("raw metadata overrode observed-width candidacy")
			}
			_, found := component.Attribute("bitmap_word_bits")
			if found != (fixture.width == 32 || fixture.width == 64) {
				t.Fatal("raw metadata supplied kernel bitmap width")
			}
		})
	}
}

func TestInventoryTouchpadCandidateReservedText(t *testing.T) {
	for _, fixture := range []struct{ name, absolute, injected, want string }{
		{"false_cannot_hide", "3", "false", "true"},
		{"true_cannot_invent", "", "true", "false"},
		{"arbitrary_text_cannot_invent", "", "opaque", "false"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			component := touchpadInventoryCollect(t, touchpadInventoryFixture("proc", map[string]string{
				"name": "Generic", "abs": fixture.absolute,
				"identity_extra": "touchpad_candidate=" + fixture.injected + " category=touchpad",
			}), 64)
			if value, found := component.Attribute("touchpad_candidate"); !found || value != fixture.want {
				t.Fatal("candidate marker was not recomputed")
			}
		})
	}
}

func TestInventoryTouchpadMSCIndependentVectors(t *testing.T) {
	for _, fixture := range []struct {
		name, raw, want string
		width           int
		found           bool
	}{
		{"zero32", "0", "0", 32, true},
		{"zero64", "0", "0", 64, true},
		{"padded_zero32", "00000000 00000000", "0", 32, true},
		{"padded_zero64", "0000000000000000", "0", 64, true},
		{"scan_timestamp32", "30", "30", 32, true},
		{"scan_timestamp64", "30", "30", 64, true},
		{"high_word32", "1 00000002", "1 2", 32, true},
		{"same_bits64", "100000002", "100000002", 64, true},
		{"bit63_in32_words", "80000000 0", "80000000 0", 32, true},
		{"bit63_in64_word", "8000000000000000", "8000000000000000", 64, true},
		{"uppercase32", "000000AF", "af", 32, true},
		{"uppercase64", "00000000000000AF", "af", 64, true},
		{"whitespace32", " \t0\t00000030 ", "30", 32, true},
		{"empty", "", "", 64, false},
		{"only_whitespace", " \t ", "", 64, false},
		{"unknown_width_zero", "0", "", 0, false},
		{"unknown_width_nonzero", "30", "", 0, false},
		{"unsupported_width", "30", "", 16, false},
		{"negative_width", "30", "", -32, false},
		{"plus_sign", "+30", "", 64, false},
		{"minus_sign", "-1", "", 64, false},
		{"hex_prefix", "0x30", "", 64, false},
		{"underscore", "3_0", "", 64, false},
		{"nonhex", "3g", "", 64, false},
		{"embedded_nul", "3\x00", "", 64, false},
		{"unicode_digit", "\u0663", "", 64, false},
		{"overflow32", "100000000", "", 32, false},
		{"overflow64", "10000000000000000", "", 64, false},
		{"extra_zero_word32", "0 0 0", "", 32, false},
		{"extra_zero_word64", "0 0", "", 64, false},
		{"long_zero_word", strings.Repeat("0", 129), "", 64, false},
		{"raw_text", "fixture not capability data", "", 64, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			value, found := canonicalInputMSC(fixture.raw, fixture.width)
			if value != fixture.want || found != fixture.found {
				t.Fatal("numeric projection disagrees with independent literal")
			}
			for _, mode := range []string{"proc", "sysfs"} {
				component := touchpadInventoryCollect(t, touchpadInventoryFixture(mode, map[string]string{
					"name": "Generic", "abs": "3", "ev": "1b", "msc": fixture.raw,
				}), fixture.width)
				value, found := component.Attribute("msc_capabilities")
				if value != fixture.want || found != fixture.found {
					t.Fatalf("%s changed the MSC observation contract", mode)
				}
				if candidate, _ := component.Attribute("touchpad_candidate"); candidate != "true" {
					t.Fatal("missing or unsupported MSC hid an absolute candidate")
				}
			}
		})
	}
}

func TestInventoryTouchpadMSCAbsentAndReadErrors(t *testing.T) {
	for _, fixture := range []struct {
		name, raw string
		err       error
		found     bool
	}{
		{"missing", "", fs.ErrNotExist, false},
		{"denied", "", fs.ErrPermission, false},
		{"partial_denied", "30", fs.ErrPermission, false},
		{"completed_empty_bytes", "", nil, false},
		{"completed_zero_bitmap", "0\n", nil, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			base := touchpadInventoryFixture("sysfs", map[string]string{"name": "Generic", "abs": "3"})
			calls := 0
			source := collectionSource{
				directory: base.ReadDir,
				file: func(name string) ([]byte, error) {
					if name == "sys/class/input/event0/device/capabilities/msc" {
						calls++
						return []byte(fixture.raw), fixture.err
					}
					return base.ReadFile(name)
				},
			}
			component := touchpadInventoryCollect(t, source, 64)
			value, found := component.Attribute("msc_capabilities")
			if calls != 1 || found != fixture.found || found && value != "0" {
				t.Fatal("missing, failed or empty acquisition was confused with zero")
			}
		})
	}
}

func TestInventoryTouchpadMSCReadBoundaryPrecedence(t *testing.T) {
	for _, fixture := range []struct {
		name              string
		cancel, oversized bool
		want              error
	}{
		{"cancel", true, false, context.Canceled},
		{"overflow", false, true, ErrScalarLimit},
		{"overflow_and_cancel", true, true, ErrScalarLimit},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			base := touchpadInventoryFixture("sysfs", map[string]string{"name": "Generic", "abs": "3"})
			calls, later, widthCalls := 0, 0, 0
			boundary := false
			source := collectionSource{
				directory: func(name string) ([]fs.DirEntry, error) {
					if boundary {
						later++
					}
					return base.ReadDir(name)
				},
				file: func(name string) ([]byte, error) {
					if boundary {
						later++
					}
					if name == "sys/class/input/event0/device/capabilities/msc" {
						boundary = true
						calls++
						if fixture.cancel {
							cancel()
						}
						if fixture.oversized {
							return []byte(strings.Repeat("0", 65537)), fs.ErrPermission
						}
						return []byte("30"), nil
					}
					return base.ReadFile(name)
				},
			}
			collector := NewLinuxCollectorWithSource(source)
			collector.inputBitmapWordBits = func() int { widthCalls++; return 64 }
			snapshot, err := collector.Collect(ctx)
			requireNoCollection(t, snapshot, err, fixture.want)
			if calls != 1 || later != 0 || widthCalls != 0 {
				t.Fatal("collection continued or retried after failed MSC boundary")
			}
		})
	}
}

func TestInventoryTouchpadMSCPrimaryDoesNotMergeFallback(t *testing.T) {
	for _, mode := range []string{"proc", "sysfs"} {
		t.Run(mode, func(t *testing.T) {
			primary := touchpadInventoryFixture("proc", map[string]string{"name": "Generic", "abs": "3"})
			fallback := touchpadInventoryFixture("sysfs", map[string]string{"name": "Generic", "abs": "3", "msc": "0"})
			fallbackCalls := 0
			source := collectionSource{
				directory: func(name string) ([]fs.DirEntry, error) {
					if name == "sys/class/input" {
						fallbackCalls++
					}
					return fallback.ReadDir(name)
				},
				file: func(name string) ([]byte, error) {
					if mode == "proc" && name == "proc/bus/input/devices" {
						return primary.ReadFile(name)
					}
					if strings.HasPrefix(name, "sys/class/input/") {
						fallbackCalls++
					}
					return fallback.ReadFile(name)
				},
			}
			component := touchpadInventoryCollect(t, source, 64)
			value, found := component.Attribute("msc_capabilities")
			if mode == "proc" && (found || fallbackCalls != 0) {
				t.Fatal("proc absence was filled from a second observation")
			}
			if mode == "sysfs" && (!found || value != "0" || fallbackCalls == 0) {
				t.Fatal("sysfs fallback did not collect a canonical empty bitmap")
			}
		})
	}
}

func TestInventoryTouchpadMSCProductionPath(t *testing.T) {
	for _, mode := range []string{"proc", "sysfs"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			if mode == "proc" {
				writeFixture(t, root, map[string]string{
					"proc/bus/input/devices": "N: Name=\"Generic\"\nH: Handlers=event0\nB: ABS=3\nB: MSC=00000001 00000002\n",
				})
			} else {
				writeFixture(t, root, map[string]string{
					"sys/class/input/event0/device/name":             "Generic\n",
					"sys/class/input/event0/device/capabilities/abs": "3\n",
					"sys/class/input/event0/device/capabilities/msc": "00000001 00000002\n",
				})
			}
			component := touchpadInventoryCollect(t, NewRootPathSource(root), 32)
			value, found := component.Attribute("msc_capabilities")
			if !found || value != "1 2" {
				t.Fatal("checked production path lost the bounded MSC bitmap")
			}
			if candidate, _ := component.Attribute("touchpad_candidate"); candidate != "true" {
				t.Fatal("checked production path lost the absolute candidate")
			}
		})
	}
}

func TestInventoryTouchpadWidthCancellationPublishesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	source := touchpadInventoryFixture("proc", map[string]string{"name": "Generic", "abs": "3", "msc": "30"})
	collector := NewLinuxCollectorWithSource(source)
	calls := 0
	collector.inputBitmapWordBits = func() int { calls++; cancel(); return 64 }
	snapshot, err := collector.Collect(ctx)
	requireNoCollection(t, snapshot, err, context.Canceled)
	if calls != 1 {
		t.Fatal("width observation was skipped or retried")
	}
}
