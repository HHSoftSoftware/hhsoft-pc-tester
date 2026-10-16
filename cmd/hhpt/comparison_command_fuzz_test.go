package main

import (
	"encoding/json"
	"fmt"
	"testing"
)

func FuzzDecodeComparisonInventoryHostile(f *testing.F) {
	valid := []byte(`{"schema_version":1,"components":[],"system_fingerprint":{"algorithm":"hmac-sha256","key_epoch":"epoch-1","key_id":"key-1","identified_parts":0,"unidentified_parts":0}}`)
	if _, err := decodeComparisonInventory(valid); err != nil {
		f.Fatalf("valid seed did not decode: %v", err)
	}
	f.Add(valid)
	f.Add([]byte{})
	f.Add([]byte(`{"schema_version":1,"Components":[],"system_fingerprint":{}}`))
	f.Add([]byte(`{"schema_version":1,"components":[],"components":[],"system_fingerprint":{}}`))
	f.Add([]byte(`{"schema_version":1,"components":[],"system_fingerprint":{},"invalid_alias":{}}`))
	f.Fuzz(func(t *testing.T, encoded []byte) {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("decodeComparisonInventory panicked for %d hostile bytes: %v", len(encoded), recovered)
			}
		}()
		_, _ = decodeComparisonInventory(encoded)
	})
}

func FuzzComparisonInventoryExactSchemaFailClosed(f *testing.F) {
	f.Add(byte(0), "")
	f.Add(byte(1), "")
	f.Add(byte(2), "")
	f.Add(byte(3), "")
	f.Add(byte(4), "")
	f.Add(byte(5), "")
	f.Add(byte(6), "unknown")
	caseAliases := []string{"Components", "Type", "Name", "Value", "Key_ID", "Status"}
	f.Fuzz(func(t *testing.T, selector byte, fieldName string) {
		location := selector % 6
		fieldName = "fuzz_" + fieldName
		if selector%12 < 6 {
			fieldName = caseAliases[location]
		}
		encoded := comparisonInventoryWithExtraField(location, fieldName)
		if _, err := decodeComparisonInventory(encoded); err == nil {
			t.Fatalf("exact-schema parser accepted added object field %q at location %d", fieldName, location)
		}
	})
}

func comparisonInventoryWithExtraField(location byte, fieldName string) []byte {
	fieldJSON, _ := json.Marshal(fieldName)
	extra := fmt.Sprintf(",%s:null", fieldJSON)
	rootExtra, componentExtra, attributeExtra, identityExtra, fingerprintExtra, resultExtra := "", "", "", "", "", ""
	switch location {
	case 0:
		rootExtra = extra
	case 1:
		componentExtra = extra
	case 2:
		attributeExtra = extra
	case 3:
		identityExtra = extra
	case 4:
		fingerprintExtra = extra
	default:
		resultExtra = extra
	}
	return []byte(fmt.Sprintf(`{"schema_version":1,"components":[{"type":"block","id":"block:sda","attributes":[{"name":"model","value":"disk"%s}],"identity":{"algorithm":"hmac-sha256","key_epoch":"epoch-1","source":"serial","value":"part-1","confidence":"durable"%s}%s}],"system_fingerprint":{"algorithm":"hmac-sha256","key_epoch":"epoch-1","key_id":"key-1","value":"system-1","identified_parts":1,"unidentified_parts":0%s},"test_results":[{"id":"cpu.quick","version":1,"status":"pass","evidence":{}%s}]%s}`,
		attributeExtra, identityExtra, componentExtra, fingerprintExtra, resultExtra, rootExtra))
}
