package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

func TestSaveRejectsWrongConfirmationWithoutAuthorizationOrWrite(t *testing.T) {
	arguments := saveTestArguments(t)
	var audit, stdout, stderr bytes.Buffer
	operations := saveTestOperations(t, &audit)
	operations.authorize = func(context.Context, reporttarget.Options, reporttarget.AuthorizationRequest) (reporttarget.Authorization, error) {
		t.Fatal("wrong confirmation reached authorization")
		return reporttarget.Authorization{}, nil
	}
	if result := runSaveWith(t.Context(), arguments, strings.NewReader("1\nRPT-000000\nQ\n"), &stdout, &stderr, operations); result != 0 {
		t.Fatalf("save returned %d: %s", result, stderr.String())
	}
	if strings.Count(audit.String(), "HHPT_SAVE_REJECTED reason=confirmation\n") != 1 || strings.Contains(audit.String(), "HHPT_SAVE_SAVED") {
		t.Fatalf("wrong confirmation audit: %q", audit.String())
	}
	if !strings.Contains(stderr.String(), "no storage device was written") {
		t.Fatal("wrong confirmation did not report its fail-closed outcome")
	}
}

func TestSaveAuditsSuccessOnlyAfterAuthorizationPersistenceAndUnmount(t *testing.T) {
	for _, failure := range []string{"", "authorization", "mount", "persist", "unmount"} {
		t.Run(failure, func(t *testing.T) {
			arguments := saveTestArguments(t)
			var audit, stdout, stderr bytes.Buffer
			operations := saveTestOperations(t, &audit)
			var phases []string
			operations.authorize = func(_ context.Context, _ reporttarget.Options, request reporttarget.AuthorizationRequest) (reporttarget.Authorization, error) {
				phases = append(phases, "authorization")
				if request.Device != "/dev/sda1" || request.ConfirmationCode != "RPT-ABC123" {
					t.Fatal("selected media and exact confirmation were not reauthorized")
				}
				if failure == "authorization" {
					return reporttarget.Authorization{}, errors.New("fixture authorization rejected")
				}
				return reporttarget.Authorization{MountPoint: reporttarget.MountPoint, ID: "private-authorization-fixture"}, nil
			}
			operations.mount = func(context.Context, reporttarget.Options, reporttarget.Authorization) (func() error, error) {
				phases = append(phases, "mount")
				if failure == "mount" {
					return nil, errors.New("fixture mount rejected")
				}
				return func() error {
					phases = append(phases, "unmount")
					if strings.Contains(audit.String(), "HHPT_SAVE_SAVED") {
						t.Fatal("success was emitted before unmount")
					}
					if failure == "unmount" {
						return errors.New("fixture unmount failed")
					}
					return nil
				}, nil
			}
			operations.persist = func(_ context.Context, _ reporttarget.Options, _ reporttarget.Authorization, source *os.File, size int64, name string, digest [sha256.Size]byte) (reporttarget.Receipt, error) {
				phases = append(phases, "persist")
				if strings.Contains(audit.String(), "HHPT_SAVE_SAVED") {
					t.Fatal("success was emitted before persistence")
				}
				if failure == "persist" {
					return reporttarget.Receipt{}, errors.New("fixture persist failed")
				}
				// Inspect the supplied descriptor: Linux deliberately gives a pinned
				// input a diagnostic name rather than reopening the original path.
				encoded, err := io.ReadAll(io.NewSectionReader(source, 0, size+1))
				if err != nil || int64(len(encoded)) != size || sha256.Sum256(encoded) != digest || name != "hhpt-save-fixture.hhpcr" {
					t.Fatal("persistence did not receive the verified report bytes and digest")
				}
				return reporttarget.Receipt{File: name, Bytes: size, SHA256: fmt.Sprintf("sha256:%x", digest),
					AuthorizationID: "private-authorization-fixture", Device: "/dev/sda1",
					FileSynced: true, ReadBackVerified: true, DirectorySynced: true}, nil
			}
			result := runSaveWith(t.Context(), arguments, strings.NewReader("1\nRPT-ABC123\nQ\n"), &stdout, &stderr, operations)
			if strings.Contains(audit.String(), "private-") {
				t.Fatal("audit disclosed private identity or authorization fields")
			}
			if failure == "" {
				if result != 0 || strings.Join(phases, ",") != "authorization,mount,persist,unmount" || strings.Count(audit.String(), "HHPT_SAVE_SAVED ") != 1 {
					t.Fatalf("save failed: result=%d phases=%v audit=%q stderr=%q", result, phases, audit.String(), stderr.String())
				}
			} else {
				if strings.Contains(audit.String(), "HHPT_SAVE_SAVED") {
					t.Fatal("failed operation emitted a successful save receipt")
				}
				if failure == "authorization" && len(phases) != 1 || failure == "mount" && len(phases) != 2 {
					t.Fatal("failed prerequisite reached a later write operation")
				}
				if (failure == "persist" || failure == "unmount") && (result != 1 || strings.Join(phases, ",") != "authorization,mount,persist,unmount") {
					t.Fatal("failed persistence did not attempt unmount and return failure")
				}
			}
		})
	}
}

func TestSaveAuditSinkFailureStopsBeforeMount(t *testing.T) {
	for _, failure := range []string{"open", "candidate", "confirm", "rejection"} {
		t.Run(failure, func(t *testing.T) {
			arguments := saveTestArguments(t)
			var stdout, stderr bytes.Buffer
			writer := &failingSaveAudit{phase: failure}
			operations := saveTestOperations(t, writer)
			if failure == "open" {
				operations.openAudit = func(string) (io.Writer, func(), error) {
					return nil, func() {}, errors.New("fixture audit unavailable")
				}
			}
			if result := runSaveWith(t.Context(), arguments, strings.NewReader("1\nRPT-000000\n"), &stdout, &stderr, operations); result != 1 {
				t.Fatalf("audit failure returned %d", result)
			}
		})
	}
}

func TestSaveAuditRejectsUnsafeFieldsWithoutWriting(t *testing.T) {
	validCandidate := reporttarget.Candidate{Eligible: true, Device: "/dev/sda1", ConfirmationCode: "RPT-ABC123"}
	for _, mutate := range []func(*reporttarget.Candidate){
		func(value *reporttarget.Candidate) { value.Device = "/dev/sda1\nHHPT_SAVE_SAVED file=injected.hhpcr" },
		func(value *reporttarget.Candidate) { value.Device = "/dev/disk/by-id/private" },
		func(value *reporttarget.Candidate) { value.ConfirmationCode = "RPT-ABC123\n" },
		func(value *reporttarget.Candidate) { value.Eligible = false },
	} {
		candidate := validCandidate
		mutate(&candidate)
		var output bytes.Buffer
		if err := writeSaveCandidateAudit(&output, "CANDIDATE", 1, candidate); err == nil || output.Len() != 0 {
			t.Fatal("unsafe candidate fields reached audit output")
		}
	}
	validReceipt := reporttarget.Receipt{File: "report.hhpcr", Bytes: 100, SHA256: "sha256:" + strings.Repeat("a", 64),
		FileSynced: true, ReadBackVerified: true, DirectorySynced: true}
	for _, mutate := range []func(*reporttarget.Receipt){
		func(value *reporttarget.Receipt) { value.File = "../report.hhpcr" },
		func(value *reporttarget.Receipt) { value.File = "report.hhpcr\nHHPT_SAVE_SAVED" },
		func(value *reporttarget.Receipt) { value.SHA256 = strings.Repeat("a", 64) },
		func(value *reporttarget.Receipt) { value.Bytes = reporttarget.MaximumReportBytes + 1 },
		func(value *reporttarget.Receipt) { value.ReadBackVerified = false },
		func(value *reporttarget.Receipt) { value.FileSynced = false },
		func(value *reporttarget.Receipt) { value.DirectorySynced = false },
	} {
		receipt := validReceipt
		mutate(&receipt)
		var output bytes.Buffer
		if err := writeSaveReceiptAudit(&output, receipt); err == nil || output.Len() != 0 {
			t.Fatal("unsafe or unverified receipt reached audit output")
		}
	}
}

func TestSaveAuditAllowsOnlyOptionalFixedConsole(t *testing.T) {
	existing := filepath.Join(t.TempDir(), "existing.log")
	if err := os.WriteFile(existing, []byte("preserved fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{existing, filepath.Join(t.TempDir(), "new.log"), "/dev/sda", "/dev/null", "/dev/../dev/console"} {
		if _, closeAudit, err := openSaveAudit(path); err == nil {
			closeAudit()
			t.Fatal("non-console audit path accepted")
		}
	}
	contents, err := os.ReadFile(existing)
	if err != nil || string(contents) != "preserved fixture" {
		t.Fatal("rejected audit output changed an existing file")
	}
	writer, closeAudit, err := openSaveAudit("")
	if err != nil {
		t.Fatal(err)
	}
	defer closeAudit()
	if _, err := io.WriteString(writer, "discarded fixture"); err != nil {
		t.Fatal(err)
	}
}

func TestSaveWithoutAuditRetainsInteractiveConfirmation(t *testing.T) {
	arguments := saveTestArguments(t)
	arguments = append(arguments[:2], arguments[4:]...)
	operations := saveTestOperations(t, io.Discard)
	operations.openAudit = openSaveAudit
	var stdout, stderr bytes.Buffer
	if result := runSaveWith(t.Context(), arguments, strings.NewReader("1\nRPT-000000\nQ\n"), &stdout, &stderr, operations); result != 0 {
		t.Fatalf("unaudited save returned %d: %s", result, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Type RPT-ABC123") || !strings.Contains(stderr.String(), "no storage device was written") {
		t.Fatal("optional audit changed the interactive confirmation contract")
	}
}

func TestSaveCustomNamesWorkWithAndWithoutAudit(t *testing.T) {
	for _, name := range []string{"report.v1.hhpcr", strings.Repeat("x", 122) + ".hhpcr"} {
		for _, audited := range []bool{false, true} {
			t.Run(fmt.Sprintf("length-%d/audit-%t", len(name), audited), func(t *testing.T) {
				arguments := saveTestArguments(t)
				source := arguments[len(arguments)-1]
				arguments = append(arguments[:len(arguments)-1], "-name", name, source)
				var audit, stdout, stderr bytes.Buffer
				operations := saveTestOperations(t, &audit)
				if !audited {
					arguments = append(arguments[:2], arguments[4:]...)
					operations.openAudit = openSaveAudit
				}
				operations.authorize = func(context.Context, reporttarget.Options, reporttarget.AuthorizationRequest) (reporttarget.Authorization, error) {
					return reporttarget.Authorization{MountPoint: reporttarget.MountPoint}, nil
				}
				operations.mount = func(context.Context, reporttarget.Options, reporttarget.Authorization) (func() error, error) {
					return func() error { return nil }, nil
				}
				operations.persist = func(_ context.Context, _ reporttarget.Options, _ reporttarget.Authorization, _ *os.File, size int64, selectedName string, digest [sha256.Size]byte) (reporttarget.Receipt, error) {
					if selectedName != name {
						t.Fatal("custom filename changed before persistence")
					}
					return reporttarget.Receipt{File: name, Bytes: size, SHA256: fmt.Sprintf("sha256:%x", digest),
						FileSynced: true, ReadBackVerified: true, DirectorySynced: true}, nil
				}
				if result := runSaveWith(t.Context(), arguments, strings.NewReader("1\nRPT-ABC123\n"), &stdout, &stderr, operations); result != 0 {
					t.Fatalf("valid custom name returned %d: %s", result, stderr.String())
				}
				if audited && !strings.Contains(audit.String(), "HHPT_SAVE_SAVED file="+name+" ") {
					t.Fatal("valid custom filename missing from receipt marker")
				}
			})
		}
	}
}

func saveTestArguments(t *testing.T) []string {
	t.Helper()
	directory := t.TempDir()
	key := bytes.Repeat([]byte{0x27}, 32)
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x46}, ed25519.SeedSize))
	encoded, err := captureSelfIssuedBundle(inventory.NewSnapshot(nil), nil, key, "ephemeral-session", privateKey,
		privateKey.Public().(ed25519.PublicKey), "save-fixture", time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(directory, "source.hhpcr")
	if err := os.WriteFile(reportPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return []string{"-fingerprint-key", writeTestKey(t, directory, "fingerprint.key", key), "-audit-output", "/dev/console", reportPath}
}

func saveTestOperations(t *testing.T, audit io.Writer) saveOperations {
	t.Helper()
	return saveOperations{
		discover: func(context.Context, reporttarget.Options) reporttarget.Catalog {
			return reporttarget.Catalog{Status: reporttarget.StatusPass, Candidates: []reporttarget.Candidate{{Eligible: true,
				Device: "/dev/sda1", ConfirmationCode: "RPT-ABC123", MediaIdentity: "private-media-fixture", FingerprintKeyID: "private-key-fixture"}}}
		},
		authorize: func(context.Context, reporttarget.Options, reporttarget.AuthorizationRequest) (reporttarget.Authorization, error) {
			t.Fatal("unexpected authorization")
			return reporttarget.Authorization{}, nil
		},
		mount: func(context.Context, reporttarget.Options, reporttarget.Authorization) (func() error, error) {
			t.Fatal("unexpected writable mount")
			return nil, nil
		},
		persist: func(context.Context, reporttarget.Options, reporttarget.Authorization, *os.File, int64, string, [sha256.Size]byte) (reporttarget.Receipt, error) {
			t.Fatal("unexpected persistent write")
			return reporttarget.Receipt{}, nil
		},
		openAudit: func(path string) (io.Writer, func(), error) {
			if path != "/dev/console" {
				t.Fatal("audit path changed")
			}
			return audit, func() {}, nil
		},
	}
}

type failingSaveAudit struct{ phase string }

func (writer *failingSaveAudit) Write(data []byte) (int, error) {
	marker := map[string]string{"candidate": "HHPT_SAVE_CANDIDATE", "confirm": "HHPT_SAVE_CONFIRM", "rejection": "HHPT_SAVE_REJECTED", "receipt": "HHPT_SAVE_SAVED"}[writer.phase]
	if strings.HasPrefix(string(data), marker) {
		return 0, errors.New("fixture write failure")
	}
	return len(data), nil
}
