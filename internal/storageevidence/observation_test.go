package storageevidence

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
)

func TestDecodeObservationExactContentFreeShape(t *testing.T) {
	target := storagetest.Target{ID: "block:a", Number: "8:0", CapacityBytes: 512, LogicalBlockBytes: 512, Exclusions: []string{}, Ranges: []storagetest.Range{{Offset: 0, Length: 512}}}
	const literal = `{"interface":"a","identity":{"major":8,"minor":0,"capacity_bytes":512,"logical_block_bytes":512},"status":"pass","error":"none","ranges":[{"offset":0,"length":512,"read_count":2,"first_bytes":512,"second_bytes":512,"outcome":"matched"}],"read_bytes":1024,"compared_bytes":512,"matched_ranges":1}`
	value, summary, err := DecodeObservation(target, []byte(literal))
	if err != nil || value.Status != "pass" || summary.ReadBytes != 1024 || summary.ComparedBytes != 512 || summary.MatchedRanges != 1 {
		t.Fatalf("literal sample: %+v %+v %v", value, summary, err)
	}
	pretty, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := DecodeObservation(target, append(pretty, '\n')); err != nil {
		t.Fatal("insignificant whitespace rejected")
	}
	for _, item := range []struct {
		name string
		raw  []byte
	}{
		{"unknown content", []byte(strings.Replace(literal, `"status":"pass"`, `"status":"pass","media_content":"SENTINEL"`, 1))},
		{"content hash", []byte(strings.Replace(literal, `"status":"pass"`, `"status":"pass","content_sha256":"SENTINEL"`, 1))},
		{"duplicate", []byte(strings.Replace(literal, `"status":"pass"`, `"status":"pass","status":"pass"`, 1))},
		{"escaped duplicate", []byte(strings.Replace(literal, `"status":"pass"`, `"status":"pass","st\u0061tus":"pass"`, 1))},
		{"case alias", []byte(strings.Replace(literal, `"status"`, `"Status"`, 1))},
		{"nested duplicate", []byte(strings.Replace(literal, `"major":8`, `"major":8,"major":8`, 1))},
		{"missing required field", []byte(strings.Replace(literal, `"error":"none",`, ``, 1))},
		{"null array", []byte(strings.Replace(literal, `[{"offset":0,"length":512,"read_count":2,"first_bytes":512,"second_bytes":512,"outcome":"matched"}]`, `null`, 1))},
		{"number alias", []byte(strings.Replace(literal, `"read_count":2`, `"read_count":2e0`, 1))},
		{"wrong scalar type", []byte(strings.Replace(literal, `"first_bytes":512`, `"first_bytes":true`, 1))},
		{"negative count", []byte(strings.Replace(literal, `"first_bytes":512`, `"first_bytes":-1`, 1))},
		{"trailing record", []byte(literal + "{}")},
		{"invalid UTF8", append([]byte(literal), 0xff)},
		{"byte cap", append([]byte(literal), bytes.Repeat([]byte{' '}, 8193)...)},
	} {
		t.Run(item.name, func(t *testing.T) {
			got, summary, err := DecodeObservation(target, item.raw)
			if err != storagetest.ErrObservation || got.Interface != "" || summary.ReadBytes != 0 {
				t.Fatalf("forged payload accepted: %+v %+v %v", got, summary, err)
			}
		})
	}
}
