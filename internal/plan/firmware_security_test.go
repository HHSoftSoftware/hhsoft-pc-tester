package plan

import (
	"bytes"
	"encoding/json"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func firmwarePlanSnapshot() inventory.Snapshot {
	return inventory.NewSnapshot([]inventory.Component{
		powerPlanControl(),
		inventory.NewComponent(inventory.ComponentType("firmware_security"), "firmware_security:uefi", map[string]string{"secure_boot": "1", "secure_boot_state": "reported", "setup_mode_flag": "0", "setup_mode_flag_state": "reported", "uefi_platform_bits": "64", "uefi_platform_bits_state": "reported"}),
		inventory.NewComponent(inventory.ComponentType("tpm"), "tpm:1", map[string]string{"version_major": "2", "version_major_state": "reported"}),
	})
}
func TestFirmwareSecurityIndependentGraphVector(t *testing.T) {
	snapshot := firmwarePlanSnapshot()
	graph, err := ObserveHardwareGraph(snapshot, bytes.Repeat([]byte{'p'}, 32), "firmware-test")
	want := HardwareGraph{Algorithm: "hmac-sha256", KeyEpoch: "firmware-test", Value: "hmac-sha256:ef708ce82448d2ff5cf5ae1ffa5f80817e7958b4e61b757b9ad84cbfab69bf65", Components: []HardwareComponent{
		{Type: inventory.ComponentType("firmware_security"), ID: "firmware_security:uefi", Value: "hmac-sha256:bd74189da81edee9b762542d1a910f7f230f5b068c7ccc2d360fb11a0c4e4347"},
		{Type: inventory.ComponentSystem, ID: "system", Value: "hmac-sha256:3ef7953c83044f1e0bb7c9f762fd1b2f6691ea6009e31e1a5804cb11692a0173"},
		{Type: inventory.ComponentType("tpm"), ID: "tpm:1", Value: "hmac-sha256:4c2036762d1c25efa12bb4d89b9e2b80a27b212309b195a9b5e724ad7bdabe7b"},
	}}
	if err != nil || !reflect.DeepEqual(graph, want) {
		t.Fatal("independent literal graph", err, graph)
	}
}
func TestFirmwareSecurityEveryConfigurationFieldChangesGraph(t *testing.T) {
	key := bytes.Repeat([]byte{'p'}, 32)
	base := firmwarePlanSnapshot()
	left, err := ObserveHardwareGraph(base, key, "firmware-test")
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := Build(base, ProfileQuick, "x86_64", key, "firmware-test")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		field, value string
		index        int
	}{
		{"secure_boot", "0", 0}, {"secure_boot_state", "unavailable", 0}, {"setup_mode_flag", "1", 0}, {"setup_mode_flag_state", "unsupported", 0}, {"uefi_platform_bits", "32", 0}, {"uefi_platform_bits_state", "malformed", 0}, {"version_major", "1", 2}, {"version_major_state", "unavailable", 2},
	} {
		t.Run(tc.field, func(t *testing.T) {
			changed := firmwarePlanSnapshot()
			attrs := map[string]string{}
			for _, a := range changed.Components[tc.index].Attributes {
				attrs[a.Name] = a.Value
			}
			attrs[tc.field] = tc.value
			c := changed.Components[tc.index]
			changed.Components[tc.index] = inventory.NewComponent(c.Type, c.ID, attrs)
			right, err := ObserveHardwareGraph(changed, key, "firmware-test")
			if err != nil || right.Value == left.Value {
				t.Fatal("configuration silently volatile", err)
			}
			want := HardwareDelta{Changed: []HardwareComponentRef{{Type: c.Type, ID: c.ID}}}
			if delta := CompareHardwareGraphs(left, right); !reflect.DeepEqual(delta, want) {
				t.Fatal("exact graph delta", delta)
			}
			next, err := Build(changed, ProfileQuick, "x86_64", key, "firmware-test")
			if err != nil || next.ID == frozen.ID || next.DefinitionsDigest != frozen.DefinitionsDigest || !reflect.DeepEqual(next.Tests, frozen.Tests) || !reflect.DeepEqual(next.Capabilities, frozen.Capabilities) {
				t.Fatal("configuration did not invalidate plan or created a worker", err)
			}
		})
	}
}
func TestFirmwareSecurityPresenceAndUnknownMetadataStayBound(t *testing.T) {
	key := bytes.Repeat([]byte{'p'}, 32)
	base := firmwarePlanSnapshot()
	left, err := ObserveHardwareGraph(base, key, "firmware-test")
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"remove_tpm", "add_tpm", "unknown_attribute", "other_type_attribute"} {
		t.Run(variant, func(t *testing.T) {
			next := firmwarePlanSnapshot()
			switch variant {
			case "remove_tpm":
				next.Components = next.Components[:2]
			case "add_tpm":
				next.Components = append(next.Components, inventory.NewComponent(inventory.ComponentType("tpm"), "tpm:2", map[string]string{"version_major_state": "unavailable"}))
			case "unknown_attribute":
				next.Components[0].Attributes = append(next.Components[0].Attributes, inventory.Attribute{Name: "future_field", Value: "kept"})
			default:
				next.Components[1].Attributes = append(next.Components[1].Attributes, inventory.Attribute{Name: "secure_boot_state", Value: "kept"})
			}
			right, err := ObserveHardwareGraph(next, key, "firmware-test")
			if err != nil || right.Value == left.Value {
				t.Fatal("metadata or membership dropped", err)
			}
		})
	}
}
func TestFirmwareSecurityNoObservationPreservesHistoricalGraph(t *testing.T) {
	for _, state := range []string{"missing", "empty"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			dmi := filepath.Join(root, "sys/class/dmi/id")
			if err := os.MkdirAll(dmi, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dmi, "product_uuid"), []byte("11111111-2222-3333-4444-555555555555"), 0600); err != nil {
				t.Fatal(err)
			}
			if state == "empty" {
				for _, dir := range []string{"sys/class/tpm", "sys/firmware/efi/efivars"} {
					if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			snapshot, err := inventory.NewLinuxCollector(root).Collect(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(snapshot)
			const want = `{"schema_version":1,"components":[{"type":"system","id":"system","attributes":[{"name":"product_uuid","value":"11111111-2222-3333-4444-555555555555"}]}]}`
			if err != nil || string(raw) != want {
				t.Fatal("old snapshot bytes changed")
			}
			graph, err := ObserveHardwareGraph(snapshot, bytes.Repeat([]byte{'p'}, 32), "firmware-test")
			if err != nil || graph.Value != "hmac-sha256:9c181c7743d2bd663de28772ea8df728e71990a3918085e136cf2b9338eb12c3" {
				t.Fatal("old literal graph changed", err)
			}
		})
	}
}
