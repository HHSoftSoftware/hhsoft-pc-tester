package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/graphics"
)

func TestGraphicsEvidenceKeepsRuntimeOnlyFieldsOutOfItsSchema(t *testing.T) {
	for _, fixture := range []struct {
		schema   string
		evidence any
	}{
		{graphics.Schema, completeGraphicsEvidence()},
		{graphics.VisualSchema, completeDisplayEvidence()},
	} {
		encoded, err := json.Marshal(fixture.evidence)
		if err != nil {
			t.Fatal(err)
		}
		if status, err := testEvidenceStatus(encoded, fixture.schema); err != nil || status != "pass" {
			t.Fatalf("valid worker schema required a runtime-only field: %s %v", status, err)
		}
		for _, key := range []string{"-", "cleanup_uncertain", "CleanupUncertain"} {
			malformed := append([]byte(nil), encoded[:len(encoded)-1]...)
			name, err := json.Marshal(key)
			if err != nil {
				t.Fatal(err)
			}
			malformed = append(malformed, ',')
			malformed = append(malformed, name...)
			malformed = append(malformed, []byte(`:false}`)...)
			if _, err := testEvidenceStatus(malformed, fixture.schema); err == nil {
				t.Fatalf("worker JSON injected runtime-only field %s", key)
			}
		}
	}
	flagged := completeGraphicsEvidence()
	flagged.CleanupUncertain = true
	encoded, err := json.Marshal(flagged)
	if err != nil || bytes.Contains(encoded, []byte("Cleanup")) || bytes.Contains(encoded, []byte("cleanup")) {
		t.Fatal("runtime cleanup state entered the worker wire schema")
	}
}
