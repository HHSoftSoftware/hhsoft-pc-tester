package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestLaunchSelectionInputRoleNumbers(t *testing.T) {
	if retestArtifactInput != 1 || retestPublicKeyInput != 2 || retestFingerprintKeyInput != 3 || retestSigningKeyInput != 4 || retestLaunchSelectionInput != 5 {
		t.Fatal("checked input role numbers changed")
	}
}

func TestLaunchSelectionInputBoundsAndOwnership(t *testing.T) {
	for _, test := range []struct {
		name string
		size int
		pass bool
	}{
		{"minimum", 1, true},
		{"maximum", 16384, true},
		{"empty", 0, false},
		{"oversize", 16385, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRetestInputFixture(test.size)
			data, err := readRetestInputWithOperations(t.Context(), "owned-launch", retestLaunchSelectionInput, fixture.operations())
			if test.pass {
				if err != nil || !bytes.Equal(data, fixture.readable.data) {
					t.Fatal("valid bounded acquisition failed")
				}
				reads := fixture.readable.readSize
				if len(reads) < 2 || reads[len(reads)-1] != 1 {
					t.Fatal("exact EOF was not probed")
				}
				fixture.readable.data[0] ^= 0xff
				if data[0] == fixture.readable.data[0] {
					t.Fatal("returned bytes alias the provider")
				}
				assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
			} else {
				assertRetestInputFailure(t, data, err, false)
				if len(fixture.readable.readSize) != 0 {
					t.Fatal("invalid size reached the readable owner")
				}
				assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1}, []string{"metadata"})
			}
		})
	}
}

func TestLaunchSelectionInputRejectsUnstableOwners(t *testing.T) {
	for _, test := range []struct {
		name    string
		change  func(*retestInputFixture)
		cleanup bool
	}{
		{"different_identity", func(f *retestInputFixture) {
			replacement := *f.readable.info
			replacement.id++
			f.readable.info = &replacement
		}, false},
		{"changed_stamp", func(f *retestInputFixture) {
			f.readable.hook = func(operation string) {
				if operation == "read" {
					f.metadata.info.modified = f.metadata.info.modified.Add(time.Second)
				}
			}
		}, false},
		{"growth_at_eof", func(f *retestInputFixture) { f.readable.data = append(f.readable.data, 1) }, false},
		{"partial_error", func(f *retestInputFixture) { f.readable.reads = []retestInputTestRead{{7, io.ErrUnexpectedEOF}} }, false},
		{"no_progress", func(f *retestInputFixture) { f.readable.reads = []retestInputTestRead{{0, nil}} }, false},
		{"owner_with_error", func(f *retestInputFixture) { f.readableErr = errors.New("opaque provider detail") }, false},
		{"readable_close", func(f *retestInputFixture) { f.readable.closeErr = errors.New("opaque provider detail") }, true},
		{"metadata_close", func(f *retestInputFixture) { f.metadata.closeErr = errors.New("opaque provider detail") }, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRetestInputFixture(96)
			test.change(fixture)
			data, err := readRetestInputWithOperations(t.Context(), "owned-launch", retestLaunchSelectionInput, fixture.operations())
			assertRetestInputFailure(t, data, err, test.cleanup)
			assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
			if err.Error() != errRetestInput.Error() && !test.cleanup {
				t.Fatal("provider detail escaped the acquisition boundary")
			}
		})
	}
}

func TestLaunchSelectionInputCancellationDiscardsBytes(t *testing.T) {
	for _, point := range []string{"read", "readable_close", "metadata_close"} {
		t.Run(point, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fixture := newRetestInputFixture(96)
			fixture.readable.hook = func(operation string) {
				if point == "read" && operation == "read" || point == "readable_close" && operation == "close" {
					cancel()
				}
			}
			fixture.metadata.hook = func(operation string) {
				if point == "metadata_close" && operation == "close" {
					cancel()
				}
			}
			data, err := readRetestInputWithOperations(ctx, "owned-launch", retestLaunchSelectionInput, fixture.operations())
			if data != nil || !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation returned launch bytes")
			}
			assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
		})
	}
}
