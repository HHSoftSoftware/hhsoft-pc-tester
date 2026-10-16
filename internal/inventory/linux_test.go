package inventory

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLinuxCollectorReadsInventoryFixture(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sysfs PCI/USB fixture names contain colons, which Windows filesystems reject")
	}
	root := t.TempDir()
	writeFixture(t, root, map[string]string{
		"proc/cpuinfo":           "processor : 0\nvendor_id : GenuineFixture\nmodel name : Test CPU\nphysical id : 0\ncore id : 0\nflags : fpu sse2\n\nprocessor : 1\nvendor_id : GenuineFixture\nmodel name : Test CPU\nphysical id : 0\ncore id : 1\nflags : fpu sse2\n",
		"proc/meminfo":           "MemTotal:       16384000 kB\nMemAvailable:   8192000 kB\n",
		"proc/bus/input/devices": "I: Bus=0011 Vendor=0001 Product=0001 Version=ab41\nN: Name=\"Fixture Keyboard\"\nP: Phys=isa0060/serio0/input0\nH: Handlers=sysrq kbd event0\nB: EV=120013\n\nI: Bus=0018 Vendor=04f3 Product=31be Version=0100\nN: Name=\"Fixture Touchpad\"\nH: Handlers=mouse0 event1\nB: EV=b\nB: ABS=260800000000003\n",
		"sys/devices/system/cpu/cpu0/topology/core_id":     "0\n",
		"sys/devices/system/cpu/cpu1/topology/core_id":     "1\n",
		"sys/class/dmi/id/sys_vendor":                      "Fixture Systems\n",
		"sys/class/dmi/id/product_name":                    "Model One\n",
		"sys/class/dmi/id/product_uuid":                    "11111111-2222-3333-4444-555555555555\n",
		"sys/class/dmi/id/board_vendor":                    "Fixture Boards\n",
		"sys/class/dmi/id/board_name":                      "Board One\n",
		"sys/class/block/nvme0n1/size":                     "2000000\n",
		"sys/class/block/nvme0n1/queue/logical_block_size": "512\n",
		"sys/class/block/nvme0n1/device/model":             "Fixture   NVMe\n",
		"sys/class/block/nvme0n1p1/partition":              "1\n",
		"sys/class/block/nvme0n1p1/size":                   "1000000\n",
		"sys/bus/pci/devices/0000:01:00.0/class":           "0x030000\n",
		"sys/bus/pci/devices/0000:01:00.0/vendor":          "0x1234\n",
		"sys/bus/pci/devices/0000:01:00.0/device":          "0xabcd\n",
		"sys/class/net/eth0/address":                       "02:00:00:00:00:01\n",
		"sys/class/net/eth0/operstate":                     "down\n",
		"sys/class/net/eth0/device/vendor":                 "0x8086\n",
		"sys/class/sound/card0/id":                         "FixtureAudio\n",
		"sys/class/sound/card0/device/vendor":              "0x8086\n",
		"sys/class/sound/controlC0/dev":                    "116:0\n",
		"sys/class/drm/card0/device/vendor":                "0x1234\n",
		"sys/class/drm/card0/device/device":                "0xabcd\n",
		"sys/class/drm/card0-HDMI-A-1/status":              "connected\n",
		"sys/class/video4linux/video0/name":                "Fixture Camera\n",
		"sys/class/video4linux/video0/index":               "0\n",
		"sys/class/graphics/fb0/name":                      "fixturefb\n",
		"sys/class/graphics/fb0/bits_per_pixel":            "32\n",
		"sys/bus/usb/devices/1-2/idVendor":                 "cafe\n",
		"sys/bus/usb/devices/1-2/idProduct":                "4001\n",
		"sys/bus/usb/devices/1-2/manufacturer":             "Fixture USB\n",
		"sys/bus/usb/devices/1-2:1.0/bInterfaceClass":      "03\n",
		"sys/bus/usb/devices/1-2:1.1/bInterfaceClass":      "01\n",
	})

	collector := NewLinuxCollector(root)
	first, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !snapshotsEqual(first, second) {
		t.Fatalf("repeated snapshots differ:\n%#v\n%#v", first, second)
	}

	wantCounts := map[ComponentType]int{
		ComponentCPU: 2, ComponentMemory: 1, ComponentSystem: 1, ComponentBaseboard: 1,
		ComponentBlock: 2, ComponentPCI: 1, ComponentNetwork: 1,
		ComponentInput: 2, ComponentSound: 1, ComponentVideo: 4, ComponentUSB: 1,
	}
	counts := make(map[ComponentType]int)
	for _, component := range first.Components {
		counts[component.Type]++
	}
	for componentType, want := range wantCounts {
		if counts[componentType] != want {
			t.Errorf("%s count = %d, want %d", componentType, counts[componentType], want)
		}
	}

	assertAttribute(t, first, ComponentCPU, "cpu:1", "core_id", "1")
	assertAttribute(t, first, ComponentMemory, "memory:system", "total_kib", "16384000")
	assertAttribute(t, first, ComponentSystem, "system", "product_name", "Model One")
	assertAttribute(t, first, ComponentBaseboard, "baseboard", "vendor", "Fixture Boards")
	assertAttribute(t, first, ComponentBlock, "block:nvme0n1", "model", "Fixture NVMe")
	assertAttribute(t, first, ComponentBlock, "block:nvme0n1p1", "kind", "partition")
	assertAttribute(t, first, ComponentPCI, "pci:0000:01:00.0", "class", "0x030000")
	assertAttribute(t, first, ComponentNetwork, "network:eth0", "address", "02:00:00:00:00:01")
	assertAttribute(t, first, ComponentInput, "input:0", "category", "keyboard")
	assertAttribute(t, first, ComponentInput, "input:1", "category", "touchpad")
	assertAttribute(t, first, ComponentSound, "sound:card0", "id", "FixtureAudio")
	assertAttribute(t, first, ComponentVideo, "video:drm:card0-HDMI-A-1", "status", "connected")
	assertAttribute(t, first, ComponentVideo, "video:capture:video0", "name", "Fixture Camera")
	assertAttribute(t, first, ComponentUSB, "usb:1-2", "product_id", "4001")
	assertAttribute(t, first, ComponentUSB, "usb:1-2", "audio_interface", "true")
	if _, ok := findComponent(first, ComponentUSB, "usb:1-2:1.0"); ok {
		t.Fatal("USB interface without idVendor became a device component")
	}
}

func TestLinuxCollectorUsesOnlyReadOnlyVirtualFiles(t *testing.T) {
	source := &recordingMissingSource{}
	snapshot, err := NewLinuxCollectorWithSource(source).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Components) != 0 {
		t.Fatalf("components = %#v", snapshot.Components)
	}
	for _, name := range source.paths {
		cleaned := strings.TrimPrefix(name, "/")
		if cleaned == "dev" || strings.HasPrefix(cleaned, "dev/") {
			t.Fatalf("collector attempted device-node access: %s", name)
		}
	}
}

type recordingMissingSource struct {
	paths []string
}

func (source *recordingMissingSource) ReadFile(name string) ([]byte, error) {
	source.paths = append(source.paths, name)
	return nil, fs.ErrNotExist
}

func (source *recordingMissingSource) ReadDir(name string) ([]fs.DirEntry, error) {
	source.paths = append(source.paths, name)
	return nil, fs.ErrNotExist
}

func writeFixture(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		fullPath := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func assertAttribute(t *testing.T, snapshot Snapshot, componentType ComponentType, id, name, want string) {
	t.Helper()
	component, ok := findComponent(snapshot, componentType, id)
	if !ok {
		t.Fatalf("missing %s %s", componentType, id)
	}
	got, ok := component.Attribute(name)
	if !ok || got != want {
		t.Fatalf("%s %s = %q, want %q", id, name, got, want)
	}
}

func findComponent(snapshot Snapshot, componentType ComponentType, id string) (Component, bool) {
	for _, component := range snapshot.Components {
		if component.Type == componentType && component.ID == id {
			return component, true
		}
	}
	return Component{}, false
}

func snapshotsEqual(left, right Snapshot) bool {
	if left.SchemaVersion != right.SchemaVersion || len(left.Components) != len(right.Components) {
		return false
	}
	for index := range left.Components {
		if left.Components[index].Type != right.Components[index].Type ||
			left.Components[index].ID != right.Components[index].ID ||
			len(left.Components[index].Attributes) != len(right.Components[index].Attributes) {
			return false
		}
		for attributeIndex := range left.Components[index].Attributes {
			if left.Components[index].Attributes[attributeIndex] != right.Components[index].Attributes[attributeIndex] {
				return false
			}
		}
	}
	return true
}
