package inventory

import "strconv"

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
		attributes := make(map[string]string, len(component.Attributes)+1)
		for _, attribute := range collectionItems(collector, component.Attributes) {
			if !collector.check() {
				return nil
			}
			if attribute.Name != "bitmap_word_bits" {
				attributes[attribute.Name] = attribute.Value
			}
		}
		if wordBits == 32 || wordBits == 64 {
			attributes["bitmap_word_bits"] = strconv.Itoa(wordBits)
		}
		attributes["category"] = ClassifyInputWithBitmapWidth(attributes["name"], attributes["handlers"],
			attributes["key_capabilities"], firstNonEmpty(attributes["relative_capabilities"], attributes["rel_capabilities"]),
			firstNonEmpty(attributes["absolute_capabilities"], attributes["abs_capabilities"]),
			firstNonEmpty(attributes["properties"], attributes["prop_capabilities"]), wordBits)
		// Registry text cannot supply this reserved observation when the kernel
		// layout is unknown; that cannot establish a functional input pass.
		if !collector.check() {
			return nil
		}
		components[index] = NewComponent(component.Type, component.ID, attributes)
	}
	return components
}
