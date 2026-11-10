package inventory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"testing"
)

func pcieCurrentAttributes(t *testing.T, snapshot Snapshot, address string) map[string]string {
	t.Helper()
	for _, component := range snapshot.Components {
		if component.Type == ComponentPCI && component.ID == "pci:"+address {
			fields := make(map[string]string)
			for _, attribute := range component.Attributes {
				if strings.HasPrefix(attribute.Name, "current_link_") {
					fields[attribute.Name] = attribute.Value
				}
			}
			return fields
		}
	}
	t.Fatal("expected PCI component missing")
	return nil
}

func TestPCIeNegotiatedFiniteLiteralsThroughCollector(t *testing.T) {
	// Independent ABI literals; do not generate expected values from parsers.
	for _, speed := range []struct{ raw, value string }{{"2.5 GT/s PCIe", "2500"}, {"5.0 GT/s PCIe", "5000"}, {"8.0 GT/s PCIe", "8000"}, {"16.0 GT/s PCIe", "16000"}, {"32.0 GT/s PCIe", "32000"}, {"64.0 GT/s PCIe", "64000"}} {
		for _, width := range []string{"1", "2", "4", "8", "12", "16", "32"} {
			for _, frame := range []string{"", "\n"} {
				t.Run(speed.value+"/"+width+fmt.Sprint(len(frame)), func(t *testing.T) {
					snapshot, diagnostics, err := NewLinuxCollectorWithSource(pcieTestFields(map[string][]byte{"current_link_speed": []byte(speed.raw + frame), "current_link_width": []byte(width + frame)})).CollectWithDiagnostics(t.Context())
					want := map[string]string{"current_link_speed_state": "reported", "current_link_speed_mts": speed.value, "current_link_width_state": "reported", "current_link_width_lanes": width}
					if err != nil || diagnostics != (CollectionDiagnostics{}) || !reflect.DeepEqual(pcieCurrentAttributes(t, snapshot, pcieTestAddress), want) || len(pcieTestAttributes(t, snapshot, pcieTestAddress)) != 0 {
						t.Fatal("finite observation or independent maximum absence changed")
					}
				})
			}
		}
	}
}

func TestPCIeNegotiatedNonreportedAndMalformed(t *testing.T) {
	for _, tc := range []struct{ field, raw, state string }{
		{"speed", "Unknown", "unknown"}, {"speed", "Unknown\n", "unknown"},
		{"speed", "128.0 GT/s PCIe", "unsupported"}, {"speed", "0.0 GT/s PCIe", "unsupported"},
		{"speed", "18446744073709551.6 GT/s PCIe", "unsupported"},
		{"speed", "18446744073709551.7 GT/s PCIe", "malformed"},
		{"speed", "18446744073709552.0 GT/s PCIe", "malformed"},
		{"speed", "8 GT/s PCIe", "malformed"}, {"speed", "8.00 GT/s PCIe", "malformed"},
		{"speed", "08.0 GT/s PCIe", "malformed"}, {"speed", "8.0 GB/s PCIe", "malformed"},
		{"speed", "8.0 GT/s PCIe\nCANARY", "malformed"}, {"speed", "８.0 GT/s PCIe", "malformed"},
		{"width", "0", "unsupported"}, {"width", "3", "unsupported"}, {"width", "63", "unsupported"},
		{"width", "64", "unsupported"}, {"width", "255", "unsupported"}, {"width", "255\n", "unsupported"},
		{"width", "18446744073709551615", "unsupported"}, {"width", "18446744073709551616", "malformed"},
		{"width", "Unknown", "malformed"}, {"width", "08", "malformed"}, {"width", "１６", "malformed"},
		{"width", "16 lanes", "malformed"}, {"width", "0x10", "malformed"},
	} {
		t.Run(tc.field+"/"+tc.raw, func(t *testing.T) {
			fields := map[string][]byte{"current_link_" + tc.field: []byte(tc.raw)}
			snapshot, diagnostics, err := NewLinuxCollectorWithSource(pcieTestFields(fields)).CollectWithDiagnostics(t.Context())
			want := map[string]string{"current_link_speed_state": "unavailable", "current_link_width_state": "unavailable"}
			want["current_link_"+tc.field+"_state"] = tc.state
			if err != nil || diagnostics != (CollectionDiagnostics{}) || !reflect.DeepEqual(pcieCurrentAttributes(t, snapshot, pcieTestAddress), want) {
				t.Fatal("reserved/malformed bytes acquired a value or wrong state")
			}
		})
	}
	for _, field := range []string{"speed", "width"} {
		valid := "16"
		if field == "speed" {
			valid = "8.0 GT/s PCIe"
		}
		for _, raw := range []string{"", "\n", " " + valid, valid + " ", "\t" + valid, "+" + valid, "-" + valid, valid + "\r\n", valid + "\n\n", valid + "\x00"} {
			fields := map[string][]byte{"current_link_" + field: []byte(raw)}
			snapshot, _, err := NewLinuxCollectorWithSource(pcieTestFields(fields)).CollectWithDiagnostics(t.Context())
			if err != nil || pcieCurrentAttributes(t, snapshot, pcieTestAddress)["current_link_"+field+"_state"] != "malformed" {
				t.Fatal("framing was silently normalized", field)
			}
		}
	}
	state, value := parsePCIeMaximumWidth([]byte("255"))
	if state != "unknown" || value != "" {
		t.Fatal("maximum sentinel policy changed")
	}
}

func TestPCIeNegotiatedValuesAreNotClampedOrConditionedOnMaxima(t *testing.T) {
	for _, maxima := range []map[string][]byte{
		nil,
		{"max_link_speed": []byte("2.5 GT/s PCIe"), "max_link_width": []byte("1")},
		{"max_link_speed": []byte("INVALID"), "max_link_width": []byte("255")},
	} {
		fields := map[string][]byte{"current_link_speed": []byte("64.0 GT/s PCIe"), "current_link_width": []byte("32")}
		for key, value := range maxima {
			fields[key] = value
		}
		snapshot, diagnostics, err := NewLinuxCollectorWithSource(pcieTestFields(fields)).CollectWithDiagnostics(t.Context())
		want := map[string]string{"current_link_speed_state": "reported", "current_link_speed_mts": "64000", "current_link_width_state": "reported", "current_link_width_lanes": "32"}
		if err != nil || diagnostics != (CollectionDiagnostics{}) || !reflect.DeepEqual(pcieCurrentAttributes(t, snapshot, pcieTestAddress), want) {
			t.Fatal("maximum capability constrained an independent current observation")
		}
	}
}

func TestPCIeNegotiatedAbsenceErrorsAndFreshCollection(t *testing.T) {
	const legacy = `{"schema_version":1,"components":[{"type":"pci","id":"pci:0000:00:01.0","attributes":[{"name":"address","value":"0000:00:01.0"}]}]}`
	absent, _, err := NewLinuxCollectorWithSource(pcieTestFields(nil)).CollectWithDiagnostics(t.Context())
	raw, _ := json.Marshal(absent)
	if err != nil || string(raw) != legacy {
		t.Fatal("absent fields changed historical bytes")
	}
	for _, fail := range []string{"current_link_speed", "current_link_width", "max_link_speed", "class"} {
		for _, prefix := range []bool{false, true} {
			t.Run(fail+fmt.Sprint(prefix), func(t *testing.T) {
				poison := &pciePoisonError{}
				fields := map[string][]byte{"current_link_speed": []byte("Unknown"), "current_link_width": []byte("0")}
				backing := pcieTestFields(fields)
				failed := true
				source := pcieTestSource([]string{pcieTestAddress}, func(name string) ([]byte, error) {
					if failed && name == pcieTestBase+fail {
						if prefix {
							return []byte("8.0 GT/s PCIe\nPCIE-PRIVATE-CANARY"), poison
						}
						return nil, poison
					}
					return backing.ReadFile(name)
				})
				collector := NewLinuxCollectorWithSource(source)
				got, diagnostics, err := collector.CollectWithDiagnostics(t.Context())
				want := map[string]string{"current_link_speed_state": "unknown", "current_link_width_state": "unsupported"}
				if strings.HasPrefix(fail, "current_") {
					want[fail+"_state"] = "read_error"
				}
				b, _ := json.Marshal(got)
				if err != nil || diagnostics != (CollectionDiagnostics{ScalarFailures: 1}) || poison.calls != 0 || !reflect.DeepEqual(pcieCurrentAttributes(t, got, pcieTestAddress), want) || bytes.Contains(b, []byte("PCIE-PRIVATE-CANARY")) {
					t.Fatal("ordinary error escaped or polluted a peer read")
				}
				failed = false
				delete(fields, "current_link_speed")
				delete(fields, "current_link_width")
				got, diagnostics, err = collector.CollectWithDiagnostics(t.Context())
				b, _ = json.Marshal(got)
				if err != nil || diagnostics != (CollectionDiagnostics{}) || string(b) != legacy {
					t.Fatal("fresh collection retained prior observation state")
				}
			})
		}
	}
}

func TestPCIeNegotiatedFatalAcquisitionBoundaries(t *testing.T) {
	t.Run("attempts", func(t *testing.T) {
		var reads []string
		source := pcieTestSource([]string{pcieTestAddress}, func(name string) ([]byte, error) { reads = append(reads, name); return nil, fs.ErrNotExist })
		c := NewLinuxCollectorWithSource(source).newCollection(t.Context())
		c.acquisitionBudget.attempts = 12
		parts := c.collectPCI()
		snapshot, err := c.snapshot(parts, NewSnapshot)
		pcieRequireEmpty(t, snapshot, CollectionDiagnostics{}, err, ErrAcquisitionBudget)
		if len(parts) != 0 || len(reads) != 12 || reads[11] != pcieTestBase+"current_link_speed" {
			t.Fatal("current width read exceeded the shared budget")
		}
	})
	for _, limit := range []int{16, 17} {
		c := NewLinuxCollectorWithSource(pcieTestFields(map[string][]byte{"current_link_speed": []byte("8.0 GT/s PCIe\n"), "current_link_width": []byte("16\n")})).newCollection(t.Context())
		c.acquisitionBudget.bytes = limit
		c.acquisitionBudget.attempts = 13
		parts := c.collectPCI()
		snapshot, err := c.snapshot(parts, NewSnapshot)
		if limit == 16 {
			pcieRequireEmpty(t, snapshot, CollectionDiagnostics{}, err, ErrAcquisitionBudget)
		} else if err != nil || len(parts) != 1 || c.acquisitionBudget.bytes != 0 || c.acquisitionBudget.attempts != 0 {
			t.Fatal("exact full PCI read budget failed")
		}
	}
	for _, field := range []string{"current_link_speed", "current_link_width"} {
		for _, size := range []int{65536, 65537} {
			snapshot, diagnostics, err := NewLinuxCollectorWithSource(pcieTestFields(map[string][]byte{field: bytes.Repeat([]byte{'9'}, size)})).CollectWithDiagnostics(t.Context())
			if size == 65537 {
				pcieRequireEmpty(t, snapshot, diagnostics, err, ErrScalarLimit)
			} else if err != nil || pcieCurrentAttributes(t, snapshot, pcieTestAddress)[field+"_state"] != "malformed" {
				t.Fatal("exact scalar boundary incorrectly admitted or truncated")
			}
		}
		ctx, cancel := context.WithCancel(t.Context())
		reads := 0
		source := pcieTestSource([]string{pcieTestAddress}, func(name string) ([]byte, error) {
			if strings.HasPrefix(name, pcieTestBase+"current_link_") {
				reads++
				if name == pcieTestBase+field {
					cancel()
				}
				return []byte("16"), nil
			}
			return nil, fs.ErrNotExist
		})
		snapshot, diagnostics, err := NewLinuxCollectorWithSource(source).CollectWithDiagnostics(ctx)
		cancel()
		pcieRequireEmpty(t, snapshot, diagnostics, err, context.Canceled)
		want := 1
		if field == "current_link_width" {
			want = 2
		}
		if reads != want {
			t.Fatal("collection continued after cancellation")
		}
	}
}

func TestPCIeNegotiatedLastWidthOf326Devices(t *testing.T) {
	addresses := make([]string, 326)
	for i := range addresses {
		addresses[i] = fmt.Sprintf("%04x:00:00.0", i)
	}
	for _, fail := range []bool{false, true} {
		reached := 0
		source := pcieTestSource(addresses, func(name string) ([]byte, error) {
			if name == "sys/bus/pci/devices/0145:00:00.0/current_link_width" {
				reached++
				if fail {
					return bytes.Repeat([]byte{'9'}, 65537), nil
				}
			}
			if strings.HasSuffix(name, "/current_link_speed") {
				return []byte("2.5 GT/s PCIe"), nil
			}
			if strings.HasSuffix(name, "/current_link_width") {
				return []byte("1"), nil
			}
			return nil, fs.ErrNotExist
		})
		snapshot, diagnostics, err := NewLinuxCollectorWithSource(source).CollectWithDiagnostics(t.Context())
		if reached != 1 {
			t.Fatal("last current width skipped or retried")
		}
		if fail {
			pcieRequireEmpty(t, snapshot, diagnostics, err, ErrScalarLimit)
		} else if err != nil || len(snapshot.Components) != 326 || pcieCurrentAttributes(t, snapshot, "0145:00:00.0")["current_link_width_lanes"] != "1" {
			t.Fatal("complete enumeration changed")
		}
	}
}

func TestPCIeNegotiatedOwnedCloseBeforeObservation(t *testing.T) {
	for _, outcome := range []string{"clean", "close_failure", "cancel_after_close"} {
		ctx, cancel := context.WithCancel(t.Context())
		handle := scalarBytes([]byte("16\n"), 1, false)
		poison := &pciePoisonError{}
		var want error
		if outcome == "close_failure" {
			handle.closeErr = poison
			want = ErrScalarClose
		}
		if outcome == "cancel_after_close" {
			handle.onClose = cancel
			want = context.Canceled
		}
		budget := newInventoryAcquisitionBudget()
		raw, observed, err := readInventoryScalarWithBudget(ctx, "fixture-current-width", func(string) (inventoryScalar, error) { return handle, nil }, &budget)
		cancel()
		if err != want || handle.closes != 1 || poison.calls != 0 {
			t.Fatal("owned current read changed failure/closure precedence")
		}
		if want != nil {
			if observed || raw != nil {
				t.Fatal("unclosed/cancelled current bytes escaped")
			}
			continue
		}
		state, value := parsePCIeNegotiatedWidth(raw)
		if !observed || state != "reported" || value != "16" {
			t.Fatal("owned completed width not admitted")
		}
	}
}
