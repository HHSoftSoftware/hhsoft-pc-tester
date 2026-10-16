package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestFixtureExclusionRequiresEveryExactBoundComponent(t *testing.T) {
	snapshot := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentSystem, "system", nil),
		inventory.NewComponent(inventory.ComponentBlock, "block:sda", map[string]string{"kind": "device"}),
		inventory.NewComponent(inventory.ComponentBlock, "block:sda1", map[string]string{"kind": "partition"}),
		inventory.NewComponent(inventory.ComponentUSB, "usb:1-1", nil),
	})
	filtered, err := excludeFixtureComponents(snapshot, []string{"block:sda", "block:sda1", "usb:1-1"})
	if err != nil || len(filtered.Components) != 1 || filtered.Components[0].ID != "system" {
		t.Fatalf("filtered snapshot = %#v, error %v", filtered, err)
	}
	if _, err := excludeFixtureComponents(snapshot, []string{"block:sdb"}); err == nil {
		t.Fatal("absent fixture component was accepted")
	}
}

func TestLoadFixtureComponentsRejectsAmbiguousContent(t *testing.T) {
	directory := t.TempDir()
	validPath := filepath.Join(directory, "fixture-components")
	if err := os.WriteFile(validPath, []byte("block:sda\nusb:1-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if components, err := loadFixtureComponents(validPath); err != nil || len(components) != 2 {
		t.Fatalf("components = %#v, error %v", components, err)
	}
	for index, value := range []string{"usb:1-1\nusb:1-1\n", "system\n", "block:sda1 extra\n"} {
		path := filepath.Join(directory, string(rune('a'+index)))
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadFixtureComponents(path); err == nil {
			t.Fatalf("invalid fixture content %q was accepted", value)
		}
	}
}
