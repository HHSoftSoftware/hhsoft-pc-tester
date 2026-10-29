package plan

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func powerPlanControl() inventory.Component {
	return inventory.NewComponent(inventory.ComponentSystem, "system", map[string]string{"product_uuid": "11111111-2222-3333-4444-555555555555"})
}

func powerPlanSnapshot(attributes map[string]string) inventory.Snapshot {
	return inventory.NewSnapshot([]inventory.Component{
		powerPlanControl(),
		inventory.NewComponent(inventory.ComponentType("power_supply"), "power_supply:1", attributes),
	})
}

func TestPowerSupplyTelemetryDoesNotChangeGraphOrPlan(t *testing.T) {
	// Literal component/graph vectors bind the fixed HMAC domain and inputs.
	key := bytes.Repeat([]byte{'p'}, 32)
	values := map[string]string{
		"supply_type": "Battery", "scope": "System", "present": "1", "online": "1",
		"status": "Charging", "capacity_percent": "99", "cycle_count": "2",
		"energy_full_uwh": "100", "energy_full_design_uwh": "200",
		"charge_full_uah": "10", "charge_full_design_uah": "20",
	}
	attributes := make(map[string]string)
	for name, value := range values {
		attributes[name] = value
		attributes[name+"_state"] = "reported"
	}
	snapshot := powerPlanSnapshot(attributes)
	graph, err := ObserveHardwareGraph(snapshot, key, "power-test")
	want := HardwareGraph{
		Algorithm: "hmac-sha256", KeyEpoch: "power-test",
		Value: "hmac-sha256:a3548e918a2b878a22dbc856adc06dd01347bcf56c758f446a0cdc34bcaace72",
		Components: []HardwareComponent{
			{Type: inventory.ComponentType("power_supply"), ID: "power_supply:1", Value: "hmac-sha256:b6794b20fc3000be779a3bc7f8d2d187a74d00f0a0d90881e11d4382ade3193d"},
			{Type: inventory.ComponentSystem, ID: "system", Value: "hmac-sha256:3ef7953c83044f1e0bb7c9f762fd1b2f6691ea6009e31e1a5804cb11692a0173"},
		},
	}
	if err != nil || !reflect.DeepEqual(graph, want) {
		t.Fatal("independent graph vector failed", err)
	}
	frozen, err := Build(snapshot, ProfileQuick, "x86_64", key, "power-test")
	if err != nil {
		t.Fatal(err)
	}
	frozenBytes, err := json.Marshal(frozen)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ name, replacement string }{
		{"supply_type", "UPS"}, {"scope", "Device"}, {"present", "0"}, {"online", "2"},
		{"status", "Discharging"}, {"capacity_percent", "0"}, {"cycle_count", "1"},
		{"energy_full_uwh", "-1"}, {"energy_full_design_uwh", "0"},
		{"charge_full_uah", "2147483647"}, {"charge_full_design_uah", "1"},
	} {
		for _, stateChange := range []bool{false, true} {
			name := item.name
			if stateChange {
				name += "_state"
			}
			t.Run(name, func(t *testing.T) {
				mutated := make(map[string]string)
				for key, value := range attributes {
					mutated[key] = value
				}
				if stateChange {
					delete(mutated, item.name)
					mutated[name] = "unavailable"
				} else {
					mutated[name] = item.replacement
				}
				observed := powerPlanSnapshot(mutated)
				actual, err := ObserveHardwareGraph(observed, key, "power-test")
				if err != nil || !reflect.DeepEqual(actual, want) {
					t.Fatal("telemetry changed graph identity", err)
				}
				next, err := Build(observed, ProfileQuick, "x86_64", key, "power-test")
				if err != nil {
					t.Fatal(err)
				}
				nextBytes, err := json.Marshal(next)
				if err != nil || !bytes.Equal(nextBytes, frozenBytes) {
					t.Fatal("telemetry changed frozen plan bytes")
				}
			})
		}
	}
}

func TestPowerSupplyProjectionDoesNotWidenOtherTypes(t *testing.T) {
	key := bytes.Repeat([]byte{'p'}, 32)
	for _, name := range []string{
		"supply_type", "scope", "present", "online", "status", "capacity_percent", "cycle_count",
		"energy_full_uwh", "energy_full_design_uwh", "charge_full_uah", "charge_full_design_uah",
		"supply_type_state", "scope_state", "present_state", "online_state", "status_state",
		"capacity_percent_state", "cycle_count_state", "energy_full_uwh_state", "energy_full_design_uwh_state",
		"charge_full_uah_state", "charge_full_design_uah_state",
	} {
		t.Run(name, func(t *testing.T) {
			before := inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentCPU, "cpu:0", map[string]string{name: "first"})})
			after := inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentCPU, "cpu:0", map[string]string{name: "second"})})
			left, err := ObserveHardwareGraph(before, key, "power-test")
			if err != nil {
				t.Fatal(err)
			}
			right, err := ObserveHardwareGraph(after, key, "power-test")
			if err != nil {
				t.Fatal(err)
			}
			// Only these two names were already volatile for every type.
			previouslyVolatile := name == "online" || name == "status"
			if (left.Value == right.Value) != previouslyVolatile {
				t.Fatal("new-type projection changed legacy policy")
			}
		})
	}
	t.Run("future_attribute", func(t *testing.T) {
		left, err := ObserveHardwareGraph(powerPlanSnapshot(map[string]string{"future_attribute": "first"}), key, "power-test")
		if err != nil {
			t.Fatal(err)
		}
		right, err := ObserveHardwareGraph(powerPlanSnapshot(map[string]string{"future_attribute": "second"}), key, "power-test")
		if err != nil || left.Value == right.Value {
			t.Fatal("unknown new-type attribute was silently ignored")
		}
	})
}

func powerPlanCollectedFixture(t *testing.T, names []string, capacity string, emptyClass bool) inventory.Snapshot {
	t.Helper()
	root := t.TempDir()
	dmi := filepath.Join(root, "sys/class/dmi/id")
	if err := os.MkdirAll(dmi, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dmi, "product_uuid"), []byte("11111111-2222-3333-4444-555555555555"), 0600); err != nil {
		t.Fatal(err)
	}
	if emptyClass {
		if err := os.MkdirAll(filepath.Join(root, "sys/class/power_supply"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range names {
		directory := filepath.Join(root, "sys/class/power_supply", name)
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "type"), []byte("Battery\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "capacity"), []byte(capacity), 0600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := inventory.NewLinuxCollector(root).Collect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestPowerSupplyNoDataPreservesHistoricalGraphBindings(t *testing.T) {
	key := bytes.Repeat([]byte{'p'}, 32)
	const expectedSnapshot = "{\"schema_version\":1,\"components\":[{\"type\":\"system\",\"id\":\"system\",\"attributes\":[{\"name\":\"product_uuid\",\"value\":\"11111111-2222-3333-4444-555555555555\"}]}]}"
	var firstPlan []byte
	for _, emptyClass := range []bool{false, true} {
		snapshot := powerPlanCollectedFixture(t, nil, "", emptyClass)
		raw, err := json.Marshal(snapshot)
		if err != nil || string(raw) != expectedSnapshot {
			t.Fatal("historical no-supply snapshot bytes changed")
		}
		graph, err := ObserveHardwareGraph(snapshot, key, "power-test")
		if err != nil || graph.Value != "hmac-sha256:9c181c7743d2bd663de28772ea8df728e71990a3918085e136cf2b9338eb12c3" ||
			len(graph.Components) != 1 || graph.Components[0].Value != "hmac-sha256:3ef7953c83044f1e0bb7c9f762fd1b2f6691ea6009e31e1a5804cb11692a0173" {
			t.Fatal("historical graph HMAC changed", err)
		}
		frozen, err := Build(snapshot, ProfileQuick, "x86_64", key, "power-test")
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(frozen)
		if err != nil {
			t.Fatal(err)
		}
		if firstPlan == nil {
			firstPlan = append([]byte(nil), encoded...)
		} else if !bytes.Equal(firstPlan, encoded) {
			t.Fatal("absent class and observed empty class changed plan bytes")
		}
	}
}

func TestPowerSupplySameCountReplacementIsNotObservedIdentity(t *testing.T) {
	key := bytes.Repeat([]byte{'p'}, 32)
	before := powerPlanCollectedFixture(t, []string{"hid-PRIVATE-AA-11-battery"}, "99", false)
	replacement := powerPlanCollectedFixture(t, []string{"hid-PRIVATE-BB-22-battery"}, "1", false)
	more := powerPlanCollectedFixture(t, []string{"z-PRIVATE-CC-33", "a-PRIVATE-DD-44"}, "50", false)
	left, err := ObserveHardwareGraph(before, key, "power-test")
	if err != nil {
		t.Fatal(err)
	}
	right, err := ObserveHardwareGraph(replacement, key, "power-test")
	if err != nil {
		t.Fatal(err)
	}
	if left.Value != "hmac-sha256:a3548e918a2b878a22dbc856adc06dd01347bcf56c758f446a0cdc34bcaace72" || !reflect.DeepEqual(left, right) {
		t.Fatal("same-count replacement was assigned persistent identity")
	}
	if delta := CompareHardwareGraphs(left, right); !reflect.DeepEqual(delta, HardwareDelta{}) {
		t.Fatal("same ordinal invented replacement evidence")
	}
	added, err := ObserveHardwareGraph(more, key, "power-test")
	if err != nil {
		t.Fatal(err)
	}
	wantAdded := HardwareDelta{Added: []HardwareComponentRef{{Type: inventory.ComponentType("power_supply"), ID: "power_supply:2"}}}
	if delta := CompareHardwareGraphs(left, added); !reflect.DeepEqual(delta, wantAdded) {
		t.Fatal("reported cardinality change was not retained")
	}
	wantRemoved := HardwareDelta{Removed: []HardwareComponentRef{{Type: inventory.ComponentType("power_supply"), ID: "power_supply:2"}}}
	if delta := CompareHardwareGraphs(added, left); !reflect.DeepEqual(delta, wantRemoved) {
		t.Fatal("reported row removal was not retained")
	}
	for _, snapshot := range []inventory.Snapshot{before, replacement, more} {
		frozen, err := Build(snapshot, ProfileQuick, "x86_64", key, "power-test")
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(frozen)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "PRIVATE-") {
			t.Fatal("raw supply names reached the plan")
		}
	}
}

func TestPowerSupplyAddsNoCatalogWorkerOrStorageActivation(t *testing.T) {
	key := bytes.Repeat([]byte{'p'}, 32)
	control := inventory.NewComponent(inventory.ComponentBlock, "block:fixture", map[string]string{"kind": "device"})
	before := inventory.NewSnapshot([]inventory.Component{powerPlanControl(), control})
	after := inventory.NewSnapshot([]inventory.Component{powerPlanControl(), control, inventory.NewComponent(inventory.ComponentType("power_supply"), "power_supply:1", map[string]string{"online": "2", "online_state": "reported"})})
	left, err := Build(before, ProfileQuick, "x86_64", key, "power-test")
	if err != nil {
		t.Fatal(err)
	}
	right, err := Build(after, ProfileQuick, "x86_64", key, "power-test")
	if err != nil {
		t.Fatal(err)
	}
	if left.SchemaVersion != 3 || right.SchemaVersion != 3 || len(left.Tests) != 22 || len(right.Tests) != 22 ||
		left.DefinitionsDigest != right.DefinitionsDigest || !reflect.DeepEqual(left.Tests, right.Tests) || !reflect.DeepEqual(left.Capabilities, right.Capabilities) {
		t.Fatal("power inventory changed frozen catalog or worker selection")
	}
	states := make(map[string]TestStatus)
	for _, row := range right.Tests {
		states[row.ID] = row.Status
	}
	if states["storage.quick"] != TestUnsupported || states["storage.destructive"] != TestExcluded ||
		states["storage.readiness.quick"] != TestScheduled || states["inspection.exterior.quick"] != TestScheduled {
		t.Fatal("planned storage or current inspection policy changed")
	}
}
