package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

// These are synthetic provider diagnostics, never hardware paths or credentials.
type planObservationProviderError struct{ calls *[3]int }

func (e *planObservationProviderError) Error() string {
	e.calls[0]++
	return "SYNTHETIC-PROVIDER-DETAIL-MUST-NOT-APPEAR"
}
func (e *planObservationProviderError) Is(error) bool { e.calls[1]++; return false }
func (e *planObservationProviderError) Unwrap() error { e.calls[2]++; return nil }

type planObservationSource struct {
	*inventory.RootPathSource
	failed    map[string]bool
	reads     map[string]int
	callbacks [3]int
	cancel    context.CancelFunc
}

func (s *planObservationSource) ReadFile(name string) ([]byte, error) {
	s.reads[name]++
	if s.failed[name] {
		return []byte("SYNTHETIC-DISCARDED-BYTES"), &planObservationProviderError{&s.callbacks}
	}
	data, err := s.RootPathSource.ReadFile(name)
	if s.cancel != nil && name == "sys/class/dmi/id/product_name" {
		s.cancel()
	}
	return data, err
}

func newPlanObservationSource(t *testing.T) (*planObservationSource, string) {
	t.Helper()
	root := t.TempDir()
	for name, value := range map[string]string{
		"proc/cpuinfo":                  "processor: 0\nmodel name: Fixture CPU\n",
		"proc/meminfo":                  "MemTotal: 1024 kB\n",
		"proc/bus/input/devices":        "",
		"sys/class/dmi/id/product_name": "Fixture Unit\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return &planObservationSource{
		RootPathSource: inventory.NewRootPathSource(root),
		failed:         make(map[string]bool), reads: make(map[string]int),
	}, root
}

func planObservationArguments() []string {
	return []string{"-fingerprint-key", "synthetic-key", "-architecture", "x86_64", "-key-epoch", "fixture-epoch"}
}

func requirePlanObservationAbsent(t *testing.T, name string) {
	t.Helper()
	if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed command acquired a plan destination")
	}
}

func TestPlanObservationSamePassRealProvider(t *testing.T) {
	for _, item := range []struct {
		name    string
		failed  []string
		warning string
	}{
		{"scalar", []string{"sys/class/dmi/id/product_uuid"}, "inventory: metadata reads failed (scalar=1 cpu=0 memory=0 input=0)\n"},
		{"cpu", []string{"proc/cpuinfo"}, "inventory: metadata reads failed (scalar=0 cpu=1 memory=0 input=0)\n"},
		{"memory", []string{"proc/meminfo"}, "inventory: metadata reads failed (scalar=0 cpu=0 memory=1 input=0)\n"},
		{"input", []string{"proc/bus/input/devices"}, "inventory: metadata reads failed (scalar=0 cpu=0 memory=0 input=1)\n"},
		{"all", []string{"sys/class/dmi/id/product_uuid", "proc/cpuinfo", "proc/meminfo", "proc/bus/input/devices"}, "inventory: metadata reads failed (scalar=1 cpu=1 memory=1 input=1)\n"},
	} {
		t.Run(item.name, func(t *testing.T) {
			source, _ := newPlanObservationSource(t)
			for _, name := range item.failed {
				source.failed[name] = true
			}
			collector := inventory.NewLinuxCollectorWithSource(source)
			calls := 0
			var stdout, stderr bytes.Buffer
			code := runPlanWithDiagnosticInput(t.Context(), planObservationArguments(), &stdout, &stderr,
				func(ctx context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
					calls++
					return collector.CollectWithDiagnostics(ctx)
				}, observationCLIOutput(t), observationCLIKey(t))
			if code != 0 || calls != 1 || stdout.Len() == 0 || stderr.String() != item.warning {
				t.Fatal("real same-pass collection did not produce the exact bounded warning and plan")
			}
			for _, name := range []string{"proc/cpuinfo", "proc/meminfo", "proc/bus/input/devices", "sys/class/dmi/id/product_uuid"} {
				if source.reads[name] != 1 {
					t.Fatal("diagnostics caused a second acquisition")
				}
			}
			if source.callbacks != [3]int{} || bytes.Contains(stdout.Bytes(), []byte("SYNTHETIC-")) ||
				strings.Contains(stderr.String(), "sys/") || strings.Contains(stderr.String(), "proc/") {
				t.Fatal("provider callbacks, bytes or locations escaped the fixed diagnostic boundary")
			}
			// This separate legacy invocation observes the same fixture; it is not
			// a second pass inside the command under test.
			var legacy, legacyError bytes.Buffer
			if code := runPlanWithInput(t.Context(), planObservationArguments(), &legacy, &legacyError,
				collector.Collect, observationCLIOutput(t), observationCLIKey(t)); code != 0 || legacyError.Len() != 0 {
				t.Fatal("legacy plan prerequisite failed")
			}
			if !bytes.Equal(stdout.Bytes(), legacy.Bytes()) || source.callbacks != [3]int{} {
				t.Fatal("diagnostics changed canonical plan bytes or provider callback isolation")
			}
		})
	}
}

func TestPlanObservationCanonicalPlanInputs(t *testing.T) {
	for _, item := range []struct {
		name              string
		warning, selected bool
	}{
		{"quiet_default", false, false}, {"warning_default", true, false},
		{"quiet_selected", false, true}, {"warning_selected", true, true},
	} {
		t.Run(item.name, func(t *testing.T) {
			snapshot, before := inventoryPlanFingerprintSnapshot(), inventoryPlanFingerprintSnapshot()
			arguments := planObservationArguments()
			if item.selected {
				arguments = append(arguments, "-launch-selection-file", "synthetic-selection")
			}
			read := func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
				if path == "synthetic-selection" && kind == retestLaunchSelectionInput {
					return []byte("{\"schema_version\":1,\"profile\":\"quick\",\"architecture\":\"x86_64\",\"excluded_tests\":[\"cpu.quick\"]}"), nil
				}
				return observationCLIKey(t)(ctx, path, kind)
			}
			diagnostics := inventory.CollectionDiagnostics{}
			wantWarning := ""
			if item.warning {
				diagnostics = inventory.CollectionDiagnostics{ScalarFailures: 2, CPUFailures: 3, MemoryFailures: 4, InputFailures: 5}
				wantWarning = "inventory: metadata reads failed (scalar=2 cpu=3 memory=4 input=5)\n"
			}
			calls := 0
			var stdout, stderr, legacy, legacyError bytes.Buffer
			code := runPlanWithDiagnosticInput(t.Context(), arguments, &stdout, &stderr,
				func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
					calls++
					return snapshot, diagnostics, nil
				}, observationCLIOutput(t), read)
			oldCode := runPlanWithInput(t.Context(), arguments, &legacy, &legacyError,
				func(context.Context) (inventory.Snapshot, error) { return snapshot, nil }, observationCLIOutput(t), read)
			if code != 0 || oldCode != 0 || calls != 1 || stderr.String() != wantWarning || legacyError.Len() != 0 ||
				!bytes.Equal(stdout.Bytes(), legacy.Bytes()) || !reflect.DeepEqual(snapshot, before) {
				t.Fatal("warning changed selection, snapshot ownership or canonical plan bytes")
			}
			current, err := plan.Decode(stdout.Bytes())
			if err != nil {
				t.Fatal("current plan failed independent decoding")
			}
			prior, err := plan.Decode(legacy.Bytes())
			if err != nil || current.ID != prior.ID || !reflect.DeepEqual(current, prior) {
				t.Fatal("decoded plan, keyed hardware graph or plan identity changed")
			}
		})
	}
}

func TestPlanObservationQuietAndReset(t *testing.T) {
	for _, name := range []string{"absence", "empty", "reset"} {
		t.Run(name, func(t *testing.T) {
			source, root := newPlanObservationSource(t)
			// UUID is absent; an empty file is also a completed quiet observation.
			if name == "empty" {
				if err := os.WriteFile(filepath.Join(root, "sys", "class", "dmi", "id", "product_uuid"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			collector := inventory.NewLinuxCollectorWithSource(source)
			if name == "reset" {
				source.failed["sys/class/dmi/id/product_uuid"] = true
				var first, warning bytes.Buffer
				if code := runPlanWithDiagnosticInput(t.Context(), planObservationArguments(), &first, &warning,
					collector.CollectWithDiagnostics, observationCLIOutput(t), observationCLIKey(t)); code != 0 ||
					warning.String() != "inventory: metadata reads failed (scalar=1 cpu=0 memory=0 input=0)\n" {
					t.Fatal("reset prerequisite did not observe a real provider failure")
				}
				delete(source.failed, "sys/class/dmi/id/product_uuid")
			}
			var stdout, stderr bytes.Buffer
			code := runPlanWithDiagnosticInput(t.Context(), planObservationArguments(), &stdout, &stderr,
				collector.CollectWithDiagnostics, observationCLIOutput(t), observationCLIKey(t))
			if code != 0 || stdout.Len() == 0 || stderr.Len() != 0 || source.callbacks != [3]int{} {
				t.Fatal("clean absence/empty read warned, or counters leaked across invocations")
			}
		})
	}
}

func TestPlanObservationInvalidDiagnostics(t *testing.T) {
	for _, item := range []struct {
		name        string
		diagnostics inventory.CollectionDiagnostics
	}{
		{"one_counter", inventory.CollectionDiagnostics{CPUFailures: 65537}},
		{"aggregate", inventory.CollectionDiagnostics{ScalarFailures: 65536, InputFailures: 1}},
		{"uint32_max", inventory.CollectionDiagnostics{MemoryFailures: ^uint32(0)}},
	} {
		t.Run(item.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			destinations := 0
			code := runPlanWithDiagnosticInput(t.Context(), planObservationArguments(), &stdout, &stderr,
				func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
					return inventoryPlanFingerprintSnapshot(), item.diagnostics, nil
				}, func(string, io.Writer, func(io.Writer) error) error { destinations++; return nil }, observationCLIKey(t))
			if code != 1 || destinations != 0 || stdout.Len() != 0 ||
				stderr.String() != "collect inventory: inventory: invalid observation diagnostics\n" {
				t.Fatal("invalid counters reached a plan or exposed diagnostic values")
			}
		})
	}
}

func TestPlanObservationAdmissionAndFailurePrecedence(t *testing.T) {
	for _, name := range []string{
		"missing_key", "short_key", "key_error", "cancelled_context", "cancelled_key",
		"selection_conflict", "selection_read_error", "selection_invalid", "selection_cancel",
		"fatal_close", "fatal_close_cancel", "cancelled_collection", "invalid_profile",
		"nil_collector", "nil_destination", "nil_reader", "nil_stderr",
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			arguments := planObservationArguments()
			wantCode, wantReads, wantCollects := 1, 0, 0
			switch name {
			case "missing_key":
				arguments = nil
				wantCode = 2
			case "selection_conflict":
				arguments = append(arguments, "-selection-file", "never-opened", "-launch-selection-file", "synthetic-selection")
				wantCode = 2
			case "selection_read_error", "selection_invalid", "selection_cancel":
				arguments = append(arguments, "-launch-selection-file", "synthetic-selection")
				wantReads = 1
			case "short_key", "key_error", "cancelled_key":
				wantReads = 1
			case "fatal_close", "fatal_close_cancel", "cancelled_collection":
				wantReads, wantCollects = 1, 1
			case "invalid_profile":
				arguments = append(arguments, "-profile", "not-a-profile")
				wantReads, wantCollects = 1, 1
			}
			if name == "cancelled_context" {
				cancel()
			}
			reads, collects, destinations := 0, 0, 0
			var callbacks [3]int
			read := retestInputReader(func(context.Context, string, retestInputKind) ([]byte, error) {
				reads++
				switch name {
				case "key_error", "selection_read_error":
					return nil, &planObservationProviderError{&callbacks}
				case "short_key":
					return make([]byte, 31), nil
				case "selection_invalid":
					return []byte("{}"), nil
				case "selection_cancel":
					cancel()
					return []byte("{\"schema_version\":1,\"profile\":\"quick\",\"architecture\":\"x86_64\",\"excluded_tests\":[]}"), nil
				case "cancelled_key":
					cancel()
				}
				return inventoryPlanFingerprintKey(0x41), nil
			})
			collect := inventoryDiagnosticCollector(func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
				collects++
				if name == "fatal_close" || name == "fatal_close_cancel" {
					if name == "fatal_close_cancel" {
						cancel()
					}
					// Deliberately injected established close failure, not a
					// claim that this command fixture performs a device close.
					return inventoryPlanFingerprintSnapshot(), inventory.CollectionDiagnostics{CPUFailures: ^uint32(0)}, inventory.ErrBulkClose
				}
				if name == "cancelled_collection" {
					cancel()
				}
				return inventoryPlanFingerprintSnapshot(), inventory.CollectionDiagnostics{InputFailures: 1}, nil
			})
			destination := destinationWriter(func(string, io.Writer, func(io.Writer) error) error { destinations++; return nil })
			if name == "nil_collector" {
				collect = nil
			}
			if name == "nil_destination" {
				destination = nil
			}
			if name == "nil_reader" {
				read = nil
			}
			var stdout, stderr bytes.Buffer
			var diagnostic io.Writer = &stderr
			if name == "nil_stderr" {
				diagnostic = nil
			}
			code := runPlanWithDiagnosticInput(ctx, arguments, &stdout, diagnostic, collect, destination, read)
			if code != wantCode || reads != wantReads || collects != wantCollects || destinations != 0 ||
				stdout.Len() != 0 || strings.Contains(stderr.String(), "metadata reads failed") || callbacks != [3]int{} {
				t.Fatal("plan admission or fatal/cancellation precedence changed")
			}
			if name == "fatal_close" || name == "fatal_close_cancel" {
				if stderr.String() != "collect inventory: "+inventory.ErrBulkClose.Error()+"\n" {
					t.Fatal("cancellation or invalid diagnostics replaced the established close error")
				}
			}
		})
	}
}

func TestPlanObservationWarningBoundary(t *testing.T) {
	for _, name := range []string{"error", "short", "oversized_count", "negative_count", "cancel", "error_cancel", "quiet"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			output := filepath.Join(t.TempDir(), "plan.json")
			arguments := append(planObservationArguments(), "-output", output)
			writes, destinations := 0, 0
			var callbacks [3]int
			var observed, stdout bytes.Buffer
			stderr := observationCLIWriter{write: func(p []byte) (int, error) {
				writes++
				observed.Write(p)
				if writes == 1 {
					if string(p) != "inventory: metadata reads failed (scalar=65536 cpu=0 memory=0 input=0)\n" || len(p) > 128 {
						t.Fatal("warning was not the independently fixed bounded maximum")
					}
					switch name {
					case "error":
						return 0, &planObservationProviderError{&callbacks}
					case "short":
						return len(p) - 1, nil
					case "oversized_count":
						return len(p) + 1, nil
					case "negative_count":
						return -1, nil
					case "cancel":
						cancel()
						return len(p), nil
					case "error_cancel":
						cancel()
						return 0, &planObservationProviderError{&callbacks}
					}
				}
				return len(p), nil
			}}
			code := runPlanWithDiagnosticInput(ctx, arguments, &stdout, stderr,
				func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
					d := inventory.CollectionDiagnostics{ScalarFailures: 65536}
					if name == "quiet" {
						d = inventory.CollectionDiagnostics{}
					}
					return inventoryPlanFingerprintSnapshot(), d, nil
				}, func(path string, out io.Writer, write func(io.Writer) error) error {
					destinations++
					return writeDestination(path, out, write)
				}, observationCLIKey(t))
			if name == "quiet" {
				if code != 0 || writes != 0 || destinations != 1 {
					t.Fatal("quiet command touched warning writer")
				}
				if b, err := os.ReadFile(output); err != nil || len(b) == 0 {
					t.Fatal("quiet positive did not create a plan")
				}
				return
			}
			requirePlanObservationAbsent(t, output)
			want := "inventory: metadata reads failed (scalar=65536 cpu=0 memory=0 input=0)\nwrite test plan: inventory: observation warning could not be written\n"
			if name == "cancel" {
				want = "inventory: metadata reads failed (scalar=65536 cpu=0 memory=0 input=0)\nwrite test plan: context canceled\n"
			}
			if code != 1 || destinations != 0 || stdout.Len() != 0 || observed.String() != want || callbacks != [3]int{} {
				t.Fatal("failed warning opened a destination, lost error precedence or exposed callback text")
			}
		})
	}
}

func TestPlanObservationRealFatalCollection(t *testing.T) {
	for _, name := range []string{"overflow", "nonregular", "cancel"} {
		t.Run(name, func(t *testing.T) {
			source, root := newPlanObservationSource(t)
			target := filepath.Join(root, "sys", "class", "dmi", "id", "product_name")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var collect inventoryDiagnosticCollector
			want := inventory.ErrScalarLimit
			switch name {
			case "overflow":
				if err := os.WriteFile(target, bytes.Repeat([]byte{'x'}, 65537), 0600); err != nil {
					t.Fatal(err)
				}
				collect = inventory.NewLinuxCollector(root).CollectWithDiagnostics
			case "nonregular":
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
				collect = inventory.NewLinuxCollector(root).CollectWithDiagnostics
				want = inventory.ErrScalarRead
			case "cancel":
				source.cancel = cancel
				collect = inventory.NewLinuxCollectorWithSource(source).CollectWithDiagnostics
				want = context.Canceled
			}
			output := filepath.Join(t.TempDir(), "plan.json")
			calls := 0
			var stdout, stderr bytes.Buffer
			code := runPlanWithDiagnosticInput(ctx, append(planObservationArguments(), "-output", output), &stdout, &stderr,
				func(ctx context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
					calls++
					snapshot, diagnostics, err := collect(ctx)
					if err != want || snapshot.SchemaVersion != 0 || snapshot.Components != nil || diagnostics != (inventory.CollectionDiagnostics{}) {
						t.Fatal("real fatal acquisition retained partial success")
					}
					return snapshot, diagnostics, err
				}, writeDestination, observationCLIKey(t))
			if code != 1 || calls != 1 || stdout.Len() != 0 || stderr.String() != "collect inventory: "+want.Error()+"\n" {
				t.Fatal("real acquisition failure reached plan output or warning success")
			}
			requirePlanObservationAbsent(t, output)
		})
	}
}

func TestPlanObservationOwnedKeyClosure(t *testing.T) {
	for _, name := range []string{"success", "metadata_close", "readable_close", "cancel_after_close"} {
		t.Run(name, func(t *testing.T) {
			h := newInventoryPlanFingerprintHarness(t, inventoryPlanFingerprintCommands()[1])
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var callbacks [3]int
			if name == "metadata_close" {
				h.fixture.metadata.closeErr = &planObservationProviderError{&callbacks}
			}
			if name == "readable_close" {
				h.fixture.readable.closeErr = &planObservationProviderError{&callbacks}
			}
			reads, collects, destinations := 0, 0, 0
			read := func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
				reads++
				b, err := h.readInput(ctx, path, kind)
				h.requireClosed()
				if name == "cancel_after_close" {
					cancel()
				}
				return b, err
			}
			var stdout, stderr bytes.Buffer
			code := runPlanWithDiagnosticInput(ctx, h.args, &stdout, &stderr,
				func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
					collects++
					h.requireClosed()
					return inventoryPlanFingerprintSnapshot(), inventory.CollectionDiagnostics{CPUFailures: 1}, nil
				}, func(path string, out io.Writer, write func(io.Writer) error) error {
					destinations++
					h.requireClosed()
					if path != "-" || stderr.String() != "inventory: metadata reads failed (scalar=0 cpu=1 memory=0 input=0)\n" {
						t.Fatal("warning and owned key did not precede destination")
					}
					return write(out)
				}, read)
			h.requireClosed()
			if reads != 1 || callbacks != [3]int{} {
				t.Fatal("key acquisition or provider error isolation changed")
			}
			if name == "success" {
				if code != 0 || collects != 1 || destinations != 1 || stdout.Len() == 0 {
					t.Fatal("owned positive prerequisite failed")
				}
			} else if code != 1 || collects != 0 || destinations != 0 || stdout.Len() != 0 || strings.Contains(stderr.String(), "metadata reads failed") {
				t.Fatal("key closure or cancellation failure continued into collection")
			}
		})
	}
}

type planObservationDestination struct {
	bytes.Buffer
	closed int
}

func (w *planObservationDestination) Close() error {
	w.closed++
	return errors.New("synthetic destination close failure")
}

func TestPlanObservationDestinationCloseFailure(t *testing.T) {
	destination := &planObservationDestination{}
	opens := 0
	var stdout, stderr bytes.Buffer
	code := runPlanWithDiagnosticInput(t.Context(), append(planObservationArguments(), "-output", "synthetic-output"), &stdout, &stderr,
		func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
			return inventoryPlanFingerprintSnapshot(), inventory.CollectionDiagnostics{InputFailures: 1}, nil
		}, func(path string, out io.Writer, write func(io.Writer) error) error {
			return writeDestinationWith(path, out, write, func(name string, _ int, _ os.FileMode) (io.WriteCloser, error) {
				opens++
				if name != "synthetic-output" || stderr.String() != "inventory: metadata reads failed (scalar=0 cpu=0 memory=0 input=1)\n" {
					t.Fatal("destination acquisition preceded warning delivery")
				}
				return destination, nil
			})
		}, observationCLIKey(t))
	if code != 1 || opens != 1 || destination.closed != 1 || destination.Len() == 0 || stdout.Len() != 0 {
		t.Fatal("owned output close failure was reported as success")
	}
}

// The legacy core deliberately formats arbitrary injected collector errors.
// This is distinct from real PathSource errors suppressed inside acquisition.
func TestPlanObservationInjectedErrorFormattingRemainsLegacy(t *testing.T) {
	var callbacks [3]int
	var stdout, stderr bytes.Buffer
	destinations := 0
	code := runPlanWithDiagnosticInput(t.Context(), planObservationArguments(), &stdout, &stderr,
		func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
			return inventoryPlanFingerprintSnapshot(), inventory.CollectionDiagnostics{ScalarFailures: 1}, &planObservationProviderError{&callbacks}
		}, func(string, io.Writer, func(io.Writer) error) error { destinations++; return nil }, observationCLIKey(t))
	if code != 1 || destinations != 0 || stdout.Len() != 0 || callbacks != [3]int{1, 0, 0} ||
		stderr.String() != "collect inventory: SYNTHETIC-PROVIDER-DETAIL-MUST-NOT-APPEAR\n" {
		t.Fatal("arbitrary injected collector errors no longer follow the retained core contract")
	}
}
