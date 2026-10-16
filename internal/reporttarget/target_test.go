package reporttarget

import (
	"encoding/json"
	"testing"
)

func TestAuthorizationRejectsTampering(t *testing.T) {
	candidate := Candidate{
		Device: "/dev/sdb1", Parent: "/dev/sdb", Topology: "/sys/devices/pci/usb1/1-1/block/sdb/sdb1",
		DeviceMajorMinor: "8:17", ParentMajorMinor: "8:16", SizeBytes: 32 << 20, ParentSizeBytes: 64 << 20,
		FingerprintKeyID: "hmac-sha256:" + repeated('1', 64), MediaIdentity: "hmac-sha256:" + repeated('2', 64), MediaIdentitySource: "serial",
	}
	fingerprintKey := []byte("12345678901234567890123456789012")
	authorization, err := NewAuthorization(candidate, "11111111-1111-4111-8111-111111111111", 12, fingerprintKey)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(authorization)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeAuthorization(encoded); err != nil {
		t.Fatal(err)
	}
	authorization.Device.SizeBytes++
	encoded, _ = json.Marshal(authorization)
	tampered, err := DecodeAuthorization(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateAuthorizationMAC(tampered, fingerprintKey); err == nil {
		t.Fatal("tampered authorization passed")
	}
}

func repeated(value byte, count int) string {
	result := make([]byte, count)
	for index := range result {
		result[index] = value
	}
	return string(result)
}
