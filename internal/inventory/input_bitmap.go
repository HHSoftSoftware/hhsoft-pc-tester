package inventory

import (
	"strconv"
	"strings"
)

// ObservedInputBitmapWordBits observes the kernel format without changing state.
// Zero means that a functional input test cannot rely on registry bitmaps.
func ObservedInputBitmapWordBits() int { return kernelInputBitmapWordBits() }

func observedInputBitmapWordBits(personality func() (uintptr, error), machine func() (string, error)) int {
	value, err := personality()
	if err != nil || value&0xff != 0 {
		// PER_LINUX32 and other personalities can override uname.machine.
		return 0
	}
	name, err := machine()
	if err != nil {
		return 0
	}
	return inputBitmapWordBitsForMachine(name)
}

func inputBitmapWordBitsForMachine(machine string) int {
	switch machine {
	case "x86_64", "aarch64":
		return 64
	case "i386", "i486", "i586", "i686":
		return 32
	default:
		return 0
	}
}

func (collector *linuxCollection) collectInput() []Component {
	components := collector.collectInputDevices()
	if !collector.check() {
		return nil
	}
	if len(components) == 0 {
		return components
	}
	observe := collector.inputBitmapWordBits
	if observe == nil {
		observe = kernelInputBitmapWordBits
	}
	if !collector.check() {
		return nil
	}
	wordBits := observe()
	if !collector.check() {
		return nil
	}
	for index, component := range collectionItems(collector, components) {
		if !collector.check() {
			return nil
		}
		attributes := make(map[string]string, len(component.Attributes)+2)
		for _, attribute := range collectionItems(collector, component.Attributes) {
			if !collector.check() {
				return nil
			}
			if attribute.Name != "bitmap_word_bits" && attribute.Name != "touchpad_candidate" {
				attributes[attribute.Name] = attribute.Value
			}
		}
		if wordBits == 32 || wordBits == 64 {
			attributes["bitmap_word_bits"] = strconv.Itoa(wordBits)
		}
		absolute := firstNonEmpty(attributes["absolute_capabilities"], attributes["abs_capabilities"])
		attributes["category"] = ClassifyInputWithBitmapWidth(attributes["name"], attributes["handlers"],
			attributes["key_capabilities"], firstNonEmpty(attributes["relative_capabilities"], attributes["rel_capabilities"]),
			absolute,
			firstNonEmpty(attributes["properties"], attributes["prop_capabilities"]), wordBits)
		// Match the adapter's discovery predicate, not descriptor support.
		// In particular, a nonempty ABS word of "0" still requires inspection.
		category := attributes["category"]
		attributes["touchpad_candidate"] = strconv.FormatBool(category == "touchpad" ||
			absolute != "" && (category == "input" || category == "pointer"))
		if value, ok := canonicalInputMSC(attributes["msc_capabilities"], wordBits); ok {
			attributes["msc_capabilities"] = value
		} else {
			// Absent, malformed or unknown-width data cannot become an empty set.
			delete(attributes, "msc_capabilities")
		}
		// Registry text cannot supply either reserved derived observation.
		// A discovery candidate cannot establish a functional input pass.
		if !collector.check() {
			return nil
		}
		components[index] = NewComponent(component.Type, component.ID, attributes)
	}
	return components
}

// canonicalInputMSC preserves all represented bits in a bounded 64-bit
// projection, including unknown codes. The observed kernel word width controls
// word ordering; the collector never chooses a width from registry text.
// Raw acquisition remains subject to the existing scalar/proc reader budgets.
// Only a nonempty, valid zero bitmap becomes "0"; unavailable data stay absent.
func canonicalInputMSC(value string, wordBits int) (string, bool) {
	if (wordBits != 32 && wordBits != 64) || len(value) == 0 || len(value) > 128 {
		return "", false
	}
	words := strings.Fields(value)
	if len(words) == 0 || len(words) > 64/wordBits {
		return "", false
	}
	var bits uint64
	for _, word := range words {
		if len(word) == 0 || len(word) > wordBits/4 {
			return "", false
		}
		for _, digit := range word {
			if !(digit >= '0' && digit <= '9' || digit >= 'a' && digit <= 'f' || digit >= 'A' && digit <= 'F') {
				return "", false
			}
		}
		part, err := strconv.ParseUint(word, 16, wordBits)
		if err != nil {
			return "", false
		}
		bits = bits<<uint(wordBits) | part
	}
	if wordBits == 32 && bits>>32 != 0 {
		return strconv.FormatUint(bits>>32, 16) + " " + strconv.FormatUint(bits&0xffffffff, 16), true
	}
	return strconv.FormatUint(bits, 16), true
}
