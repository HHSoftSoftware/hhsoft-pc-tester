//go:build linux

package main

import (
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

func TestDeploymentKeyMountRequiresExactReadOnlyPolicy(t *testing.T) {
	candidate := reporttarget.Candidate{Device: "/dev/sdb1", DeviceMajorMinor: "8:17"}
	valid := "31 24 8:17 / /run/hhpt/deployment-key-media ro,nosuid,nodev,noexec - ext4 /dev/sdb1 ro,noload\n"
	if !deploymentKeyMountMatches(strings.NewReader(valid), "ext4", candidate) {
		t.Fatal("valid read-only deployment-key mount was rejected")
	}
	for _, invalid := range []string{
		strings.Replace(valid, "ro,nosuid", "rw,nosuid", 1),
		strings.Replace(valid, "nodev,", "", 1),
		strings.Replace(valid, "8:17", "8:18", 1),
		strings.Replace(valid, "/dev/sdb1", "/dev/sdc1", 1),
		strings.Replace(valid, " - ext4 ", " - vfat ", 1),
	} {
		if deploymentKeyMountMatches(strings.NewReader(invalid), "ext4", candidate) {
			t.Fatalf("invalid mount policy accepted: %q", invalid)
		}
	}
}
