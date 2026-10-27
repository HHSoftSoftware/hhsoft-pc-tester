package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
)

func comparisonThermalFixture(t *testing.T, name string) []byte {
	t.Helper()
	encoded, err := os.ReadFile("../../internal/app/testdata/" + name + ".json")
	if err != nil {
		t.Fatal("synthetic comparison fixture unavailable")
	}
	var payload app.InventoryPayload
	if json.Unmarshal(encoded, &payload) != nil {
		t.Fatal("invalid synthetic comparison fixture")
	}
	encoded, err = json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestComparisonDecodesOnlyValidatedNewThermalClaims(t *testing.T) {
	for _, name := range []string{"session-v4-unavailable", "session-v4-global-heat", "session-v4-local-heat", "historical-session-v3"} {
		payload, err := decodeComparisonInventory(comparisonThermalFixture(t, name))
		if err != nil || len(payload.TestResults) != 22 {
			t.Fatalf("valid comparison fixture rejected: %v", err)
		}
	}
	encoded := comparisonThermalFixture(t, "session-v4-local-heat")
	for _, fixture := range []struct{ old, replacement string }{
		{`"workers":1`, `"workers":2`},
		{`"test_results":`, `"test_results":[],"test_results":`},
		{`"test_results":`, `"test_re\u0073ults":[],"test_results":`},
		{`"evidence":`, `"evidence":{},"evidence":`},
		{`"thermal":`, `"Thermal":`},
		{`"thermal":`, `"thermal":null,"thermal":`},
		{`"local_thermal_sources":`, `"local_thermal_sourceſ":`},
		{`"evidence_typed_sha256":"sha256:d0ade46bdda5d5cde8647afd1a1b93c01d51346d10ffcc28cdf71c91688e46d9"`, `"evidence_typed_sha256":"sha256:` + strings.Repeat("0", 64) + `"`},
	} {
		if !bytes.Contains(encoded, []byte(fixture.old)) {
			t.Fatal("comparison mutation did not target fixture")
		}
		malformed := bytes.Replace(encoded, []byte(fixture.old), []byte(fixture.replacement), 1)
		if _, err := decodeComparisonInventory(malformed); err == nil {
			t.Fatal("comparison accepted malformed thermal claim")
		}
	}
}
