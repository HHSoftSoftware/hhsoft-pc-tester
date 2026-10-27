package quickevidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const hotCPU = `{"schema":"hhsoft.pc-tester.cpu/v2","status":"aborted","workers":1,"iterations":0,"state_aggregate":0,"known_answer_sha256":"","duration_ms":10,"safety":{"global_timeout_ms":1000,"thermal_limit_millicelsius":90000,"thermal_sensors":1,"thermal_samples":1,"peak_millicelsius":95000,"monitor_status":"failed","monitor_failure":"thermal_limit","cleanup_confirmed":true,"thermal_observations":1,"thermal_coverage":"available","thermal_rejected_readings":0,"thermal_unavailable_readings":0,"thermal_enumeration_errors":0}}`

func TestTypedDigestBindsFullTypedEvidenceAcrossSerialization(t *testing.T) {
	var indented bytes.Buffer
	if err := json.Indent(&indented, []byte(hotCPU), "", "  "); err != nil {
		t.Fatal(err)
	}
	original := append(indented.Bytes(), '\n')
	compact, err := DecodeLocal("cpu.quick", []byte(hotCPU))
	if err != nil {
		t.Fatal(err)
	}
	spaced, err := DecodeLocal("cpu.quick", original)
	if err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256([]byte("hhsoft.pc-tester.local-thermal-evidence/v1\x00hhsoft.pc-tester.cpu/v2\x00" + hotCPU))
	if compact.TypedSHA256 != "sha256:"+hex.EncodeToString(expected[:]) || compact != spaced {
		t.Fatal("typed digest depends on source formatting or uses the wrong domain")
	}
	if sha256.Sum256(original) == sha256.Sum256([]byte(hotCPU)) {
		t.Fatal("fixture did not distinguish file bytes")
	}
	changed, err := DecodeLocal("cpu.quick", []byte(strings.Replace(hotCPU, `"duration_ms":10`, `"duration_ms":11`, 1)))
	if err != nil || changed.TypedSHA256 == compact.TypedSHA256 {
		t.Fatal("digest omitted nonthermal worker evidence")
	}
}

func TestLocalDecoderRejectsMalformedAndUnconfirmedEvidence(t *testing.T) {
	for name, raw := range map[string]string{
		"missing cleanup": strings.Replace(hotCPU, `"cleanup_confirmed":true,`, "", 1),
		"duplicate":       strings.Replace(hotCPU, `"cleanup_confirmed":true`, `"cleanup_confirmed":false,"cleanup_confirmed":true`, 1),
		"alias":           strings.Replace(hotCPU, "cleanup_confirmed", "Cleanup_confirmed", 1),
		"old schema":      strings.Replace(hotCPU, "cpu/v2", "cpu/v1", 1),
		"heat erased":     strings.Replace(hotCPU, `"thermal_samples":1`, `"thermal_samples":0`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeLocal("cpu.quick", []byte(raw)); err == nil {
				t.Fatal("invalid local evidence accepted")
			}
		})
	}
	if _, err := DecodeLocal("cpu.quick", []byte(strings.Replace(hotCPU, `"cleanup_confirmed":true`, `"cleanup_confirmed":false`, 1))); !errors.Is(err, ErrCleanupUnconfirmed) {
		t.Fatal("cleanup failure lost its terminal classification")
	}
}
