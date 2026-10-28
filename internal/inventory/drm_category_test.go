package inventory

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func expectedDRMCategorySnapshot() Snapshot {
	return NewSnapshot([]Component{
		NewComponent(ComponentVideo, "video:drm:card0", map[string]string{
			"interface": "card0", "category": "display_controller", "device_number": "226:0",
			"vendor_id": "0x1234", "device_id": "0xabcd", "subsystem_vendor_id": "0x5678", "subsystem_device_id": "0xef01",
		}),
		NewComponent(ComponentVideo, "video:drm:card0-HDMI-A-1", map[string]string{
			"interface": "card0-HDMI-A-1", "category": "display_connector", "status": "connected", "enabled": "enabled",
		}),
		NewComponent(ComponentVideo, "video:drm:renderD128", map[string]string{
			"interface": "renderD128", "category": "render_interface", "device_number": "226:128",
			"vendor_id": "0x1234", "device_id": "0xabcd", "subsystem_vendor_id": "0x5678", "subsystem_device_id": "0xef01",
		}),
	})
}

func TestInventoryDRMCategoriesRequestOnlyOwnedAttributes(t *testing.T) {
	// Literal category contract, independent of production path lists.
	values := map[string]string{
		"sys/class/drm/card0/dev":                          "226:0\n",
		"sys/class/drm/card0/device/vendor":                "0x1234\n",
		"sys/class/drm/card0/device/device":                "0xabcd\n",
		"sys/class/drm/card0/device/subsystem_vendor":      "0x5678\n",
		"sys/class/drm/card0/device/subsystem_device":      "0xef01\n",
		"sys/class/drm/card0-HDMI-A-1/status":              "connected\n",
		"sys/class/drm/card0-HDMI-A-1/enabled":             "enabled\n",
		"sys/class/drm/renderD128/dev":                     "226:128\n",
		"sys/class/drm/renderD128/device/vendor":           "0x1234\n",
		"sys/class/drm/renderD128/device/device":           "0xabcd\n",
		"sys/class/drm/renderD128/device/subsystem_vendor": "0x5678\n",
		"sys/class/drm/renderD128/device/subsystem_device": "0xef01\n",
	}
	requests := map[string]int{}
	source := collectionSource{
		directory: func(name string) ([]fs.DirEntry, error) {
			if name == "sys/class/drm" {
				return enumerationEntries("renderD128", "version", "card0-HDMI-A-1", "card0"), nil
			}
			return nil, fs.ErrNotExist
		},
		file: func(name string) ([]byte, error) {
			if !strings.HasPrefix(name, "sys/class/drm/") {
				return nil, fs.ErrNotExist
			}
			requests[name]++
			if value, ok := values[name]; ok {
				return []byte(value), nil
			}
			// Another category's role must not hide as optional.
			return bytes.Repeat([]byte{'x'}, 65537), nil
		},
	}
	snapshot, err := NewLinuxCollectorWithSource(source).Collect(t.Context())
	if err != nil || !snapshotsEqual(snapshot, expectedDRMCategorySnapshot()) {
		t.Fatal("wrong DRM category acquisition", err)
	}
	if len(requests) != 12 {
		t.Fatalf("DRM request set has %d paths, want 12", len(requests))
	}
	for name := range values {
		if requests[name] != 1 {
			t.Fatalf("declared role omitted or repeated: %s", name)
		}
	}
}

func TestInventoryDRMCategoriesPreserveOptionalAbsence(t *testing.T) {
	for _, observationErr := range []error{fs.ErrNotExist, fs.ErrPermission} {
		source := collectionSource{
			directory: func(name string) ([]fs.DirEntry, error) {
				if name == "sys/class/drm" {
					return enumerationEntries("card0", "card0-HDMI-A-1", "renderD128"), nil
				}
				return nil, fs.ErrNotExist
			},
			file: func(string) ([]byte, error) { return []byte("uncredited partial value"), observationErr },
		}
		want := NewSnapshot([]Component{
			NewComponent(ComponentVideo, "video:drm:card0", map[string]string{"interface": "card0", "category": "display_controller"}),
			NewComponent(ComponentVideo, "video:drm:card0-HDMI-A-1", map[string]string{"interface": "card0-HDMI-A-1", "category": "display_connector"}),
			NewComponent(ComponentVideo, "video:drm:renderD128", map[string]string{"interface": "renderD128", "category": "render_interface"}),
		})
		snapshot, err := NewLinuxCollectorWithSource(source).Collect(t.Context())
		if err != nil || !snapshotsEqual(snapshot, want) {
			t.Fatal("optional DRM observation changed discovered components", err)
		}
	}
}

func TestInventoryDRMDeclaredScalarFailuresRemainHard(t *testing.T) {
	for _, category := range []struct {
		name   string
		fields []string
	}{
		{"card0", []string{"dev", "device/vendor", "device/device", "device/subsystem_vendor", "device/subsystem_device"}},
		{"card0-HDMI-A-1", []string{"status", "enabled"}},
		{"renderD128", []string{"dev", "device/vendor", "device/device", "device/subsystem_vendor", "device/subsystem_device"}},
	} {
		for _, field := range category.fields {
			for _, mode := range []string{"directory", "overflow"} {
				t.Run(category.name+"/"+field+"/"+mode, func(t *testing.T) {
					root := t.TempDir()
					name := filepath.Join(root, "sys", "class", "drm", category.name, filepath.FromSlash(field))
					if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(name, []byte("valid observation\n"), 0600); err != nil {
						t.Fatal(err)
					}
					collector := NewLinuxCollector(root)
					positive, err := collector.Collect(t.Context())
					if err != nil || len(positive.Components) != 1 {
						t.Fatal("declared-role positive control failed", err)
					}
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
						if err := os.WriteFile(name, bytes.Repeat([]byte{' '}, 65537), 0600); err != nil {
							t.Fatal(err)
						}
						want = ErrScalarLimit
					}
					snapshot, err := collector.Collect(t.Context())
					requireNoCollection(t, snapshot, err, want)
				})
			}
		}
	}
}

func TestInventoryDRMFinalCategoryReadCancellation(t *testing.T) {
	for _, item := range []struct{ name, field string }{
		{"card0", "device/subsystem_device"}, {"card0-HDMI-A-1", "enabled"}, {"renderD128", "device/subsystem_device"},
	} {
		t.Run(item.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reached := 0
			source := collectionSource{
				directory: func(name string) ([]fs.DirEntry, error) {
					if name == "sys/class/drm" {
						return enumerationEntries(item.name), nil
					}
					return nil, fs.ErrNotExist
				},
				file: func(name string) ([]byte, error) {
					if name == "sys/class/drm/"+item.name+"/"+item.field {
						reached++
						cancel()
						return []byte("uncredited"), nil
					}
					return nil, fs.ErrNotExist
				},
			}
			snapshot, err := NewLinuxCollectorWithSource(source).Collect(ctx)
			requireNoCollection(t, snapshot, err, context.Canceled)
			if reached != 1 {
				t.Fatal("last category observation was not reached")
			}
		})
	}
}
