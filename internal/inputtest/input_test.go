//go:build linux

package inputtest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestParseDevicesSelectsRequestedEventInterfaces(t *testing.T) {
	registry := `N: Name="Fixture Keyboard"
H: Handlers=sysrq kbd event2

N: Name="Fixture Touchpad"
H: Handlers=mouse0 event11

N: Name="Power Button"
H: Handlers=kbd event0
`
	devices, err := parseDevices(strings.NewReader(registry), []string{"keyboard", "touchpad"})
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 || devices[0].interfaceName != "event11" || devices[1].interfaceName != "event2" {
		t.Fatalf("unexpected devices %#v", devices)
	}
}

func TestActivityStoresOnlyAggregateCounts(t *testing.T) {
	result := DeviceResult{Category: "keyboard", Readable: true}
	if accountEvent(&result, inputEvent{Type: 1, Code: 30, Value: 1}) || accountEvent(&result, inputEvent{Type: 1, Code: 30, Value: 0}) || accountEvent(&result, inputEvent{Type: 0, Code: 30, Value: 0}) {
		t.Fatal("normal input incorrectly reported evidence loss")
	}
	if result.Events != 2 || result.KeyPresses != 1 || result.KeyReleases != 1 {
		t.Fatalf("unexpected aggregate %#v", result)
	}
	if !activityComplete([]DeviceResult{result}, []string{"keyboard"}) {
		t.Fatal("keyboard activity was not accepted")
	}
}

func TestOutputEventsAndDroppedRecordsCannotPass(t *testing.T) {
	keyboard := DeviceResult{Category: "keyboard", Readable: true}
	accountEvent(&keyboard, inputEvent{Type: 17, Code: 0, Value: 1})
	accountEvent(&keyboard, inputEvent{Type: 18, Code: 0, Value: 1})
	if activityComplete([]DeviceResult{keyboard}, []string{"keyboard"}) {
		t.Fatal("output events passed input activity")
	}
	if !accountEvent(&keyboard, inputEvent{Type: 0, Code: 3}) || !keyboard.EvidenceLoss {
		t.Fatal("SYN_DROPPED was ignored")
	}
}

func TestMissingRequestedCategoryIsNotComplete(t *testing.T) {
	if hasEveryCategory([]string{"keyboard"}, []string{"keyboard", "touchscreen"}) {
		t.Fatal("missing touchscreen accepted")
	}
}

func TestClassificationRejectsButtonAndOutputDevicesAsKeyboards(t *testing.T) {
	if inventory.ClassifyInput("Power Button", "kbd event0", "10000000000000", "", "", "") == "keyboard" {
		t.Fatal("power button classified as keyboard")
	}
	if inventory.ClassifyInput("PC Speaker", "kbd event1", "", "", "", "") == "keyboard" {
		t.Fatal("output device classified as keyboard")
	}
}

func TestPropertiesClassifyGenericTouchHardware(t *testing.T) {
	if inventory.ClassifyInput("I2C HID Device", "event7", "", "", "3", "2") != "touchscreen" {
		t.Fatal("direct absolute device was not classified as touchscreen")
	}
	if inventory.ClassifyInput("I2C HID Device", "mouse0 event8", "", "", "3", "5") != "touchpad" {
		t.Fatal("buttonpad absolute device was not classified as touchpad")
	}
}

func TestDeviceLimitFailsClosed(t *testing.T) {
	var registry strings.Builder
	for deviceIndex := 0; deviceIndex <= maximumInputDevices; deviceIndex++ {
		fmt.Fprintf(&registry, "N: Name=\"Keyboard %d\"\nH: Handlers=kbd event%d\n\n", deviceIndex, deviceIndex)
	}
	if _, err := parseDevices(strings.NewReader(registry.String()), []string{"keyboard"}); err == nil {
		t.Fatal("oversized device registry was truncated")
	}
}

func TestRequestedCategoriesFailClosed(t *testing.T) {
	if _, err := requestedCategories([]string{"keyboard", "modem"}); err == nil {
		t.Fatal("unknown category accepted")
	}
}
