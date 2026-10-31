package inventory

import "testing"

func TestInventoryTouchscreenCandidateProcAndSysfsParity(t *testing.T) {
	for _, row := range []struct {
		name, device, absolute, properties, category, candidate string
	}{
		{"direct", "Generic", "3", "2", "touchscreen", "true"},
		{"direct_multiword", "Generic", "3", "0 2", "touchscreen", "true"},
		{"misleading_touchpad", "Fixture Touchpad", "3", "2", "touchpad", "true"},
		{"named_unknown", "Fixture Touchscreen", "", "", "touchscreen", "true"},
		{"multitouch_visible", "Generic", "800000000003", "2", "touchscreen", "true"},
		{"indirect", "Fixture Touchpad", "3", "1", "touchpad", "false"},
		{"generic_absolute", "Generic", "3", "", "input", "false"},
		{"relative_mouse", "Fixture Mouse", "", "", "pointer", "false"},
	} {
		t.Run(row.name, func(t *testing.T) {
			for _, mode := range []string{"proc", "sysfs"} {
				for _, width := range []int{32, 64} {
					component := touchpadInventoryCollect(t, touchpadInventoryFixture(mode, map[string]string{
						"name": row.device, "abs": row.absolute, "prop": row.properties,
						"msc": "0", "identity_extra": "touchscreen_candidate=forged",
					}), width)
					category, categoryOK := component.Attribute("category")
					candidate, candidateOK := component.Attribute("touchscreen_candidate")
					if !categoryOK || !candidateOK || category != row.category || candidate != row.candidate {
						t.Fatal("collector and DIRECT discovery candidate disagree")
					}
					if row.name == "misleading_touchpad" {
						if marker, _ := component.Attribute("touchpad_candidate"); marker != "true" {
							t.Fatal("new DIRECT discovery changed retained touchpad candidacy")
						}
					}
					if msc, ok := component.Attribute("msc_capabilities"); !ok || msc != "0" {
						t.Fatal("canonical empty metadata was lost")
					}
				}
			}
		})
	}
}

func TestInventoryTouchscreenMarkerCannotBeSuppliedByRegistry(t *testing.T) {
	for _, row := range []struct{ name, absolute, properties, injected, want string }{
		{"cannot_hide_direct", "3", "2", "false", "true"},
		{"cannot_invent_direct", "3", "1", "true", "false"},
		{"cannot_publish_arbitrary_text", "", "", "opaque", "false"},
	} {
		t.Run(row.name, func(t *testing.T) {
			component := touchpadInventoryCollect(t, touchpadInventoryFixture("proc", map[string]string{
				"name": "Generic", "abs": row.absolute, "prop": row.properties,
				"identity_extra": "touchscreen_candidate=" + row.injected + " bitmap_word_bits=32",
			}), 64)
			if candidate, ok := component.Attribute("touchscreen_candidate"); !ok || candidate != row.want {
				t.Fatal("reserved touchscreen observation was accepted from raw text")
			}
			if width, _ := component.Attribute("bitmap_word_bits"); width != "64" {
				t.Fatal("registry text replaced the observed kernel width")
			}
		})
	}
}

func TestInventoryTouchscreenDiscoveryHintDoesNotInventWidth(t *testing.T) {
	component := touchpadInventoryCollect(t, touchpadInventoryFixture("proc", map[string]string{
		"name": "Fixture Touchscreen", "abs": "3", "prop": "2",
		"identity_extra": "bitmap_word_bits=64",
	}), 0)
	if candidate, _ := component.Attribute("touchscreen_candidate"); candidate != "true" {
		t.Fatal("named unsupported device was hidden")
	}
	if _, ok := component.Attribute("bitmap_word_bits"); ok {
		t.Fatal("unknown width became functional authority")
	}
	if TouchscreenCandidate("touchpad", "3", "2", 0) ||
		TouchscreenCandidate("touchpad", "3", "10000000000000002", 64) {
		t.Fatal("unknown width or malformed property word supplied a DIRECT bit")
	}
}
