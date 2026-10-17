package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

func TestSaveNeverAnnouncesSuccessAfterIncompleteDurabilityOrAudit(t *testing.T) {
	for _, failure := range []string{"directory-sync-error", "directory-unsynced", "file-unsynced", "unverified", "receipt-audit"} {
		t.Run(failure, func(t *testing.T) {
			arguments := saveTestArguments(t)
			var audit, stdout, stderr bytes.Buffer
			operations := saveTestOperations(t, &audit)
			if failure == "receipt-audit" {
				operations.openAudit = func(string) (io.Writer, func(), error) {
					return &failingSaveAudit{phase: "receipt"}, func() {}, nil
				}
			}
			operations.authorize = func(context.Context, reporttarget.Options, reporttarget.AuthorizationRequest) (reporttarget.Authorization, error) {
				return reporttarget.Authorization{MountPoint: reporttarget.MountPoint}, nil
			}
			unmounted := false
			operations.mount = func(context.Context, reporttarget.Options, reporttarget.Authorization) (func() error, error) {
				return func() error { unmounted = true; return nil }, nil
			}
			operations.persist = func(_ context.Context, _ reporttarget.Options, _ reporttarget.Authorization, _ *os.File, size int64, name string, digest [sha256.Size]byte) (reporttarget.Receipt, error) {
				if failure == "directory-sync-error" {
					return reporttarget.Receipt{}, errors.New("fixture directory sync failure")
				}
				return reporttarget.Receipt{File: name, Bytes: size, SHA256: fmt.Sprintf("sha256:%x", digest),
					FileSynced: failure != "file-unsynced", ReadBackVerified: failure != "unverified", DirectorySynced: failure != "directory-unsynced"}, nil
			}
			code := runSaveWith(t.Context(), arguments, strings.NewReader("1\nRPT-ABC123\n"), &stdout, &stderr, operations)
			if code != 1 || !unmounted || strings.Contains(stdout.String(), "Saved and read-back verified") || strings.Contains(audit.String(), "HHPT_SAVE_SAVED") {
				t.Fatalf("incomplete save was acknowledged: code=%d unmounted=%t stdout=%q audit=%q stderr=%q", code, unmounted, stdout.String(), audit.String(), stderr.String())
			}
		})
	}
}

func TestPersistCommandRequiresVerifiedAndSyncedReceipt(t *testing.T) {
	for _, failure := range []string{"", "directory-sync-error", "directory-unsynced", "file-unsynced", "unverified"} {
		t.Run(failure, func(t *testing.T) {
			saved := saveTestArguments(t)
			keyPath, sourcePath := saved[1], saved[len(saved)-1]
			key, err := os.ReadFile(keyPath)
			if err != nil {
				t.Fatal(err)
			}
			candidate := reporttarget.Candidate{Device: "/dev/sdb1", Parent: "/dev/sdb", Topology: "/sys/devices/fixture",
				DeviceMajorMinor: "8:17", ParentMajorMinor: "8:16", SizeBytes: 32 << 20, ParentSizeBytes: 64 << 20,
				FingerprintKeyID: "hmac-sha256:" + strings.Repeat("1", 64), MediaIdentity: "hmac-sha256:" + strings.Repeat("2", 64), MediaIdentitySource: "serial"}
			authorization, err := reporttarget.NewAuthorization(candidate, "11111111-1111-4111-8111-111111111111", 123, key)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(authorization)
			if err != nil {
				t.Fatal(err)
			}
			authorizationPath := filepath.Join(t.TempDir(), "authorization.json")
			if err := os.WriteFile(authorizationPath, encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			persist := func(_ context.Context, _ reporttarget.Options, _ reporttarget.Authorization, source *os.File, size int64, name string, digest [sha256.Size]byte) (reporttarget.Receipt, error) {
				calls++
				if source == nil || size <= 0 || digest == ([sha256.Size]byte{}) {
					t.Fatal("command did not verify its source before persistence")
				}
				if failure == "directory-sync-error" {
					return reporttarget.Receipt{}, errors.New("fixture directory sync failure")
				}
				return reporttarget.Receipt{Schema: reporttarget.ReceiptSchema, File: name, Bytes: size, SHA256: fmt.Sprintf("sha256:%x", digest),
					FileSynced: failure != "file-unsynced", ReadBackVerified: failure != "unverified", DirectorySynced: failure != "directory-unsynced"}, nil
			}
			var stdout, stderr bytes.Buffer
			code := runPersistWith(t.Context(), []string{"-authorization", authorizationPath, "-fingerprint-key", keyPath, "-self-issued", sourcePath}, &stdout, &stderr, persist)
			if calls != 1 {
				t.Fatalf("valid fixture did not reach persistence: %s", stderr.String())
			}
			if failure == "" {
				if code != 0 || !strings.Contains(stdout.String(), `"directory_synced": true`) {
					t.Fatalf("valid save rejected: %d %s", code, stderr.String())
				}
			} else if code != 1 || stdout.Len() != 0 {
				t.Fatalf("failed save emitted successful CLI receipt: code=%d stdout=%q", code, stdout.String())
			}
		})
	}
}
