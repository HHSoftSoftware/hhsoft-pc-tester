package storagetest

import (
	"context"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func fixtureID(value string) bool {
	if len(value) < 5 || len(value) > 128 || !strings.HasPrefix(value, "block:") && !strings.HasPrefix(value, "usb:") {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._:-", c)) {
			return false
		}
	}
	name := value[strings.IndexByte(value, ':')+1:]
	return name != "" && name != "." && name != ".."
}

// Resolve only the requested fixture rows from the current, unfiltered typed
// inventory. No attributes or unrelated inventory instances are exported.
func resolveFixtures(ctx context.Context, current inventory.Snapshot, fixtures []string, nodes []Node, graph topology) ([]FixtureObservation, error) {
	if current.SchemaVersion != inventory.SnapshotSchemaVersion || current.Components == nil {
		return nil, ErrAdmission
	}
	selected := make(map[string]int, len(fixtures))
	for i, id := range fixtures {
		selected[id] = i
	}
	result := make([]FixtureObservation, len(fixtures))
	seen := make([]bool, len(fixtures))
	for _, component := range current.Components {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		index, wanted := selected[component.ID]
		if !wanted {
			continue
		}
		if seen[index] {
			return nil, ErrAdmission
		}
		seen[index] = true
		observation := FixtureObservation{ID: component.ID, Type: string(component.Type)}
		if strings.HasPrefix(component.ID, "block:") {
			if component.Type != inventory.ComponentBlock {
				return nil, ErrAdmission
			}
			node, exists := graph.ids[component.ID]
			if !exists {
				return nil, ErrAdmission
			}
			count := 0
			for _, attribute := range component.Attributes {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if attribute.Name == "device_number" {
					count++
					observation.DeviceNumber = attribute.Value
				}
			}
			if count != 1 || observation.DeviceNumber != nodes[node].Number {
				return nil, ErrAdmission
			}
		} else if component.Type != inventory.ComponentUSB {
			return nil, ErrAdmission
		}
		result[index] = observation
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, found := range seen {
		if !found {
			return nil, ErrAdmission
		}
	}
	return result, nil
}
