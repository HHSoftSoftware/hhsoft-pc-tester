package main

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

func validatePointerEvidence(result inputtest.PointerResult) bool {
	if result.DurationMilliseconds < 0 || result.DurationMilliseconds > 3600000 {
		return false
	}
	if result.LimitMilliseconds < 10 || result.LimitMilliseconds > 300000 {
		return result.Status == inputtest.StatusError && result.StopReason == "error" && result.LimitMilliseconds == 0 && result.DurationMilliseconds == 0 && len(result.Devices) == 0
	}
	switch result.Status {
	case inputtest.StatusPass:
		if result.StopReason != "completed" || result.DurationMilliseconds > result.LimitMilliseconds || result.Detail != "" || len(result.Devices) == 0 {
			return false
		}
	case inputtest.StatusInconclusive:
		if result.StopReason != "observation_timeout" && result.StopReason != "error" {
			return false
		}
	case inputtest.StatusAborted:
		if result.StopReason != "operator_cancelled" {
			return false
		}
	case inputtest.StatusError:
		if result.StopReason != "error" && result.StopReason != "deadline" {
			return false
		}
	default:
		return false
	}
	previous := ""
	for _, device := range result.Devices {
		if !canonicalPointerInterface(device.Interface) || device.Interface <= previous || !validatePointerDeviceEvidence(device) || result.Status == inputtest.StatusPass && device.Status != inputtest.StatusPass {
			return false
		}
		previous = device.Interface
	}
	return true
}

func canonicalPointerInterface(name string) bool {
	value, found := strings.CutPrefix(name, "event")
	if !found || len(value) == 0 || len(value) > 10 {
		return false
	}
	number, err := strconv.ParseUint(value, 10, 32)
	return err == nil && strconv.FormatUint(number, 10) == value
}

func pointerButtonOrder(button string) int {
	switch button {
	case "left":
		return 0
	case "right":
		return 1
	case "middle":
		return 2
	default:
		return -1
	}
}

func validatePointerDeviceEvidence(device inputtest.PointerDeviceResult) bool {
	switch device.Status {
	case inputtest.StatusPass, inputtest.StatusInconclusive, inputtest.StatusError, inputtest.StatusAborted:
	default:
		return false
	}
	if device.XPositive > device.Frames || device.XNegative > device.Frames-device.XPositive ||
		device.YPositive > device.Frames || device.YNegative > device.Frames-device.YPositive {
		return false
	}
	advertised := []string(nil)
	if capabilities := device.Capabilities; capabilities != nil {
		last := -1
		for _, button := range capabilities.Buttons {
			order := pointerButtonOrder(button)
			if order <= last {
				return false
			}
			last = order
		}
		advertised = capabilities.Buttons
		supported := device.Identity != nil && capabilities.Key && capabilities.Relative && capabilities.RelativeX && capabilities.RelativeY && !capabilities.Absolute && slices.Contains(advertised, "left")
		if device.Supported != supported {
			return false
		}
	} else if device.Supported {
		return false
	}
	if (!device.Readable || !device.Supported) && device.Frames != 0 {
		return false
	}
	if device.Supported && device.Readable && (device.Status == inputtest.StatusPass || device.Frames > 0 || len(device.Buttons) > 0) && len(device.Buttons) != len(advertised) {
		return false
	}
	last := -1
	for _, button := range device.Buttons {
		order := pointerButtonOrder(button.Button)
		if order <= last || !slices.Contains(advertised, button.Button) || button.Presses > device.Frames || button.Releases > device.Frames-button.Presses || button.Pairs > button.Presses || button.Pairs > button.Releases {
			return false
		}
		last = order
		if device.Status == inputtest.StatusPass && (button.Pairs == 0 || button.Pairs != button.Presses || button.Pairs != button.Releases) {
			return false
		}
	}
	if device.Status == inputtest.StatusPass {
		return device.Supported && device.Readable && !device.EvidenceLoss && device.Detail == "" && device.Frames > 0 &&
			device.XPositive > 0 && device.XNegative > 0 && device.YPositive > 0 && device.YNegative > 0
	}
	return true
}

func pointerEvidenceHasPassedDevice(encoded []byte) bool {
	var result inputtest.PointerResult
	if json.Unmarshal(encoded, &result) != nil {
		return false
	}
	for _, device := range result.Devices {
		if device.Status == inputtest.StatusPass {
			return true
		}
	}
	return false
}

// An aggregate pass must describe every planned pointer interface. A partial
// result can retain completed observations only for interfaces whose hardware
// still matches the frozen plan. Unrelated hot-plug keeps its integrity failure.
func validatePointerPlanEvidence(encoded []byte, test plan.Test, frozen, final plan.Plan, snapshot inventory.Snapshot) bool {
	var result inputtest.PointerResult
	if json.Unmarshal(encoded, &result) != nil || !validatePointerEvidence(result) ||
		result.LimitMilliseconds > int64(test.TimeoutSeconds)*1000 || len(test.ComponentIDs) == 0 {
		return false
	}
	complete := result.Status == inputtest.StatusPass
	initialDigests, finalDigests := map[string]string{}, map[string]string{}
	for _, component := range frozen.HardwareGraph.Components {
		if component.Type == inventory.ComponentInput {
			initialDigests[component.ID] = component.Value
		}
	}
	for _, component := range final.HardwareGraph.Components {
		if component.Type == inventory.ComponentInput {
			finalDigests[component.ID] = component.Value
		}
	}
	var finalRefs []string
	for _, row := range final.Tests {
		if row.ID == test.ID {
			finalRefs = row.ComponentIDs
		}
	}
	if complete && !slices.Equal(test.ComponentIDs, finalRefs) {
		return false
	}
	components := make(map[string]inventory.Component)
	for _, component := range snapshot.Components {
		if component.Type == inventory.ComponentInput {
			components[component.ID] = component
		}
	}
	expected := map[string]inventory.Component{}
	for _, id := range test.ComponentIDs {
		component, exists := components[id]
		category, _ := component.Attribute("category")
		if !exists || category != "pointer" || !slices.Contains(finalRefs, id) || initialDigests[id] == "" || initialDigests[id] != finalDigests[id] {
			if complete {
				return false
			}
			continue
		}
		interfaceName, hasInterface := component.Attribute("interface")
		handlers, hasHandlers := component.Attribute("handlers")
		interfaces := []string{}
		if hasInterface {
			if !canonicalPointerInterface(interfaceName) {
				return false
			}
			interfaces = append(interfaces, interfaceName)
		}
		if hasHandlers {
			for _, handler := range strings.Fields(handlers) {
				if strings.HasPrefix(handler, "event") {
					if !canonicalPointerInterface(handler) || hasInterface && handler != interfaceName {
						return false
					}
					if !hasInterface {
						interfaces = append(interfaces, handler)
					}
				}
			}
		}
		if len(interfaces) == 0 {
			return false
		}
		for _, name := range interfaces {
			if _, duplicate := expected[name]; duplicate {
				return false
			}
			expected[name] = component
		}
	}
	if complete && len(expected) != len(result.Devices) {
		return false
	}
	for _, device := range result.Devices {
		if device.Status != inputtest.StatusPass {
			continue
		}
		component, exists := expected[device.Interface]
		if !exists || !pointerIdentityMatches(component, *device.Identity) || !pointerCapabilitiesMatch(component, *device.Capabilities) {
			return false
		}
	}
	return true
}

func pointerAttribute(component inventory.Component, names ...string) (string, bool) {
	value, found := "", false
	for _, name := range names {
		candidate, exists := component.Attribute(name)
		if exists {
			if found && candidate != value {
				return "", false
			}
			value, found = candidate, true
		}
	}
	return value, found
}

func pointerIdentityMatches(component inventory.Component, identity inputtest.PointerIdentity) bool {
	for _, field := range []struct {
		names []string
		value uint16
	}{
		{[]string{"bus"}, identity.Bus}, {[]string{"vendor", "vendor_id"}, identity.Vendor},
		{[]string{"product", "product_id"}, identity.Product}, {[]string{"version"}, identity.Version},
	} {
		encoded, exists := pointerAttribute(component, field.names...)
		value, err := strconv.ParseUint(encoded, 16, 16)
		if !exists || len(encoded) != 4 || err != nil || uint16(value) != field.value {
			return false
		}
	}
	return true
}

func pointerCapabilitiesMatch(component inventory.Component, capabilities inputtest.PointerCapabilities) bool {
	events, eventOK := pointerAttribute(component, "ev_capabilities", "event_capabilities")
	relative, relativeOK := pointerAttribute(component, "rel_capabilities", "relative_capabilities")
	keys, keyOK := pointerAttribute(component, "key_capabilities")
	layout, layoutOK := pointerAttribute(component, "bitmap_word_bits")
	wordBits := 0
	switch layout {
	case "32":
		wordBits = 32
	case "64":
		wordBits = 64
	}
	if !eventOK || !relativeOK || !keyOK || !layoutOK || wordBits == 0 {
		return false
	}
	// The layout is independently observed in inventory and HMAC-bound by the
	// frozen plan. Submitted button evidence cannot choose its interpretation.
	for _, check := range []struct {
		bitmap string
		bit    int
		want   bool
	}{
		{events, 0, true}, {events, 1, capabilities.Key}, {events, 2, capabilities.Relative}, {events, 3, capabilities.Absolute},
		{relative, 0, capabilities.RelativeX}, {relative, 1, capabilities.RelativeY},
		{keys, 272, slices.Contains(capabilities.Buttons, "left")}, {keys, 273, slices.Contains(capabilities.Buttons, "right")},
		{keys, 274, slices.Contains(capabilities.Buttons, "middle")},
	} {
		got, ok := pointerBitmapBit(check.bitmap, check.bit, wordBits)
		if !ok || got != check.want {
			return false
		}
	}
	return true
}

func pointerBitmapBit(value string, bit, wordBits int) (bool, bool) {
	words := strings.Fields(value)
	if len(words) == 0 || len(words) > 32 {
		return false, false
	}
	set := false
	for index, word := range words {
		if len(word) == 0 || len(word) > wordBits/4 {
			return false, false
		}
		parsed, err := strconv.ParseUint(word, 16, wordBits)
		if err != nil {
			return false, false
		}
		if len(words)-1-index == bit/wordBits {
			set = parsed&(uint64(1)<<uint(bit%wordBits)) != 0
		}
	}
	return set, true
}
