package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/comparison"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestCompareVerifiesAndClassifiesSignedReports(t *testing.T) {
	directory := t.TempDir()
	fingerprintKey := bytes.Repeat([]byte{0x53}, 32)
	beforePrivateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x74}, ed25519.SeedSize))
	afterPrivateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x75}, ed25519.SeedSize))
	beforePublicKeyPath := writeTestKey(t, directory, "before.pub", beforePrivateKey.Public().(ed25519.PublicKey))
	afterPublicKeyPath := writeTestKey(t, directory, "after.pub", afterPrivateKey.Public().(ed25519.PublicKey))
	beforeSnapshot := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentBlock, "block:nvme0n1", map[string]string{"wwid": "eui.old", "model": "SSD"}),
		inventory.NewComponent(inventory.ComponentBlock, "block:sdb", map[string]string{"wwid": "naa.removed", "model": "SSD"}),
		inventory.NewComponent(inventory.ComponentNetwork, "network:eth0", map[string]string{"address": "00:11:22:33:44:55"}),
	})
	afterSnapshot := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentBlock, "block:nvme0n1", map[string]string{"wwid": "eui.new", "model": "SSD"}),
		inventory.NewComponent(inventory.ComponentBlock, "block:sdc", map[string]string{"wwid": "naa.added", "model": "SSD"}),
		inventory.NewComponent(inventory.ComponentNetwork, "network:eth0", map[string]string{"address": "00:11:22:33:44:55"}),
	})
	writeSignedComparisonFixture(t, filepath.Join(directory, "before.hhpcr"), beforeSnapshot, fingerprintKey, beforePrivateKey, "before-key", "before")
	writeSignedComparisonFixture(t, filepath.Join(directory, "after.hhpcr"), afterSnapshot, fingerprintKey, afterPrivateKey, "after-key", "after")

	var stdout, stderr bytes.Buffer
	arguments := []string{"-public-key", beforePublicKeyPath, "-key-id", "before-key", "-after-public-key", afterPublicKeyPath, "-after-key-id", "after-key", filepath.Join(directory, "before.hhpcr"), filepath.Join(directory, "after.hhpcr")}
	if code := runCompare(arguments, &stdout, &stderr); code != 0 {
		t.Fatalf("compare returned %d: %s", code, stderr.String())
	}
	var result comparison.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != comparison.StatusChanged || !result.Comparable || !result.Complete || result.Summary.Matched != 1 || result.Summary.Replaced != 1 || result.Summary.Added != 1 || result.Summary.Removed != 1 {
		t.Fatalf("comparison = %+v", result)
	}
}

func TestCompareRejectsUntrustedAfterReportBeforeOutput(t *testing.T) {
	directory := t.TempDir()
	fingerprintKey := bytes.Repeat([]byte{0x17}, 32)
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x29}, ed25519.SeedSize))
	publicKeyPath := writeTestKey(t, directory, "signing.pub", privateKey.Public().(ed25519.PublicKey))
	wrongPublicKeyPath := writeTestKey(t, directory, "wrong.pub", ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x31}, ed25519.SeedSize)).Public().(ed25519.PublicKey))
	reportPath := filepath.Join(directory, "report.hhpcr")
	outputPath := filepath.Join(directory, "comparison.json")
	snapshot := inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentBlock, "block:sda", map[string]string{"serial": "drive"})})
	writeSignedComparisonFixture(t, reportPath, snapshot, fingerprintKey, privateKey, "test", "session")

	arguments := []string{"-public-key", publicKeyPath, "-key-id", "test", "-after-public-key", wrongPublicKeyPath, "-after-key-id", "test", "-output", outputPath, reportPath, reportPath}
	if code := runCompare(arguments, bytes.NewBuffer(nil), bytes.NewBuffer(nil)); code != 1 {
		t.Fatalf("compare returned %d", code)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("comparison exists after verification failure: %v", err)
	}
}

func TestDecodeComparisonInventoryRejectsCaseAlias(t *testing.T) {
	encoded := []byte(`{"schema_version":1,"Components":[],"system_fingerprint":{}}`)
	if _, err := decodeComparisonInventory(encoded); err == nil {
		t.Fatal("case-variant inventory field was accepted")
	}
}

func writeSignedComparisonFixture(t *testing.T, path string, snapshot inventory.Snapshot, fingerprintKey []byte, privateKey ed25519.PrivateKey, keyID, sessionID string) {
	t.Helper()
	encoded, err := captureBundle(snapshot, nil, fingerprintKey, "epoch-1", privateKey, keyID, sessionID, time.Date(2026, time.October, 13, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}
