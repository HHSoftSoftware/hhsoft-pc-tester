package deploymentkey

import (
	"bytes"
	"errors"
	"testing"
)

func TestRecordRoundTripAndKeyID(t *testing.T) {
	record, err := New("shop-2026.1", bytes.NewReader(bytes.Repeat([]byte{0x4c}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != record {
		t.Fatalf("decoded record = %#v", decoded)
	}
	firstID, err := KeyID(record)
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := KeyID(decoded)
	if err != nil || firstID != secondID || len(firstID) != 76 {
		t.Fatalf("key IDs = %q and %q, error %v", firstID, secondID, err)
	}
}

func TestDecodeRejectsEveryCorruptedRegion(t *testing.T) {
	record, err := New("shop-2026", bytes.NewReader(bytes.Repeat([]byte{0x5d}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{0, 8, 12, len(encoded) - 33, len(encoded) - 1} {
		corrupt := append([]byte(nil), encoded...)
		corrupt[index] ^= 0x01
		if _, err := Decode(corrupt); !errors.Is(err, ErrInvalid) {
			t.Fatalf("byte %d corruption got %v", index, err)
		}
	}
	for _, corrupt := range [][]byte{encoded[:len(encoded)-1], append(encoded, 0), []byte("not a key")} {
		if _, err := Decode(corrupt); !errors.Is(err, ErrInvalid) {
			t.Fatalf("malformed key got %v", err)
		}
	}
}

func TestEpochValidation(t *testing.T) {
	for _, valid := range []string{"A", "shop-1", "Store_West.2026"} {
		if !ValidEpoch(valid) {
			t.Fatalf("valid epoch %q rejected", valid)
		}
	}
	for _, invalid := range []string{"", "-shop", "1shop", "ephemeral-session", "has space", "shop/1", string(bytes.Repeat([]byte{'a'}, 65))} {
		if ValidEpoch(invalid) {
			t.Fatalf("invalid epoch %q accepted", invalid)
		}
	}
}
