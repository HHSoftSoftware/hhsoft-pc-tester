//go:build linux

package inventory

import (
	"runtime"
	"syscall"
)

// /proc and sysfs input bitmaps use kernel unsigned-long words. A 32-bit
// executable on a 64-bit kernel must not substitute its process word size.
func kernelInputBitmapWordBits() int {
	// Both observations must concern the same thread personality. This query
	// value reads the current personality; it never changes any process state.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return observedInputBitmapWordBits(func() (uintptr, error) {
		personality, _, err := syscall.Syscall(syscall.SYS_PERSONALITY, 0xffffffff, 0, 0)
		if err != 0 {
			return 0, err
		}
		return personality, nil
	}, func() (string, error) {
		var information syscall.Utsname
		if err := syscall.Uname(&information); err != nil {
			return "", err
		}
		machine := make([]byte, 0, len(information.Machine))
		for _, value := range information.Machine {
			if value == 0 {
				break
			}
			machine = append(machine, byte(value))
		}
		return string(machine), nil
	})
}
