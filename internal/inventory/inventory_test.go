package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestSnapshotIsCanonical(t *testing.T) {
	input := []Component{
		{Type: ComponentUSB, ID: " usb:2 ", Attributes: []Attribute{{Name: "product", Value: " Example   Device "}, {Name: "vendor", Value: "abcd"}}},
		{Type: ComponentCPU, ID: "cpu:0", Attributes: []Attribute{{Name: "z", Value: "last"}, {Name: "a", Value: "first"}}},
	}

	first := NewSnapshot(input)
	second := NewSnapshot([]Component{input[1], input[0]})
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstJSON, secondJSON) {
		t.Fatalf("snapshots differ:\n%s\n%s", firstJSON, secondJSON)
	}
	if first.SchemaVersion != SnapshotSchemaVersion {
		t.Fatalf("schema version = %d", first.SchemaVersion)
	}
	if first.Components[0].Type != ComponentCPU || first.Components[1].ID != "usb:2" {
		t.Fatalf("components not sorted: %#v", first.Components)
	}
	if product, ok := first.Components[1].Attribute("product"); !ok || product != "Example Device" {
		t.Fatalf("product = %q, %v", product, ok)
	}
}

func TestLinuxCollectorMissingPathsAreNormal(t *testing.T) {
	snapshot, err := NewLinuxCollector(t.TempDir()).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Components) != 0 {
		t.Fatalf("components = %#v", snapshot.Components)
	}
}

func TestLinuxCollectorHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewLinuxCollector(t.TempDir()).Collect(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestLinuxCollectorRequiresSource(t *testing.T) {
	_, err := (&LinuxCollector{}).Collect(context.Background())
	if !errors.Is(err, ErrNoPathSource) {
		t.Fatalf("error = %v", err)
	}
}
