package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestComparisonDecoderAcceptsCoverageAndRejectsUnknownValues(t *testing.T) {
	payload, err := app.BuildInventoryPayload(inventory.NewSnapshot(nil), make([]byte, 32), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	payload.CoverageScope = app.CoverageDirectCapture
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeComparisonInventory(encoded); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`"complete"`, `"Direct_Capture"`, `null`, `""`, `{}`} {
		malformed := bytes.Replace(encoded, []byte(`"direct_capture"`), []byte(value), 1)
		if _, err := decodeComparisonInventory(malformed); err == nil {
			t.Fatalf("comparison accepted coverage scope %s", value)
		}
	}
	for _, value := range []string{`"complete"`, `null`, `""`} {
		malformed := bytes.Replace(encoded, []byte(`"coverage_scope"`), []byte(`"test_results":[{"id":"cpu.quick","version":1,"status":"pass","evidence":{},"planned_status":`+value+`}],"coverage_scope"`), 1)
		if _, err := decodeComparisonInventory(malformed); err == nil {
			t.Fatalf("comparison accepted planned status %s", value)
		}
	}
}
