package inventory

import (
	"strconv"
	"testing"
)

func TestKeyboardClassificationUsesKernelBitmapWidth(t *testing.T) {
	// KEY_Z=44 and KEY_SPACE=57 are in the second 32-bit word but in
	// the low 64-bit word. No keyboard name shortcut is present.
	for _, item := range []struct {
		width int
		bits  string
	}{{32, "2001000 0"}, {64, "200100000000000"}} {
		t.Run(strconv.Itoa(item.width), func(t *testing.T) {
			if got := ClassifyInputWithBitmapWidth("Generic HID", "kbd event4", item.bits, "", "", "", item.width); got != "keyboard" {
				t.Fatalf("kernel width classified as %s", got)
			}
			if got := ClassifyInputWithBitmapWidth("Generic HID", "kbd event4", item.bits, "", "", "", 0); got == "keyboard" {
				t.Fatal("unknown bitmap width inferred generic keyboard")
			}
			root := t.TempDir()
			writeFixture(t, root, map[string]string{"proc/bus/input/devices": "I: Bus=0003 Vendor=1234 Product=5678 Version=0100\nN: Name=\"Generic HID\"\nH: Handlers=kbd event4\nB: EV=3\nB: KEY=" + item.bits + "\n"})
			collector := NewLinuxCollector(root)
			collector.inputBitmapWordBits = func() int { return item.width }
			components := collector.newCollection(t.Context()).collectInput()
			if len(components) != 1 {
				t.Fatal("missing component")
			}
			category, _ := components[0].Attribute("category")
			if category != "keyboard" {
				t.Fatal("final inventory retained process-width classification")
			}
		})
	}
	for _, name := range []string{"USB Mouse", "Touchpad", "Touchscreen"} {
		if ClassifyInputWithBitmapWidth(name, "mouse0 event1", "0", "3", "", "", 32) != ClassifyInput(name, "mouse0 event1", "0", "3", "", "") {
			t.Fatal("pointer classification changed")
		}
	}
}
