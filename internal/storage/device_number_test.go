package storage

import "testing"

func TestDeviceNumberAcceptsOnlyCanonicalUint32Parts(t *testing.T) {
	for _, test := range []struct {
		text  string
		value deviceNumber
	}{
		{"0:0", deviceNumber{}},
		{"8:16", deviceNumber{major: 8, minor: 16}},
		{"259:65536", deviceNumber{major: 259, minor: 65536}},
		{"4294967295:4294967295", deviceNumber{major: ^uint32(0), minor: ^uint32(0)}},
	} {
		t.Run(test.text, func(t *testing.T) {
			value, ok := parseDeviceNumber(test.text)
			if !ok || value != test.value || value.String() != test.text {
				t.Fatalf("canonical identity was not retained: %q, %#v, %v", test.text, value, ok)
			}
		})
	}
	for index, text := range []string{"", ":", "8", "8:", ":0", "8:0:1", "+8:0", "8:+0", "-8:0", "8:-1",
		"00:0", "08:0", "8:00", "8:01", " 8:0", "8:0 ", "8:0\n", "8:0\r", "8:\t0", "8 :0", "8: 0",
		"4294967296:0", "0:4294967296", "00000000000:0", "８:0", "8:０", "8:\x000", "8:0x10"} {
		if _, ok := parseDeviceNumber(text); ok {
			t.Errorf("invalid identity %d was accepted", index)
		}
	}
}
