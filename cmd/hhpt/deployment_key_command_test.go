package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/deploymentkey"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

func TestDeploymentKeyInitAndInspect(t *testing.T) {
	path := filepath.Join(t.TempDir(), deploymentkey.Filename)
	var stdout, stderr bytes.Buffer
	if result := runDeploymentKeyInit([]string{"-epoch", "shop-2026", "-output", path}, &stdout, &stderr, bytes.NewReader(bytes.Repeat([]byte{0x71}, 32))); result != 0 {
		t.Fatalf("init returned %d: %s", result, stderr.String())
	}
	metadata, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && metadata.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %o", metadata.Mode().Perm())
	}
	record, err := readDeploymentKey(path)
	if err != nil || record.Epoch != "shop-2026" || !bytes.Equal(record.Key[:], bytes.Repeat([]byte{0x71}, 32)) {
		t.Fatalf("record = %#v, error %v", record, err)
	}
	stdout.Reset()
	stderr.Reset()
	if result := runDeploymentKeyInspect([]string{path}, &stdout, &stderr); result != 0 {
		t.Fatalf("inspect returned %d: %s", result, stderr.String())
	}
	if !strings.Contains(stdout.String(), "epoch=shop-2026 key_id=hmac-sha256:") || strings.Contains(stdout.String(), "71717171") {
		t.Fatalf("unsafe inspect output %q", stdout.String())
	}
	if result := runDeploymentKeyInit([]string{"-epoch", "shop-2027", "-output", path}, &stdout, &stderr, bytes.NewReader(make([]byte, 32))); result == 0 {
		t.Fatal("existing deployment key was overwritten")
	}
}

func TestEligibleDeploymentKeyMedia(t *testing.T) {
	base := reporttarget.Candidate{Device: "/dev/sdb1", NodeReady: true, MediaIdentity: "hmac-sha256:media", EligibleReadOnly: true}
	readWrite := base
	readWrite.Eligible = true
	readOnly := base
	readOnly.ReadOnly = true
	readOnly.Exclusion = "report media is read-only"
	mounted := readOnly
	mounted.Mounted = true
	wrong := base
	wrong.EligibleReadOnly = false
	wrong.Exclusion = "a partition on the report-media parent is mounted"
	if got := eligibleDeploymentKeyMedia([]reporttarget.Candidate{readWrite, readOnly, mounted, wrong}); len(got) != 2 || got[0].Device != readWrite.Device || got[1].Exclusion != readOnly.Exclusion {
		t.Fatalf("eligible key media = %#v", got)
	}
}

func TestDeploymentFixtureComponentIDs(t *testing.T) {
	candidate := reporttarget.Candidate{Parent: "/dev/sda", Device: "/dev/sda1", Topology: "/sys/devices/pci0000:00/0000:00:0d.0/usb1/1-2/1-2:1.0/host0/target0:0:0/0:0:0:0/block/sda/sda1"}
	components, err := deploymentFixtureComponentIDs(candidate)
	if err != nil || len(components) != 3 || components[0] != "block:sda" || components[1] != "block:sda1" || components[2] != "usb:1-2" {
		t.Fatalf("fixture components = %#v, error %v", components, err)
	}
	candidate.Topology = "/sys/devices/pci0000:00/block/sda/sda1"
	if _, err := deploymentFixtureComponentIDs(candidate); err == nil {
		t.Fatal("non-USB fixture topology was accepted")
	}
}

func TestUISelectsDeploymentKeyOnlyWhenEnabled(t *testing.T) {
	wait := func(context.Context, time.Duration, io.Reader) (bool, error) { return true, nil }
	var output bytes.Buffer
	if result := runUIWith(context.Background(), []string{"-deployment-key-media"}, strings.NewReader("k\n"), &output, &output, wait); result != deploymentKeySelectionExitCode {
		t.Fatalf("deployment selection returned %d: %s", result, output.String())
	}
	output.Reset()
	if result := runUIWith(context.Background(), []string{"-legacy", "-deployment-key-media"}, strings.NewReader("k\n1\n"), &output, &output, wait); result != 0 || strings.Contains(output.String(), "Stable deployment-key media selected") {
		t.Fatalf("legacy deployment selection was exposed: %d %s", result, output.String())
	}
}
