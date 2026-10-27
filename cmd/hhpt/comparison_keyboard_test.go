package main

import (
	"bytes"
	"testing"
)

func TestComparisonAcceptsOnlyBoundKeyboardV5Claims(t *testing.T) {
	encoded := comparisonThermalFixture(t, "session-v5-keyboard-pass")
	payload, err := decodeComparisonInventory(encoded)
	if err != nil || len(payload.TestResults) != 22 {
		t.Fatalf("valid keyboard comparison rejected: %v", err)
	}
	for _, change := range []struct{ old, replacement string }{
		{`"hhsoft.pc-tester.quick-session/v5"`, `"hhsoft.pc-tester.quick-session/v4"`},
		{`"id":"input.keyboard.quick","version":2`, `"id":"input.keyboard.quick","version":1`},
		{`"duration_ms":6`, `"duration_ms":7`},
		{`"baseline_neutral":true`, `"baseline_neutral":false`},
		{`"fixed_keys":`, `"fixed_key\u0053":`},
		{`"fixed_keys":`, `"fixed_key\u0073":[],"fixed_keys":`},
		{`"cleanup_confirmed":true,"devices":`, `"cleanup_confirmed":false,"cleanup_confirmed":true,"devices":`},
	} {
		if !bytes.Contains(encoded, []byte(change.old)) {
			t.Fatal("comparison mutation did not target keyboard fixture")
		}
		if _, err := decodeComparisonInventory(bytes.Replace(encoded, []byte(change.old), []byte(change.replacement), 1)); err == nil {
			t.Fatal("comparison accepted unbound keyboard evidence")
		}
	}
}
