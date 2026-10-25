//go:build linux

package workerexec

import (
	"encoding/binary"
	"errors"
	"runtime"
	"syscall"
	"unsafe"
)

// waitid(WNOWAIT) preserves the child identity until group cleanup is over.
// Linux's child siginfo record is 128 bytes; its PID starts at byte 12 on
// 32-bit ABIs and byte 16 on 64-bit ABIs. MIPS swaps the errno/code words.
// See https://man7.org/linux/man-pages/man2/waitid.2.html and
// https://go.dev/src/internal/syscall/unix/siginfo_linux.go.
func observeChild(pid int) (bool, error) {
	if pid <= 0 {
		return false, syscall.EINVAL
	}
	var aligned [16]uint64
	for {
		clear(aligned[:])
		_, _, errno := syscall.Syscall6(syscall.SYS_WAITID, 1, uintptr(pid),
			uintptr(unsafe.Pointer(&aligned[0])), syscall.WEXITED|syscall.WNOHANG|syscall.WNOWAIT, 0, 0)
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return false, errno
		}
		break
	}
	raw := (*[128]byte)(unsafe.Pointer(&aligned[0]))[:]
	pidOffset := 12
	if unsafe.Sizeof(uintptr(0)) == 8 {
		pidOffset = 16
	}
	observed := int32(binary.NativeEndian.Uint32(raw[pidOffset : pidOffset+4]))
	if observed == 0 {
		return false, nil
	}
	codeOffset := 8
	switch runtime.GOARCH {
	case "mips", "mipsle", "mips64", "mips64le":
		codeOffset = 4
	}
	code := binary.NativeEndian.Uint32(raw[codeOffset : codeOffset+4])
	if int(observed) != pid || binary.NativeEndian.Uint32(raw[:4]) != uint32(syscall.SIGCHLD) || code < 1 || code > 3 {
		return false, errors.New("workerexec: unexpected child exit observation")
	}
	return true, nil
}
