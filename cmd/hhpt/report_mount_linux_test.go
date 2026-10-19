//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

func TestReportMountRejectsReplacementBeforeWritableOperation(t *testing.T) {
	for _, replacementValidation := range []int{1, 2} {
		t.Run(map[int]string{1: "before probe", 2: "after probe"}[replacementValidation], func(t *testing.T) {
			device, directory, authorization := reportMountFixture(t, "vfat")
			validations, mounts := 0, 0
			err := performReportMount(t.Context(), reporttarget.Options{}, authorization, device, directory, reportMountOperations{
				validate: func(context.Context, reporttarget.Options, reporttarget.Authorization) error {
					validations++
					if validations == replacementValidation {
						replaceReportMountFixture(t, authorization.Device.Device)
					}
					return nil
				},
				check: fixtureReportDescriptorCheck,
				mount: func(string, string, string, uintptr, string) error { mounts++; return nil },
			})
			if err == nil || mounts != 0 {
				t.Fatalf("replacement reached writable operation: mounts=%d error=%v", mounts, err)
			}
		})
	}
}

func TestReportMountRejectsUnsupportedFilesystemBeforeWritableOperation(t *testing.T) {
	device, directory, authorization := reportMountFixture(t, "unknown")
	mounts := 0
	err := performReportMount(t.Context(), reporttarget.Options{}, authorization, device, directory, reportMountOperations{
		validate: func(context.Context, reporttarget.Options, reporttarget.Authorization) error { return nil },
		check:    fixtureReportDescriptorCheck,
		mount:    func(string, string, string, uintptr, string) error { mounts++; return nil },
	})
	if err == nil || mounts != 0 {
		t.Fatalf("unsupported filesystem reached writable operation: mounts=%d error=%v", mounts, err)
	}
}

func TestReportMountUsesBoundDescriptorsAndExactFlags(t *testing.T) {
	device, directory, authorization := reportMountFixture(t, "vfat")
	original, err := device.Stat()
	if err != nil {
		t.Fatal(err)
	}
	validations, mounts := 0, 0
	err = performReportMount(t.Context(), reporttarget.Options{}, authorization, device, directory, reportMountOperations{
		validate: func(context.Context, reporttarget.Options, reporttarget.Authorization) error {
			validations++
			return nil
		},
		check: fixtureReportDescriptorCheck,
		mount: func(source, target, filesystem string, flags uintptr, data string) error {
			mounts++
			if validations != 2 || !strings.HasPrefix(source, "/proc/self/fd/") || !strings.HasPrefix(target, "/proc/self/fd/") ||
				filesystem != "vfat" || flags != syscall.MS_NODEV|syscall.MS_NOSUID|syscall.MS_NOEXEC || data != "" {
				t.Fatal("mount was not bound to descriptors, explicit filesystem, and exact policy")
			}
			// Simulate a pathname swap in the final check-to-mount interval.
			// The kernel-facing source must still name the opened fixture inode.
			replaceReportMountFixture(t, authorization.Device.Device)
			pinned, statErr := os.Stat(source)
			if statErr != nil || !os.SameFile(original, pinned) {
				t.Fatal("mount source followed the replacement pathname")
			}
			return nil
		},
	})
	if err != nil || mounts != 1 {
		t.Fatalf("bound mount failed: mounts=%d error=%v", mounts, err)
	}
}

func TestReportMountStopsWhenAuthorizationExpiresDuringProbe(t *testing.T) {
	device, directory, authorization := reportMountFixture(t, "ext4")
	validations, mounts := 0, 0
	err := performReportMount(t.Context(), reporttarget.Options{}, authorization, device, directory, reportMountOperations{
		validate: func(context.Context, reporttarget.Options, reporttarget.Authorization) error {
			validations++
			if validations == 2 {
				return errors.New("authorization expired")
			}
			return nil
		},
		check: fixtureReportDescriptorCheck,
		mount: func(string, string, string, uintptr, string) error { mounts++; return nil },
	})
	if err == nil || mounts != 0 {
		t.Fatalf("expired authorization reached writable operation: mounts=%d error=%v", mounts, err)
	}
}

func TestReportMountProductionCheckRejectsRegularFiles(t *testing.T) {
	device, directory, authorization := reportMountFixture(t, "vfat")
	if err := checkReportMountDescriptors(device, directory, authorization); err == nil {
		t.Fatal("regular fixture file passed the production block-device check")
	}
}

func TestFailedMountCannotHideUnconfirmedCleanup(t *testing.T) {
	for _, cleanupFailed := range []bool{false, true} {
		calls := 0
		err := failedReportMountCleanup(func() error {
			calls++
			if cleanupFailed {
				return errors.New("raw-kernel-fixture-diagnostic")
			}
			return nil
		})
		if calls != 1 || err == nil || errors.Is(err, errReportMountCleanup) != cleanupFailed ||
			!strings.Contains(err.Error(), "could not be mounted") || strings.Contains(err.Error(), "raw-kernel") {
			t.Fatalf("mount/cleanup failure was not preserved safely: calls=%d error=%v", calls, err)
		}
	}
}

func reportMountFixture(t *testing.T, filesystem string) (*os.File, *os.File, reporttarget.Authorization) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "report-partition")
	if err := os.WriteFile(path, reportFilesystemFixture(filesystem), 0o600); err != nil {
		t.Fatal(err)
	}
	device, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = device.Close() })
	directory, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = directory.Close() })
	return device, directory, reporttarget.Authorization{Device: reporttarget.DeviceBinding{Device: path}}
}

func replaceReportMountFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.Rename(path, path+".removed"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, reportFilesystemFixture("exfat"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fixtureReportDescriptorCheck(device, _ *os.File, authorization reporttarget.Authorization) error {
	bound, err := device.Stat()
	if err != nil {
		return err
	}
	current, err := os.Lstat(authorization.Device.Device)
	if err != nil || !os.SameFile(bound, current) {
		return errors.New("fixture device path changed")
	}
	return nil
}
