package safemode

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestMountIdentityAcceptsConsistentVirtualAliases(t *testing.T) {
	for _, test := range []struct {
		name     string
		contents string
		want     map[string]MountState
	}{
		{
			name: "same-device-different-path-source-and-access",
			contents: "31 1 0:31 / /run rw,nosuid - tmpfs tmpfs rw\n" +
				"32 1 0:31 /subdirectory /alias ro - tmpfs different-source rw\n",
			want: map[string]MountState{},
		},
		{
			name: "different-devices-different-types",
			contents: "31 1 0:31 / /run rw - tmpfs same-source rw\n" +
				"32 1 0:32 / /proc rw - proc same-source rw\n" +
				"33 1 0:33 / /sys ro - sysfs same-source rw\n" +
				"34 1 0:34 / /merged ro - overlay overlay ro,lowerdir=/lower\n",
			want: map[string]MountState{},
		},
		{
			name: "virtual-aliases-and-block-aliases",
			contents: "31 1 0:31 / /run rw - ramfs ramfs rw\n" +
				"32 1 8:0 / /disk ro - ext4 first-source rw\n" +
				"33 1 0:31 / /ram-alias ro - ramfs other-source rw\n" +
				"34 1 8:0 / /disk-alias rw - ext4 second-source rw\n" +
				"35 1 8:16 / /other ro - ext4 third-source ro\n",
			want: map[string]MountState{"8:0": {Mounted: true, Writable: true}, "8:16": {Mounted: true}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			mounts, err := ParseMounts([]byte(test.contents))
			if err != nil || !reflect.DeepEqual(mounts, test.want) {
				t.Fatalf("ordinary mount identity changed: got %v, error %v; want %v", mounts, err, test.want)
			}
		})
	}
}

func TestMountIdentityRejectsDifferentKnownTypesForOneVirtualDevice(t *testing.T) {
	for _, pair := range [][2]string{{"tmpfs", "proc"}, {"tmpfs", "ramfs"}, {"proc", "sysfs"}, {"overlay", "tmpfs"}} {
		for _, order := range [][2]string{pair, {pair[1], pair[0]}} {
			t.Run(order[0]+"-then-"+order[1], func(t *testing.T) {
				contents := "31 1 0:31 / /first ro - " + order[0] + " source ro\n" +
					"32 1 0:31 / /second ro - " + order[1] + " source ro\n"
				mounts, err := ParseMounts([]byte(contents))
				if err == nil || mounts == nil || len(mounts) != 0 {
					t.Fatalf("conflicting virtual identity accepted or leaked into block projection: %v, %v", mounts, err)
				}
			})
		}
	}
}

func TestMountIdentityRetainsOnlyObservedWritablePrefix(t *testing.T) {
	first := "31 1 0:31 / /run ro - tmpfs tmpfs ro\n"
	conflict := "32 1 0:31 / /proc ro - proc proc ro\n"
	one := "33 1 8:0 / /one rw - ext4 source rw\n"
	two := "34 1 8:16 / /two rw - ext4 source rw\n"
	for _, test := range []struct {
		name     string
		contents string
		want     int
	}{
		{"before-blocks", first + conflict + one + two, 0},
		{"between-blocks", first + one + conflict + two, 1},
		{"after-blocks", one + two + first + conflict, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			mounts, err := ParseMounts([]byte(test.contents))
			if err == nil || len(mounts) != test.want {
				t.Fatalf("wrong retained prefix: %v, %v", mounts, err)
			}
			for _, state := range mounts {
				if state != (MountState{Mounted: true, Writable: true}) {
					t.Fatalf("observed unsafe mount lost: %v", state)
				}
			}
			count, err := parseMounts([]byte(test.contents), map[string]bool{"8:0": true, "8:16": true})
			if err == nil || count != test.want {
				t.Fatalf("safety projection changed unsafe count: got %d, error %v; want %d", count, err, test.want)
			}
		})
	}
}

func TestMountIdentityDoesNotRetainStateBetweenObservations(t *testing.T) {
	for _, filesystem := range []string{"tmpfs", "proc", "tmpfs"} {
		contents := "31 1 0:31 / /target ro - " + filesystem + " source ro\n"
		if mounts, err := ParseMounts([]byte(contents)); err != nil || len(mounts) != 0 {
			t.Fatalf("an earlier independent observation contaminated %s: %v, %v", filesystem, mounts, err)
		}
	}
}

func TestMountIdentityDoesNotRelaxMalformedAliasChecks(t *testing.T) {
	prefix := "30 1 8:0 / /disk rw - ext4 source rw\n31 1 0:31 / /run ro - tmpfs tmpfs ro\n"
	for name, suffix := range map[string]string{
		"duplicate-mount-id":   "31 1 0:31 / /alias ro - tmpfs source ro\n",
		"noncanonical-number":  "32 1 0:031 / /alias ro - tmpfs source ro\n",
		"unknown-type":         "32 1 0:31 / /alias ro - unknown source ro\n",
		"filesystem-case":      "32 1 0:31 / /alias ro - TMPFS source ro\n",
		"ambiguous-access":     "32 1 0:31 / /alias ro,rw - tmpfs source ro\n",
		"missing-super-access": "32 1 0:31 / /alias ro - tmpfs source\n",
		"writable-overlay":     "32 1 0:32 / /alias rw - overlay source rw\n",
		"truncated-record":     "32 1 0:31 / /alias ro -\n",
		"oversized-record":     strings.Repeat("x", 64<<10),
	} {
		t.Run(name, func(t *testing.T) {
			mounts, err := ParseMounts([]byte(prefix + suffix))
			if err == nil || !reflect.DeepEqual(mounts, map[string]MountState{"8:0": {Mounted: true, Writable: true}}) {
				t.Fatalf("malformed alias accepted or unsafe prefix lost: %v, %v", mounts, err)
			}
		})
	}
}

func TestMountIdentityChecksLastRecordAtExistingBounds(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprintf("conflict-%t", conflict), func(t *testing.T) {
			var contents strings.Builder
			for index := 1; index <= 4096; index++ {
				number, filesystem := index, "tmpfs"
				if conflict && index == 4096 {
					number, filesystem = 1, "proc"
				}
				prefix := fmt.Sprintf("%d 0 0:%d / /run ro - %s ", index, number, filesystem)
				suffix := " ro\n"
				contents.WriteString(prefix + strings.Repeat("x", 1024-len(prefix)-len(suffix)) + suffix)
			}
			if contents.Len() != 4<<20 {
				t.Fatal("fixture does not reach the exact existing byte and record bounds")
			}
			mounts, err := ParseMounts([]byte(contents.String()))
			if (err != nil) != conflict || mounts == nil || len(mounts) != 0 {
				t.Fatalf("last bounded record was not inspected correctly: %v, %v", mounts, err)
			}
			if _, err := ParseMounts([]byte(contents.String() + "\n")); err == nil {
				t.Fatal("byte cap was widened")
			}
		})
	}
}

func TestMountIdentitySafetyResultsKeepTheirWireAndFailureCounts(t *testing.T) {
	for _, test := range []struct {
		name       string
		contents   string
		registered bool
		want       string
	}{
		{
			"diskless-consistent",
			"31 1 0:31 / /run rw - tmpfs tmpfs rw\n32 1 0:31 / /alias ro - tmpfs other rw\n33 1 0:32 / /proc rw - proc proc rw\n",
			false,
			`{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"pass","storage_observed":true,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0}`,
		},
		{
			"diskless-conflict",
			"31 1 0:31 / /run rw - tmpfs tmpfs rw\n32 1 0:31 / /proc rw - proc proc rw\n",
			false,
			`{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"error","storage_observed":false,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0,"storage_detail":"block mount safety could not be established"}`,
		},
		{
			"writable-before-conflict",
			"30 1 8:0 / /disk rw - ext4 source rw\n31 1 0:31 / /run rw - tmpfs tmpfs rw\n32 1 0:31 / /proc rw - proc proc rw\n",
			true,
			`{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"fail","storage_observed":false,"network_observed":true,"writable_block_mounts":1,"active_non_loop_interfaces":0,"storage_detail":"block mount safety could not be established"}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := fixture(t)
			if test.registered {
				writeFixture(t, root, "sys/class/block/sda/dev", "8:0\n")
			}
			writeFixture(t, root, "proc/self/mountinfo", test.contents)
			result := check(t.Context(), root)
			encoded, err := json.Marshal(result)
			if err != nil || string(encoded) != test.want {
				t.Fatalf("safety wire changed: got %s, error %v; want %s", encoded, err, test.want)
			}
		})
	}
}

func TestMountIdentityPreservesAlreadyCancelledObservation(t *testing.T) {
	root := fixture(t)
	writeFixture(t, root, "proc/self/mountinfo", "31 1 0:31 / /run rw - tmpfs tmpfs rw\n32 1 0:31 / /proc rw - proc proc rw\n")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result := check(ctx, root)
	if result.Status != "aborted" || result.Safe() || result.StorageObserved || result.NetworkObserved || result.WritableBlockMounts != 0 || result.ActiveNonLoopInterfaces != 0 {
		t.Fatalf("cancelled observation acquired success or unsafe-count claims: %#v", result)
	}
}
