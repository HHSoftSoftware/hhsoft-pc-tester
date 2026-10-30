package safemode

import (
	"reflect"
	"testing"
)

func TestStorageMountFactsPreserveExactMemoryTypes(t *testing.T) {
	const root = "10 1 0:5 / / rw - tmpfs tmpfs rw\n"
	const aliases = "11 10 0:5 / /tmp rw - tmpfs tmpfs rw\n12 10 0:7 / /ram rw - ramfs ramfs rw\n"
	const block = "13 10 8:0 / /media ro - ext4 /dev/fixture ro\n14 10 8:0 / /alias rw - ext4 /dev/fixture rw\n"
	facts, err := ParseMountFacts([]byte(root + aliases + block))
	expectedBlocks := map[string]MountState{"8:0": {Mounted: true, Writable: true}}
	if err != nil || facts.RootDevice != "0:5" || !reflect.DeepEqual(facts.BlockDevices, expectedBlocks) || !reflect.DeepEqual(facts.MemoryFilesystems, map[string]string{"0:5": "tmpfs", "0:7": "ramfs"}) {
		t.Fatalf("exact memory/block projection changed: %+v, %v", facts, err)
	}
	legacy, err := ParseMounts([]byte(root + aliases + block))
	if err != nil || !reflect.DeepEqual(legacy, expectedBlocks) {
		t.Fatal("legacy writable-alias projection changed")
	}
	for _, filesystem := range []string{"rootfs", "devtmpfs", "overlay", "proc", "sysfs", "hugetlbfs"} {
		t.Run(filesystem, func(t *testing.T) {
			facts, err := ParseMountFacts([]byte("10 1 0:5 / / ro - " + filesystem + " fixture ro\n"))
			if err != nil || facts.RootDevice != "0:5" || len(facts.MemoryFilesystems) != 0 {
				t.Fatal("non-memory filesystem was promoted to RAM backing")
			}
		})
	}
}

func TestStorageMountFactsRejectIncompleteOrAmbiguousViews(t *testing.T) {
	const root = "10 1 0:5 / / rw - tmpfs tmpfs rw\n"
	const unsafe = "11 10 8:0 / /media rw - ext4 /dev/fixture rw\n"
	for _, test := range []struct{ name, data string }{
		{"empty", ""},
		{"missing_root", unsafe},
		{"extra_root", root + "12 1 0:6 / / rw - tmpfs tmpfs rw\n"},
		{"same_number_extra_root", root + "12 1 0:5 / / ro - tmpfs tmpfs ro\n"},
		{"late_malformed", root + unsafe + "invalid\n"},
		{"conflicting_memory_alias", root + "12 10 0:5 / /ram rw - ramfs ramfs rw\n"},
		{"unknown_zero_major_type", root + "12 10 0:7 / /data ro - ext4 fixture ro\n"},
		{"writable_overlay", root + "12 10 0:7 / /data rw - overlay overlay rw\n"},
		{"duplicate_mount_id", root + unsafe + unsafe},
	} {
		t.Run(test.name, func(t *testing.T) {
			facts, err := ParseMountFacts([]byte(test.data))
			if err == nil || !reflect.DeepEqual(facts, MountFacts{}) {
				t.Fatal("partial or ambiguous view became admission authority")
			}
		})
	}
	legacy, err := ParseMounts([]byte(root + unsafe + "invalid\n"))
	if err == nil || !legacy["8:0"].Writable {
		t.Fatal("complete-only projection erased the legacy unsafe diagnostic")
	}
}
