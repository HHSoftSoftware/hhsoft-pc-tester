//go:build linux

package inputtest

import (
	"reflect"
	"testing"
)

func TestPointerWheelMetadataRequiresSecondByteAndBindsEveryBit(t *testing.T) {
	for bits := 0; bits < 16; bits++ {
		_, capabilities, err := queryPointerMetadata(func(request uintptr, buffer []byte) (int, error) {
			count, err := pointerMetadataFixture(request, buffer)
			if request == 0x80084522 {
				if bits&1 != 0 {
					buffer[0] |= 64
				}
				if bits&2 != 0 {
					buffer[1] |= 1
				}
				if bits&4 != 0 {
					buffer[1] |= 8
				}
				if bits&8 != 0 {
					buffer[1] |= 16
				}
				count = 2
			}
			return count, err
		})
		want := []PointerWheelCapability{}
		if bits&6 != 0 {
			want = append(want, PointerWheelCapability{Axis: "vertical", Legacy: bits&2 != 0, HighResolution: bits&4 != 0})
		}
		if bits&9 != 0 {
			want = append(want, PointerWheelCapability{Axis: "horizontal", Legacy: bits&1 != 0, HighResolution: bits&8 != 0})
		}
		if err != nil || !supportedPointer(capabilities) || !reflect.DeepEqual(capabilities.WheelAxes, want) {
			t.Fatalf("capability combination %d was reinterpreted", bits)
		}
	}
	for _, count := range []int{-1, 0, 1, 9} {
		_, _, err := queryPointerMetadata(func(request uintptr, buffer []byte) (int, error) {
			observed, err := pointerMetadataFixture(request, buffer)
			if request == 0x80084522 {
				return count, err
			}
			return observed, err
		})
		if err == nil {
			t.Fatal("short or impossible relative bitmap proved wheel absence")
		}
	}
}
