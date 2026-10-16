//go:build linux

package reporttarget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverAndAuthorizeRemovableUSBPartition(t *testing.T) {
	root := t.TempDir()
	fixtureUSBPartition(t, root, "8:17", "8:16")
	options := Options{Root: root, FingerprintKey: bytes.Repeat([]byte{7}, 32)}
	catalog := discoverWith(t.Context(), options, directFixtureNode)
	if catalog.Status != StatusPass || len(catalog.Candidates) != 1 {
		t.Fatalf("unexpected catalog: %#v", catalog)
	}
	candidate := catalog.Candidates[0]
	if !candidate.Eligible || candidate.ConfirmationCode == "" || candidate.MediaIdentity == "" || strings.Contains(candidate.MediaIdentity, "FIXTURE") {
		t.Fatalf("unsafe candidate: %#v", candidate)
	}
	if _, err := authorizeWith(t.Context(), options, AuthorizationRequest{Device: candidate.Device, ConfirmationCode: "RPT-WRONG"}, directFixtureNode); err == nil {
		t.Fatal("incorrect typed confirmation passed")
	}
	authorization, err := authorizeWith(t.Context(), options, AuthorizationRequest{Device: candidate.Device, ConfirmationCode: candidate.ConfirmationCode}, directFixtureNode)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAuthorization(authorization); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverRejectsMountedPartition(t *testing.T) {
	root := t.TempDir()
	fixtureUSBPartition(t, root, "8:17", "8:16")
	mustTargetWrite(t, filepath.Join(root, "proc/self/mountinfo"), "1 0 8:17 / /foreign rw - ext4 /dev/sdb1 rw\n")
	catalog := discoverWith(t.Context(), Options{Root: root, FingerprintKey: bytes.Repeat([]byte{7}, 32)}, directFixtureNode)
	if catalog.Status != StatusInconclusive || catalog.Candidates[0].Eligible || !catalog.Candidates[0].Mounted {
		t.Fatalf("mounted candidate was eligible: %#v", catalog)
	}
}

func TestReadOnlyCandidateRetainsOnlyReadEligibility(t *testing.T) {
	root := t.TempDir()
	fixtureUSBPartition(t, root, "8:17", "8:16")
	mustTargetWrite(t, filepath.Join(root, "sys/class/block/sdb/ro"), "1\n")
	catalog := discoverWith(t.Context(), Options{Root: root, FingerprintKey: bytes.Repeat([]byte{7}, 32)}, directFixtureNode)
	candidate := catalog.Candidates[0]
	if candidate.Eligible || !candidate.EligibleReadOnly || !candidate.ReadOnly {
		t.Fatalf("read-only candidate policy = %#v", candidate)
	}
}

func TestMappedReadOnlyCandidateIsNeverEligible(t *testing.T) {
	root := t.TempDir()
	fixtureUSBPartition(t, root, "8:17", "8:16")
	mustTargetWrite(t, filepath.Join(root, "sys/class/block/sdb/ro"), "1\n")
	mustTargetWrite(t, filepath.Join(root, "sys/class/block/sdb/holders/dm-0"), "mapped\n")
	catalog := discoverWith(t.Context(), Options{Root: root, FingerprintKey: bytes.Repeat([]byte{7}, 32)}, directFixtureNode)
	candidate := catalog.Candidates[0]
	if candidate.Eligible || candidate.EligibleReadOnly || candidate.Exclusion != "report media participates in another block mapping" {
		t.Fatalf("mapped read-only candidate policy = %#v", candidate)
	}
}

func TestPersistRequiresBoundMountAndCreatesExclusiveReport(t *testing.T) {
	root := t.TempDir()
	mountPath := filepath.Join(root, strings.TrimPrefix(MountPoint, "/"))
	mustTargetMkdir(t, mountPath)
	mountMetadata, err := os.Stat(mountPath)
	if err != nil {
		t.Fatal(err)
	}
	device := fileDevice(mountMetadata, false)
	fixtureUSBPartition(t, root, device, "8:16")
	options := Options{Root: root, FingerprintKey: bytes.Repeat([]byte{7}, 32)}
	catalog := discoverWith(t.Context(), options, directFixtureNode)
	authorization, err := authorizeWith(t.Context(), options, AuthorizationRequest{Device: "/dev/sdb1", ConfirmationCode: catalog.Candidates[0].ConfirmationCode}, directFixtureNode)
	if err != nil {
		t.Fatal(err)
	}
	mustTargetWrite(t, filepath.Join(root, "proc/self/mountinfo"), "1 0 "+device+" / "+MountPoint+" rw,nosuid,nodev,noexec - ext4 /dev/sdb1 rw\n")
	sourcePath := filepath.Join(root, "source.hhpcr")
	mustTargetWrite(t, sourcePath, "signed-report-fixture")
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	expectedSHA256 := sha256.Sum256([]byte("signed-report-fixture"))
	receipt, err := persistWith(context.Background(), options, authorization, source, int64(len("signed-report-fixture")), "session.hhpcr", expectedSHA256, directFixtureNode)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Bytes != int64(len("signed-report-fixture")) || !receipt.FileSynced || !receipt.ReadBackVerified {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}
	if _, err := persistWith(context.Background(), options, authorization, source, int64(len("signed-report-fixture")), "session.hhpcr", expectedSHA256, directFixtureNode); err == nil {
		t.Fatal("existing report was overwritten")
	}
	wrongSHA256 := sha256.Sum256([]byte("different verified report"))
	if _, err := persistWith(context.Background(), options, authorization, source, int64(len("signed-report-fixture")), "changed-after-verification.hhpcr", wrongSHA256, directFixtureNode); err == nil {
		t.Fatal("bytes changed after verification passed")
	}
	mustTargetWrite(t, filepath.Join(root, "proc/uptime"), "1023.00 67.89\n")
	if _, err := persistWith(context.Background(), options, authorization, source, int64(len("signed-report-fixture")), "expired.hhpcr", expectedSHA256, directFixtureNode); err == nil {
		t.Fatal("expired authorization passed")
	}
}

func TestPersistDetectsHotplugDuringCopy(t *testing.T) {
	root := t.TempDir()
	mountPath := filepath.Join(root, strings.TrimPrefix(MountPoint, "/"))
	mustTargetMkdir(t, mountPath)
	mountMetadata, err := os.Stat(mountPath)
	if err != nil {
		t.Fatal(err)
	}
	device := fileDevice(mountMetadata, false)
	fixtureUSBPartition(t, root, device, "8:16")
	options := Options{Root: root, FingerprintKey: bytes.Repeat([]byte{7}, 32)}
	catalog := discoverWith(t.Context(), options, directFixtureNode)
	authorization, err := authorizeWith(t.Context(), options, AuthorizationRequest{Device: "/dev/sdb1", ConfirmationCode: catalog.Candidates[0].ConfirmationCode}, directFixtureNode)
	if err != nil {
		t.Fatal(err)
	}
	mustTargetWrite(t, filepath.Join(root, "proc/self/mountinfo"), "1 0 "+device+" / "+MountPoint+" rw,nosuid,nodev,noexec - ext4 /dev/sdb1 rw\n")
	sourcePath := filepath.Join(root, "source.hhpcr")
	mustTargetWrite(t, sourcePath, "signed-report-fixture")
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	inspections := 0
	hotplugDuringCopy := func(_, _, _ string) bool {
		inspections++
		return inspections < 2
	}
	expectedSHA256 := sha256.Sum256([]byte("signed-report-fixture"))
	if _, err := persistWith(context.Background(), options, authorization, source, int64(len("signed-report-fixture")), "hotplugged.hhpcr", expectedSHA256, hotplugDuringCopy); err == nil {
		t.Fatal("hotplug during persistence passed")
	}
}

func fixtureUSBPartition(t *testing.T, root, device, parentDevice string) {
	t.Helper()
	parent := filepath.Join(root, "sys/devices/pci0000/usb1/1-1/1-1:1.0/host0/target0/0:0:0:0/block/sdb")
	partition := filepath.Join(parent, "sdb1")
	for _, path := range []string{filepath.Join(parent, "holders"), filepath.Join(partition, "holders"), filepath.Join(root, "proc/self"), filepath.Join(root, "sys/class/block")} {
		mustTargetMkdir(t, path)
	}
	for path, value := range map[string]string{
		filepath.Join(root, "proc/self/mountinfo"): "", filepath.Join(root, "proc/uptime"): "123.45 67.89\n",
		filepath.Join(root, "proc/sys/kernel/random/boot_id"): "11111111-1111-4111-8111-111111111111\n", filepath.Join(root, "sys/devices/pci0000/usb1/1-1/serial"): "FIXTURE-USB-1234\n",
		filepath.Join(parent, "dev"): parentDevice + "\n", filepath.Join(parent, "size"): "131072\n", filepath.Join(parent, "ro"): "0\n", filepath.Join(parent, "removable"): "1\n",
		filepath.Join(partition, "partition"): "1\n", filepath.Join(partition, "dev"): device + "\n", filepath.Join(partition, "size"): "65536\n", filepath.Join(partition, "ro"): "0\n",
	} {
		mustTargetWrite(t, path, value)
	}
	if err := os.Symlink(parent, filepath.Join(root, "sys/class/block/sdb")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(partition, filepath.Join(root, "sys/class/block/sdb1")); err != nil {
		t.Fatal(err)
	}
}

func directFixtureNode(_, _, _ string) bool { return true }

func mustTargetMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustTargetWrite(t *testing.T, path, value string) {
	t.Helper()
	mustTargetMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}
