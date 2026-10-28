package inventory

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type procCase struct {
	role                          inventoryProcRole
	path                          string
	limit                         int
	kind                          ComponentType
	id, attribute, value, content string
}

func procCases() []procCase {
	return []procCase{
		{procMemory, "proc/meminfo", 262144, ComponentMemory, "memory:system", "total_kib", "4096", "MemTotal: 4096 kB\n"},
		{procCPU, "proc/cpuinfo", 16777216, ComponentCPU, "cpu:7", "model", "complete CPU", "processor: 7\nmodel name: complete CPU\n"},
		{procInput, "proc/bus/input/devices", 8388608, ComponentInput, "input:0", "name", "complete keyboard", "N: Name=\"complete keyboard\"\n"},
	}
}

func TestInventoryProcProductionRolesAndCustomRootPaths(t *testing.T) {
	for _, item := range procCases() {
		for _, mode := range []string{"complete-large", "overflow", "nonregular"} {
			t.Run(fmt.Sprintf("%d/%s", item.role, mode), func(t *testing.T) {
				root := t.TempDir()
				relative := strings.Replace(item.path, "proc/", "observations/", 1)
				name := filepath.Join(root, filepath.FromSlash(relative))
				if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
					t.Fatal(err)
				}
				var want error
				if mode == "nonregular" {
					if err := os.Mkdir(name, 0700); err != nil {
						t.Fatal(err)
					}
					want = ErrBulkRead
				} else {
					value := []byte(strings.Repeat(" ", 65537) + "\r\n" + item.content)
					if mode == "overflow" {
						value = bytes.Repeat([]byte{' '}, item.limit+1)
						want = ErrBulkLimit
					}
					if err := os.WriteFile(name, value, 0600); err != nil {
						t.Fatal(err)
					}
				}
				collector := NewLinuxCollector(root)
				collector.Paths.Proc = "observations"
				collector.inputBitmapWordBits = func() int { return 64 }
				snapshot, err := collector.Collect(t.Context())
				if want != nil {
					requireNoCollection(t, snapshot, err, want)
				} else {
					if err != nil || len(snapshot.Components) != 1 {
						t.Fatal("positive custom-path route not reached", err)
					}
					assertAttribute(t, snapshot, item.kind, item.id, item.attribute, item.value)
				}
			})
		}
	}
}

func TestInventoryProcLegacyDecoratorLengthAndObservationPolicy(t *testing.T) {
	for _, item := range procCases() {
		root := t.TempDir()
		backing := filepath.Join(root, filepath.FromSlash(item.path))
		if err := os.MkdirAll(filepath.Dir(backing), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(backing, bytes.Repeat([]byte{' '}, item.limit+1), 0600); err != nil {
			t.Fatal(err)
		}
		large := bytes.Repeat([]byte{' '}, item.limit+1)
		for _, oversized := range []bool{false, true} {
			for _, observationErr := range []error{nil, fs.ErrPermission, io.EOF} {
				for _, canceled := range []bool{false, true} {
					ctx, cancel := context.WithCancel(t.Context())
					calls := 0
					source := &scalarDecoratedSource{RootPathSource: NewRootPathSource(root), file: func(name string) ([]byte, error) {
						if name != item.path {
							return nil, fs.ErrNotExist
						}
						calls++
						if canceled {
							cancel()
						}
						if oversized {
							return large, observationErr
						}
						return []byte(item.content), observationErr
					}}
					collector := NewLinuxCollectorWithSource(source)
					collector.inputBitmapWordBits = func() int { return 64 }
					snapshot, err := collector.Collect(ctx)
					if calls != 1 {
						t.Fatal("declared decorator override bypassed")
					}
					switch {
					case oversized:
						requireNoCollection(t, snapshot, err, ErrBulkLimit)
					case canceled:
						requireNoCollection(t, snapshot, err, context.Canceled)
					case observationErr != nil:
						if err != nil || len(snapshot.Components) != 0 {
							t.Fatal("failed legacy observation produced records", err)
						}
					default:
						if err != nil || len(snapshot.Components) != 1 {
							t.Fatal("decorator positive control failed", err)
						}
						assertAttribute(t, snapshot, item.kind, item.id, item.attribute, item.value)
					}
					cancel()
				}
			}
		}
	}
}

func TestInventoryProcHardFailuresCannotUseSysfsFallback(t *testing.T) {
	for _, role := range []inventoryProcRole{procCPU, procInput} {
		var item procCase
		for _, candidate := range procCases() {
			if candidate.role == role {
				item = candidate
			}
		}
		for _, mode := range []string{"unknown", "overflow", "cancel"} {
			ctx, cancel := context.WithCancel(t.Context())
			reached, fallback := 0, 0
			source := collectionSource{file: func(name string) ([]byte, error) {
				if name == item.path {
					reached++
					if mode == "overflow" {
						return bytes.Repeat([]byte{' '}, item.limit+1), fs.ErrPermission
					}
					if mode == "cancel" {
						cancel()
					}
					return []byte(item.content), fs.ErrPermission
				}
				if role == procInput && name == "sys/class/input/event9/device/name" {
					return []byte("fallback input"), nil
				}
				return nil, fs.ErrNotExist
			}, directory: func(name string) ([]fs.DirEntry, error) {
				if role == procCPU && name == "sys/devices/system/cpu" {
					fallback++
					return enumerationEntries("cpu9"), nil
				}
				if role == procInput && name == "sys/class/input" {
					fallback++
					return enumerationEntries("event9"), nil
				}
				return nil, fs.ErrNotExist
			}}
			collector := NewLinuxCollectorWithSource(source)
			collector.inputBitmapWordBits = func() int { return 64 }
			snapshot, err := collector.Collect(ctx)
			if reached != 1 {
				t.Fatal("primary observation was not reached")
			}
			switch mode {
			case "overflow":
				requireNoCollection(t, snapshot, err, ErrBulkLimit)
			case "cancel":
				requireNoCollection(t, snapshot, err, context.Canceled)
			default:
				if err != nil || fallback != 1 || len(snapshot.Components) != 1 {
					t.Fatal("optional fallback control failed", err)
				}
				if role == procCPU {
					assertAttribute(t, snapshot, ComponentCPU, "cpu:9", "logical_index", "9")
				} else {
					assertAttribute(t, snapshot, ComponentInput, "input:event9", "name", "fallback input")
				}
			}
			if mode != "unknown" && fallback != 0 {
				t.Fatal("hard primary error reached fallback acquisition")
			}
			cancel()
		}
		// The owned nonregular path is hard even when a real fallback exists.
		root := t.TempDir()
		writeFixture(t, root, map[string]string{"sys/devices/system/cpu/cpu9/online": "1", "sys/class/input/event9/device/name": "fallback input"})
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(item.path)), 0700); err != nil {
			t.Fatal(err)
		}
		collector := NewLinuxCollector(root)
		collector.inputBitmapWordBits = func() int { return 64 }
		snapshot, err := collector.Collect(t.Context())
		requireNoCollection(t, snapshot, err, ErrBulkRead)
	}
}

func TestInventoryProcDistinctRecordsAreNeverCountTruncated(t *testing.T) {
	for _, role := range []inventoryProcRole{procCPU, procInput} {
		var content strings.Builder
		for index := 0; index < 325; index++ {
			if role == procCPU {
				fmt.Fprintf(&content, "processor: %d\nmodel name: CPU-%03d\n\n", index, index)
			} else {
				fmt.Fprintf(&content, "N: Name=\"Input-%03d\"\n\n", index)
			}
		}
		name := "proc/cpuinfo"
		if role == procInput {
			name = "proc/bus/input/devices"
		}
		root := t.TempDir()
		writeFixture(t, root, map[string]string{name: content.String()})
		collector := NewLinuxCollector(root)
		collector.inputBitmapWordBits = func() int { return 64 }
		snapshot, err := collector.Collect(t.Context())
		if err != nil || len(snapshot.Components) != 325 {
			t.Fatal("complete record set failed", err)
		}
		for index := 0; index < 325; index++ {
			if role == procCPU {
				assertAttribute(t, snapshot, ComponentCPU, fmt.Sprintf("cpu:%d", index), "model", fmt.Sprintf("CPU-%03d", index))
			} else {
				assertAttribute(t, snapshot, ComponentInput, fmt.Sprintf("input:%d", index), "name", fmt.Sprintf("Input-%03d", index))
			}
		}
	}
}

func TestInventoryProcPerCallStateAndInputOwnership(t *testing.T) {
	for _, item := range procCases() {
		bad := true
		small := []byte(item.content)
		saved := append([]byte(nil), small...)
		source := collectionSource{file: func(name string) ([]byte, error) {
			if name != item.path {
				return nil, fs.ErrNotExist
			}
			if bad {
				return bytes.Repeat([]byte{' '}, item.limit+1), nil
			}
			return small, nil
		}}
		collector := NewLinuxCollectorWithSource(source)
		collector.inputBitmapWordBits = func() int { return 64 }
		snapshot, err := collector.Collect(t.Context())
		requireNoCollection(t, snapshot, err, ErrBulkLimit)
		bad = false
		for attempt := 0; attempt < 2; attempt++ {
			snapshot, err = collector.Collect(t.Context())
			if err != nil || len(snapshot.Components) != 1 || !bytes.Equal(small, saved) {
				t.Fatal("failure persisted or source bytes mutated", err)
			}
			assertAttribute(t, snapshot, item.kind, item.id, item.attribute, item.value)
		}
	}
	var source *RootPathSource
	var ctx *nilScalarContext
	for _, collection := range []*linuxCollection{NewLinuxCollectorWithSource(source).newCollection(t.Context()), NewLinuxCollectorWithSource(collectionSource{}).newCollection(ctx)} {
		data, observed := collection.readProc("unused", procMemory)
		if data != nil || observed || collection.err == nil {
			t.Fatal("typed-nil collection dependency admitted")
		}
	}
	collection := NewLinuxCollectorWithSource(collectionSource{file: func(string) ([]byte, error) { t.Fatal("invalid role reached provider"); return nil, nil }}).newCollection(t.Context())
	data, observed := collection.readProc("unused", 255)
	if data != nil || observed || collection.err != ErrBulkRead {
		t.Fatal("invalid role was not hard")
	}
}
