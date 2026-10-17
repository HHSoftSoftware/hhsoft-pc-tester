package safemode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sys/class/block"), 0700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "proc/self/mountinfo", "1 0 0:1 / / rw - tmpfs tmpfs rw\n")
	writeFixture(t, root, "sys/class/net/lo/flags", "0x49\n")
	return root
}

func writeFixture(t *testing.T, root, path, value string) {
	t.Helper()
	path = filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestDisklessOfflineSystemIsSafeWithoutHardwareReadinessProviders(t *testing.T) {
	root := fixture(t)
	// The mount source is opaque; an alias, including "-", is not a delimiter
	// or evidence of a block device. The kernel major/minor is authoritative.
	writeFixture(t, root, "proc/self/mountinfo", "1 0 0:1 / / rw - tmpfs - rw\n")
	result := check(t.Context(), root)
	if !result.Safe() {
		t.Fatalf("diskless offline fixture was blocked: %#v", result)
	}
}

func TestWritableMappedMountsCannotHideBehindMissingPhysicalInventoryOrSourceAliases(t *testing.T) {
	for _, device := range []struct{ name, number string }{{"dm-0", "253:0"}, {"md0", "9:0"}, {"loop0", "7:0"}} {
		for _, mode := range []string{"rw", "ro"} {
			t.Run(device.name+"/"+mode, func(t *testing.T) {
				root := fixture(t)
				writeFixture(t, root, "sys/class/block/"+device.name+"/dev", device.number+"\n")
				writeFixture(t, root, "proc/self/mountinfo", fmt.Sprintf("1 0 %s / /media\\040fixture\\011name %s - ext4 UUID=fixture rw\n", device.number, mode))
				// No /dev tree exists. Safety must use device numbers without
				// opening a block node, reading sectors, or needing hardware IDs.
				result := check(t.Context(), root)
				if !result.StorageObserved || !result.NetworkObserved {
					t.Fatalf("complete fixture was not observed: %#v", result)
				}
				if mode == "rw" && (result.Safe() || result.Status != "fail" || result.WritableBlockMounts != 1) {
					t.Fatalf("writable mapping was not blocked: %#v", result)
				}
				if mode == "ro" && !result.Safe() {
					t.Fatalf("read-only mapping was blocked: %#v", result)
				}
			})
		}
	}
}

func TestWritableAliasesCountOneDeviceAndVirtualFilesystemsDoNotCount(t *testing.T) {
	root := fixture(t)
	writeFixture(t, root, "sys/class/block/dm-0/dev", "253:0\n")
	writeFixture(t, root, "proc/self/mountinfo", "1 0 0:1 / / rw - tmpfs tmpfs rw\n"+
		"2 1 0:2 / /proc rw,nosuid - proc proc rw\n"+
		"3 1 0:3 / /overlay ro - overlay overlay ro,lowerdir=/fixture/lower\n"+
		"4 1 253:0 / /first ro - ext4 alias rw\n"+
		"5 1 253:0 / /second rw - ext4 other-alias rw\n"+
		"6 1 253:0 /subdir /third rw - ext4 third-alias rw\n")
	result := check(t.Context(), root)
	if result.Status != "fail" || !result.StorageObserved || result.WritableBlockMounts != 1 {
		t.Fatalf("incorrect writable-device count: %#v", result)
	}
}

func TestWritableOverlayCannotCertifyZeroWritesWhenBackingMountIsHidden(t *testing.T) {
	for _, access := range []struct{ mount, superblock string }{{"rw", "rw"}, {"ro", "rw"}, {"ro", "ro"}} {
		t.Run(access.mount+"-"+access.superblock, func(t *testing.T) {
			root := fixture(t)
			// The upper/work paths name persistent storage, but its mount need
			// not still be visible. No path-prefix guess can certify RAM backing.
			writeFixture(t, root, "proc/self/mountinfo", "1 0 0:1 / / rw - tmpfs tmpfs rw\n"+
				fmt.Sprintf("2 1 0:2 / /merged %s - overlay overlay %s,lowerdir=/fixture/lower,upperdir=/persist/upper,workdir=/persist/work\n", access.mount, access.superblock))
			result := check(t.Context(), root)
			if access.mount == "rw" {
				if result.Safe() || result.StorageObserved || result.Status != "error" || !result.NetworkObserved {
					t.Fatalf("writable overlay was certified safe: %#v", result)
				}
			} else if !result.Safe() {
				t.Fatalf("read-only overlay was blocked: %#v", result)
			}
		})
	}
}

func TestUnknownOrMalformedMountObservationsFailClosed(t *testing.T) {
	for _, contents := range []string{"", "truncated\n",
		"1 0 8:1 / / rw - ext4 alias rw\n",  // Device absent from registry.
		"1 0 0:1 / / rw - btrfs alias rw\n", // Synthetic number is not proof of RAM backing.
		"1 0 0:1 / / rw,ro - tmpfs tmpfs rw\n",
		"1 0 0:1 / / nosuid - tmpfs tmpfs rw\n",
		"1 0 0:1 / / rw - tmpfs tmpfs unknown\n",
		"01 0 0:1 / / rw - tmpfs tmpfs rw\n",
		"1 0 0:1 / / rw - tmpfs tmpfs rw\n1 0 0:2 / /other rw - tmpfs tmpfs rw\n",
		"1 0 invalid / / rw - tmpfs tmpfs rw\n"} {
		t.Run(fmt.Sprintf("case-%x", contents), func(t *testing.T) {
			root := fixture(t)
			writeFixture(t, root, "proc/self/mountinfo", contents)
			result := check(t.Context(), root)
			if result.Safe() || result.StorageObserved || result.Status != "error" {
				t.Fatalf("unknown mount state passed: %#v", result)
			}
		})
	}
}

func TestMalformedOrMissingBlockRegistryCannotCertifyZeroMounts(t *testing.T) {
	for _, value := range []string{"", "unknown\n", "08:0\n", "8:0 extra\n", "0:1\n"} {
		t.Run(fmt.Sprintf("value-%x", value), func(t *testing.T) {
			root := fixture(t)
			writeFixture(t, root, "sys/class/block/dm-0/dev", value)
			if result := check(t.Context(), root); result.Safe() || result.StorageObserved {
				t.Fatalf("malformed device registry passed: %#v", result)
			}
		})
	}
	root := fixture(t)
	if err := os.Remove(filepath.Join(root, "sys/class/block")); err != nil {
		t.Fatal(err)
	}
	if result := check(t.Context(), root); result.Safe() || result.StorageObserved {
		t.Fatalf("missing registry passed: %#v", result)
	}
}

func TestEveryActiveNonLoopInterfaceBlocksIncludingSoftwareAndUnboundDevices(t *testing.T) {
	for _, name := range []string{"eth0", "wlan0", "tun0", "bridge0"} {
		t.Run(name, func(t *testing.T) {
			root := fixture(t)
			writeFixture(t, root, "sys/class/net/"+name+"/flags", "0x1003\n")
			result := check(t.Context(), root)
			if result.Safe() || result.Status != "fail" || !result.NetworkObserved || result.ActiveNonLoopInterfaces != 1 {
				t.Fatalf("active interface passed without a driver/provider: %#v", result)
			}
			writeFixture(t, root, "sys/class/net/"+name+"/flags", "0x1002\n")
			if result := check(t.Context(), root); !result.Safe() {
				t.Fatalf("inactive interface was blocked: %#v", result)
			}
		})
	}
}

func TestUnknownNetworkStateNeverBecomesOffline(t *testing.T) {
	for _, value := range []string{"", "unknown", "1", "0x100000000", "0x0\n0x1"} {
		t.Run(fmt.Sprintf("value-%x", value), func(t *testing.T) {
			root := fixture(t)
			writeFixture(t, root, "sys/class/net/tun0/flags", value)
			if result := check(t.Context(), root); result.Safe() || result.NetworkObserved {
				t.Fatalf("unknown network flags passed: %#v", result)
			}
		})
	}
	root := fixture(t)
	if err := os.Remove(filepath.Join(root, "sys/class/net/lo/flags")); err != nil {
		t.Fatal(err)
	}
	if result := check(t.Context(), root); result.Safe() || result.NetworkObserved {
		t.Fatalf("missing loopback observation passed: %#v", result)
	}
}

func TestObservationFailuresAreBoundedAndRetainIndependentUnsafeEvidence(t *testing.T) {
	root := fixture(t)
	writeFixture(t, root, "proc/self/mountinfo", strings.Repeat("x", (4<<20)+1))
	writeFixture(t, root, "sys/class/net/tun0/flags", "0x1\n")
	result := check(t.Context(), root)
	if result.Safe() || result.StorageObserved || !result.NetworkObserved || result.ActiveNonLoopInterfaces != 1 || result.Status != "fail" {
		t.Fatalf("failed observation discarded known unsafe state: %#v", result)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result := check(ctx, root); result.Safe() || result.Status != "aborted" || result.StorageObserved || result.NetworkObserved {
		t.Fatalf("cancelled observation passed: %#v", result)
	}
}
