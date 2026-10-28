//go:build linux

package inventory

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func newDRMSysfsTopology(t *testing.T) (string, map[string]string) {
	t.Helper()
	root := t.TempDir()
	pci := filepath.Join(root, "sys/devices/pci0000:00/0000:00:02.0")
	card := filepath.Join(pci, "drm/card0")
	render := filepath.Join(pci, "drm/renderD128")
	connector := filepath.Join(card, "card0-HDMI-A-1")
	class := filepath.Join(root, "sys/class/drm")
	for _, directory := range []string{card, render, connector, class} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	paths := map[string]string{
		"card_dev": filepath.Join(card, "dev"), "render_dev": filepath.Join(render, "dev"),
		"vendor": filepath.Join(pci, "vendor"), "device": filepath.Join(pci, "device"),
		"subsystem_vendor": filepath.Join(pci, "subsystem_vendor"), "subsystem_device": filepath.Join(pci, "subsystem_device"),
		"status": filepath.Join(connector, "status"), "enabled": filepath.Join(connector, "enabled"),
	}
	values := map[string]string{"card_dev": "226:0\n", "render_dev": "226:128\n", "vendor": "0x1234\n", "device": "0xabcd\n", "subsystem_vendor": "0x5678\n", "subsystem_device": "0xef01\n", "status": "connected\n", "enabled": "enabled\n"}
	for name, value := range values {
		if err := os.WriteFile(paths[name], []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Normal sysfs layout: class links lead to device-tree objects. A connector
	// names its card; the card and render interface name the parent bus device.
	for _, link := range []struct{ name, target string }{
		{filepath.Join(class, "card0"), card}, {filepath.Join(class, "renderD128"), render},
		{filepath.Join(class, "card0-HDMI-A-1"), connector},
		{filepath.Join(card, "device"), pci}, {filepath.Join(render, "device"), pci},
		{filepath.Join(connector, "device"), card},
	} {
		relative, err := filepath.Rel(filepath.Dir(link.name), link.target)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(relative, link.name); err != nil {
			t.Fatal(err)
		}
	}
	return root, paths
}

func TestInventoryDRMSymlinkTopologyRetainsCategoryData(t *testing.T) {
	root, _ := newDRMSysfsTopology(t)
	class := filepath.Join(root, "sys/class/drm")
	for _, check := range []struct {
		name      string
		directory bool
	}{
		{"card0/device/device", false}, {"renderD128/device/device", false},
		{"card0-HDMI-A-1/device/device", true}, {"card0-HDMI-A-1/status", false},
	} {
		info, err := os.Stat(filepath.Join(class, filepath.FromSlash(check.name)))
		if err != nil || info.IsDir() != check.directory || (!check.directory && !info.Mode().IsRegular()) {
			t.Fatal("fixture did not establish the required followed object type", check.name, err)
		}
	}
	collector := NewLinuxCollector(root)
	for attempt := 0; attempt < 2; attempt++ {
		snapshot, err := collector.Collect(t.Context())
		if err != nil || !snapshotsEqual(snapshot, expectedDRMCategorySnapshot()) {
			t.Fatal("real DRM topology failed category collection", err)
		}
	}
}

func TestInventoryDRMSymlinkTopologyAllowsAbsentOptionalAttributes(t *testing.T) {
	root, paths := newDRMSysfsTopology(t)
	for _, name := range []string{"card_dev", "render_dev", "vendor", "device", "subsystem_vendor", "subsystem_device", "status", "enabled"} {
		if err := os.Remove(paths[name]); err != nil {
			t.Fatal(err)
		}
	}
	want := NewSnapshot([]Component{
		NewComponent(ComponentVideo, "video:drm:card0", map[string]string{"interface": "card0", "category": "display_controller"}),
		NewComponent(ComponentVideo, "video:drm:card0-HDMI-A-1", map[string]string{"interface": "card0-HDMI-A-1", "category": "display_connector"}),
		NewComponent(ComponentVideo, "video:drm:renderD128", map[string]string{"interface": "renderD128", "category": "render_interface"}),
	})
	snapshot, err := NewLinuxCollector(root).Collect(t.Context())
	if err != nil || !snapshotsEqual(snapshot, want) {
		t.Fatal("optional absence lost a discovered DRM component", err)
	}
}

func TestInventoryDRMSymlinkTopologyRejectsDeclaredScalarTypes(t *testing.T) {
	for _, category := range []struct{ name, target string }{
		{"controller", "device"}, {"connector", "status"}, {"render", "device"},
	} {
		for _, mode := range []string{"directory", "overflow"} {
			t.Run(category.name+"/"+mode, func(t *testing.T) {
				root, paths := newDRMSysfsTopology(t)
				if category.name == "render" {
					// The bus scalar is shared. Remove only the class-card link so
					// the render read itself must reach the hostile field.
					if err := os.Remove(filepath.Join(root, "sys/class/drm/card0")); err != nil {
						t.Fatal(err)
					}
				}
				positive, err := NewLinuxCollector(root).Collect(t.Context())
				wantCount := 3
				if category.name == "render" {
					wantCount = 2
				}
				if err != nil || len(positive.Components) != wantCount {
					t.Fatal("topology positive control failed", err)
				}
				name := paths[category.target]
				var want error
				if mode == "directory" {
					if err := os.Remove(name); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(name, 0700); err != nil {
						t.Fatal(err)
					}
					want = ErrScalarRead
				} else {
					if err := os.WriteFile(name, bytes.Repeat([]byte{'x'}, 65537), 0600); err != nil {
						t.Fatal(err)
					}
					want = ErrScalarLimit
				}
				snapshot, err := NewLinuxCollector(root).Collect(t.Context())
				requireNoCollection(t, snapshot, err, want)
			})
		}
	}
}
