//go:build !linux && !windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDeploymentDiscoveryKeyOtherPlatformAcquisitionIsRejected(t *testing.T) {
	h := newDeploymentInputFixture(t)
	h.path = filepath.Join(t.TempDir(), "discovery.key")
	h.args = []string{"-discovery-key", h.path, "-audit-output", h.auditPath}
	if err := os.WriteFile(h.path, h.key, 0600); err != nil {
		t.Fatal(err)
	}
	h.acquire = readRetestInput
	h.rejected(h.run(), "read media discovery key: input acquisition failed", 1)
	data, err := os.ReadFile(h.path)
	if err != nil || !bytes.Equal(data, h.key) {
		t.Fatal("unsupported-platform input fixture changed")
	}
}
