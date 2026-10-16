//go:build linux

package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMountsAggregatesWritableAliases(t *testing.T) {
	mounts, err := parseMounts(strings.NewReader("1 0 8:1 / /target ro - ext4 /dev/sda1 rw\n2 0 8:1 / /other rw - ext4 /dev/sda1 rw\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !mounts["8:1"].mounted || !mounts["8:1"].writable {
		t.Fatalf("unexpected mount state: %#v", mounts["8:1"])
	}
}

func TestRunRejectsWritableMountBeforeOpeningRegularDevice(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "proc/self/mountinfo"), "1 0 8:0 / /media rw - ext4 /dev/sda rw\n")
	mustWrite(t, filepath.Join(root, "sys/class/block/sda/dev"), "8:0\n")
	mustWrite(t, filepath.Join(root, "sys/class/block/sda/size"), "2048\n")
	mustWrite(t, filepath.Join(root, "sys/class/block/sda/ro"), "0\n")
	mustWrite(t, filepath.Join(root, "sys/class/block/sda/removable"), "0\n")
	mustWrite(t, filepath.Join(root, "dev/sda"), "must not be read")

	result := runPlatform(t.Context(), Options{Root: root})
	if result.Status != StatusError || result.WritableMounts != 1 || result.MediaContentRead || result.MediaContentWrites {
		t.Fatalf("unexpected unsafe-mount result: %#v", result)
	}
}

func TestRunEnforcesMountPolicyForExcludedBlockInterfaces(t *testing.T) {
	for _, device := range []struct{ name, number string }{
		{"dm-0", "253:0"},
		{"md0", "9:0"},
		{"loop0", "7:0"},
	} {
		for _, mode := range []string{"rw", "ro"} {
			t.Run(device.name+"/"+mode, func(t *testing.T) {
				root := fixtureReadinessMedia(t)
				mustWrite(t, filepath.Join(root, "sys/class/block", device.name, "dev"), device.number+"\n")
				// Escaped spaces/tabs in mount paths and a mapper alias do not
				// change the kernel device number used for the safety decision.
				mountinfo := fmt.Sprintf("1 0 %s / /mounted\\040data\\011set %s - ext4 /dev/mapper/alias rw\n", device.number, mode)
				mustWrite(t, filepath.Join(root, "proc/self/mountinfo"), mountinfo)
				result := runPlatform(t.Context(), Options{Root: root})
				if len(result.Devices) != 1 || result.Devices[0].Interface != "sda" || result.EligibleDevices != 1 {
					t.Fatalf("non-hardware block device entered readiness evidence: %#v", result)
				}
				if result.MediaContentRead || result.MediaContentWrites {
					t.Fatalf("mount policy performed media I/O: %#v", result)
				}
				if mode == "rw" {
					if result.Status != StatusError || result.WritableMounts != 1 || result.Devices[0].NodePresent {
						t.Fatalf("writable block mount did not stop readiness inspection: %#v", result)
					}
				} else if result.Status != StatusInconclusive || result.WritableMounts != 0 || !result.Devices[0].NodePresent {
					t.Fatalf("read-only mount incorrectly stopped readiness inspection: %#v", result)
				}
			})
		}
	}
}

func TestRunDoesNotRejectWritableVirtualFilesystems(t *testing.T) {
	root := fixtureReadinessMedia(t)
	mustWrite(t, filepath.Join(root, "proc/self/mountinfo"),
		"1 0 0:1 / /run rw,nosuid,nodev - tmpfs tmpfs rw\n"+
			"2 0 0:2 / /proc rw,nosuid,nodev,noexec - proc proc rw\n"+
			"3 0 0:3 / / rw - overlay overlay rw,lowerdir=/media/ro,upperdir=/run/upper\n")
	result := runPlatform(t.Context(), Options{Root: root})
	if result.Status != StatusInconclusive || result.WritableMounts != 0 || len(result.Devices) != 1 || !result.Devices[0].NodePresent {
		t.Fatalf("virtual filesystem mount incorrectly stopped readiness inspection: %#v", result)
	}
}

func TestRunCountsWritableBlockDevicesOnceAcrossMountAliases(t *testing.T) {
	root := fixtureReadinessMedia(t)
	mustWrite(t, filepath.Join(root, "sys/class/block/dm-0/dev"), "253:0\n")
	mustWrite(t, filepath.Join(root, "proc/self/mountinfo"),
		"1 0 253:0 / /first ro - ext4 /dev/mapper/alias rw\n"+
			"2 0 253:0 / /second rw - ext4 /dev/dm-0 rw\n"+
			"3 0 253:0 /subdir /third rw - ext4 /dev/dm-0 rw\n")
	result := runPlatform(t.Context(), Options{Root: root})
	if result.Status != StatusError || result.WritableMounts != 1 || result.Devices[0].NodePresent {
		t.Fatalf("writable mount aliases were missed or counted as different block devices: %#v", result)
	}
}

func fixtureReadinessMedia(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "sys/class/block/sda/dev"), "8:0\n")
	mustWrite(t, filepath.Join(root, "sys/class/block/sda/size"), "2048\n")
	mustWrite(t, filepath.Join(root, "sys/class/block/sda/ro"), "0\n")
	mustWrite(t, filepath.Join(root, "sys/class/block/sda/removable"), "0\n")
	// A regular fixture file makes readiness inspection observable without
	// ever creating or opening a real block device.
	mustWrite(t, filepath.Join(root, "dev/sda"), "must not be read")
	return root
}

func TestRunDoesNotTreatRegularFileAsBlockDevice(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "proc/self/mountinfo"), "1 0 0:1 / / ro - tmpfs tmpfs ro\n")
	mustWrite(t, filepath.Join(root, "sys/class/block/sda/dev"), "8:0\n")
	mustWrite(t, filepath.Join(root, "sys/class/block/sda/size"), "2048\n")
	mustWrite(t, filepath.Join(root, "sys/class/block/sda/ro"), "0\n")
	mustWrite(t, filepath.Join(root, "sys/class/block/sda/removable"), "0\n")
	mustWrite(t, filepath.Join(root, "dev/sda"), "fixture")

	result := runPlatform(t.Context(), Options{Root: root})
	if result.Status != StatusInconclusive || result.EligibleDevices != 1 || result.ReadableDevices != 0 || result.MediaContentRead || result.MediaContentWrites {
		t.Fatalf("unexpected regular-file result: %#v", result)
	}
}

func TestValidInterfaceName(t *testing.T) {
	for _, name := range []string{"sda", "nvme0n1", "mmcblk0"} {
		if !validInterfaceName(name) {
			t.Fatalf("rejected %q", name)
		}
	}
	for _, name := range []string{"", "../sda", "a/b"} {
		if validInterfaceName(name) {
			t.Fatalf("accepted %q", name)
		}
	}
}

func mustWrite(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}
