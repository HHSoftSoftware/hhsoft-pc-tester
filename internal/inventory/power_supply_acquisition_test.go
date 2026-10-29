package inventory

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func powerSupplySource(entries []fs.DirEntry, read func(string) ([]byte, error)) collectionSource {
	return collectionSource{
		directory: func(name string) ([]fs.DirEntry, error) {
			if name == "sys/class/power_supply" {
				return entries, nil
			}
			return nil, fs.ErrNotExist
		},
		file: func(name string) ([]byte, error) {
			if strings.HasPrefix(name, "sys/class/power_supply/") {
				return read(name)
			}
			return nil, fs.ErrNotExist
		},
	}
}

func TestPowerSupplyEveryScalarUsesRawAdmission(t *testing.T) {
	// Independent role table; the existing free-text scalar tests stay intact.
	roles := []struct{ input, output, raw, value string }{
		{"type", "supply_type", "Battery\n", "Battery"},
		{"scope", "scope", "Device\n", "Device"},
		{"present", "present", "0\n", "0"},
		{"online", "online", "2\n", "2"},
		{"status", "status", "Not charging\n", "Not charging"},
		{"capacity", "capacity_percent", "0\n", "0"},
		{"cycle_count", "cycle_count", "1\n", "1"},
		{"energy_full", "energy_full_uwh", "-1\n", "-1"},
		{"energy_full_design", "energy_full_design_uwh", "0\n", "0"},
		{"charge_full", "charge_full_uah", "2\n", "2"},
		{"charge_full_design", "charge_full_design_uah", "1\n", "1"},
	}
	for _, role := range roles {
		for _, variant := range []string{"valid", "empty", "data_error", "bound", "overflow", "overflow_cancel"} {
			t.Run(role.input+"/"+variant, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				target := "sys/class/power_supply/private-entry/" + role.input
				reached, stopped := 0, false
				var requests []string
				source := powerSupplySource(enumerationEntries("private-entry"), func(name string) ([]byte, error) {
					if stopped {
						t.Fatal("read continued after hard acquisition failure")
					}
					requests = append(requests, name)
					if name != target {
						return nil, fs.ErrNotExist
					}
					reached++
					switch variant {
					case "valid":
						return []byte(role.raw), nil
					case "empty":
						return []byte{}, nil
					case "data_error":
						return []byte(role.raw + "PRIVATE-SCALAR-BYTES"), fs.ErrPermission
					case "bound":
						return bytes.Repeat([]byte{' '}, 65536), nil
					default:
						stopped = true
						if variant == "overflow_cancel" {
							cancel()
						}
						return bytes.Repeat([]byte{' '}, 65537), fs.ErrPermission
					}
				})
				snapshot, err := NewLinuxCollectorWithSource(source).Collect(ctx)
				if reached != 1 {
					t.Fatal("target scalar was not reached exactly once")
				}
				if variant == "overflow" || variant == "overflow_cancel" {
					requireNoCollection(t, snapshot, err, ErrScalarLimit)
					return
				}
				if err != nil || len(snapshot.Components) != 1 {
					t.Fatal("finite observation failed", err)
				}
				if len(requests) != len(roles) {
					t.Fatal("field allowlist was not fully consumed")
				}
				for index, expected := range roles {
					if requests[index] != "sys/class/power_supply/private-entry/"+expected.input {
						t.Fatal("undeclared field or wrong role read")
					}
				}
				component := snapshot.Components[0]
				wantState := "malformed"
				if variant == "valid" {
					wantState = "reported"
				}
				if variant == "data_error" {
					wantState = "unavailable"
				}
				state, exists := component.Attribute(role.output + "_state")
				value, hasValue := component.Attribute(role.output)
				if !exists || state != wantState || hasValue != (variant == "valid") || (hasValue && value != role.value) {
					t.Fatal("raw completion or finite state was lost")
				}
				wantAttributes := 11
				if variant == "valid" {
					wantAttributes++
				}
				if len(component.Attributes) != wantAttributes {
					t.Fatal("partial bytes or undeclared attributes escaped")
				}
				for _, attribute := range component.Attributes {
					if strings.Contains(attribute.Value, "PRIVATE-SCALAR-BYTES") {
						t.Fatal("failed observation leaked bytes")
					}
				}
			})
		}
	}
}

func TestPowerSupplyOwnedScalarDiscardsFaultedFiniteBytes(t *testing.T) {
	for _, variant := range []string{"complete", "data_error", "later_error", "close_error", "read_error_close", "cancel_close", "cancel_close_error", "protocol_cancel"} {
		t.Run(variant, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			handle := scalarBytes([]byte("100\n"), 0, true)
			want, observed := error(nil), variant == "complete"
			switch variant {
			case "data_error", "read_error_close":
				handle.read = func(p []byte) (int, error) { return copy(p, "100\n"), fs.ErrPermission }
			case "later_error":
				calls := 0
				handle.read = func(p []byte) (int, error) {
					calls++
					if calls == 1 {
						return copy(p, "10"), nil
					}
					return copy(p, "0\n"), fs.ErrPermission
				}
			case "cancel_close", "cancel_close_error":
				handle.onClose = cancel
				want = context.Canceled
			case "protocol_cancel":
				handle.read = func(p []byte) (int, error) { cancel(); return len(p) + 1, fs.ErrPermission }
				want = ErrScalarRead
			}
			if variant == "close_error" || variant == "read_error_close" || variant == "cancel_close_error" {
				handle.closeErr = errors.New("PRIVATE-CLOSE-DETAIL")
				want = ErrScalarClose
			}
			opens := 0
			raw, gotObserved, err := readInventoryScalar(ctx, "sys/class/power_supply/private/capacity", func(name string) (inventoryScalar, error) {
				opens++
				if name != "sys/class/power_supply/private/capacity" {
					t.Fatal("wrong owned scalar path")
				}
				return handle, nil
			})
			if opens != 1 || handle.closes != 1 || handle.stats != 1 {
				t.Fatal("owned scalar ledger is incomplete")
			}
			wantReads := 1
			if variant == "later_error" {
				wantReads = 2
			}
			if len(handle.requests) != wantReads {
				t.Fatal("read fault prerequisite not reached")
			}
			for _, size := range handle.requests {
				if size != 4096 {
					t.Fatal("scalar read budget changed")
				}
			}
			if err != want || gotObserved != observed {
				t.Fatal("owned acquisition precedence changed")
			}
			if !observed {
				if raw != nil {
					t.Fatal("uncommitted finite bytes escaped owner closure")
				}
				return
			}
			if string(raw) != "100\n" {
				t.Fatal("valid owned prerequisite changed")
			}
			value, state := parsePowerSupplyScalar(powerSupplyTestField(t, "capacity"), raw)
			if value != "100" || state != "reported" {
				t.Fatal("completed finite value rejected")
			}
		})
	}
}

func TestPowerSupplyDirectoryFailureDoesNotBecomeAbsence(t *testing.T) {
	var typedNil *enumerationEntry
	for _, variant := range []string{"error", "entries_error", "nil_entry", "typed_nil", "duplicate", "unsafe_name"} {
		t.Run(variant, func(t *testing.T) {
			calls, reads := 0, 0
			source := powerSupplySource(nil, func(string) ([]byte, error) { reads++; return []byte("Battery"), nil })
			source.directory = func(name string) ([]fs.DirEntry, error) {
				if name != "sys/class/power_supply" {
					return nil, fs.ErrNotExist
				}
				calls++
				switch variant {
				case "error":
					return nil, fs.ErrPermission
				case "entries_error":
					return enumerationEntries("private"), fs.ErrNotExist
				case "nil_entry":
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
			if calls != 1 || reads != 0 {
				t.Fatal("invalid directory reached field reads")
			}
		})
	}
}

func TestPowerSupplyOwnedBatchesHaveNoInstanceCapOrNameReread(t *testing.T) {
	const count = 129
	provided := make([]fs.DirEntry, count)
	calls := make([]int, count)
	for index := range provided {
		number := count - 1 - index
		provided[index] = &enumerationEntry{name: fmt.Sprintf("private-%03d", number), onName: func() { calls[number]++ }}
	}
	original := append([]fs.DirEntry(nil), provided...)
	owner := &enumerationDirectory{batches: []enumerationBatch{
		{provided[:64], nil}, {provided[64:128], nil}, {provided[128:], io.EOF},
	}}
	entries, err := readInventoryDirectory(t.Context(), "sys/class/power_supply", func(string) (inventoryDirectory, error) { return owner, nil })
	if err != nil || owner.closes != 1 || owner.stats != 1 || !reflect.DeepEqual(owner.requests, []int{64, 64, 64}) {
		t.Fatal("owned full enumeration did not complete", err)
	}
	if !reflect.DeepEqual(provided, original) {
		t.Fatal("provider slice was reordered")
	}
	source := powerSupplySource(entries, func(name string) ([]byte, error) {
		var number int
		if _, err := fmt.Sscanf(strings.TrimPrefix(name, "sys/class/power_supply/"), "private-%03d/", &number); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "/type") {
			switch number % 4 {
			case 0:
				return []byte("Battery"), nil
			case 1:
				return []byte("Mains"), nil
			case 2:
				return []byte("FutureType"), nil
			}
		}
		return nil, fs.ErrNotExist
	})
	snapshot, err := NewLinuxCollectorWithSource(source).Collect(t.Context())
	if err != nil || len(snapshot.Components) != count {
		t.Fatal("supply count was capped", err)
	}
	byID := make(map[string]Component)
	for _, component := range snapshot.Components {
		byID[component.ID] = component
	}
	for number := 0; number < count; number++ {
		if calls[number] != 1 {
			t.Fatal("provider Name was reread after caching")
		}
		component, exists := byID[fmt.Sprintf("power_supply:%d", number+1)]
		if !exists || component.Type != ComponentType("power_supply") {
			t.Fatal("ordinal missing")
		}
		value, hasValue := component.Attribute("supply_type")
		state, _ := component.Attribute("supply_type_state")
		switch number % 4 {
		case 0:
			if value != "Battery" || !hasValue || state != "reported" {
				t.Fatal("battery lost")
			}
		case 1:
			if value != "Mains" || !hasValue || state != "reported" {
				t.Fatal("adapter lost")
			}
		case 2:
			if hasValue || state != "unsupported" {
				t.Fatal("unknown label echoed or row lost")
			}
		case 3:
			if hasValue || state != "unavailable" {
				t.Fatal("unavailable row lost")
			}
		}
	}
	owner = &enumerationDirectory{batches: []enumerationBatch{{enumerationEntries("private"), io.EOF}}, closeErr: fs.ErrPermission}
	rejected, err := readInventoryDirectory(t.Context(), "sys/class/power_supply", func(string) (inventoryDirectory, error) { return owner, nil })
	if rejected != nil || err != ErrDirectoryClose || owner.closes != 1 || len(owner.requests) != 1 {
		t.Fatal("completed directory escaped failed Close")
	}
}

func TestPowerSupplyCancellationAtFinalRoleAndNameRejectsWholeSnapshot(t *testing.T) {
	for _, phase := range []string{"before", "final_name", "final_field", "ordinary_error"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reached, fields := 0, 0
			entries := enumerationEntries("a-private", "z-private")
			if phase == "final_name" {
				entries[1].(*enumerationEntry).onName = func() { reached++; cancel() }
			}
			source := powerSupplySource(entries, func(name string) ([]byte, error) {
				fields++
				if ctx.Err() != nil {
					t.Fatal("read after cancellation")
				}
				if name == "sys/class/power_supply/z-private/charge_full_design" {
					reached++
					cancel()
					if phase == "ordinary_error" {
						return []byte("100"), fs.ErrPermission
					}
				}
				return []byte("1"), nil
			})
			if phase == "before" {
				cancel()
			}
			snapshot, err := NewLinuxCollectorWithSource(source).Collect(ctx)
			requireNoCollection(t, snapshot, err, context.Canceled)
			if phase == "before" && (fields != 0 || reached != 0) {
				t.Fatal("pre-stop performed work")
			}
			if phase != "before" && reached != 1 {
				t.Fatal("cancellation boundary was not reached")
			}
			if phase == "final_name" && fields != 0 {
				t.Fatal("canceled name reached fields")
			}
			if (phase == "final_field" || phase == "ordinary_error") && fields != 22 {
				t.Fatal("last field prerequisite missing")
			}
		})
	}
}

func TestPowerSupplyConcurrentCollectionsKeepIndependentContexts(t *testing.T) {
	arrived, release := make(chan struct{}, 2), make(chan struct{})
	source := powerSupplySource(enumerationEntries("private"), func(name string) ([]byte, error) {
		if strings.HasSuffix(name, "/type") {
			arrived <- struct{}{}
			<-release
			return []byte("Battery"), nil
		}
		return nil, fs.ErrNotExist
	})
	collector := NewLinuxCollectorWithSource(source)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var stopped, live Snapshot
	var stoppedErr, liveErr error
	var done sync.WaitGroup
	done.Add(2)
	go func() { defer done.Done(); stopped, stoppedErr = collector.Collect(ctx) }()
	go func() { defer done.Done(); live, liveErr = collector.Collect(t.Context()) }()
	<-arrived
	<-arrived
	cancel()
	close(release)
	done.Wait()
	requireNoCollection(t, stopped, stoppedErr, context.Canceled)
	if liveErr != nil || len(live.Components) != 1 {
		t.Fatal("another context inherited cancellation")
	}
	// A later invocation of the same collector also receives fresh state.
	again, err := collector.Collect(t.Context())
	if err != nil || !reflect.DeepEqual(again, live) {
		t.Fatal("collection error was retained across calls")
	}
}

type powerSupplyDecoratedSource struct {
	*RootPathSource
	requests []string
}

func (source *powerSupplyDecoratedSource) ReadFile(name string) ([]byte, error) {
	if strings.HasPrefix(name, "sys/class/power_supply/") {
		source.requests = append(source.requests, name)
		if strings.HasSuffix(name, "/type") {
			return []byte("PRIVATE-FUTURE-ENUM"), nil
		}
		return nil, fs.ErrPermission
	}
	return source.RootPathSource.ReadFile(name)
}

func TestPowerSupplyDecoratorKeepsItsReadOverride(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "sys/class/power_supply/private-name")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "type"), []byte("Battery"), 0600); err != nil {
		t.Fatal(err)
	}
	source := &powerSupplyDecoratedSource{RootPathSource: NewRootPathSource(root)}
	snapshot, err := NewLinuxCollectorWithSource(source).Collect(t.Context())
	if err != nil || len(snapshot.Components) != 1 {
		t.Fatal("decorated collection failed", err)
	}
	if len(source.requests) != 11 {
		t.Fatal("decorator was bypassed")
	}
	component := snapshot.Components[0]
	if value, exists := component.Attribute("supply_type"); exists || value != "" {
		t.Fatal("underlying or private enum escaped")
	}
	if state, _ := component.Attribute("supply_type_state"); state != "unsupported" {
		t.Fatal("override was not parsed")
	}
	if len(component.Attributes) != 11 {
		t.Fatal("decorated failed observations leaked bytes")
	}
}
