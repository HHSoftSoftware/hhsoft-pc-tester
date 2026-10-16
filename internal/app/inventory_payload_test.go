package app

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestBuildInventoryPayloadRedactsAndFingerprints(t *testing.T) {
	snapshot := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentSystem, "system", map[string]string{"vendor": "HHSoft", "product_uuid": "11111111-2222-3333-4444-555555555555"}),
		inventory.NewComponent(inventory.ComponentBlock, "block:nvme0n1", map[string]string{"model": "Fast Drive", "wwid": "eui.private"}),
		inventory.NewComponent(inventory.ComponentNetwork, "network:eth0", map[string]string{"address": "00:11:22:33:44:55", "operational_state": "up"}),
	})
	payload, err := BuildInventoryPayload(snapshot, []byte("01234567890123456789012345678901"), "test-1")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, secret := range []string{"11111111-2222-3333-4444-555555555555", "eui.private", "00:11:22:33:44:55"} {
		if strings.Contains(text, secret) {
			t.Fatalf("raw identifier was exported: %s", secret)
		}
	}
	if payload.SystemFingerprint.KeyID == "" || payload.SystemFingerprint.Value == "" || payload.SystemFingerprint.IdentifiedParts != 3 || payload.SystemFingerprint.UnidentifiedParts != 0 {
		t.Fatalf("unexpected system fingerprint: %#v", payload.SystemFingerprint)
	}
	for _, component := range payload.Components {
		if component.Identity.Value == "" || component.Identity.Source == "" {
			t.Fatalf("missing pseudonym for %s", component.ID)
		}
	}
}

func TestBuildInventoryPayloadMarksMissingIdentity(t *testing.T) {
	snapshot := inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentCPU, "cpu:0", map[string]string{"model": "Example CPU"}),
		inventory.NewComponent(inventory.ComponentSystem, "system", map[string]string{"product_uuid": "00000000-0000-0000-0000-000000000000"}),
	})
	payload, err := BuildInventoryPayload(snapshot, []byte("01234567890123456789012345678901"), "test-1")
	if err != nil {
		t.Fatal(err)
	}
	if payload.SystemFingerprint.KeyID == "" || payload.SystemFingerprint.Value != "" || payload.SystemFingerprint.IdentifiedParts != 0 || payload.SystemFingerprint.UnidentifiedParts != 2 {
		t.Fatalf("missing identities were treated as stable: %#v", payload.SystemFingerprint)
	}
	if payload.Components[0].Identity.Confidence != "unavailable" || payload.Components[1].Identity.Confidence != "unavailable" {
		t.Fatal("missing identifiers are not explicit")
	}
}

func TestBuildInventoryPayloadRequiresFullKey(t *testing.T) {
	_, err := BuildInventoryPayload(inventory.NewSnapshot(nil), []byte("short"), "test-1")
	if !errors.Is(err, ErrFingerprintKeyLength) {
		t.Fatalf("got %v, want key length error", err)
	}
}

func TestBuildReportPayloadValidatesAndSortsTestEvidence(t *testing.T) {
	second, err := NewTestResult("storage.quick", 1, TestSkip, map[string]string{"reason": "not present"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := NewTestResult("graphics.quick", 1, TestPass, map[string]string{"renderer": "hardware"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := BuildReportPayload(inventory.NewSnapshot(nil), []byte("01234567890123456789012345678901"), "test-1", []TestResult{second, first})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.TestResults) != 2 || payload.TestResults[0].ID != "graphics.quick" || payload.TestResults[1].ID != "storage.quick" {
		t.Fatalf("test results = %#v", payload.TestResults)
	}
	if _, err := BuildReportPayload(inventory.NewSnapshot(nil), []byte("01234567890123456789012345678901"), "test-1", []TestResult{first, first}); err == nil {
		t.Fatal("duplicate test result was accepted")
	}
}

func TestBindTestResultRequiresPlanAndGraphTogether(t *testing.T) {
	result, err := NewTestResult("cpu.quick", 1, TestPass, map[string]string{"status": "pass"})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := BindTestResult(result, "sha256:"+strings.Repeat("1", 64), "hmac-sha256:"+strings.Repeat("2", 64))
	if err != nil || bound.PlanID == "" || bound.HardwareGraph == "" {
		t.Fatalf("binding failed: result=%+v err=%v", bound, err)
	}
	if _, err := BindTestResult(result, "sha256:"+strings.Repeat("1", 64), ""); err == nil {
		t.Fatal("accepted a partial test-result plan binding")
	}
}

func TestNewTestResultRejectsInvalidMetadata(t *testing.T) {
	if _, err := NewTestResult("Graphics Quick", 1, TestPass, map[string]bool{"ran": true}); err == nil {
		t.Fatal("invalid test ID was accepted")
	}
	if _, err := NewTestResult("graphics.quick", 1, TestStatus("unknown"), map[string]bool{"ran": true}); err == nil {
		t.Fatal("invalid status was accepted")
	}
}
