//go:build linux

package usbtest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunRejectsRegularUSBNodeWithoutTransfer(t *testing.T) {
	root := t.TempDir()
	mustUSBDevice(t, root, "usb1", "1", "1", "09", "1", "4")
	mustWrite(t, filepath.Join(root, "dev/bus/usb/001/001"), "fixture")

	result := runPlatform(t.Context(), Options{Root: root})
	if result.Status != StatusInconclusive || result.Controllers != 1 || result.RootPorts != 4 || result.AuthorizedDevices != 1 || result.ReadableDevices != 0 || result.USBTransfersGenerated {
		t.Fatalf("unexpected regular-node result: %#v", result)
	}
	if !result.Devices[0].NodePresent || result.Devices[0].ReadinessIssue != "USB device path is not a direct character node" {
		t.Fatalf("USBFS path did not use a padded bus number: %#v", result.Devices[0])
	}
}

func TestRunReportsUnauthorizedDevice(t *testing.T) {
	root := t.TempDir()
	mustUSBDevice(t, root, "usb1", "1", "1", "09", "1", "4")
	mustUSBDevice(t, root, "1-1", "1", "2", "00", "0", "0")

	result := runPlatform(t.Context(), Options{Root: root})
	if result.Status != StatusInconclusive || result.AuthorizedDevices != 1 || result.Devices[0].ReadinessIssue != "USB device is not authorized" {
		t.Fatalf("unexpected authorization result: %#v", result)
	}
}

func TestRunRejectsInvalidDeviceNumber(t *testing.T) {
	root := t.TempDir()
	mustUSBDevice(t, root, "usb1", "999", "1", "09", "1", "4")

	result := runPlatform(t.Context(), Options{Root: root})
	if result.Status != StatusError {
		t.Fatalf("unexpected invalid-number result: %#v", result)
	}
}

func mustUSBDevice(t *testing.T, root, name, bus, device, class, authorized, ports string) {
	t.Helper()
	base := filepath.Join(root, "sys/bus/usb/devices", name)
	mustWrite(t, filepath.Join(base, "idVendor"), "1234\n")
	mustWrite(t, filepath.Join(base, "busnum"), bus+"\n")
	mustWrite(t, filepath.Join(base, "devnum"), device+"\n")
	mustWrite(t, filepath.Join(base, "bDeviceClass"), class+"\n")
	mustWrite(t, filepath.Join(base, "authorized"), authorized+"\n")
	mustWrite(t, filepath.Join(base, "maxchild"), ports+"\n")
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
