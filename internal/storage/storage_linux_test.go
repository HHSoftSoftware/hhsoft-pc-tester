//go:build linux

package storage

import (
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
