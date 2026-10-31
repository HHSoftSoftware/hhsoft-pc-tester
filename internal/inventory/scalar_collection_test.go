package inventory

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type scalarRoleGroup struct {
	name, directory, base string
	entries, fields       []string
}

// Literal coverage of the acquisition contract, including the three direct
// discriminators. These fixtures run full Collect with each role made large.
func scalarRoleGroups() []scalarRoleGroup {
	return []scalarRoleGroup{
		{"cpu", "sys/devices/system/cpu", "sys/devices/system/cpu/cpu0", []string{"cpu0"}, []string{"online", "topology/core_id", "topology/physical_package_id", "topology/thread_siblings_list", "topology/core_siblings_list", "cpufreq/cpuinfo_max_freq", "cpu_capacity"}},
		{"dmi", "", "sys/class/dmi/id", nil, []string{"sys_vendor", "product_name", "product_family", "product_version", "product_serial", "product_uuid", "product_sku", "chassis_vendor", "chassis_type", "chassis_version", "chassis_serial", "board_vendor", "board_name", "board_version", "board_serial", "board_asset_tag"}},
		{"block", "sys/class/block", "sys/class/block/disk0", []string{"disk0"}, []string{"dev", "size", "ro", "removable", "queue/logical_block_size", "queue/physical_block_size", "queue/rotational", "device/vendor", "device/model", "device/serial", "wwid", "device/type", "partition"}},
		{"pci", "sys/bus/pci/devices", "sys/bus/pci/devices/0000:00:01.0", []string{"0000:00:01.0"}, []string{"class", "vendor", "device", "subsystem_vendor", "subsystem_device", "revision", "irq", "numa_node", "modalias"}},
		{"network", "sys/class/net", "sys/class/net/eth0", []string{"eth0"}, []string{"address", "type", "ifindex", "iflink", "mtu", "operstate", "carrier", "speed", "duplex", "device/vendor", "device/device"}},
		{"input", "sys/class/input", "sys/class/input/event0/device", []string{"event0"}, []string{"name", "phys", "uniq", "id/bustype", "id/vendor", "id/product", "id/version", "capabilities/ev", "capabilities/key", "capabilities/abs", "capabilities/rel", "capabilities/msc", "properties"}},
		{"sound", "sys/class/sound", "sys/class/sound/card0", []string{"card0"}, []string{"id", "device/vendor", "device/device"}},
		{"drm", "sys/class/drm", "sys/class/drm/card0", []string{"card0"}, []string{"dev", "device/vendor", "device/device", "device/subsystem_vendor", "device/subsystem_device"}},
		{"video", "sys/class/video4linux", "sys/class/video4linux/video0", []string{"video0"}, []string{"name", "index", "dev", "device/modalias"}},
		{"framebuffer", "sys/class/graphics", "sys/class/graphics/fb0", []string{"fb0"}, []string{"name", "virtual_size", "bits_per_pixel", "dev"}},
		{"usb", "sys/bus/usb/devices", "sys/bus/usb/devices/1-1", []string{"1-1", "1-1:1.0"}, []string{"idProduct", "manufacturer", "product", "serial", "bcdDevice", "bDeviceClass", "bDeviceSubClass", "bDeviceProtocol", "busnum", "devnum", "speed", "authorized", "idVendor"}},
		{"usb-interface", "sys/bus/usb/devices", "sys/bus/usb/devices/1-1:1.0", []string{"1-1", "1-1:1.0"}, []string{"bInterfaceClass"}},
		// Preserve earlier group indices; cover all three DRM category bindings.
		{"drm-connector", "sys/class/drm", "sys/class/drm/card0-HDMI-A-1", []string{"card0-HDMI-A-1"}, []string{"status", "enabled"}},
		{"drm-render", "sys/class/drm", "sys/class/drm/renderD128", []string{"renderD128"}, []string{"dev", "device/vendor", "device/device", "device/subsystem_vendor", "device/subsystem_device"}},
	}
}

func scalarRoleSource(group scalarRoleGroup, target string, value []byte, outcome error, reached *int, cancel func()) PathSource {
	return collectionSource{
		directory: func(name string) ([]fs.DirEntry, error) {
			if name == group.directory {
				return enumerationEntries(group.entries...), nil
			}
			return nil, fs.ErrNotExist
		},
		file: func(name string) ([]byte, error) {
			if name == target {
				(*reached)++
				if cancel != nil {
					cancel()
				}
				return value, outcome
			}
			// A readable device vendor is a real prerequisite for all later USB
			// attributes and nested-interface classification; interfaces lack it.
			if name == "sys/bus/usb/devices/1-1/idVendor" {
				return []byte("1234\n"), nil
			}
			return nil, fs.ErrNotExist
		},
	}
}

func TestInventoryScalarEveryDeclaredAndDirectRoleFailsClosed(t *testing.T) {
	count := 0
	for _, group := range scalarRoleGroups() {
		for _, field := range group.fields {
			count++
			target := path.Join(group.base, field)
			t.Run(group.name+"/"+field, func(t *testing.T) {
				for _, simultaneous := range []bool{false, true} {
					ctx, cancel := context.WithCancel(t.Context())
					reached := 0
					var outcome error
					var stop func()
					if simultaneous {
						outcome, stop = fs.ErrPermission, cancel
					}
					source := scalarRoleSource(group, target, bytes.Repeat([]byte{' '}, 65537), outcome, &reached, stop)
					collector := NewLinuxCollectorWithSource(source)
					collector.inputBitmapWordBits = func() int { return 64 }
					snapshot, err := collector.Collect(ctx)
					requireNoCollection(t, snapshot, err, ErrScalarLimit)
					if reached != 1 {
						t.Fatalf("role was skipped or retried: %d", reached)
					}
					cancel()
				}
			})
		}
	}
	if count != 106 {
		t.Fatalf("scalar role coverage changed: %d", count)
	}
}

func TestInventoryScalarLongValuesRetainLiteralMeaningThroughLimit(t *testing.T) {
	for _, fixture := range []struct {
		group, field  string
		kind          ComponentType
		id, attribute string
	}{
		{"cpu", "topology/thread_siblings_list", ComponentCPU, "cpu:0", "thread_siblings"},
		{"pci", "modalias", ComponentPCI, "pci:0000:00:01.0", "modalias"},
		{"video", "device/modalias", ComponentVideo, "video:capture:video0", "modalias"},
		{"usb", "product", ComponentUSB, "usb:1-1", "product"},
		{"dmi", "product_name", ComponentSystem, "system", "product_name"},
	} {
		for _, group := range scalarRoleGroups() {
			if group.name != fixture.group {
				continue
			}
			for _, size := range []int{1, 65535, 65536} {
				t.Run(fmt.Sprintf("%s/%d", fixture.group, size), func(t *testing.T) {
					value := strings.Repeat("x", size)
					reached := 0
					source := scalarRoleSource(group, path.Join(group.base, fixture.field), []byte(value), nil, &reached, nil)
					snapshot, err := NewLinuxCollectorWithSource(source).Collect(t.Context())
					if err != nil || reached != 1 {
						t.Fatal("long scalar disappeared", err)
					}
					assertAttribute(t, snapshot, fixture.kind, fixture.id, fixture.attribute, value)
				})
			}
		}
	}
}

func TestInventoryScalarOptionalObservationPolicyStillDiscardsPartialValues(t *testing.T) {
	group := scalarRoleGroups()[4] // A real network attribute on an enumerated device.
	for _, failure := range []error{fs.ErrNotExist, fs.ErrPermission, errors.New("unsupported observation")} {
		reached := 0
		source := scalarRoleSource(group, path.Join(group.base, "speed"), []byte("must not survive"), failure, &reached, nil)
		snapshot, err := NewLinuxCollectorWithSource(source).Collect(t.Context())
		if err != nil || reached != 1 || len(snapshot.Components) != 1 {
			t.Fatal("optional observation policy changed", err)
		}
		if _, present := snapshot.Components[0].Attribute("speed_mbps"); present {
			t.Fatal("partial optional value admitted")
		}
		assertAttribute(t, snapshot, ComponentNetwork, "network:eth0", "name", "eth0")
	}
}

type scalarDecoratedSource struct {
	*RootPathSource
	file func(string) ([]byte, error)
}

func (s *scalarDecoratedSource) ReadFile(name string) ([]byte, error) { return s.file(name) }

func TestInventoryScalarProductionAndDecoratorDispatch(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "sys", "class", "dmi", "id", "product_name")
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, bytes.Repeat([]byte{'x'}, 65537), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewLinuxCollector(root).Collect(t.Context())
	requireNoCollection(t, snapshot, err, ErrScalarLimit)
	calls := 0
	decorator := &scalarDecoratedSource{RootPathSource: NewRootPathSource(root), file: func(name string) ([]byte, error) {
		if name == "sys/class/dmi/id/product_name" {
			calls++
			return []byte("decorated model\n"), nil
		}
		return nil, fs.ErrNotExist
	}}
	snapshot, err = NewLinuxCollectorWithSource(decorator).Collect(t.Context())
	if err != nil || calls != 1 {
		t.Fatal("decorator ReadFile bypassed", err)
	}
	assertAttribute(t, snapshot, ComponentSystem, "system", "product_name", "decorated model")
	decorator.file = func(name string) ([]byte, error) {
		if name == "sys/class/dmi/id/product_name" {
			return bytes.Repeat([]byte{' '}, 65537), fs.ErrPermission
		}
		return nil, fs.ErrNotExist
	}
	snapshot, err = NewLinuxCollectorWithSource(decorator).Collect(t.Context())
	requireNoCollection(t, snapshot, err, ErrScalarLimit)
}

func TestInventoryScalarNormalizationRequiresCompletedRawAdmission(t *testing.T) {
	for _, size := range []int{65536, 65537} {
		collection := NewLinuxCollectorWithSource(collectionSource{file: func(string) ([]byte, error) { return bytes.Repeat([]byte{' '}, size), nil }}).newCollection(t.Context())
		value, found := collection.readValue("scalar")
		if value != "" || found {
			t.Fatal("whitespace became a nonempty value")
		}
		if size == 65537 && collection.err != ErrScalarLimit || size == 65536 && collection.err != nil {
			t.Fatal("raw bound was applied after trimming")
		}
	}
	var source *RootPathSource
	var ctx *nilScalarContext
	for _, collection := range []*linuxCollection{
		NewLinuxCollectorWithSource(source).newCollection(t.Context()),
		NewLinuxCollectorWithSource(collectionSource{}).newCollection(ctx),
	} {
		value, found := collection.readValue("scalar")
		if value != "" || found || collection.err == nil {
			t.Fatal("typed-nil dependency admitted")
		}
	}
}

func TestInventoryScalarLastUSBAndInterfaceBreakCancellation(t *testing.T) {
	for _, target := range []string{"sys/bus/usb/devices/1-1/authorized", "sys/bus/usb/devices/1-1:1.0/bInterfaceClass"} {
		for _, observationErr := range []error{nil, fs.ErrPermission} {
			ctx, cancel := context.WithCancel(t.Context())
			reached := 0
			group := scalarRoleGroups()[10]
			source := scalarRoleSource(group, target, []byte("01"), observationErr, &reached, cancel)
			snapshot, err := NewLinuxCollectorWithSource(source).Collect(ctx)
			requireNoCollection(t, snapshot, err, context.Canceled)
			if reached != 1 {
				t.Fatal("last USB scalar cancellation was not reached")
			}
			cancel()
		}
	}
	// Positive nested-interface control proves the early-break branch itself.
	reached := 0
	group := scalarRoleGroups()[10]
	source := scalarRoleSource(group, "sys/bus/usb/devices/1-1:1.0/bInterfaceClass", []byte("01"), nil, &reached, nil)
	snapshot, err := NewLinuxCollectorWithSource(source).Collect(t.Context())
	if err != nil || reached != 1 {
		t.Fatal("USB interface control did not reach real branch", err)
	}
	assertAttribute(t, snapshot, ComponentUSB, "usb:1-1", "audio_interface", "true")
}

func TestInventoryScalarPrimaryProcRecordsRetainSeparateAcquisition(t *testing.T) {
	for _, primary := range []string{"proc/cpuinfo", "proc/meminfo", "proc/bus/input/devices"} {
		root := t.TempDir()
		name := filepath.Join(root, filepath.FromSlash(primary))
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		prefix := strings.Repeat(" ", 65537) + "\n"
		content := prefix
		switch primary {
		case "proc/cpuinfo":
			content += "processor: 0\nmodel name: primary CPU\n"
		case "proc/meminfo":
			content += "MemTotal: 4096 kB\n"
		default:
			content += "N: Name=\"primary keyboard\"\n"
		}
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		collector := NewLinuxCollector(root)
		collector.inputBitmapWordBits = func() int { return 64 }
		snapshot, err := collector.Collect(t.Context())
		if err != nil || len(snapshot.Components) != 1 {
			t.Fatal("scalar policy incorrectly bounded primary proc data", err)
		}
		switch primary {
		case "proc/cpuinfo":
			assertAttribute(t, snapshot, ComponentCPU, "cpu:0", "model", "primary CPU")
		case "proc/meminfo":
			assertAttribute(t, snapshot, ComponentMemory, "memory:system", "total_kib", "4096")
		default:
			assertAttribute(t, snapshot, ComponentInput, "input:0", "name", "primary keyboard")
		}
	}
}

func TestInventoryScalarRepeatedAndLargeDistinctCollections(t *testing.T) {
	bad := true
	var entries []fs.DirEntry
	for index := 324; index >= 0; index-- {
		entries = append(entries, &enumerationEntry{name: fmt.Sprintf("disk%03d", index)})
	}
	source := collectionSource{
		directory: func(name string) ([]fs.DirEntry, error) {
			if name == "sys/class/block" {
				return entries, nil
			}
			return nil, fs.ErrNotExist
		},
		file: func(name string) ([]byte, error) {
			if name == "sys/class/block/disk000/size" && bad {
				return bytes.Repeat([]byte{'9'}, 65537), nil
			}
			if strings.HasPrefix(name, "sys/class/block/") && strings.HasSuffix(name, "/size") {
				return []byte("4096\n"), nil
			}
			return nil, fs.ErrNotExist
		},
	}
	collector := NewLinuxCollectorWithSource(source)
	snapshot, err := collector.Collect(t.Context())
	requireNoCollection(t, snapshot, err, ErrScalarLimit)
	bad = false
	for attempt := 0; attempt < 2; attempt++ {
		snapshot, err = collector.Collect(t.Context())
		if err != nil || len(snapshot.Components) != 325 {
			t.Fatal("scalar failure persisted or instance count was capped", err)
		}
		for index := 0; index < 325; index++ {
			assertAttribute(t, snapshot, ComponentBlock, fmt.Sprintf("block:disk%03d", index), "size_sectors", "4096")
		}
	}
	if entries[0].Name() != "disk324" {
		t.Fatal("provider slice changed")
	}
	snapshot, err = NewLinuxCollector(t.TempDir()).Collect(t.Context())
	if err != nil || snapshot.SchemaVersion != 1 || len(snapshot.Components) != 0 {
		t.Fatal("diskless collection regressed", err)
	}
}

func TestInventoryScalarConcurrentCollectionsOwnCancellationAndError(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	var calls atomic.Int32
	source := collectionSource{file: func(name string) ([]byte, error) {
		if name == "sys/class/dmi/id/product_name" {
			if calls.Add(1) == 1 {
				close(entered)
				<-release
			}
			return []byte("independent system"), nil
		}
		return nil, fs.ErrNotExist
	}}
	collector := NewLinuxCollectorWithSource(source)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type answer struct {
		snapshot Snapshot
		err      error
	}
	first, second := make(chan answer, 1), make(chan answer, 1)
	go func() { s, e := collector.Collect(ctx); first <- answer{s, e} }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first scalar boundary not reached")
	}
	go func() { s, e := collector.Collect(t.Context()); second <- answer{s, e} }()
	select {
	case got := <-second:
		if got.err != nil || len(got.snapshot.Components) != 1 {
			t.Fatal("independent scalar collection inherited state", got.err)
		}
		assertAttribute(t, got.snapshot, ComponentSystem, "system", "product_name", "independent system")
	case <-time.After(3 * time.Second):
		t.Fatal("second scalar collection shared blocked state")
	}
	cancel()
	once.Do(func() { close(release) })
	select {
	case got := <-first:
		requireNoCollection(t, got.snapshot, got.err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("scalar source boundary did not return")
	}
}
