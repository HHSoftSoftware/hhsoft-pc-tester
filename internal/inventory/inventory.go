package inventory

import (
	"cmp"
	"context"
	"sort"
	"strings"
)

const SnapshotSchemaVersion = 1

type ComponentType string

const (
	ComponentCPU       ComponentType = "cpu"
	ComponentMemory    ComponentType = "memory"
	ComponentSystem    ComponentType = "system"
	ComponentBaseboard ComponentType = "baseboard"
	ComponentBlock     ComponentType = "block"
	ComponentPCI       ComponentType = "pci"
	ComponentNetwork   ComponentType = "network"
	ComponentInput     ComponentType = "input"
	ComponentSound     ComponentType = "sound"
	ComponentVideo     ComponentType = "video"
	ComponentUSB       ComponentType = "usb"
)

type Attribute struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Component struct {
	Type       ComponentType `json:"type"`
	ID         string        `json:"id"`
	Attributes []Attribute   `json:"attributes,omitempty"`
}

type Snapshot struct {
	SchemaVersion int         `json:"schema_version"`
	Components    []Component `json:"components"`
}

type Collector interface {
	Collect(context.Context) (Snapshot, error)
}

func NewComponent(componentType ComponentType, id string, attributes map[string]string) Component {
	normalized := make(map[string]string, len(attributes))
	for name, value := range attributes {
		name = strings.TrimSpace(name)
		value = normalizeValue(value)
		if name != "" && value != "" {
			normalized[name] = value
		}
	}

	component := Component{
		Type: componentType,
		ID:   strings.TrimSpace(id),
	}
	for name, value := range normalized {
		component.Attributes = append(component.Attributes, Attribute{Name: name, Value: value})
	}
	sort.Slice(component.Attributes, func(i, j int) bool {
		return component.Attributes[i].Name < component.Attributes[j].Name
	})
	return component
}

func NewSnapshot(components []Component) Snapshot {
	canonical := make([]Component, len(components))
	for index, component := range components {
		attributes := make(map[string]string, len(component.Attributes))
		for _, attribute := range component.Attributes {
			attributes[attribute.Name] = attribute.Value
		}
		canonical[index] = NewComponent(component.Type, component.ID, attributes)
	}

	sort.Slice(canonical, func(i, j int) bool {
		left, right := canonical[i], canonical[j]
		if difference := cmp.Compare(left.Type, right.Type); difference != 0 {
			return difference < 0
		}
		if difference := cmp.Compare(left.ID, right.ID); difference != 0 {
			return difference < 0
		}
		for index := 0; index < min(len(left.Attributes), len(right.Attributes)); index++ {
			if difference := cmp.Compare(left.Attributes[index].Name, right.Attributes[index].Name); difference != 0 {
				return difference < 0
			}
			if difference := cmp.Compare(left.Attributes[index].Value, right.Attributes[index].Value); difference != 0 {
				return difference < 0
			}
		}
		return len(left.Attributes) < len(right.Attributes)
	})

	return Snapshot{SchemaVersion: SnapshotSchemaVersion, Components: canonical}
}

func (component Component) Attribute(name string) (string, bool) {
	index := sort.Search(len(component.Attributes), func(index int) bool {
		return component.Attributes[index].Name >= name
	})
	if index < len(component.Attributes) && component.Attributes[index].Name == name {
		return component.Attributes[index].Value, true
	}
	return "", false
}

func normalizeValue(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
