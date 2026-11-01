package inventory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

// Every callback panics: classifying optional file errors must not invoke one.
type observationOpaqueFault struct{}

func (*observationOpaqueFault) Error() string { panic("provider Error called") }
func (*observationOpaqueFault) Is(error) bool { panic("provider Is called") }
func (*observationOpaqueFault) Unwrap() error { panic("provider Unwrap called") }

type observationSliceFault []byte

func (observationSliceFault) Error() string { panic("slice Error called") }

func TestInventoryObservationAbsenceClassification(t *testing.T) {
	cycle := &fs.PathError{Op: "open", Path: "unreported-location"}
	cycle.Err = cycle
	var lastAllowed error = fs.ErrNotExist
	for range 31 {
		lastAllowed = &fs.PathError{Op: "open", Path: "unreported-location", Err: lastAllowed}
	}
	var typedNil *observationOpaqueFault
	for _, item := range []struct {
		name string
		err  error
		want bool
	}{
		{"sentinel", fs.ErrNotExist, true},
		{"open_wrapper", &fs.PathError{Op: "open", Path: "unreported-location", Err: fs.ErrNotExist}, true},
		{"concrete_errno", syscall.ENOENT, true},
		{"last_allowed_wrapper", lastAllowed, true},
		{"one_more_wrapper", &fs.PathError{Op: "open", Err: lastAllowed}, false},
		{"permission", fs.ErrPermission, false},
		{"read_not_open", &fs.PathError{Op: "read", Err: fs.ErrNotExist}, false},
		{"opaque_callbacks", &observationOpaqueFault{}, false},
		{"uncomparable", observationSliceFault("unreported-provider-detail"), false},
		{"nil", nil, false},
		{"typed_nil", typedNil, false},
		{"mixed_causes", errors.Join(fs.ErrNotExist, fs.ErrPermission), false},
		{"cyclic_wrapper", cycle, false},
		{"nil_path_wrapper", (*fs.PathError)(nil), false},
	} {
		t.Run(item.name, func(t *testing.T) {
			if knownObservationAbsence(item.err) != item.want {
				t.Fatal("incorrect trusted-absence classification")
			}
		})
	}
}

func TestInventoryObservationOwnedReadAccounting(t *testing.T) {
	for _, role := range []struct {
		name  string
		limit int
		want  CollectionDiagnostics
	}{
		{"scalar", inventoryScalarLimit, CollectionDiagnostics{ScalarFailures: 1}},
		{"cpu", inventoryCPULimit, CollectionDiagnostics{CPUFailures: 1}},
		{"memory", inventoryMemoryLimit, CollectionDiagnostics{MemoryFailures: 1}},
		{"input", inventoryInputLimit, CollectionDiagnostics{InputFailures: 1}},
	} {
		t.Run(role.name, func(t *testing.T) {
			for _, kind := range []string{"open_failure", "read_failure", "read_absence", "open_absence", "empty_complete"} {
				t.Run(kind, func(t *testing.T) {
					budget := newInventoryAcquisitionBudget()
					if err := budget.chargeAttempt(); err != nil {
						t.Fatal(err)
					}
					var handle *scalarHandle
					var openErr error
					want := role.want
					charged := 0
					switch kind {
					case "open_failure":
						openErr = &fs.PathError{Op: "open", Path: "unreported-location", Err: &observationOpaqueFault{}}
					case "open_absence":
						openErr, want = fs.ErrNotExist, CollectionDiagnostics{}
					case "empty_complete":
						handle, want = scalarBytes(nil, 0, true), CollectionDiagnostics{}
					case "read_absence":
						handle = &scalarHandle{info: &scalarInformation{}, read: func([]byte) (int, error) { return 0, fs.ErrNotExist }}
					case "read_failure":
						charged = 3
						handle = &scalarHandle{info: &scalarInformation{}, read: func(p []byte) (int, error) {
							return copy(p, "bad"), &observationOpaqueFault{}
						}}
					}
					data, observed, err := readInventoryBytesWithBudget(t.Context(), "unreported-location", role.limit,
						func(string) (inventoryScalar, error) { return handle, openErr }, &budget)
					if err != nil || observed != (kind == "empty_complete") || len(data) != 0 || budget.diagnostics != want {
						t.Fatal("ordinary observation outcome or count changed")
					}
					if budget.bytes != maxInventoryAcquisitionBytes-charged || budget.attempts != maxInventoryFileAttempts-1 {
						t.Fatal("discarded bytes or attempt was not charged exactly once")
					}
					if handle != nil && (handle.stats != 1 || handle.closes != 1) {
						t.Fatal("owned observation did not stat and close exactly once")
					}
				})
			}
		})
	}
}

func TestInventoryObservationOwnedFailurePrecedence(t *testing.T) {
	for _, item := range []struct {
		name      string
		allowance int
		raw       string
		closeErr  error
		want      error
		count     uint32
	}{
		{"close_over_read", maxInventoryAcquisitionBytes, "x", fs.ErrPermission, errInventoryFileClose, 1},
		{"cancel_after_read", maxInventoryAcquisitionBytes, "x", nil, context.Canceled, 1},
		{"aggregate_over_read", 0, "x", nil, ErrAcquisitionBudget, 0},
		{"role_over_read", maxInventoryAcquisitionBytes, strings.Repeat("x", inventoryScalarLimit+1), nil, errInventoryFileLimit, 0},
		{"close_over_limit", maxInventoryAcquisitionBytes, strings.Repeat("x", inventoryScalarLimit+1), fs.ErrPermission, errInventoryFileClose, 0},
		{"malformed_read", maxInventoryAcquisitionBytes, "", nil, errInventoryFileRead, 0},
	} {
		t.Run(item.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			remaining := item.raw
			handle := &scalarHandle{info: &scalarInformation{}, closeErr: item.closeErr, read: func(p []byte) (int, error) {
				if item.name == "malformed_read" {
					return len(p) + 1, &observationOpaqueFault{}
				}
				n := copy(p, remaining)
				remaining = remaining[n:]
				if item.name == "cancel_after_read" {
					cancel()
				}
				if remaining == "" {
					return n, &observationOpaqueFault{}
				}
				return n, nil
			}}
			budget := newInventoryAcquisitionBudget()
			budget.bytes = item.allowance
			data, observed, err := readInventoryBytesWithBudget(ctx, "unreported-location", inventoryScalarLimit,
				func(string) (inventoryScalar, error) { return handle, nil }, &budget)
			if data != nil || observed || err != item.want || handle.closes != 1 || budget.diagnostics.ScalarFailures != item.count {
				t.Fatal("hard acquisition, closure or cancellation precedence changed")
			}
		})
	}
}

func TestInventoryObservationAggregateAccounting(t *testing.T) {
	for _, role := range []struct {
		name string
		kind inventoryProcRole
		want CollectionDiagnostics
	}{
		{"scalar", 0, CollectionDiagnostics{ScalarFailures: 1}},
		{"cpu", procCPU, CollectionDiagnostics{CPUFailures: 1}},
		{"memory", procMemory, CollectionDiagnostics{MemoryFailures: 1}},
		{"input", procInput, CollectionDiagnostics{InputFailures: 1}},
	} {
		t.Run(role.name, func(t *testing.T) {
			for _, kind := range []string{"absence", "data_plus_absence", "opaque_failure"} {
				t.Run(kind, func(t *testing.T) {
					raw, failure := []byte(nil), error(fs.ErrNotExist)
					if kind == "data_plus_absence" {
						raw = []byte("discarded")
					} else if kind == "opaque_failure" {
						failure = observationSliceFault("unreported-provider-detail")
					}
					calls := 0
					collection := NewLinuxCollectorWithSource(collectionSource{file: func(string) ([]byte, error) {
						calls++
						return raw, failure
					}}).newCollection(t.Context())
					var data []byte
					var observed bool
					if role.kind == 0 {
						data, observed = collection.readScalar("unreported-location")
					} else {
						data, observed = collection.readProc("unreported-location", role.kind)
					}
					want := role.want
					if kind == "absence" {
						want = CollectionDiagnostics{}
					}
					if data != nil || observed || collection.err != nil || calls != 1 || collection.acquisitionBudget.diagnostics != want {
						t.Fatal("aggregate provider observation accounting changed")
					}
					if collection.acquisitionBudget.bytes != maxInventoryAcquisitionBytes-len(raw) {
						t.Fatal("aggregate error prefix was refunded")
					}
				})
			}
		})
	}
}

func TestInventoryObservationDiagnosticsDiscardOnFatalCollection(t *testing.T) {
	for _, kind := range []string{"bulk_limit", "directory_failure", "cancellation"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cpuReads := 0
			source := collectionSource{file: func(name string) ([]byte, error) {
				if name == "proc/cpuinfo" {
					cpuReads++
					return nil, &observationOpaqueFault{}
				}
				if name == "proc/meminfo" {
					if kind == "bulk_limit" {
						return []byte(strings.Repeat("x", inventoryMemoryLimit+1)), &observationOpaqueFault{}
					}
					if kind == "cancellation" {
						cancel()
					}
				}
				return nil, fs.ErrNotExist
			}, directory: func(name string) ([]fs.DirEntry, error) {
				if name == "sys/class/block" && kind == "directory_failure" {
					return nil, fs.ErrPermission
				}
				return nil, fs.ErrNotExist
			}}
			want := error(ErrBulkLimit)
			if kind == "directory_failure" {
				want = ErrDirectoryRead
			} else if kind == "cancellation" {
				want = context.Canceled
			}
			snapshot, diagnostics, err := NewLinuxCollectorWithSource(source).CollectWithDiagnostics(ctx)
			if err != want || !reflect.DeepEqual(snapshot, Snapshot{}) || diagnostics != (CollectionDiagnostics{}) || cpuReads != 1 {
				t.Fatal("fatal collection exposed a partial snapshot or partial diagnostics")
			}
		})
	}
}

func TestInventoryObservationDiagnosticsResetAndLegacySnapshot(t *testing.T) {
	fail, cpuReads := true, 0
	source := collectionSource{file: func(name string) ([]byte, error) {
		if name == "proc/cpuinfo" {
			cpuReads++
			if fail {
				return nil, &observationOpaqueFault{}
			}
		}
		if name == "sys/class/dmi/id/product_name" {
			return []byte(" Fixture Unit \n"), nil
		}
		return nil, fs.ErrNotExist
	}}
	collector := NewLinuxCollectorWithSource(source)
	first, diagnostics, err := collector.CollectWithDiagnostics(t.Context())
	if err != nil || diagnostics != (CollectionDiagnostics{CPUFailures: 1}) || first.SchemaVersion != 1 || cpuReads != 1 {
		t.Fatal("diagnostics did not come from exactly one successful pass")
	}
	fail = false
	second, quiet, err := collector.CollectWithDiagnostics(t.Context())
	if err != nil || quiet != (CollectionDiagnostics{}) || cpuReads != 2 {
		t.Fatal("previous diagnostic contaminated a fresh collection")
	}
	legacy, err := collector.Collect(t.Context())
	if err != nil || cpuReads != 3 || !reflect.DeepEqual(first, second) || !reflect.DeepEqual(first, legacy) {
		t.Fatal("legacy collection, component identity or single-pass behavior changed")
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	legacyJSON, err := json.Marshal(legacy)
	if err != nil || !bytes.Equal(firstJSON, legacyJSON) || bytes.Contains(firstJSON, []byte("Failures")) {
		t.Fatal("diagnostics leaked into the schema-1 snapshot")
	}
}

func TestInventoryObservationDiagnosticsNoDriveAnd325Instances(t *testing.T) {
	t.Run("no_drive", func(t *testing.T) {
		snapshot, diagnostics, err := NewLinuxCollectorWithSource(collectionSource{}).CollectWithDiagnostics(t.Context())
		if err != nil || snapshot.SchemaVersion != 1 || diagnostics.HasFailures() {
			t.Fatal("clean diskless inventory became a failed observation")
		}
		for _, component := range snapshot.Components {
			if component.Type == ComponentBlock {
				t.Fatal("invented block device")
			}
		}
	})
	t.Run("all_325", func(t *testing.T) {
		var entries []fs.DirEntry
		for i := 324; i >= 0; i-- {
			entries = append(entries, &enumerationEntry{name: fmt.Sprintf("disk%03d", i)})
		}
		source := collectionSource{file: func(name string) ([]byte, error) {
			if name == "sys/class/block/disk000/size" {
				return nil, &observationOpaqueFault{}
			}
			return nil, fs.ErrNotExist
		}, directory: func(name string) ([]fs.DirEntry, error) {
			if name == "sys/class/block" {
				return entries, nil
			}
			return nil, fs.ErrNotExist
		}}
		collector := NewLinuxCollectorWithSource(source)
		snapshot, diagnostics, err := collector.CollectWithDiagnostics(t.Context())
		if err != nil || len(snapshot.Components) != 325 || diagnostics != (CollectionDiagnostics{ScalarFailures: 1}) {
			t.Fatal("diagnostics imposed an instance limit or lost a failed optional field")
		}
		for i, component := range snapshot.Components {
			if component.Type != ComponentBlock || component.ID != fmt.Sprintf("block:disk%03d", i) {
				t.Fatal("literal device identity changed")
			}
		}
		legacy, err := collector.Collect(t.Context())
		if err != nil || !reflect.DeepEqual(snapshot, legacy) || entries[0].Name() != "disk324" {
			t.Fatal("snapshot or provider entry ownership changed")
		}
	})
}

func TestInventoryObservationDiagnosticsConcurrentCalls(t *testing.T) {
	collector := NewLinuxCollectorWithSource(collectionSource{file: func(name string) ([]byte, error) {
		if name == "proc/cpuinfo" {
			return nil, fs.ErrPermission
		}
		return nil, fs.ErrNotExist
	}})
	type result struct {
		diagnostics CollectionDiagnostics
		err         error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			_, diagnostics, err := collector.CollectWithDiagnostics(t.Context())
			results <- result{diagnostics, err}
		}()
	}
	for range 2 {
		got := <-results
		if got.err != nil || got.diagnostics != (CollectionDiagnostics{CPUFailures: 1}) {
			t.Fatal("independent collection counters were shared")
		}
	}
}

func TestInventoryObservationDiagnosticsCountBoundary(t *testing.T) {
	budget := newInventoryAcquisitionBudget()
	budget.diagnostics.ScalarFailures = 65535
	if err := budget.recordObservationFailure(inventoryCPULimit); err != nil || !budget.diagnostics.Valid() || budget.diagnostics.CPUFailures != 1 {
		t.Fatal("exact aggregate diagnostic boundary rejected")
	}
	before := budget.diagnostics
	if err := budget.recordObservationFailure(inventoryInputLimit); err != ErrAcquisitionBudget || budget.diagnostics != before {
		t.Fatal("diagnostic count exceeded its bound or wrapped")
	}
	if (CollectionDiagnostics{ScalarFailures: 65536, InputFailures: 1}).Valid() || (CollectionDiagnostics{ScalarFailures: ^uint32(0)}).Valid() {
		t.Fatal("invalid public counter sum accepted")
	}
	fresh := newInventoryAcquisitionBudget()
	if err := fresh.recordObservationFailure(-1); err != errInventoryFileRead || fresh.diagnostics.HasFailures() {
		t.Fatal("unknown observation role counted")
	}
}
