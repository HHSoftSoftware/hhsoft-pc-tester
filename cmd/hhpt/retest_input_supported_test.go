//go:build linux || windows

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRetestInputRealRegularFiles(t *testing.T) {
	for _, test := range []struct {
		name string
		kind retestInputKind
		size int
		pass bool
	}{
		{"artifact_minimum", retestArtifactInput, 22, true},
		{"artifact_multichunk", retestArtifactInput, (64 << 10) + 17, true},
		{"public_key", retestPublicKeyInput, 32, true},
		{"fingerprint_key", retestFingerprintKeyInput, 32, true},
		{"signing_key", retestSigningKeyInput, 64, true},
		{"short_fingerprint", retestFingerprintKeyInput, 31, false},
		{"long_fingerprint", retestFingerprintKeyInput, 33, false},
		{"short_signing", retestSigningKeyInput, 63, false},
		{"long_signing", retestSigningKeyInput, 65, false},
		{"short_artifact", retestArtifactInput, 21, false},
		{"empty", retestArtifactInput, 0, false},
		{"short_key", retestPublicKeyInput, 31, false},
		{"long_key", retestPublicKeyInput, 33, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "input")
			want := bytes.Repeat([]byte{0xa6}, test.size)
			if err := os.WriteFile(path, want, 0600); err != nil {
				t.Fatal(err)
			}
			data, err := readRetestInput(context.Background(), path, test.kind)
			if test.pass {
				if err != nil || !bytes.Equal(data, want) {
					t.Fatalf("length=%d error=%v", len(data), err)
				}
			} else {
				assertRetestInputFailure(t, data, err, false)
			}
			// Removal also exposes an accidentally retained Windows owner.
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		})
	}
	data, err := readRetestInput(context.Background(), filepath.Join(t.TempDir(), "absent"), retestArtifactInput)
	if data != nil || !errors.Is(err, errRetestInput) {
		t.Fatalf("missing input: %v", err)
	}
}

type retestInputTrackedMetadata struct {
	retestInputMetadata
	closed *int
}

func (file *retestInputTrackedMetadata) Close() error {
	(*file.closed)++
	return file.retestInputMetadata.Close()
}

func TestRetestInputRealDirectoryRejectedBeforeReadable(t *testing.T) {
	ops := realRetestInputOperations()
	open := ops.openMetadata
	acquired, closed, readable := 0, 0, 0
	ops.openMetadata = func(path string) (retestInputMetadata, error) {
		owner, err := open(path)
		if owner != nil {
			acquired++
			return &retestInputTrackedMetadata{owner, &closed}, err
		}
		return nil, err
	}
	ops.openReadable = func(string, retestInputMetadata) (retestInputReadable, error) {
		readable++
		return nil, errors.New("unexpected readable acquisition")
	}
	data, err := readRetestInputWithOperations(context.Background(), t.TempDir(), retestArtifactInput, ops)
	assertRetestInputFailure(t, data, err, false)
	if acquired != 1 || closed != 1 || readable != 0 {
		t.Fatalf("acquired=%d closed=%d readable=%d", acquired, closed, readable)
	}
}
