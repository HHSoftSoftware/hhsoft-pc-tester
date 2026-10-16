//go:build linux

package networktest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunPassesOfflineDriverBoundHardwareInterface(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "sys/class/net/eth0/flags"), "0x1002\n")
	mustWrite(t, filepath.Join(root, "sys/class/net/eth0/operstate"), "down\n")
	mustWrite(t, filepath.Join(root, "sys/class/net/eth0/carrier"), "0\n")
	mustWrite(t, filepath.Join(root, "sys/class/net/eth0/device/uevent"), "DRIVER=e1000e\n")

	result := runPlatform(t.Context(), Options{Root: root})
	if result.Status != StatusPass || result.HardwareInterfaces != 1 || result.DriverBoundInterfaces != 1 || result.ActiveNonLoopInterfaces != 0 || result.NetworkTrafficGenerated {
		t.Fatalf("unexpected offline result: %#v", result)
	}
}

func TestRunFailsClosedWhenInterfaceIsActive(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "sys/class/net/wlan0/flags"), "0x1003\n")
	mustWrite(t, filepath.Join(root, "sys/class/net/wlan0/operstate"), "up\n")
	mustWrite(t, filepath.Join(root, "sys/class/net/wlan0/wireless/present"), "1\n")
	mustWrite(t, filepath.Join(root, "sys/class/net/wlan0/device/uevent"), "DRIVER=iwlwifi\n")

	result := runPlatform(t.Context(), Options{Root: root})
	if result.Status != StatusError || result.ActiveNonLoopInterfaces != 1 || result.NetworkTrafficGenerated {
		t.Fatalf("unexpected active-interface result: %#v", result)
	}
	allowed := runPlatform(t.Context(), Options{Root: root, AllowActive: true})
	if allowed.Status != StatusPass || allowed.RequireOffline {
		t.Fatalf("unexpected opt-in result: %#v", allowed)
	}
}

func TestRunIgnoresSoftwareInterfaces(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "sys/class/net/lo/flags"), "0x49\n")
	mustWrite(t, filepath.Join(root, "sys/class/net/tun0/flags"), "0x1002\n")

	result := runPlatform(t.Context(), Options{Root: root})
	if result.Status != StatusInconclusive || len(result.Interfaces) != 0 {
		t.Fatalf("unexpected software-interface result: %#v", result)
	}
	mustWrite(t, filepath.Join(root, "sys/class/net/tun0/flags"), "0x1003\n")
	result = runPlatform(t.Context(), Options{Root: root})
	if result.Status != StatusError || result.ActiveNonLoopInterfaces != 1 {
		t.Fatalf("active software interface was not rejected: %#v", result)
	}
}

func TestSanitizeDriver(t *testing.T) {
	if value := sanitizeDriver(" virtio_net "); value != "virtio_net" {
		t.Fatalf("driver = %q", value)
	}
	if value := sanitizeDriver("invalid driver"); value != "" {
		t.Fatalf("sanitized driver = %q", value)
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
