//go:build linux

package inventory

import (
	"runtime"
	"syscall"
	"testing"
)

func TestNativeInputBitmapWidthUsesKernelInsteadOfProcess(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	personality, _, queryErr := syscall.Syscall(syscall.SYS_PERSONALITY, 0xffffffff, 0, 0)
	if queryErr != 0 {
		t.Fatal("current kernel personality could not be observed")
	}
	var information syscall.Utsname
	if err := syscall.Uname(&information); err != nil {
		t.Fatal("kernel architecture could not be observed")
	}
	machine := []byte{}
	for _, value := range information.Machine {
		if value == 0 {
			break
		}
		machine = append(machine, byte(value))
	}
	want := 0
	switch string(machine) {
	case "x86_64", "aarch64":
		want = 64
	case "i386", "i486", "i586", "i686":
		want = 32
	}
	if personality&0xff != 0 {
		want = 0
	}
	if got := kernelInputBitmapWordBits(); got != want {
		t.Fatalf("kernel bitmap width = %d, want %d", got, want)
	}
	if personality&0xff == 0 && runtime.GOARCH == "386" && string(machine) == "x86_64" && kernelInputBitmapWordBits() != 64 {
		t.Fatal("32-bit process substituted its width for the 64-bit kernel")
	}
}
