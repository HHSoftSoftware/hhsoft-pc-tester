package inventory

import "strconv"

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

func (collector *LinuxCollector) collectInput() []Component {
	components := collector.collectInputDevices()
	if len(components) == 0 {
		return components
	}
	observe := collector.inputBitmapWordBits
	if observe == nil {
		observe = kernelInputBitmapWordBits
	}
	wordBits := observe()
	for index, component := range components {
		attributes := make(map[string]string, len(component.Attributes)+1)
		for _, attribute := range component.Attributes {
			if attribute.Name != "bitmap_word_bits" {
				attributes[attribute.Name] = attribute.Value
			}
		}
		if wordBits == 32 || wordBits == 64 {
			attributes["bitmap_word_bits"] = strconv.Itoa(wordBits)
		}
		// Registry text cannot supply this reserved observation when the kernel
		// layout is unknown; that cannot establish a functional input pass.
		components[index] = NewComponent(component.Type, component.ID, attributes)
	}
	return components
}
