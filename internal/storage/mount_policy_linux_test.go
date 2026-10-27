//go:build linux

package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRejectsIncompleteMountSafetyBeforeAnyNodeOpen(t *testing.T) {
	cases := []struct {
		name, contents string
		writable       int
	}{
		{"empty", "", 0},
		{"missing separator", "1 0 8:0 / /target ro missing\n", 0},
		{"missing fields", "1 0 8:0 / /target ro - ext4 source\n", 0},
		{"extra fields", "1 0 8:0 / /target ro - ext4 source ro extra\n", 0},
		{"missing access", "1 0 8:0 / /target relatime - ext4 source rw\n", 0},
		{"contradictory access", "1 0 8:0 / /target ro,rw - ext4 source rw\n", 0},
		{"duplicate access", "1 0 8:0 / /target ro,ro - ext4 source ro\n", 0},
		{"missing superblock access", "1 0 8:0 / /target ro - ext4 source relatime\n", 0},
		{"duplicate mount ID", "1 0 8:0 / /one ro - ext4 source ro\n1 0 8:0 / /two ro - ext4 source ro\n", 0},
		{"noncanonical mount ID", "01 0 8:0 / /target ro - ext4 source ro\n", 0},
		{"relative mountpoint", "1 0 8:0 / relative ro - ext4 source ro\n", 0},
		{"unknown writable device", "1 0 8:16 / /target rw - ext4 source rw\n", 1},
		{"unknown read-only device", "1 0 8:16 / /target ro - ext4 source ro\n", 0},
		{"writable overlay", "1 0 0:2 / /target rw - overlay overlay rw\n", 0},
	}
	for _, filesystem := range []string{"btrfs", "fuse", "fuse.test", "unknown"} {
		for _, access := range []string{"rw", "ro"} {
			cases = append(cases, struct {
				name, contents string
				writable       int
			}{filesystem + "-" + access, fmt.Sprintf("1 0 0:27 / /target %s - %s source %s\n", access, filesystem, access), 0})
		}
	}
	for _, example := range cases {
		t.Run(example.name, func(t *testing.T) {
			root := fixtureReadinessMedia(t)
			mustWrite(t, filepath.Join(root, "proc/self/mountinfo"), example.contents)
			fixture := validNodeOperations()
			result := runPlatformWith(t.Context(), Options{Root: root}, fixture.operations())
			if result.Status != StatusError || result.WritableMounts != example.writable || result.ReadableDevices != 0 || fixture.openCalls != 0 || fixture.file.closeCalls != 0 {
				t.Fatalf("incomplete mount safety admitted readiness: status=%s writable=%d readable=%d opens=%d closes=%d", result.Status, result.WritableMounts, result.ReadableDevices, fixture.openCalls, fixture.file.closeCalls)
			}
			if result.MediaContentRead || result.MediaContentWrites {
				t.Fatal("mount validation claimed media I/O")
			}
		})
	}
}

func TestRunRetainsWritableObservationOnLaterFailures(t *testing.T) {
	const writable = "1 0 8:0 / /mounted rw - ext4 source rw\n"
	for _, phase := range []string{"mount syntax", "unknown mount", "registry identity", "registry cancellation"} {
		t.Run(phase, func(t *testing.T) {
			root := fixtureReadinessMedia(t)
			contents := writable
			if phase == "mount syntax" {
				contents += "truncated\n"
			}
			if phase == "unknown mount" {
				contents += "2 0 8:16 / /other ro - ext4 source ro\n"
			}
			mustWrite(t, filepath.Join(root, "proc/self/mountinfo"), contents)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fixture := validNodeOperations()
			if phase == "registry identity" {
				fixture.identityOK = false
			}
			if phase == "registry cancellation" {
				fixture.identityHook = func(int) { cancel() }
			}
			result := runPlatformWith(ctx, Options{Root: root}, fixture.operations())
			wantStatus := StatusError
			if phase == "registry cancellation" {
				wantStatus = StatusAborted
			}
			if result.Status != wantStatus || result.WritableMounts != 1 || fixture.openCalls != 0 || result.ReadableDevices != 0 {
				t.Fatalf("later failure erased or credited an unsafe observation: status=%s writable=%d opens=%d readable=%d", result.Status, result.WritableMounts, fixture.openCalls, result.ReadableDevices)
			}
		})
	}
}

func TestRunSupportsValidVirtualAndReadOnlyMountViews(t *testing.T) {
	for _, example := range []struct {
		name, contents string
		mounted        bool
	}{
		{"tmpfs", neutralMountInfo, false},
		{"proc", "1 0 0:2 / /proc rw - proc proc rw\n", false},
		{"known readonly", "1 0 8:0 / /mounted ro - ext4 source ro\n", true},
		{"readonly overlay", "1 0 0:2 / /merged ro - overlay overlay rw\n", false},
	} {
		t.Run(example.name, func(t *testing.T) {
			root := fixtureReadinessMedia(t)
			mustWrite(t, filepath.Join(root, "proc/self/mountinfo"), example.contents)
			fixture := validNodeOperations()
			result := runPlatformWith(t.Context(), Options{Root: root}, fixture.operations())
			if result.Status != StatusPass || result.WritableMounts != 0 || result.ReadableDevices != 1 || len(result.Devices) != 1 || result.Devices[0].Mounted != example.mounted || fixture.openCalls != 1 || fixture.file.closeCalls != 1 {
				t.Fatalf("valid mount view rejected: status=%s writable=%d readable=%d opens=%d closes=%d", result.Status, result.WritableMounts, result.ReadableDevices, fixture.openCalls, fixture.file.closeCalls)
			}
		})
	}
}

func TestRunDisklessRequiresValidNonemptyVirtualMountView(t *testing.T) {
	for _, contents := range []string{neutralMountInfo, ""} {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "sys/class/block"), 0755); err != nil {
			t.Fatal("cannot create empty registry fixture")
		}
		mustWrite(t, filepath.Join(root, "proc/self/mountinfo"), contents)
		fixture := validNodeOperations()
		result := runPlatformWith(t.Context(), Options{Root: root}, fixture.operations())
		wantStatus := StatusInconclusive
		if contents == "" {
			wantStatus = StatusError
		}
		if result.Status != wantStatus || result.EligibleDevices != 0 || result.ReadableDevices != 0 || result.WritableMounts != 0 || fixture.identityCalls != 0 || fixture.openCalls != 0 {
			t.Fatal("diskless result did not preserve observation requirements")
		}
	}
}

type mountReadFailure struct {
	read bool
}

func (reader *mountReadFailure) Read(buffer []byte) (int, error) {
	if reader.read {
		return 0, errors.New("fixture metadata read failure")
	}
	reader.read = true
	return copy(buffer, "1 0 8:0 / /mounted rw - ext4 source rw\n"), nil
}

func TestMountReadFailurePreservesParsedUnsafeObservation(t *testing.T) {
	mounts, err := parseMounts(&mountReadFailure{})
	if err == nil || !mounts["8:0"].Writable {
		t.Fatal("read failure discarded already observed unsafe metadata")
	}
	if _, err := parseMounts(io.LimitReader(strings.NewReader(strings.Repeat("x", (4<<20)+1)), (4<<20)+1)); err == nil {
		t.Fatal("oversized mount input accepted")
	}
}
