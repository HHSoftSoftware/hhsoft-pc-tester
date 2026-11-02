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
		{"energy_now", "energy_now_uwh", "-2\n", "-2"},
		{"charge_now", "charge_now_uah", "0\n", "0"},
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
				wantAttributes := 13
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
	for _, phase := range []string{"before", "final_name", "final_field", "previous_final_field", "ordinary_error"} {
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
				if name == "sys/class/power_supply/z-private/charge_now" || (phase == "previous_final_field" && name == "sys/class/power_supply/z-private/charge_full_design") {
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
			if (phase == "final_field" || phase == "ordinary_error") && fields != 26 {
				t.Fatal("last field prerequisite missing")
			}
			if phase == "previous_final_field" && fields != 24 {
				t.Fatal("retained prior final-field cancellation was not reached")
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
		if strings.HasSuffix(name, "/energy_now") {
			return []byte("17"), nil
		}
		if strings.HasSuffix(name, "/charge_now") {
			return []byte("-19"), nil
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
	for name, expected := range map[string]string{"energy_now_uwh": "17", "charge_now_uah": "-19"} {
		if value, exists := live.Components[0].Attribute(name); !exists || value != expected {
			t.Fatal("live context lost independent present energy/charge")
		}
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
	if len(source.requests) != 13 {
		t.Fatal("decorator was bypassed")
	}
	component := snapshot.Components[0]
	if value, exists := component.Attribute("supply_type"); exists || value != "" {
		t.Fatal("underlying or private enum escaped")
	}
	if state, _ := component.Attribute("supply_type_state"); state != "unsupported" {
		t.Fatal("override was not parsed")
	}
	if len(component.Attributes) != 13 {
		t.Fatal("decorated failed observations leaked bytes")
	}
}

type powerNowProviderError struct{}

func (powerNowProviderError) Error() string { panic("synthetic provider Error invoked") }
func (powerNowProviderError) Is(error) bool { panic("synthetic provider Is invoked") }
func (powerNowProviderError) Unwrap() error { panic("synthetic provider Unwrap invoked") }

func TestPowerSupplyNowFieldsAreIndependentAndDiagnosticsReset(t *testing.T) {
	for _, tc := range []struct {
		name, kind, energy, charge, errorInput             string
		energyValue, energyState, chargeValue, chargeState string
	}{
		{"both", "Battery", "17", "-19", "", "17", "reported", "-19", "reported"},
		{"energy_zero_charge_missing", "Mains", "0", "", "", "0", "reported", "", "unavailable"},
		{"charge_zero_energy_missing", "USB", "", "0", "", "", "unavailable", "0", "reported"},
		{"unsupported_type_bad_charge", "FutureType", "17", " 1", "", "17", "reported", "", "malformed"},
		{"missing_type_energy_error", "<absent>", "99", "-19", "energy_now", "", "unavailable", "-19", "reported"},
		{"charge_error", "UPS", "17", "99", "charge_now", "17", "reported", "", "unavailable"},
		{"malformed_type", "", "17", "-19", "", "17", "reported", "-19", "reported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inject := true
			calls := make(map[string]int)
			source := powerSupplySource(enumerationEntries("synthetic-supply"), func(name string) ([]byte, error) {
				input := strings.TrimPrefix(name, "sys/class/power_supply/synthetic-supply/")
				calls[input]++
				if input == "type" {
					if tc.kind == "<absent>" {
						return nil, fs.ErrNotExist
					}
					return []byte(tc.kind), nil
				}
				raw := ""
				if input == "energy_now" {
					raw = tc.energy
				}
				if input == "charge_now" {
					raw = tc.charge
				}
				if raw == "" {
					return nil, fs.ErrNotExist
				}
				if inject && input == tc.errorInput {
					return []byte(raw), powerNowProviderError{}
				}
				return []byte(raw), nil
			})
			collector := NewLinuxCollectorWithSource(source)
			snapshot, diagnostics, err := collector.CollectWithDiagnostics(t.Context())
			if err != nil || len(snapshot.Components) != 1 {
				t.Fatal("independent present observations failed")
			}
			wantDiagnostics := CollectionDiagnostics{}
			if tc.errorInput != "" {
				wantDiagnostics.ScalarFailures = 1
			}
			if diagnostics != wantDiagnostics {
				t.Fatal("absence, malformed input, and provider failure were conflated")
			}
			roles := []string{"type", "scope", "present", "online", "status", "capacity", "cycle_count", "energy_full", "energy_full_design", "charge_full", "charge_full_design", "energy_now", "charge_now"}
			if len(calls) != len(roles) {
				t.Fatal("unexpected observation role")
			}
			for _, role := range roles {
				if calls[role] != 1 {
					t.Fatal("role was skipped or repeated")
				}
			}
			for _, want := range []struct{ name, value, state string }{
				{"energy_now_uwh", tc.energyValue, tc.energyState},
				{"charge_now_uah", tc.chargeValue, tc.chargeState},
			} {
				value, exists := snapshot.Components[0].Attribute(want.name)
				state, stateExists := snapshot.Components[0].Attribute(want.name + "_state")
				if !stateExists || state != want.state || exists != (want.state == "reported") || value != want.value {
					t.Fatal("failed peer or supply type changed an independent unit observation")
				}
			}
			inject = false
			clear(calls)
			again, nextDiagnostics, err := collector.CollectWithDiagnostics(t.Context())
			if err != nil || len(again.Components) != 1 || nextDiagnostics != (CollectionDiagnostics{}) {
				t.Fatal("diagnostics leaked between invocations")
			}
			for _, role := range roles {
				if calls[role] != 1 {
					t.Fatal("fresh invocation lost a role")
				}
			}
			if tc.errorInput != "" {
				name := map[string]string{"energy_now": "energy_now_uwh", "charge_now": "charge_now_uah"}[tc.errorInput]
				value, exists := again.Components[0].Attribute(name)
				state, _ := again.Components[0].Attribute(name + "_state")
				if !exists || value != "99" || state != "reported" {
					t.Fatal("valid later observation inherited a prior failure")
				}
			}
		})
	}
}

func TestPowerSupplyNowOwnedScalarClosure(t *testing.T) {
	for _, input := range []string{"energy_now", "charge_now"} {
		for _, variant := range []string{"complete", "data_error", "close_error", "cancel_close", "cancel_close_error", "protocol_cancel"} {
			t.Run(input+"/"+variant, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				handle := scalarBytes([]byte("-2147483648\n"), 0, true)
				want, observed := error(nil), variant == "complete"
				switch variant {
				case "data_error":
					handle.read = func(p []byte) (int, error) { return copy(p, "-2147483648\n"), powerNowProviderError{} }
				case "cancel_close", "cancel_close_error":
					handle.onClose = cancel
					want = context.Canceled
				case "protocol_cancel":
					handle.read = func(p []byte) (int, error) { cancel(); return len(p) + 1, powerNowProviderError{} }
					want = ErrScalarRead
				}
				if variant == "close_error" || variant == "cancel_close_error" {
					handle.closeErr = powerNowProviderError{}
					want = ErrScalarClose
				}
				opens := 0
				target := "sys/class/power_supply/synthetic-supply/" + input
				data, gotObserved, err := readInventoryScalar(ctx, target, func(name string) (inventoryScalar, error) {
					opens++
					if name != target {
						t.Fatal("wrong owned present-value path")
					}
					return handle, nil
				})
				if opens != 1 || handle.closes != 1 || handle.stats != 1 || !reflect.DeepEqual(handle.requests, []int{4096}) {
					t.Fatal("owned present-value closure or bounded request changed")
				}
				if err != want || gotObserved != observed {
					t.Fatal("present-value owner precedence changed")
				}
				if !observed {
					if data != nil {
						t.Fatal("uncommitted bytes escaped failed ownership")
					}
					return
				}
				if string(data) != "-2147483648\n" {
					t.Fatal("completed raw value changed")
				}
				value, state := parsePowerSupplyScalar(powerSupplyTestField(t, input), data)
				if value != "-2147483648" || state != "reported" {
					t.Fatal("owned minimum was not retained in original units")
				}
			})
		}
	}
}

func TestPowerSupplyNowLateFinalFieldRetainsWholeCollectionPolicy(t *testing.T) {
	for _, variant := range []string{"complete", "ordinary_error", "cancel", "overflow"} {
		t.Run(variant, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			names := make([]string, 326)
			for index := range names {
				names[index] = fmt.Sprintf("supply-%03d", index)
			}
			reads, reached := 0, 0
			source := powerSupplySource(enumerationEntries(names...), func(name string) ([]byte, error) {
				reads++
				if name == "sys/class/power_supply/supply-325/charge_now" {
					reached++
					switch variant {
					case "ordinary_error":
						return []byte("-19"), powerNowProviderError{}
					case "cancel":
						cancel()
					case "overflow":
						return bytes.Repeat([]byte{'1'}, 65537), powerNowProviderError{}
					}
				}
				if strings.HasSuffix(name, "/energy_now") {
					return []byte("17"), nil
				}
				if strings.HasSuffix(name, "/charge_now") {
					return []byte("-19"), nil
				}
				return nil, fs.ErrNotExist
			})
			snapshot, diagnostics, err := NewLinuxCollectorWithSource(source).CollectWithDiagnostics(ctx)
			if reads != 4238 || reached != 1 {
				t.Fatal("complete 326-row, 13-role prerequisite not reached")
			}
			if variant == "cancel" {
				requireNoCollection(t, snapshot, err, context.Canceled)
				return
			}
			if variant == "overflow" {
				requireNoCollection(t, snapshot, err, ErrScalarLimit)
				return
			}
			if err != nil || len(snapshot.Components) != 326 {
				t.Fatal("late observation imposed a row cap or lost the collection")
			}
			wantDiagnostics := CollectionDiagnostics{}
			if variant == "ordinary_error" {
				wantDiagnostics.ScalarFailures = 1
			}
			if diagnostics != wantDiagnostics {
				t.Fatal("late ordinary failure was not isolated")
			}
			for _, component := range snapshot.Components {
				energy, hasEnergy := component.Attribute("energy_now_uwh")
				energyState, _ := component.Attribute("energy_now_uwh_state")
				charge, hasCharge := component.Attribute("charge_now_uah")
				chargeState, _ := component.Attribute("charge_now_uah_state")
				if !hasEnergy || energy != "17" || energyState != "reported" {
					t.Fatal("valid earlier field was lost")
				}
				failed := variant == "ordinary_error" && component.ID == "power_supply:326"
				if failed {
					if hasCharge || charge != "" || chargeState != "unavailable" {
						t.Fatal("late failed bytes escaped")
					}
				} else if !hasCharge || charge != "-19" || chargeState != "reported" {
					t.Fatal("unrelated present charge was lost")
				}
			}
		})
	}
}
