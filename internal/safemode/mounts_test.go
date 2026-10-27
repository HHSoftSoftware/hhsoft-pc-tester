package safemode

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseMountsRequiresCompleteUnambiguousRecords(t *testing.T) {
	for index, contents := range []string{
		"", "\n", "1 0 8:0 / /target ro missing\n",
		"1 0 8:0 / /target ro - ext4 /dev/sda\n",
		"1 0 8:0 / /target ro - ext4 /dev/sda ro extra\n",
		"1 0 8:0 / /target relatime - ext4 /dev/sda rw\n",
		"1 0 8:0 / /target ro,ro - ext4 /dev/sda ro\n",
		"1 0 8:0 / /target ro,rw - ext4 /dev/sda rw\n",
		"1 0 8:0 / /target ro, - ext4 /dev/sda ro\n",
		"1 0 8:0 / /target ro - ext4 /dev/sda relatime\n",
		"1 0 8:0 / /target ro - ext4 /dev/sda ro,rw\n",
		"01 0 8:0 / /target ro - ext4 /dev/sda ro\n",
		"1 00 8:0 / /target ro - ext4 /dev/sda ro\n",
		"0 0 8:0 / /target ro - ext4 /dev/sda ro\n",
		"1 0 08:0 / /target ro - ext4 /dev/sda ro\n",
		"1 0 8:4294967296 / /target ro - ext4 /dev/sda ro\n",
		"1 0 8:0 relative /target ro - ext4 /dev/sda ro\n",
		"1 0 8:0 / relative ro - ext4 /dev/sda ro\n",
		"1 0 8:0 / /one ro - ext4 /dev/sda ro\n1 0 8:0 / /two ro - ext4 /dev/sda ro\n",
	} {
		t.Run(fmt.Sprintf("record-%d", index), func(t *testing.T) {
			if _, err := ParseMounts([]byte(contents)); err == nil {
				t.Fatal("unavailable or malformed mount observations accepted")
			}
		})
	}
}

func TestParseMountsClassifiesAnonymousFilesystemsConservatively(t *testing.T) {
	for _, filesystem := range []string{"btrfs", "fuse", "fuse.test", "unknown"} {
		for _, access := range []string{"ro", "rw"} {
			t.Run(filesystem+"-"+access, func(t *testing.T) {
				contents := fmt.Sprintf("1 0 0:27 / /target %s - %s source %s\n", access, filesystem, access)
				if _, err := ParseMounts([]byte(contents)); err == nil {
					t.Fatal("anonymous device number was treated as proof of harmless backing")
				}
			})
		}
	}
	for _, filesystem := range []string{"tmpfs", "ramfs", "proc", "sysfs", "devtmpfs", "devpts"} {
		t.Run(filesystem, func(t *testing.T) {
			mounts, err := ParseMounts([]byte("1 0 0:1 / /target rw - " + filesystem + " source rw\n"))
			if err != nil || len(mounts) != 0 {
				t.Fatal("known virtual filesystem incorrectly became a block device")
			}
		})
	}
	if _, err := ParseMounts([]byte("1 0 0:1 / /target rw - overlay overlay rw\n")); err == nil {
		t.Fatal("writable overlay backing was accepted")
	}
}

func TestParseMountsRetainsUnsafeAliasesBeforeLaterFailure(t *testing.T) {
	prefix := "1 0 8:0 / /one ro - ext4 alias rw\n2 0 8:0 / /two rw - ext4 alias rw\n"
	for _, suffix := range []string{"truncated\n", "3 0 0:1 / /other rw - btrfs alias rw\n", strings.Repeat("x", 64<<10)} {
		mounts, err := ParseMounts([]byte(prefix + suffix))
		if err == nil || len(mounts) != 1 || mounts["8:0"] != (MountState{Mounted: true, Writable: true}) {
			t.Fatal("later failure erased a previously observed writable block mount")
		}
		count, err := parseMounts([]byte(prefix+suffix), map[string]bool{"8:0": true})
		if err == nil || count != 1 {
			t.Fatal("safety adapter lost the retained unsafe observation")
		}
	}
	for _, access := range []string{"ro", "rw"} {
		contents := prefix + "3 0 8:16 / /other " + access + " - ext4 alias " + access + "\n"
		if count, err := parseMounts([]byte(contents), map[string]bool{"8:0": true}); err == nil || count != 1 {
			t.Fatal("unknown registry device erased a known writable mount or passed validation")
		}
	}
}

func TestParseMountsBoundsCompleteSnapshotAndRecords(t *testing.T) {
	if _, err := ParseMounts(make([]byte, (4<<20)+1)); err == nil {
		t.Fatal("oversized mount snapshot accepted")
	}
	var records strings.Builder
	for index := 1; index <= 4096; index++ {
		fmt.Fprintf(&records, "%d 0 0:1 / /run rw - tmpfs tmpfs rw\n", index)
	}
	if _, err := ParseMounts([]byte(records.String())); err != nil {
		t.Fatal("bounded complete virtual mount snapshot rejected")
	}
	fmt.Fprintln(&records, "4097 0 0:1 / /run rw - tmpfs tmpfs rw")
	if _, err := ParseMounts([]byte(records.String())); err == nil {
		t.Fatal("excess mount records accepted")
	}
}
