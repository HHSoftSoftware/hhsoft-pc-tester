package main

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

type observationCLIError []byte

func (observationCLIError) Error() string { panic("provider Error called") }
func (observationCLIError) Is(error) bool { panic("provider Is called") }
func (observationCLIError) Unwrap() error { panic("provider Unwrap called") }

type observationCLISource struct{ file func(string) ([]byte, error) }

func (s observationCLISource) ReadFile(name string) ([]byte, error) {
	if s.file != nil {
		return s.file(name)
	}
	return nil, fs.ErrNotExist
}
func (observationCLISource) ReadDir(string) ([]fs.DirEntry, error) { return nil, fs.ErrNotExist }

type observationCLIWriter struct{ write func([]byte) (int, error) }

func (w observationCLIWriter) Write(p []byte) (int, error) { return w.write(p) }

func observationCLIKey(t *testing.T) retestInputReader {
	t.Helper()
	return func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
		if ctx == nil || ctx.Err() != nil || path != "synthetic-key" || kind != retestFingerprintKeyInput {
			t.Fatal("unexpected fingerprint input request")
		}
		return inventoryPlanFingerprintKey(0x41), nil
	}
}

func observationCLIOutput(t *testing.T) destinationWriter {
	t.Helper()
	return func(path string, stdout io.Writer, write func(io.Writer) error) error {
		if path != "-" {
			t.Fatal("fixture attempted a file destination")
		}
		return write(stdout)
	}
}

func TestInventoryObservationCLIUsesSamePass(t *testing.T) {
	reads := make(map[string]int)
	const discarded = "PROVIDER-DETAIL-MUST-NOT-APPEAR"
	source := observationCLISource{file: func(name string) ([]byte, error) {
		reads[name]++
		switch name {
		case "proc/cpuinfo":
			return []byte("processor: 0\nmodel name: Fixture CPU\n"), nil
		case "proc/meminfo":
			return []byte("MemTotal: 1024 kB\n"), nil
		case "sys/class/dmi/id/product_name":
			return []byte("Fixture Unit\n"), nil
		case "sys/class/dmi/id/product_uuid":
			return []byte(discarded), observationCLIError("provider-error-must-not-appear")
		default:
			return nil, fs.ErrNotExist
		}
	}}
	collector := inventory.NewLinuxCollectorWithSource(source)
	arguments := []string{"-fingerprint-key", "synthetic-key"}
	var stdout, stderr bytes.Buffer
	code := runInventoryWithDiagnosticInput(t.Context(), arguments, &stdout, &stderr, collector.CollectWithDiagnostics,
		observationCLIOutput(t), observationCLIKey(t))
	if code != 0 || reads["proc/cpuinfo"] != 1 || reads["sys/class/dmi/id/product_uuid"] != 1 {
		t.Fatal("CLI did not consume the real collector's single pass")
	}
	if stderr.String() != "inventory: metadata reads failed (scalar=1 cpu=0 memory=0 input=0)\n" {
		t.Fatal("warning was not the fixed bounded summary")
	}
	if bytes.Contains(stdout.Bytes(), []byte(discarded)) || strings.Contains(stderr.String(), "sys/") || strings.Contains(stderr.String(), "provider-") {
		t.Fatal("provider bytes or paths escaped into public output")
	}
	var legacyOutput, legacyError bytes.Buffer
	legacyCode := runInventoryWithInput(t.Context(), arguments, &legacyOutput, &legacyError, collector.Collect,
		observationCLIOutput(t), observationCLIKey(t))
	if legacyCode != 0 || legacyError.Len() != 0 || !bytes.Equal(stdout.Bytes(), legacyOutput.Bytes()) {
		t.Fatal("same observed snapshot changed the inventory JSON or fingerprint inputs")
	}
}

func TestInventoryObservationCLILeavesFingerprintPayloadExact(t *testing.T) {
	for _, kind := range []string{"quiet", "warning"} {
		t.Run(kind, func(t *testing.T) {
			snapshot := inventoryPlanFingerprintSnapshot()
			before := inventoryPlanFingerprintSnapshot()
			diagnostics := inventory.CollectionDiagnostics{}
			if kind == "warning" {
				diagnostics = inventory.CollectionDiagnostics{ScalarFailures: 2, CPUFailures: 3, MemoryFailures: 4, InputFailures: 5}
			}
			var stdout, stderr, legacyOutput, legacyError bytes.Buffer
			arguments := []string{"-fingerprint-key", "synthetic-key", "-key-epoch", "fixture-epoch"}
			calls := 0
			code := runInventoryWithDiagnosticInput(t.Context(), arguments, &stdout, &stderr,
				func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
					calls++
					return snapshot, diagnostics, nil
				}, observationCLIOutput(t), observationCLIKey(t))
			legacyCode := runInventoryWithInput(t.Context(), arguments, &legacyOutput, &legacyError,
				func(context.Context) (inventory.Snapshot, error) { return snapshot, nil }, observationCLIOutput(t), observationCLIKey(t))
			if code != 0 || legacyCode != 0 || calls != 1 || legacyError.Len() != 0 || !bytes.Equal(stdout.Bytes(), legacyOutput.Bytes()) || !reflect.DeepEqual(snapshot, before) {
				t.Fatal("diagnostics altered existing JSON, HMAC identity or correction-input components")
			}
			want := ""
			if kind == "warning" {
				want = "inventory: metadata reads failed (scalar=2 cpu=3 memory=4 input=5)\n"
			}
			if stderr.String() != want {
				t.Fatal("warning emission changed")
			}
		})
	}
}

func TestInventoryObservationCLIRejectsInvalidSummary(t *testing.T) {
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
			writes := 0
			code := runInventoryWithDiagnosticInput(t.Context(), []string{"-fingerprint-key", "synthetic-key"}, &stdout, &stderr,
				func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
					return inventoryPlanFingerprintSnapshot(), item.diagnostics, nil
				}, func(string, io.Writer, func(io.Writer) error) error { writes++; return nil }, observationCLIKey(t))
			if code != 1 || stdout.Len() != 0 || writes != 0 || stderr.String() != "collect inventory: inventory: invalid observation diagnostics\n" {
				t.Fatal("invalid diagnostic summary reached output or leaked values")
			}
		})
	}
}

func TestInventoryObservationCLIFailurePrecedence(t *testing.T) {
	for _, kind := range []string{"missing_key", "short_key", "cancelled_context", "collection_failure", "cancelled_collection", "nil_collector", "nil_destination"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			arguments := []string{"-fingerprint-key", "synthetic-key"}
			if kind == "missing_key" {
				arguments = nil
			}
			if kind == "cancelled_context" {
				cancel()
			}
			reads, collects, writes := 0, 0, 0
			collect := inventoryDiagnosticCollector(func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
				collects++
				if kind == "collection_failure" {
					return inventory.Snapshot{}, inventory.CollectionDiagnostics{CPUFailures: ^uint32(0)}, inventory.ErrBulkClose
				}
				if kind == "cancelled_collection" {
					cancel()
				}
				return inventoryPlanFingerprintSnapshot(), inventory.CollectionDiagnostics{CPUFailures: 1}, nil
			})
			if kind == "nil_collector" {
				collect = nil
			}
			output := destinationWriter(func(string, io.Writer, func(io.Writer) error) error { writes++; return nil })
			if kind == "nil_destination" {
				output = nil
			}
			var stdout, stderr bytes.Buffer
			code := runInventoryWithDiagnosticInput(ctx, arguments, &stdout, &stderr, collect, output,
				func(context.Context, string, retestInputKind) ([]byte, error) {
					reads++
					if kind == "short_key" {
						return make([]byte, 31), nil
					}
					return inventoryPlanFingerprintKey(0x41), nil
				})
			wantCode, wantReads, wantCollects := 1, 0, 0
			switch kind {
			case "missing_key":
				wantCode = 2
			case "short_key":
				wantReads = 1
			case "collection_failure", "cancelled_collection":
				wantReads, wantCollects = 1, 1
			}
			if code != wantCode || reads != wantReads || collects != wantCollects || writes != 0 || stdout.Len() != 0 || strings.Contains(stderr.String(), "metadata reads failed") {
				t.Fatal("input or fatal collection precedence changed")
			}
			if kind == "collection_failure" && strings.Contains(stderr.String(), "invalid observation diagnostics") {
				t.Fatal("diagnostics replaced the primary collection error")
			}
		})
	}
}

func TestInventoryObservationCLIWarningOwnership(t *testing.T) {
	for _, kind := range []string{"error", "short", "oversized_count"} {
		t.Run(kind, func(t *testing.T) {
			var stdout, observed bytes.Buffer
			writes, destinations := 0, 0
			stderr := observationCLIWriter{write: func(p []byte) (int, error) {
				writes++
				observed.Write(p)
				if writes == 1 {
					if len(p) > 128 {
						t.Fatal("warning exceeded its byte limit")
					}
					switch kind {
					case "error":
						return 0, observationCLIError("writer-detail-must-not-appear")
					case "short":
						return len(p) - 1, nil
					case "oversized_count":
						return len(p) + 1, nil
					}
				}
				return len(p), nil
			}}
			code := runInventoryWithDiagnosticInput(t.Context(), []string{"-fingerprint-key", "synthetic-key"}, &stdout, stderr,
				func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
					return inventoryPlanFingerprintSnapshot(), inventory.CollectionDiagnostics{ScalarFailures: 1}, nil
				}, func(string, io.Writer, func(io.Writer) error) error { destinations++; return nil }, observationCLIKey(t))
			if code != 1 || stdout.Len() != 0 || destinations != 0 || strings.Contains(observed.String(), "writer-detail") || !strings.Contains(observed.String(), "observation warning could not be written") {
				t.Fatal("failed warning opened a destination or exposed a writer error")
			}
		})
	}
}

func TestInventoryObservationCLIWarningCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var stdout bytes.Buffer
	destinations := 0
	stderr := observationCLIWriter{write: func(p []byte) (int, error) { cancel(); return len(p), nil }}
	code := runInventoryWithDiagnosticInput(ctx, []string{"-fingerprint-key", "synthetic-key"}, &stdout, stderr,
		func(context.Context) (inventory.Snapshot, inventory.CollectionDiagnostics, error) {
			return inventoryPlanFingerprintSnapshot(), inventory.CollectionDiagnostics{InputFailures: 1}, nil
		}, func(string, io.Writer, func(io.Writer) error) error { destinations++; return nil }, observationCLIKey(t))
	if code != 1 || stdout.Len() != 0 || destinations != 0 {
		t.Fatal("warning callback cancellation did not stop destination acquisition")
	}
}

func TestInventoryObservationCLIQuietAbsenceAndEmpty(t *testing.T) {
	for _, kind := range []string{"absence", "empty"} {
		t.Run(kind, func(t *testing.T) {
			collector := inventory.NewLinuxCollectorWithSource(observationCLISource{file: func(string) ([]byte, error) {
				if kind == "empty" {
					return nil, nil
				}
				return nil, &fs.PathError{Op: "open", Path: "unreported-location", Err: fs.ErrNotExist}
			}})
			var stdout, stderr bytes.Buffer
			code := runInventoryWithDiagnosticInput(t.Context(), []string{"-fingerprint-key", "synthetic-key"}, &stdout, &stderr,
				collector.CollectWithDiagnostics, observationCLIOutput(t), observationCLIKey(t))
			if code != 0 || stdout.Len() == 0 || stderr.Len() != 0 {
				t.Fatal("clean absent or empty observations emitted warnings")
			}
		})
	}
}

func TestInventoryObservationCLIWarningMaximum(t *testing.T) {
	var destination bytes.Buffer
	if err := writeInventoryObservationWarning(&destination, inventory.CollectionDiagnostics{ScalarFailures: 65536}); err != nil {
		t.Fatal("exact maximum valid diagnostic count rejected")
	}
	if destination.Len() > 128 || destination.String() != "inventory: metadata reads failed (scalar=65536 cpu=0 memory=0 input=0)\n" {
		t.Fatal("maximum warning text or bound changed")
	}
	if err := writeInventoryObservationWarning(nil, inventory.CollectionDiagnostics{}); err != nil {
		t.Fatal("quiet diagnostics required an unused destination")
	}
	if err := writeInventoryObservationWarning(nil, inventory.CollectionDiagnostics{InputFailures: 1}); err != errInventoryWarning {
		t.Fatal("missing warning destination accepted")
	}
}
