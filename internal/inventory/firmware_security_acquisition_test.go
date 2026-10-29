package inventory

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"reflect"
	"strings"
	"sync"
	"testing"
)

var firmwareAcquisitionRoles = []struct {
	path, name, raw, value string
	variable               bool
}{
	{"sys/firmware/efi/fw_platform_size", "uefi_platform_bits", "64\n", "64", false},
	{firmwareTestSecure, "secure_boot", "\x06\x00\x00\x00\x01", "1", true},
	{firmwareTestSetup, "setup_mode_flag", "\x06\x00\x00\x00\x00", "0", true},
	{"sys/class/tpm/PRIVATE-INSTANCE/tpm_version_major", "version_major", "2\n", "2", false},
}

func firmwareAcquisitionSource(t *testing.T, entries []fs.DirEntry, read func(string) ([]byte, error)) collectionSource {
	t.Helper()
	return collectionSource{directory: func(name string) ([]fs.DirEntry, error) {
		if name == "sys/class/tpm" {
			return entries, nil
		}
		if name == "sys/firmware/dmi/entries" {
			return nil, fs.ErrNotExist
		}
		if strings.Contains(name, "firmware") || strings.HasPrefix(name, "dev") {
			t.Fatal("unapproved enumeration", name)
		}
		return nil, fs.ErrNotExist
	}, file: func(name string) ([]byte, error) {
		if strings.HasPrefix(name, "sys/firmware/") || strings.HasPrefix(name, "sys/class/tpm/") || strings.HasPrefix(name, "dev/") {
			allowed := name == firmwareAcquisitionRoles[0].path || name == firmwareTestSecure || name == firmwareTestSetup || strings.HasPrefix(name, "sys/class/tpm/") && strings.HasSuffix(name, "/tpm_version_major")
			if !allowed {
				t.Fatal("unapproved security input", name)
			}
			return read(name)
		}
		return nil, fs.ErrNotExist
	}}
}

func TestFirmwareSecurityEveryRoleDiscardsFailedObservation(t *testing.T) {
	for _, role := range firmwareAcquisitionRoles {
		for _, variant := range []string{"valid", "empty", "data_error", "bound", "overflow", "overflow_cancel"} {
			t.Run(role.name+"/"+variant, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				var requests []string
				reached := 0
				source := firmwareAcquisitionSource(t, enumerationEntries("PRIVATE-INSTANCE"), func(name string) ([]byte, error) {
					requests = append(requests, name)
					if name == role.path {
						reached++
						switch variant {
						case "valid":
							return []byte(role.raw), nil
						case "empty":
							return []byte{}, nil
						case "data_error":
							return []byte(role.raw + "PRIVATE-FAILED-BYTES"), fs.ErrPermission
						case "bound":
							return bytes.Repeat([]byte{' '}, 65536), nil
						default:
							if variant == "overflow_cancel" {
								cancel()
							}
							return bytes.Repeat([]byte{'9'}, 65537), fs.ErrPermission
						}
					}
					for _, other := range firmwareAcquisitionRoles {
						if name == other.path {
							return []byte(other.raw), nil
						}
					}
					t.Fatal("unexpected role")
					return nil, fs.ErrNotExist
				})
				snapshot, err := NewLinuxCollectorWithSource(source).Collect(ctx)
				if reached != 1 {
					t.Fatal("fault boundary not reached")
				}
				if variant == "overflow" || variant == "overflow_cancel" {
					requireNoCollection(t, snapshot, err, ErrScalarLimit)
					if requests[len(requests)-1] != role.path {
						t.Fatal("read continued after limit")
					}
					return
				}
				if err != nil || len(snapshot.Components) != 2 || len(requests) != 4 {
					t.Fatal("positive field prerequisites", err)
				}
				for i, want := range firmwareAcquisitionRoles {
					if requests[i] != want.path {
						t.Fatal("exact input order changed")
					}
				}
				component := snapshot.Components[0]
				if role.name == "version_major" {
					component = snapshot.Components[1]
				}
				want := "malformed"
				if variant == "valid" {
					want = "reported"
				}
				if variant == "data_error" {
					want = "unavailable"
				}
				state, _ := component.Attribute(role.name + "_state")
				value, exists := component.Attribute(role.name)
				if state != want || exists != (variant == "valid") || exists && value != role.value {
					t.Fatal("field failure leaked favorable data")
				}
			})
		}
	}
}

func TestFirmwareSecurityOwnedRoleClosureAndFaultLedgers(t *testing.T) {
	for _, role := range firmwareAcquisitionRoles {
		for _, variant := range []string{"complete", "data_error", "later_error", "close_error", "error_close", "cancel_close", "cancel_close_error", "protocol_cancel", "handle_error", "nonregular", "typed_nil_info"} {
			t.Run(role.name+"/"+variant, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				f := scalarBytes([]byte(role.raw), 0, true)
				var want error
				var openErr error
				reads, stats := 1, 1
				switch variant {
				case "data_error", "error_close":
					f.read = func(p []byte) (int, error) { return copy(p, role.raw), fs.ErrPermission }
				case "later_error":
					calls := 0
					reads = 2
					f.read = func(p []byte) (int, error) {
						calls++
						if calls == 1 {
							return copy(p, role.raw[:1]), nil
						}
						return copy(p, role.raw[1:]), fs.ErrPermission
					}
				case "cancel_close", "cancel_close_error":
					f.onClose = cancel
					want = context.Canceled
				case "protocol_cancel":
					f.read = func(p []byte) (int, error) { cancel(); return len(p) + 1, fs.ErrPermission }
					want = ErrScalarRead
				case "handle_error":
					openErr = fs.ErrPermission
					want = ErrScalarRead
					stats = 0
					reads = 0
				case "nonregular":
					f.info = &scalarInformation{mode: fs.ModeDir}
					want = ErrScalarRead
					reads = 0
				case "typed_nil_info":
					var info *scalarInformation
					f.info = info
					want = ErrScalarRead
					reads = 0
				}
				if variant == "close_error" || variant == "error_close" || variant == "cancel_close_error" {
					f.closeErr = fs.ErrPermission
					want = ErrScalarClose
				}
				opens := 0
				raw, observed, err := readInventoryScalar(ctx, role.path, func(name string) (inventoryScalar, error) {
					opens++
					if name != role.path {
						t.Fatal("owner path changed")
					}
					return f, openErr
				})
				if opens != 1 || f.closes != 1 || f.stats != stats || len(f.requests) != reads {
					t.Fatal("reached ownership ledger", opens, f.closes, f.stats, f.requests)
				}
				for _, count := range f.requests {
					if count != 4096 {
						t.Fatal("read request changed")
					}
				}
				if variant != "complete" {
					requireScalarFailure(t, raw, observed, err, want)
					return
				}
				if err != nil || !observed || string(raw) != role.raw {
					t.Fatal("valid owned observation failed")
				}
				value, state := parseFirmwareSecurityScalar(firmwareSecurityField{name: role.name, variable: role.variable}, raw)
				if value != role.value || state != "reported" {
					t.Fatal("authentic core positive did not reach finite parser")
				}
			})
		}
	}
}

func TestFirmwareSecurityOwnedTerminalProbeStillBoundsRawBytes(t *testing.T) {
	for _, size := range []int{65536, 65537} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			f := scalarBytes(bytes.Repeat([]byte{'9'}, size), 0, false)
			raw, observed, err := readInventoryScalar(t.Context(), firmwareTestSecure, func(string) (inventoryScalar, error) { return f, nil })
			if f.closes != 1 || f.stats != 1 || len(f.requests) != 17 || f.requests[16] != 1 {
				t.Fatal("terminal probe or ownership not reached")
			}
			if size == 65537 {
				requireScalarFailure(t, raw, observed, err, ErrScalarLimit)
				return
			}
			if err != nil || !observed || len(raw) != 65536 {
				t.Fatal("raw boundary prerequisite")
			}
			if value, state := parseFirmwareSecurityScalar(firmwareSecurityField{name: "secure_boot", variable: true}, raw); value != "" || state != "malformed" {
				t.Fatal("binary parser widened shape")
			}
		})
	}
}

func TestFirmwareSecurityDirectoryFaultsAreNotAbsence(t *testing.T) {
	var typedNil *enumerationEntry
	for _, variant := range []string{"error", "entries_error", "nil", "typed_nil", "duplicate", "unsafe"} {
		t.Run(variant, func(t *testing.T) {
			reads := 0
			source := firmwareAcquisitionSource(t, nil, func(name string) ([]byte, error) {
				if strings.HasPrefix(name, "sys/class/tpm/") {
					reads++
				}
				return nil, fs.ErrNotExist
			})
			original := source.directory
			source.directory = func(name string) ([]fs.DirEntry, error) {
				if name != "sys/class/tpm" {
					return original(name)
				}
				switch variant {
				case "error":
					return nil, fs.ErrPermission
				case "entries_error":
					return enumerationEntries("private"), fs.ErrNotExist
				case "nil":
					return []fs.DirEntry{nil}, nil
				case "typed_nil":
					return []fs.DirEntry{typedNil}, nil
				case "duplicate":
					return enumerationEntries("private", "private"), nil
				default:
					return enumerationEntries("../private"), nil
				}
			}
			snapshot, err := NewLinuxCollectorWithSource(source).Collect(t.Context())
			want := ErrDirectoryEntry
			if variant == "error" || variant == "entries_error" {
				want = ErrDirectoryRead
			}
			requireNoCollection(t, snapshot, err, want)
			if reads != 0 {
				t.Fatal("invalid class reached fields")
			}
		})
	}
}

func TestFirmwareSecurityOwnedBatchesPreserveEveryPrivateOrdinal(t *testing.T) {
	const count = 129
	entries := make([]fs.DirEntry, count)
	calls := make([]int, count)
	for i := range entries {
		number := count - 1 - i
		entries[i] = &enumerationEntry{name: fmt.Sprintf("PRIVATE-%03d", number), onName: func() { calls[number]++ }}
	}
	original := append([]fs.DirEntry(nil), entries...)
	owner := &enumerationDirectory{batches: []enumerationBatch{{entries[:64], nil}, {entries[64:128], nil}, {entries[128:], io.EOF}}}
	owned, err := readInventoryDirectory(t.Context(), "sys/class/tpm", func(string) (inventoryDirectory, error) { return owner, nil })
	if err != nil || owner.closes != 1 || owner.stats != 1 || !reflect.DeepEqual(owner.requests, []int{64, 64, 64}) || !reflect.DeepEqual(entries, original) {
		t.Fatal("owned enumeration", err)
	}
	source := firmwareAcquisitionSource(t, owned, func(name string) ([]byte, error) {
		if strings.HasPrefix(name, "sys/class/tpm/") {
			return []byte("2\n"), nil
		}
		return nil, fs.ErrNotExist
	})
	snapshot, err := NewLinuxCollectorWithSource(source).Collect(t.Context())
	if err != nil || len(snapshot.Components) != count {
		t.Fatal("instance count capped", err)
	}
	found := map[string]bool{}
	for _, c := range snapshot.Components {
		found[c.ID] = true
		if c.Type != ComponentType("tpm") || !reflect.DeepEqual(c.Attributes, []Attribute{{"version_major", "2"}, {"version_major_state", "reported"}}) {
			t.Fatal("finite row")
		}
	}
	for i, n := range calls {
		if n != 1 || !found[fmt.Sprintf("tpm:%d", i+1)] {
			t.Fatal("name reread or ordinal lost")
		}
	}
	owner = &enumerationDirectory{batches: []enumerationBatch{{enumerationEntries("private"), io.EOF}}, closeErr: fs.ErrPermission}
	rejected, err := readInventoryDirectory(t.Context(), "sys/class/tpm", func(string) (inventoryDirectory, error) { return owner, nil })
	if rejected != nil || err != ErrDirectoryClose || owner.closes != 1 || len(owner.requests) != 1 {
		t.Fatal("directory Close fault lost")
	}
}

func TestFirmwareSecurityCancellationAtLastFieldDropsEarlierRows(t *testing.T) {
	for _, phase := range []string{"before", "final_name", "final_field", "ordinary_error"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			entries := enumerationEntries("a-private", "z-private")
			reached, fields := 0, 0
			if phase == "final_name" {
				entries[1].(*enumerationEntry).onName = func() { reached++; cancel() }
			}
			source := firmwareAcquisitionSource(t, entries, func(name string) ([]byte, error) {
				fields++
				if ctx.Err() != nil {
					t.Fatal("read after stop")
				}
				if name == "sys/class/tpm/z-private/tpm_version_major" {
					reached++
					cancel()
					if phase == "ordinary_error" {
						return []byte("2"), fs.ErrPermission
					}
				}
				if name == firmwareTestSecure || name == firmwareTestSetup {
					return []byte{6, 0, 0, 0, 1}, nil
				}
				return []byte("2"), nil
			})
			if phase == "before" {
				cancel()
			}
			snapshot, err := NewLinuxCollectorWithSource(source).Collect(ctx)
			requireNoCollection(t, snapshot, err, context.Canceled)
			if phase == "before" {
				if fields != 0 || reached != 0 {
					t.Fatal("prestop read")
				}
			} else if reached != 1 {
				t.Fatal("stop not reached")
			}
			if phase == "final_name" && fields != 3 {
				t.Fatal("wrong final name prerequisite")
			}
			if (phase == "final_field" || phase == "ordinary_error") && fields != 5 {
				t.Fatal("final field not reached")
			}
		})
	}
}

func TestFirmwareSecurityConcurrentCollectorsDoNotShareCancellation(t *testing.T) {
	arrived, release := make(chan struct{}, 2), make(chan struct{})
	source := firmwareAcquisitionSource(t, nil, func(name string) ([]byte, error) {
		if name == firmwareTestSecure {
			arrived <- struct{}{}
			<-release
			return []byte{6, 0, 0, 0, 1}, nil
		}
		return nil, fs.ErrNotExist
	})
	collector := NewLinuxCollectorWithSource(source)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var stopped, live Snapshot
	var stopErr, liveErr error
	var done sync.WaitGroup
	done.Add(2)
	go func() { defer done.Done(); stopped, stopErr = collector.Collect(ctx) }()
	go func() { defer done.Done(); live, liveErr = collector.Collect(t.Context()) }()
	<-arrived
	<-arrived
	cancel()
	close(release)
	done.Wait()
	requireNoCollection(t, stopped, stopErr, context.Canceled)
	if liveErr != nil || len(live.Components) != 1 {
		t.Fatal("unrelated context lost", liveErr)
	}
	again, err := collector.Collect(t.Context())
	if err != nil || !reflect.DeepEqual(again, live) {
		t.Fatal("collector retained failure")
	}
}

type firmwareDecoratedSource struct {
	*RootPathSource
	reads int
}

func (s *firmwareDecoratedSource) ReadFile(name string) ([]byte, error) {
	if strings.HasPrefix(name, "sys/firmware/") || strings.HasPrefix(name, "sys/class/tpm/") {
		s.reads++
		return []byte("PRIVATE-OVERRIDE"), nil
	}
	return s.RootPathSource.ReadFile(name)
}
func TestFirmwareSecurityDecoratorCannotBypassRawOverride(t *testing.T) {
	root := t.TempDir()
	firmwareTestWrite(t, root, firmwareTestSecure, []byte{6, 0, 0, 0, 1})
	firmwareTestWrite(t, root, "sys/class/tpm/PRIVATE/tpm_version_major", []byte("2\n"))
	source := &firmwareDecoratedSource{RootPathSource: NewRootPathSource(root)}
	snapshot, err := NewLinuxCollectorWithSource(source).Collect(t.Context())
	if err != nil || source.reads != 4 || len(snapshot.Components) != 2 {
		t.Fatal("override was not reached", err, source.reads)
	}
	for _, c := range snapshot.Components {
		for _, a := range c.Attributes {
			if !strings.HasSuffix(a.Name, "_state") || a.Value != "malformed" {
				t.Fatal("underlying favorable/private data escaped")
			}
		}
	}
}
