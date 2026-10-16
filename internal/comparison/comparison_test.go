package comparison

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestCompareClassifiesIdentityChanges(t *testing.T) {
	before := fixtureReport("f", []app.ComponentRecord{
		record(inventory.ComponentBlock, "block:nvme0n1", "a", "wwid"),
		record(inventory.ComponentCPU, "cpu:0", "", ""),
		record(inventory.ComponentNetwork, "network:eth0", "b", "address"),
		record(inventory.ComponentUSB, "usb:1-2", "c", "serial"),
		record(inventory.ComponentBlock, "block:sdb", "f", "wwid"),
	})
	after := fixtureReport("e", []app.ComponentRecord{
		record(inventory.ComponentBlock, "block:nvme0n1", "d", "wwid"),
		record(inventory.ComponentCPU, "cpu:0", "", ""),
		record(inventory.ComponentNetwork, "network:eth0", "b", "address"),
		record(inventory.ComponentUSB, "usb:1-4", "c", "serial"),
		record(inventory.ComponentBlock, "block:sdc", "e", "wwid"),
	})

	result, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Comparable || result.Complete || result.Status != StatusChanged {
		t.Fatalf("status = %+v", result)
	}
	if result.Summary.Matched != 2 || result.Summary.Moved != 1 || result.Summary.Replaced != 1 || result.Summary.Added != 1 || result.Summary.Removed != 1 || result.Summary.Unresolved != 1 {
		t.Fatalf("summary = %+v", result.Summary)
	}
	if result.Replaced[0].Before.Type != inventory.ComponentBlock || result.Replaced[0].LocationConfidence != "report-location" {
		t.Fatalf("replacement = %+v", result.Replaced[0])
	}
}

func TestCompareDifferentEpochIsInconclusive(t *testing.T) {
	before := fixtureReport("a", []app.ComponentRecord{record(inventory.ComponentBlock, "block:sda", "a", "wwid")})
	after := fixtureReport("a", []app.ComponentRecord{record(inventory.ComponentBlock, "block:sda", "a", "wwid")})
	after.Inventory.SystemFingerprint.KeyEpoch = "rotated"
	after.Inventory.Components[0].Identity.KeyEpoch = "rotated"
	after.Reference.SystemFingerprint = after.Inventory.SystemFingerprint

	result, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if result.Comparable || result.Status != StatusInconclusive || result.Summary.Unresolved != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestCompareMatchingEpochWithDifferentKeyIDsIsInconclusive(t *testing.T) {
	before := fixtureReport("a", []app.ComponentRecord{record(inventory.ComponentBlock, "block:sda", "a", "wwid")})
	after := fixtureReport("b", []app.ComponentRecord{record(inventory.ComponentBlock, "block:sda", "b", "wwid")})
	after.Inventory.SystemFingerprint.KeyID = hmacValue("e")
	after.Reference.SystemFingerprint = after.Inventory.SystemFingerprint

	result, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if result.Comparable || result.Status != StatusInconclusive || result.Summary.Replaced != 0 || result.Summary.Unresolved != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestCompareLostIdentityAtSameLocationIsUnresolved(t *testing.T) {
	before := fixtureReport("a", []app.ComponentRecord{record(inventory.ComponentBlock, "block:sda", "a", "wwid")})
	after := fixtureReport("", []app.ComponentRecord{record(inventory.ComponentBlock, "block:sda", "", "")})

	result, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusInconclusive || result.Summary.Removed != 0 || result.Summary.Unresolved != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestCompareIdentifierSourceFallbackIsUnresolved(t *testing.T) {
	before := fixtureReport("a", []app.ComponentRecord{record(inventory.ComponentBlock, "block:sda", "a", "wwid")})
	after := fixtureReport("b", []app.ComponentRecord{record(inventory.ComponentBlock, "block:sda", "b", "serial")})

	result, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusInconclusive || result.Summary.Replaced != 0 || result.Summary.Unresolved != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestCompareRejectsEmptyAndContradictoryInventory(t *testing.T) {
	empty := fixtureReport("", nil)
	if _, err := Compare(empty, empty); err == nil {
		t.Fatal("empty inventory was accepted")
	}
	before := fixtureReport("a", []app.ComponentRecord{record(inventory.ComponentBlock, "block:sda", "a", "wwid")})
	after := fixtureReport("a", []app.ComponentRecord{record(inventory.ComponentBlock, "block:sda", "b", "wwid")})
	if _, err := Compare(before, after); err == nil {
		t.Fatal("contradictory aggregate fingerprint was accepted")
	}
}

func TestCompareDuplicateIdentityIsUnresolved(t *testing.T) {
	before := fixtureReport("a", []app.ComponentRecord{
		record(inventory.ComponentUSB, "usb:1-1", "a", "serial"),
		record(inventory.ComponentUSB, "usb:1-2", "a", "serial"),
	})
	after := fixtureReport("b", []app.ComponentRecord{record(inventory.ComponentUSB, "usb:1-2", "a", "serial")})
	result, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusInconclusive || result.Summary.Matched != 0 || result.Summary.Removed != 0 || result.Summary.Unresolved == 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestCompareSourceFallbackOrIdentityLossAfterRenameIsUnresolved(t *testing.T) {
	before := fixtureReport("a", []app.ComponentRecord{recordWithAttributes(inventory.ComponentBlock, "block:sda", "a", "wwid", "sda")})
	tests := []struct {
		name  string
		after Report
	}{
		{name: "source fallback", after: fixtureReport("b", []app.ComponentRecord{recordWithAttributes(inventory.ComponentBlock, "block:sdb", "b", "serial", "sdb")})},
		{name: "identity loss", after: fixtureReport("", []app.ComponentRecord{recordWithAttributes(inventory.ComponentBlock, "block:sdb", "", "", "sdb")})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := Compare(before, test.after)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != StatusInconclusive || result.Summary.Added != 0 || result.Summary.Removed != 0 || result.Summary.Unresolved != 1 {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

func TestCompareContextualIdentityCannotHideAttributeConflict(t *testing.T) {
	before := fixtureReport("a", []app.ComponentRecord{record(inventory.ComponentBaseboard, "baseboard", "a", "asset_tag")})
	after := fixtureReport("a", []app.ComponentRecord{record(inventory.ComponentBaseboard, "baseboard", "a", "asset_tag")})
	before.Inventory.Components[0].Attributes = []inventory.Attribute{{Name: "vendor", Value: "Before"}}
	after.Inventory.Components[0].Attributes = []inventory.Attribute{{Name: "vendor", Value: "After"}}
	result, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusInconclusive || result.Summary.Matched != 0 || result.Summary.Unresolved != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestCompareDuplicateIdentityDoesNotHideChangedOccupant(t *testing.T) {
	before := fixtureReport("a", []app.ComponentRecord{
		record(inventory.ComponentUSB, "usb:1-1", "a", "serial"),
		record(inventory.ComponentUSB, "usb:1-2", "a", "serial"),
	})
	after := fixtureReport("b", []app.ComponentRecord{
		record(inventory.ComponentUSB, "usb:1-1", "a", "serial"),
		record(inventory.ComponentUSB, "usb:1-2", "b", "serial"),
	})
	result, err := Compare(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusInconclusive || result.Summary.Added != 0 || result.Summary.Removed != 0 || result.Summary.Unresolved != 2 {
		t.Fatalf("result = %+v", result)
	}
}

func TestCompareRejectsMalformedComponentMetadata(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Report)
	}{
		{name: "type", mutate: func(report *Report) { report.Inventory.Components[0].Type = inventory.ComponentType("block\x00usb") }},
		{name: "control character", mutate: func(report *Report) { report.Inventory.Components[0].ID = "block:sda\x00suffix" }},
		{name: "confidence", mutate: func(report *Report) { report.Inventory.Components[0].Identity.Confidence = "authoritative" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := fixtureReport("a", []app.ComponentRecord{record(inventory.ComponentBlock, "block:sda", "a", "wwid")})
			test.mutate(&report)
			if _, err := Compare(report, report); err == nil {
				t.Fatal("malformed signed component metadata was accepted")
			}
		})
	}
}

func TestCompareOutputIsPermutationInvariant(t *testing.T) {
	report := fixtureReport("a", []app.ComponentRecord{
		record(inventory.ComponentUSB, "usb:1-2", "b", "serial"),
		record(inventory.ComponentBlock, "block:sda", "a", "wwid"),
	})
	first, err := Compare(report, report)
	if err != nil {
		t.Fatal(err)
	}
	permuted := report
	permuted.Inventory.Components = append([]app.ComponentRecord(nil), report.Inventory.Components...)
	permuted.Inventory.Components[0], permuted.Inventory.Components[1] = permuted.Inventory.Components[1], permuted.Inventory.Components[0]
	second, err := Compare(permuted, permuted)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatal("component input ordering changed comparison output")
	}
}

func fixtureReport(systemValue string, records []app.ComponentRecord) Report {
	identified := 0
	for _, record := range records {
		if record.Identity.Value != "" {
			identified++
		}
	}
	systemFingerprintValue := ""
	if systemValue != "" {
		systemFingerprintValue = hmacValue(systemValue)
	}
	return Report{
		Reference: ReportReference{SessionID: "session", GeneratedTime: "2026-10-13T00:00:00Z", SoftwareVersion: "0.1.0-dev.0", SignerKeyID: "test", ManifestDigest: "sha256:" + strings.Repeat("1", 64), SystemFingerprint: app.SystemFingerprint{Algorithm: "hmac-sha256", KeyEpoch: "epoch", KeyID: hmacValue("d"), Value: systemFingerprintValue, IdentifiedParts: identified, UnidentifiedParts: len(records) - identified}, ComponentCount: len(records)},
		Inventory: app.InventoryPayload{SchemaVersion: inventory.SnapshotSchemaVersion, Components: records, SystemFingerprint: app.SystemFingerprint{Algorithm: "hmac-sha256", KeyEpoch: "epoch", KeyID: hmacValue("d"), Value: systemFingerprintValue, IdentifiedParts: identified, UnidentifiedParts: len(records) - identified}},
	}
}

func record(componentType inventory.ComponentType, id, identity, source string) app.ComponentRecord {
	value := ""
	confidence := "unavailable"
	if identity != "" {
		value = hmacValue(identity)
		confidence, _ = sourceConfidence(componentType, source)
	}
	return app.ComponentRecord{Type: componentType, ID: id, Identity: app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "epoch", Source: source, Value: value, Confidence: confidence}}
}

func recordWithAttributes(componentType inventory.ComponentType, id, identity, source, name string) app.ComponentRecord {
	record := record(componentType, id, identity, source)
	deviceNumber := "8:0"
	if name != "sda" {
		deviceNumber = "8:16"
	}
	record.Attributes = []inventory.Attribute{{Name: "device_number", Value: deviceNumber}, {Name: "model", Value: "SSD"}, {Name: "name", Value: name}}
	return record
}

func hmacValue(seed string) string {
	character := "0"
	if seed != "" {
		character = string(seed[0])
	}
	return "hmac-sha256:" + strings.Repeat(character, 64)
}
