//go:build linux

package reporttarget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
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

func TestReportMediaCannotInheritControllerIdentity(t *testing.T) {
	root := t.TempDir()
	fixtureUSBPartition(t, root, "8:17", "8:16")
	mustTargetWrite(t, filepath.Join(root, "sys/devices/pci0000/usb1/1-1/serial"), "\n")
	mustTargetWrite(t, filepath.Join(root, "sys/devices/pci0000/usb1/serial"), "0000:00:14.0\n")
	mustTargetWrite(t, filepath.Join(root, "sys/devices/pci0000/serial"), "FIXTURE-CONTROLLER\n")
	options := Options{Root: root, FingerprintKey: bytes.Repeat([]byte{7}, 32)}
	catalog := discoverWith(t.Context(), options, directFixtureNode)
	if catalog.Status != StatusInconclusive || len(catalog.Candidates) != 1 {
		t.Fatalf("unexpected serial-less catalog: %#v", catalog)
	}
	candidate := catalog.Candidates[0]
	if candidate.Eligible || candidate.EligibleReadOnly || candidate.MediaIdentity != "" {
		t.Fatalf("controller identity authorized serial-less media: %#v", candidate)
	}
	if _, err := authorizeWith(t.Context(), options, AuthorizationRequest{Device: candidate.Device, ConfirmationCode: candidate.ConfirmationCode}, directFixtureNode); err == nil {
		t.Fatal("serial-less media received writable authorization")
	}
}

func TestMediaIdentityStopsAtNearestUSBDevice(t *testing.T) {
	root := t.TempDir()
	hub := filepath.Join(root, "sys/devices/pci0000/usb1/1-1")
	device := filepath.Join(hub, "1-1.2")
	parent := filepath.Join(device, "1-1.2:1.0/host0/target0/0:0:0:0/block/sdb")
	mustTargetMkdir(t, parent)
	mustTargetWrite(t, filepath.Join(hub, "serial"), "FIXTURE-UPSTREAM-HUB\n")
	key := bytes.Repeat([]byte{7}, 32)
	if identity, source := mediaIdentity(key, parent); identity != "" || source != "" {
		t.Fatal("upstream USB hub identity was accepted as media identity")
	}
	mustTargetWrite(t, filepath.Join(device, "serial"), "FIXTURE-OWN-USB\n")
	first, source := mediaIdentity(key, parent)
	if first == "" || source != "serial" || strings.Contains(first, "FIXTURE") {
		t.Fatal("nearest USB device identity was not privacy preserving")
	}
	mustTargetWrite(t, filepath.Join(hub, "serial"), "FIXTURE-CHANGED-HUB\n")
	if unchanged, _ := mediaIdentity(key, parent); unchanged != first {
		t.Fatal("upstream hub influenced media identity")
	}
	mustTargetWrite(t, filepath.Join(parent, "wwid"), "FIXTURE-OWN-MEDIA-WWID\n")
	withMedia, source := mediaIdentity(key, parent)
	if withMedia == first || !strings.Contains(source, "wwid") {
		t.Fatal("block media identity was omitted")
	}
	mustTargetWrite(t, filepath.Join(parent, "wwid"), "FIXTURE-REPLACED-MEDIA-WWID\n")
	if changed, _ := mediaIdentity(key, parent); changed == withMedia {
		t.Fatal("media replacement did not change identity")
	}
}

func TestReportParentRejectsEverySiblingMapping(t *testing.T) {
	for _, example := range []string{"mounted mapping", "unmounted mapping", "class entry absent", "holder registry absent"} {
		t.Run(example, func(t *testing.T) {
			root := t.TempDir()
			fixtureUSBPartition(t, root, "8:17", "8:16")
			sibling := fixtureSiblingPartition(t, root, example != "class entry absent")
			if example == "holder registry absent" {
				if err := os.Remove(filepath.Join(sibling, "holders")); err != nil {
					t.Fatal(err)
				}
			} else {
				mustTargetWrite(t, filepath.Join(sibling, "holders/dm-0"), "mapping\n")
			}
			if example == "mounted mapping" {
				mapping := filepath.Join(root, "sys/devices/virtual/block/dm-0")
				mustTargetWrite(t, filepath.Join(mapping, "dev"), "253:0\n")
				if err := os.Symlink(mapping, filepath.Join(root, "sys/class/block/dm-0")); err != nil {
					t.Fatal(err)
				}
				mustTargetWrite(t, filepath.Join(root, "proc/self/mountinfo"), "1 0 253:0 / /boot ro - ext4 /dev/dm-0 ro\n")
			}
			options := Options{Root: root, FingerprintKey: bytes.Repeat([]byte{7}, 32)}
			catalog := discoverWith(t.Context(), options, directFixtureNode)
			if catalog.Status != StatusInconclusive {
				t.Fatalf("mapped/unknown parent appeared safe: %#v", catalog)
			}
			for _, candidate := range catalog.Candidates {
				if candidate.Eligible || candidate.EligibleReadOnly || candidate.Exclusion != "report media participates in another block mapping" {
					t.Fatalf("sibling mapping did not exclude parent: %#v", candidate)
				}
			}
			if _, err := authorizeWith(t.Context(), options, AuthorizationRequest{Device: "/dev/sdb1", ConfirmationCode: catalog.Candidates[0].ConfirmationCode}, directFixtureNode); err == nil {
				t.Fatal("mapped sibling did not block writable authorization")
			}
		})
	}
}

func TestReportParentAllowsUnusedSiblingsButRejectsMountedBacking(t *testing.T) {
	root := t.TempDir()
	fixtureUSBPartition(t, root, "8:17", "8:16")
	fixtureSiblingPartition(t, root, true)
	options := Options{Root: root, FingerprintKey: bytes.Repeat([]byte{7}, 32)}
	catalog := discoverWith(t.Context(), options, directFixtureNode)
	if catalog.Status != StatusPass || len(catalog.Candidates) != 2 || !catalog.Candidates[0].Eligible || !catalog.Candidates[1].Eligible {
		t.Fatalf("legitimate unused sibling was excluded: %#v", catalog)
	}
	for _, mounted := range []string{"8:16", "8:18"} {
		mustTargetWrite(t, filepath.Join(root, "proc/self/mountinfo"), "1 0 "+mounted+" / /boot ro - ext4 /dev/fixture ro\n")
		catalog = discoverWith(t.Context(), options, directFixtureNode)
		if catalog.Status != StatusInconclusive || catalog.Candidates[0].Eligible || !catalog.Candidates[0].Mounted {
			t.Fatalf("mounted parent/sibling was not excluded (%s): %#v", mounted, catalog)
		}
	}
}

func TestReportParentRejectsMountedSiblingMissingFromDeviceLookup(t *testing.T) {
	for _, example := range []string{"class entry absent", "invalid device number"} {
		t.Run(example, func(t *testing.T) {
			root := t.TempDir()
			fixtureUSBPartition(t, root, "8:17", "8:16")
			sibling := fixtureSiblingPartition(t, root, example != "class entry absent")
			if example == "invalid device number" {
				mustTargetWrite(t, filepath.Join(sibling, "dev"), "unknown\n")
			}
			mustTargetWrite(t, filepath.Join(root, "proc/self/mountinfo"), "1 0 8:18 / /boot ro - ext4 /dev/sdb2 ro\n")
			catalog := discoverWith(t.Context(), Options{Root: root, FingerprintKey: bytes.Repeat([]byte{7}, 32)}, directFixtureNode)
			if catalog.Status != StatusInconclusive || len(catalog.Candidates) != 1 || catalog.Candidates[0].Eligible || catalog.Candidates[0].EligibleReadOnly {
				t.Fatalf("incomplete parent device lookup accepted mounted backing: %#v", catalog)
			}
		})
	}
}

func TestMountAuthorizationRejectsChangedOrUnauthorizedMedia(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*testing.T, string, *Authorization, *Options)
	}{
		{"forged HMAC", func(_ *testing.T, _ string, auth *Authorization, _ *Options) {
			auth.ID = "hmac-sha256:" + strings.Repeat("0", 64)
		}},
		{"wrong deployment key", func(_ *testing.T, _ string, _ *Authorization, options *Options) {
			options.FingerprintKey = bytes.Repeat([]byte{9}, 32)
		}},
		{"expired", func(t *testing.T, root string, _ *Authorization, _ *Options) {
			mustTargetWrite(t, filepath.Join(root, "proc/uptime"), "1023.00 0.00\n")
		}},
		{"different boot", func(t *testing.T, root string, _ *Authorization, _ *Options) {
			mustTargetWrite(t, filepath.Join(root, "proc/sys/kernel/random/boot_id"), "22222222-2222-4222-8222-222222222222\n")
		}},
		{"same path replacement", func(t *testing.T, root string, _ *Authorization, _ *Options) {
			mustTargetWrite(t, filepath.Join(root, "sys/devices/pci0000/usb1/1-1/serial"), "FIXTURE-REPLACEMENT\n")
		}},
		{"changed device number", func(t *testing.T, root string, _ *Authorization, _ *Options) {
			mustTargetWrite(t, filepath.Join(root, "sys/class/block/sdb1/dev"), "8:33\n")
		}},
		{"read only", func(t *testing.T, root string, _ *Authorization, _ *Options) {
			mustTargetWrite(t, filepath.Join(root, "sys/class/block/sdb/ro"), "1\n")
		}},
		{"mapped", func(t *testing.T, root string, _ *Authorization, _ *Options) {
			mustTargetWrite(t, filepath.Join(root, "sys/class/block/sdb1/holders/dm-0"), "mapping\n")
		}},
		{"sibling mapping appeared", func(t *testing.T, root string, _ *Authorization, _ *Options) {
			sibling := fixtureSiblingPartition(t, root, true)
			mustTargetWrite(t, filepath.Join(sibling, "holders/dm-0"), "mapping\n")
		}},
		{"own identity disappeared", func(t *testing.T, root string, _ *Authorization, _ *Options) {
			mustTargetWrite(t, filepath.Join(root, "sys/devices/pci0000/usb1/1-1/serial"), "\n")
			mustTargetWrite(t, filepath.Join(root, "sys/devices/pci0000/usb1/serial"), "0000:00:14.0\n")
		}},
		{"already mounted", func(t *testing.T, root string, _ *Authorization, _ *Options) {
			mustTargetWrite(t, filepath.Join(root, "proc/self/mountinfo"), "1 0 8:17 / /foreign ro - ext4 /dev/sdb1 ro\n")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			fixtureUSBPartition(t, root, "8:17", "8:16")
			options := Options{Root: root, FingerprintKey: bytes.Repeat([]byte{7}, 32)}
			catalog := discoverWith(t.Context(), options, directFixtureNode)
			auth, err := authorizeWith(t.Context(), options, AuthorizationRequest{
				Device: "/dev/sdb1", ConfirmationCode: catalog.Candidates[0].ConfirmationCode,
			}, directFixtureNode)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateMountAuthorizationWith(t.Context(), options, auth, directFixtureNode); err != nil {
				t.Fatalf("unchanged authorization was rejected: %v", err)
			}
			test.change(t, root, &auth, &options)
			if err := validateMountAuthorizationWith(t.Context(), options, auth, directFixtureNode); err == nil {
				t.Fatal("invalid mount authorization was accepted")
			}
		})
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
	if _, err := source.Seek(0, io.SeekEnd); err != nil {
		t.Fatal(err)
	}
	receipt, err := persistWith(context.Background(), options, authorization, source, int64(len("signed-report-fixture")), "session.hhpcr", expectedSHA256, directFixtureNode)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Bytes != int64(len("signed-report-fixture")) || !receipt.FileSynced || !receipt.ReadBackVerified || !receipt.DirectorySynced {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}
	if _, err := persistWith(context.Background(), options, authorization, source, int64(len("signed-report-fixture")), "session.hhpcr", expectedSHA256, directFixtureNode); err == nil {
		t.Fatal("existing report was overwritten")
	}
	wrongSHA256 := sha256.Sum256([]byte("different verified report"))
	if _, err := persistWith(context.Background(), options, authorization, source, int64(len("signed-report-fixture")), "changed-after-verification.hhpcr", wrongSHA256, directFixtureNode); err == nil {
		t.Fatal("bytes changed after verification passed")
	}
	// The rejected copy preserves its file. Retrying a fresh destination must
	// still copy from byte zero even when the caller's source offset is at EOF.
	if _, err := persistWith(context.Background(), options, authorization, source, int64(len("signed-report-fixture")), "retry.hhpcr", expectedSHA256, directFixtureNode); err != nil {
		t.Fatalf("retry depended on the source descriptor offset: %v", err)
	}
	for _, name := range []string{"changed-after-verification.hhpcr", "retry.hhpcr"} {
		contents, err := os.ReadFile(filepath.Join(mountPath, name))
		if err != nil || string(contents) != "signed-report-fixture" {
			t.Fatal("retry changed the preserved copy or omitted source bytes")
		}
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

func TestPersistRejectsNewSiblingMappingBeforeCreatingReport(t *testing.T) {
	root := t.TempDir()
	fixtureUSBPartition(t, root, "8:17", "8:16")
	options := Options{Root: root, FingerprintKey: bytes.Repeat([]byte{7}, 32)}
	catalog := discoverWith(t.Context(), options, directFixtureNode)
	authorization, err := authorizeWith(t.Context(), options, AuthorizationRequest{
		Device: "/dev/sdb1", ConfirmationCode: catalog.Candidates[0].ConfirmationCode,
	}, directFixtureNode)
	if err != nil {
		t.Fatal(err)
	}
	sibling := fixtureSiblingPartition(t, root, true)
	mustTargetWrite(t, filepath.Join(sibling, "holders/dm-0"), "mapping\n")
	mustTargetWrite(t, filepath.Join(root, "proc/self/mountinfo"), "1 0 8:17 / "+MountPoint+" rw,nodev,nosuid,noexec - ext4 /dev/sdb1 rw\n")
	sourcePath := filepath.Join(root, "source.hhpcr")
	mustTargetWrite(t, sourcePath, "verified-report-fixture")
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	digest := sha256.Sum256([]byte("verified-report-fixture"))
	if _, err := persistWith(t.Context(), options, authorization, source, int64(len("verified-report-fixture")), "rejected.hhpcr", digest, directFixtureNode); err == nil || !strings.Contains(err.Error(), "no longer usable") {
		t.Fatalf("mapping was not rejected before persistence: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, strings.TrimPrefix(MountPoint, "/"), "rejected.hhpcr")); !os.IsNotExist(err) {
		t.Fatal("report was created despite newly mapped sibling")
	}
}

func fixtureSiblingPartition(t *testing.T, root string, register bool) string {
	t.Helper()
	parent, err := filepath.EvalSymlinks(filepath.Join(root, "sys/class/block/sdb"))
	if err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(parent, "sdb2")
	mustTargetMkdir(t, filepath.Join(sibling, "holders"))
	for name, value := range map[string]string{"partition": "2\n", "dev": "8:18\n", "size": "65536\n", "ro": "0\n"} {
		mustTargetWrite(t, filepath.Join(sibling, name), value)
	}
	if register {
		if err := os.Symlink(sibling, filepath.Join(root, "sys/class/block/sdb2")); err != nil {
			t.Fatal(err)
		}
	}
	return sibling
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
