//go:build linux

package reporttarget

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestPersistDirectorySyncFailurePreservesFileWithoutSuccessfulReceipt(t *testing.T) {
	root := t.TempDir()
	mountPath := filepath.Join(root, strings.TrimPrefix(MountPoint, "/"))
	mustTargetMkdir(t, mountPath)
	metadata, err := os.Stat(mountPath)
	if err != nil {
		t.Fatal(err)
	}
	device := fileDevice(metadata, false)
	fixtureUSBPartition(t, root, device, "8:16")
	options := Options{Root: root, FingerprintKey: bytes.Repeat([]byte{7}, 32)}
	catalog := discoverWith(t.Context(), options, directFixtureNode)
	authorization, err := authorizeWith(t.Context(), options, AuthorizationRequest{
		Device: "/dev/sdb1", ConfirmationCode: catalog.Candidates[0].ConfirmationCode}, directFixtureNode)
	if err != nil {
		t.Fatal(err)
	}
	mustTargetWrite(t, filepath.Join(root, "proc/self/mountinfo"), "1 0 "+device+" / "+MountPoint+" rw,nosuid,nodev,noexec - ext4 /dev/sdb1 rw\n")
	contents := []byte("verified fixture bytes retained after failed directory fsync")
	sourcePath := filepath.Join(root, "source.hhpcr")
	mustTargetWrite(t, sourcePath, string(contents))
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	calls := 0
	syncDirectory := func(directory *os.File) error {
		calls++
		opened, err := directory.Stat()
		if err != nil || !os.SameFile(metadata, opened) {
			t.Fatal("sync did not receive the bound report directory")
		}
		persisted, err := os.ReadFile(filepath.Join(mountPath, "session.hhpcr"))
		if err != nil || !bytes.Equal(persisted, contents) {
			t.Fatal("failure injection did not follow a complete report copy")
		}
		return syscall.EIO
	}
	receipt, err := persistWithDirectorySync(t.Context(), options, authorization, source, int64(len(contents)),
		"session.hhpcr", sha256.Sum256(contents), directFixtureNode, syncDirectory)
	if err == nil || calls != 1 || receipt != (Receipt{}) {
		t.Fatalf("failed directory sync was acknowledged: receipt=%+v calls=%d error=%v", receipt, calls, err)
	}
	persisted, err := os.ReadFile(filepath.Join(mountPath, "session.hhpcr"))
	if err != nil || !bytes.Equal(persisted, contents) {
		t.Fatal("failure removed or changed the recoverable report")
	}
	_, err = persistWithDirectorySync(t.Context(), options, authorization, source, int64(len(contents)),
		"session.hhpcr", sha256.Sum256(contents), directFixtureNode, syncDirectory)
	if err == nil || !errors.Is(err, os.ErrExist) || calls != 1 {
		t.Fatal("retry overwrote the preserved report or repeated a late sync")
	}
}
