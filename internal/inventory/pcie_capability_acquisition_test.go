package inventory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const pcieTestAddress = "0000:00:01.0"
const pcieTestBase = "sys/bus/pci/devices/0000:00:01.0/"

func pcieTestSource(addresses []string, file func(string) ([]byte, error)) PathSource {
	return collectionSource{
		directory: func(name string) ([]fs.DirEntry, error) {
			if name == "sys/bus/pci/devices" {
				return enumerationEntries(addresses...), nil
			}
			return nil, fs.ErrNotExist
		},
		file: file,
	}
}

func pcieTestFields(fields map[string][]byte) PathSource {
	return pcieTestSource([]string{pcieTestAddress}, func(name string) ([]byte, error) {
		if field, ok := strings.CutPrefix(name, pcieTestBase); ok {
			if value, present := fields[field]; present {
				return append([]byte(nil), value...), nil
			}
		}
		return nil, fs.ErrNotExist
	})
}

func pcieTestAttributes(t *testing.T, snapshot Snapshot, address string) map[string]string {
	t.Helper()
	for _, component := range snapshot.Components {
		if component.Type == ComponentPCI && component.ID == "pci:"+address {
			fields := make(map[string]string)
			for _, attribute := range component.Attributes {
				if strings.HasPrefix(attribute.Name, "max_link_") {
					fields[attribute.Name] = attribute.Value
				}
			}
			return fields
		}
	}
	t.Fatal("expected PCI component missing")
	return nil
}

func pcieRequireEmpty(t *testing.T, snapshot Snapshot, diagnostics CollectionDiagnostics, err, want error) {
	t.Helper()
	if !reflect.DeepEqual(snapshot, Snapshot{}) || diagnostics != (CollectionDiagnostics{}) || err != want {
		t.Fatal("fatal collection returned partial data or changed its fixed error")
	}
}

func pcieWriteDevice(t *testing.T, root, address string, fields map[string][]byte) string {
	t.Helper()
	base := filepath.Join(root, "sys", "bus", "pci", "devices", address)
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal("fixture directory creation failed")
	}
	for name, value := range fields {
		if err := os.WriteFile(filepath.Join(base, name), value, 0600); err != nil {
			t.Fatal("fixture file creation failed")
		}
	}
	return base
}

func TestPCIeMaximumCollectorIndependentFieldStates(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields map[string][]byte
		want   map[string]string
	}{
		{"both_absent", nil, map[string]string{}},
		{"speed_only", map[string][]byte{"max_link_speed": []byte("8.0 GT/s PCIe\n")}, map[string]string{"max_link_speed_state": "reported", "max_link_speed_mts": "8000", "max_link_width_state": "unavailable"}},
		{"width_only", map[string][]byte{"max_link_width": []byte("16\n")}, map[string]string{"max_link_speed_state": "unavailable", "max_link_width_state": "reported", "max_link_width_lanes": "16"}},
		{"both_reported", map[string][]byte{"max_link_speed": []byte("8.0 GT/s PCIe"), "max_link_width": []byte("16")}, map[string]string{"max_link_speed_state": "reported", "max_link_speed_mts": "8000", "max_link_width_state": "reported", "max_link_width_lanes": "16"}},
		{"both_unknown", map[string][]byte{"max_link_speed": []byte("Unknown"), "max_link_width": []byte("255")}, map[string]string{"max_link_speed_state": "unknown", "max_link_width_state": "unknown"}},
		{"valid_speed_bad_width", map[string][]byte{"max_link_speed": []byte("8.0 GT/s PCIe"), "max_link_width": []byte("16 lanes")}, map[string]string{"max_link_speed_state": "reported", "max_link_speed_mts": "8000", "max_link_width_state": "malformed"}},
		{"bad_speed_valid_width", map[string][]byte{"max_link_speed": []byte("8e0 GT/s PCIe"), "max_link_width": []byte("16")}, map[string]string{"max_link_speed_state": "malformed", "max_link_width_state": "reported", "max_link_width_lanes": "16"}},
		{"empty_speed", map[string][]byte{"max_link_speed": nil, "max_link_width": []byte("16")}, map[string]string{"max_link_speed_state": "malformed", "max_link_width_state": "reported", "max_link_width_lanes": "16"}},
		{"empty_width", map[string][]byte{"max_link_width": nil}, map[string]string{"max_link_speed_state": "unavailable", "max_link_width_state": "malformed"}},
		{"future_pair", map[string][]byte{"max_link_speed": []byte("128.0 GT/s PCIe"), "max_link_width": []byte("64")}, map[string]string{"max_link_speed_state": "unsupported", "max_link_width_state": "unsupported"}},
		{"reserved_width", map[string][]byte{"max_link_speed": []byte("8.0 GT/s PCIe"), "max_link_width": []byte("0")}, map[string]string{"max_link_speed_state": "reported", "max_link_speed_mts": "8000", "max_link_width_state": "unsupported"}},
		{"unknown_speed_reported_width", map[string][]byte{"max_link_speed": []byte("Unknown"), "max_link_width": []byte("16")}, map[string]string{"max_link_speed_state": "unknown", "max_link_width_state": "reported", "max_link_width_lanes": "16"}},
		{"whitespace_not_normalized", map[string][]byte{"max_link_speed": []byte("8.0  GT/s PCIe"), "max_link_width": []byte(" 16 ")}, map[string]string{"max_link_speed_state": "malformed", "max_link_width_state": "malformed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, diagnostics, err := NewLinuxCollectorWithSource(pcieTestFields(tc.fields)).CollectWithDiagnostics(t.Context())
			if err != nil || diagnostics != (CollectionDiagnostics{}) || len(snapshot.Components) != 1 {
				t.Fatal("clean fixture collection failed")
			}
			if got := pcieTestAttributes(t, snapshot, pcieTestAddress); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("capability fields = %v; want %v", got, tc.want)
			}
			if tc.name == "both_absent" {
				want := NewSnapshot([]Component{NewComponent(ComponentPCI, "pci:0000:00:01.0", map[string]string{"address": "0000:00:01.0"})})
				if !reflect.DeepEqual(snapshot, want) {
					t.Fatal("two clean absences changed historical component shape")
				}
			}
		})
	}
}

func TestPCIeRequestsIncludeIndependentCurrentLinkValues(t *testing.T) {
	for _, tc := range []struct {
		name   string
		maxima bool
	}{{"maxima_absent", false}, {"maxima_present", true}} {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string][]byte{"current_link_speed": []byte("2.5 GT/s PCIe"), "current_link_width": []byte("1")}
			if tc.maxima {
				fields["max_link_speed"] = []byte("8.0 GT/s PCIe")
				fields["max_link_width"] = []byte("16")
			}
			backing := pcieTestFields(fields)
			var requests []string
			source := pcieTestSource([]string{pcieTestAddress}, func(name string) ([]byte, error) {
				if suffix, ok := strings.CutPrefix(name, pcieTestBase); ok {
					requests = append(requests, suffix)
				}
				return backing.ReadFile(name)
			})
			collector := NewLinuxCollectorWithSource(source)
			before, err := collector.Collect(t.Context())
			if err != nil {
				t.Fatal("positive collection failed")
			}
			want := []string{"class", "vendor", "device", "subsystem_vendor", "subsystem_device", "revision", "irq", "numa_node", "modalias", "max_link_speed", "max_link_width", "current_link_speed", "current_link_width"}
			if !reflect.DeepEqual(requests, want) {
				t.Fatal("PCI attribute request set or order changed")
			}
			fields["current_link_speed"] = []byte("64.0 GT/s PCIe\nPRIVATE-CANARY")
			fields["current_link_width"] = []byte("32")
			requests = nil
			after, err := collector.Collect(t.Context())
			if err != nil || reflect.DeepEqual(before, after) || !reflect.DeepEqual(requests, want) || !reflect.DeepEqual(pcieTestAttributes(t, before, pcieTestAddress), pcieTestAttributes(t, after, pcieTestAddress)) {
				t.Fatal("current observations were skipped or changed maximum fields")
			}
			if !reflect.DeepEqual(pcieCurrentAttributes(t, before, pcieTestAddress), map[string]string{"current_link_speed_state": "reported", "current_link_speed_mts": "2500", "current_link_width_state": "reported", "current_link_width_lanes": "1"}) || !reflect.DeepEqual(pcieCurrentAttributes(t, after, pcieTestAddress), map[string]string{"current_link_speed_state": "malformed", "current_link_width_state": "reported", "current_link_width_lanes": "32"}) {
				t.Fatal("current independent literal fields changed or raw canary escaped")
			}
		})
	}
}

type pciePoisonError struct{ calls int }

func (e *pciePoisonError) Error() string { e.calls++; return "PCIE-PRIVATE-CANARY" }
func (e *pciePoisonError) Is(error) bool { e.calls++; return false }
func (e *pciePoisonError) Unwrap() error { e.calls++; return fs.ErrNotExist }

func TestPCIeMaximumObservationFailuresKeepFixedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, speed, width string
		failures           uint32
	}{
		{"speed_error", "read_error", "reported", 1}, {"width_error", "reported", "read_error", 1},
		{"both_errors", "read_error", "read_error", 2}, {"partial_error", "read_error", "reported", 1},
		{"opaque_absence", "read_error", "reported", 1}, {"wrapped_open_absence", "unavailable", "reported", 0},
		{"wrapped_read_absence", "read_error", "reported", 1}, {"completed_canary", "malformed", "reported", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			poison := &pciePoisonError{}
			source := pcieTestSource([]string{pcieTestAddress}, func(name string) ([]byte, error) {
				if name == pcieTestBase+"max_link_speed" {
					switch tc.name {
					case "speed_error", "both_errors", "opaque_absence":
						return nil, poison
					case "partial_error":
						return []byte("8.0 GT/s PCIe\nPCIE-PRIVATE-CANARY"), poison
					case "wrapped_open_absence":
						return nil, &fs.PathError{Op: "open", Path: "PCIE-PRIVATE-CANARY", Err: fs.ErrNotExist}
					case "wrapped_read_absence":
						return nil, &fs.PathError{Op: "read", Path: "PCIE-PRIVATE-CANARY", Err: fs.ErrNotExist}
					case "completed_canary":
						return []byte("PCIE-PRIVATE-CANARY"), nil
					default:
						return []byte("8.0 GT/s PCIe"), nil
					}
				}
				if name == pcieTestBase+"max_link_width" {
					if tc.name == "width_error" || tc.name == "both_errors" {
						return nil, poison
					}
					return []byte("16"), nil
				}
				return nil, fs.ErrNotExist
			})
			snapshot, diagnostics, err := NewLinuxCollectorWithSource(source).CollectWithDiagnostics(t.Context())
			if err != nil || diagnostics != (CollectionDiagnostics{ScalarFailures: tc.failures}) || poison.calls != 0 {
				t.Fatal("observation failure category, counter or callback safety changed")
			}
			want := map[string]string{"max_link_speed_state": tc.speed, "max_link_width_state": tc.width}
			if tc.speed == "reported" {
				want["max_link_speed_mts"] = "8000"
			}
			if tc.width == "reported" {
				want["max_link_width_lanes"] = "16"
			}
			if !reflect.DeepEqual(pcieTestAttributes(t, snapshot, pcieTestAddress), want) {
				t.Fatal("failure discarded a valid peer or retained a failed value")
			}
			raw, err := json.Marshal(struct {
				Snapshot    Snapshot
				Diagnostics CollectionDiagnostics
			}{snapshot, diagnostics})
			if err != nil || bytes.Contains(raw, []byte("PCIE-PRIVATE-CANARY")) || poison.calls != 0 {
				t.Fatal("discarded provider material reached serialized output")
			}
		})
	}
}

func TestPCIeMaximumCountersAreLocalToEachReadAndCollect(t *testing.T) {
	t.Run("preexisting_scalar_failure", func(t *testing.T) {
		poison := &pciePoisonError{}
		source := pcieTestSource([]string{pcieTestAddress}, func(name string) ([]byte, error) {
			if name == "sys/class/dmi/id/sys_vendor" {
				return nil, poison
			}
			return nil, fs.ErrNotExist
		})
		snapshot, diagnostics, err := NewLinuxCollectorWithSource(source).CollectWithDiagnostics(t.Context())
		if err != nil || diagnostics != (CollectionDiagnostics{ScalarFailures: 1}) || len(pcieTestAttributes(t, snapshot, pcieTestAddress)) != 0 || poison.calls != 0 {
			t.Fatal("unrelated earlier failure was attributed to absent PCIe observations")
		}
	})
	t.Run("same_collector_reset", func(t *testing.T) {
		fail := true
		poison := &pciePoisonError{}
		source := pcieTestSource([]string{pcieTestAddress}, func(name string) ([]byte, error) {
			if fail && name == pcieTestBase+"max_link_speed" {
				return nil, poison
			}
			return nil, fs.ErrNotExist
		})
		collector := NewLinuxCollectorWithSource(source)
		first, diagnostics, err := collector.CollectWithDiagnostics(t.Context())
		if err != nil || diagnostics != (CollectionDiagnostics{ScalarFailures: 1}) || !reflect.DeepEqual(pcieTestAttributes(t, first, pcieTestAddress), map[string]string{"max_link_speed_state": "read_error", "max_link_width_state": "unavailable"}) {
			t.Fatal("first failing observation prerequisite changed")
		}
		fail = false
		second, diagnostics, err := collector.CollectWithDiagnostics(t.Context())
		if err != nil || diagnostics != (CollectionDiagnostics{}) || len(pcieTestAttributes(t, second, pcieTestAddress)) != 0 || poison.calls != 0 {
			t.Fatal("counter or field state leaked into a fresh Collect")
		}
	})
}

func TestPCIeMaximumAttemptAndByteBudgetsRemainFatal(t *testing.T) {
	t.Run("one_remaining_attempt", func(t *testing.T) {
		var requests []string
		source := pcieTestSource([]string{pcieTestAddress}, func(name string) ([]byte, error) { requests = append(requests, name); return nil, fs.ErrNotExist })
		collection := NewLinuxCollectorWithSource(source).newCollection(t.Context())
		// Nine old PCI scalars leave exactly one attempt for the maximum pair.
		collection.acquisitionBudget.attempts = 10
		components := collection.collectPCI()
		snapshot, err := collection.snapshot(components, NewSnapshot)
		if err != ErrAcquisitionBudget || !reflect.DeepEqual(snapshot, Snapshot{}) || len(components) != 0 || len(requests) != 10 || requests[9] != pcieTestBase+"max_link_speed" || collection.acquisitionBudget.diagnostics != (CollectionDiagnostics{}) {
			t.Fatal("second-attempt exhaustion returned a partial PCI result or changed the clean first absence")
		}
	})
	for _, tc := range []struct {
		name  string
		bytes int
		want  error
	}{{"exact_pair_bytes", 17, nil}, {"one_byte_short", 16, ErrAcquisitionBudget}, {"zero_bytes", 0, ErrAcquisitionBudget}} {
		t.Run(tc.name, func(t *testing.T) {
			collection := NewLinuxCollectorWithSource(pcieTestFields(map[string][]byte{"max_link_speed": []byte("8.0 GT/s PCIe\n"), "max_link_width": []byte("16\n")})).newCollection(t.Context())
			collection.acquisitionBudget.bytes = tc.bytes
			collection.acquisitionBudget.attempts = 2
			attributes := map[string]string{"address": pcieTestAddress}
			collection.addPCIeCapabilities(attributes, strings.TrimSuffix(pcieTestBase, "/"))
			snapshot, err := collection.snapshot([]Component{NewComponent(ComponentPCI, "pci:"+pcieTestAddress, attributes)}, NewSnapshot)
			if tc.want != nil {
				pcieRequireEmpty(t, snapshot, CollectionDiagnostics{}, err, tc.want)
				return
			}
			if err != nil || collection.acquisitionBudget.bytes != 0 || collection.acquisitionBudget.attempts != 0 || !reflect.DeepEqual(pcieTestAttributes(t, snapshot, pcieTestAddress), map[string]string{"max_link_speed_state": "reported", "max_link_speed_mts": "8000", "max_link_width_state": "reported", "max_link_width_lanes": "16"}) {
				t.Fatal("exact acquisition budget changed")
			}
		})
	}
}

func TestPCIeMaximumRawByteBoundaryAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
		want error
	}{{"exact_scalar_limit", 65536, nil}, {"over_scalar_limit", 65537, ErrScalarLimit}} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, diagnostics, err := NewLinuxCollectorWithSource(pcieTestFields(map[string][]byte{"max_link_speed": bytes.Repeat([]byte{'x'}, tc.size), "max_link_width": []byte("16")})).CollectWithDiagnostics(t.Context())
			if tc.want != nil {
				pcieRequireEmpty(t, snapshot, diagnostics, err, tc.want)
				return
			}
			if err != nil || diagnostics != (CollectionDiagnostics{}) || !reflect.DeepEqual(pcieTestAttributes(t, snapshot, pcieTestAddress), map[string]string{"max_link_speed_state": "malformed", "max_link_width_state": "reported", "max_link_width_lanes": "16"}) {
				t.Fatal("completed boundary scalar was truncated or normalized")
			}
		})
	}
	for _, tc := range []struct {
		name, stop   string
		maximumReads int
	}{{"cancel_before_collect", "before", 0}, {"cancel_after_speed", "max_link_speed", 1}, {"cancel_after_width", "max_link_width", 2}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reached := 0
			backing := pcieTestFields(map[string][]byte{"max_link_speed": []byte("8.0 GT/s PCIe"), "max_link_width": []byte("16")})
			source := pcieTestSource([]string{pcieTestAddress}, func(name string) ([]byte, error) {
				if name == pcieTestBase+"max_link_speed" || name == pcieTestBase+"max_link_width" {
					reached++
					if name == pcieTestBase+tc.stop {
						cancel()
					}
				}
				return backing.ReadFile(name)
			})
			if tc.stop == "before" {
				cancel()
			}
			snapshot, diagnostics, err := NewLinuxCollectorWithSource(source).CollectWithDiagnostics(ctx)
			pcieRequireEmpty(t, snapshot, diagnostics, err, context.Canceled)
			if reached != tc.maximumReads {
				t.Fatal("cancellation admitted a later maximum read")
			}
		})
	}
}

func TestPCIeMaximumEnumeratesBeyond325AndRejectsLateFatalResults(t *testing.T) {
	addresses := make([]string, 326)
	for index := range addresses {
		addresses[index] = fmt.Sprintf("%04x:00:00.0", index)
	}
	last := "sys/bus/pci/devices/0145:00:00.0/max_link_width"
	tooLarge := bytes.Repeat([]byte{'9'}, 65537)
	for _, tc := range []struct {
		name     string
		want     error
		failures uint32
	}{{"all_326", nil, 0}, {"last_observation_error", nil, 1}, {"last_oversize", ErrScalarLimit, 0}, {"last_cancel", context.Canceled, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reached := 0
			poison := &pciePoisonError{}
			source := pcieTestSource(addresses, func(name string) ([]byte, error) {
				if name == last {
					reached++
					switch tc.name {
					case "last_observation_error":
						return nil, poison
					case "last_oversize":
						return tooLarge, nil
					case "last_cancel":
						cancel()
					}
				}
				if strings.HasPrefix(name, "sys/bus/pci/devices/") {
					if strings.HasSuffix(name, "/max_link_speed") {
						return []byte("2.5 GT/s PCIe"), nil
					}
					if strings.HasSuffix(name, "/max_link_width") {
						return []byte("8"), nil
					}
				}
				return nil, fs.ErrNotExist
			})
			snapshot, diagnostics, err := NewLinuxCollectorWithSource(source).CollectWithDiagnostics(ctx)
			if reached != 1 || poison.calls != 0 {
				t.Fatal("late device was skipped or untrusted error callback invoked")
			}
			if tc.want != nil {
				pcieRequireEmpty(t, snapshot, diagnostics, err, tc.want)
				return
			}
			if err != nil || len(snapshot.Components) != 326 || diagnostics != (CollectionDiagnostics{ScalarFailures: tc.failures}) {
				t.Fatal("enumeration truncated or ordinary failure changed collection status")
			}
			for _, address := range addresses {
				want := map[string]string{"max_link_speed_state": "reported", "max_link_speed_mts": "2500", "max_link_width_state": "reported", "max_link_width_lanes": "8"}
				if address == "0145:00:00.0" && tc.failures == 1 {
					want["max_link_width_state"] = "read_error"
					delete(want, "max_link_width_lanes")
				}
				if !reflect.DeepEqual(pcieTestAttributes(t, snapshot, address), want) {
					t.Fatal("independent device observation changed")
				}
			}
		})
	}
}

func TestPCIeMaximumOwnedRawAdmissionBeforeClassification(t *testing.T) {
	for _, name := range []string{"completed_one_byte_reads", "close_failure", "cancel_at_close", "close_failure_and_cancel", "read_error_discards_prefix", "budget_and_close_failure", "overflow_and_cancel"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			poison := &pciePoisonError{}
			handle := scalarBytes([]byte("8.0 GT/s PCIe\n"), 1, false)
			budget := newInventoryAcquisitionBudget()
			var want error
			wantObserved := false
			var failures uint32
			switch name {
			case "completed_one_byte_reads":
				wantObserved = true
			case "close_failure":
				handle.closeErr = poison
				want = ErrScalarClose
			case "cancel_at_close":
				handle.onClose = cancel
				want = context.Canceled
			case "close_failure_and_cancel":
				handle.closeErr = poison
				handle.onClose = cancel
				want = ErrScalarClose
			case "read_error_discards_prefix":
				handle.read = func(p []byte) (int, error) { return copy(p, []byte("8.0 GT/s PCIe\nPCIE-PRIVATE-CANARY")), poison }
				failures = 1
			case "budget_and_close_failure":
				budget.bytes = 13
				handle.closeErr = poison
				want = ErrScalarClose
			case "overflow_and_cancel":
				handle = scalarBytes(bytes.Repeat([]byte{'9'}, 65537), 0, true)
				read := handle.read
				total := 0
				handle.read = func(p []byte) (int, error) {
					n, e := read(p)
					total += n
					if total > 65536 {
						cancel()
					}
					return n, e
				}
				want = ErrScalarLimit
			}
			data, observed, err := readInventoryScalarWithBudget(ctx, "fixture-max-link-speed", func(string) (inventoryScalar, error) { return handle, nil }, &budget)
			if err != want || observed != wantObserved || handle.closes != 1 || poison.calls != 0 || budget.diagnostics != (CollectionDiagnostics{ScalarFailures: failures}) {
				t.Fatal("owned completion, cleanup or failure precedence changed")
			}
			if !observed {
				if data != nil {
					t.Fatal("unadmitted raw bytes survived")
				}
				return
			}
			state, value := parsePCIeMaximumSpeed(data)
			if state != "reported" || value != "8000" {
				t.Fatal("admitted literal capability changed")
			}
		})
	}
}

func TestPCIeMaximumRootSourceOwnedFixture(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fields map[string][]byte
		want   map[string]string
	}{
		{"reported", map[string][]byte{"max_link_speed": []byte("32.0 GT/s PCIe\n"), "max_link_width": []byte("12\n")}, map[string]string{"max_link_speed_state": "reported", "max_link_speed_mts": "32000", "max_link_width_state": "reported", "max_link_width_lanes": "12"}},
		{"absent", nil, map[string]string{}},
		{"empty_is_observed", map[string][]byte{"max_link_speed": nil}, map[string]string{"max_link_speed_state": "malformed", "max_link_width_state": "unavailable"}},
		{"crlf_is_not_normalized", map[string][]byte{"max_link_speed": []byte("8.0 GT/s PCIe\r\n"), "max_link_width": []byte("16\n")}, map[string]string{"max_link_speed_state": "malformed", "max_link_width_state": "reported", "max_link_width_lanes": "16"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			address := "fixture-pci"
			root := t.TempDir()
			pcieWriteDevice(t, root, address, tc.fields)
			snapshot, diagnostics, err := NewLinuxCollector(root).CollectWithDiagnostics(t.Context())
			if err != nil || diagnostics != (CollectionDiagnostics{}) || !reflect.DeepEqual(pcieTestAttributes(t, snapshot, address), tc.want) {
				t.Fatal("owned filesystem acquisition differs from literal capability contract")
			}
		})
	}
}
