package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

// File-only targets exercise the UI's retry contract without mounting media.
// The first write leaves a real partial file; later writes use exclusive opens.
type saveRecoveryFixture struct {
	arguments                               []string
	operations                              saveOperations
	audit, stdout, stderr                   bytes.Buffer
	destinations                            map[string]string
	authorizations                          []reporttarget.AuthorizationRequest
	discoveries, mounts, persists, unmounts int
	source                                  *os.File
	cleanupError                            error
}

type recoveryAuditWriter struct {
	buffer  *bytes.Buffer
	onWrite func(string) error
}

func (writer recoveryAuditWriter) Write(value []byte) (int, error) {
	if err := writer.onWrite(string(value)); err != nil {
		return 0, err
	}
	return writer.buffer.Write(value)
}

func newSaveRecoveryFixture(t *testing.T) *saveRecoveryFixture {
	t.Helper()
	fixture := &saveRecoveryFixture{arguments: saveTestArguments(t), destinations: map[string]string{
		"/dev/sda1": t.TempDir(), "/dev/sdb1": t.TempDir(),
	}}
	fixture.operations = saveTestOperations(t, &fixture.audit)
	fixture.operations.discover = func(context.Context, reporttarget.Options) reporttarget.Catalog {
		fixture.discoveries++
		secondCode := "RPT-DEF456"
		if fixture.persists > 0 {
			secondCode = "RPT-123ABC"
		}
		return reporttarget.Catalog{Status: reporttarget.StatusPass, Candidates: []reporttarget.Candidate{
			{Eligible: true, Device: "/dev/sda1", ConfirmationCode: "RPT-ABC123"},
			{Eligible: true, Device: "/dev/sdb1", ConfirmationCode: secondCode},
		}}
	}
	fixture.operations.authorize = func(_ context.Context, _ reporttarget.Options, request reporttarget.AuthorizationRequest) (reporttarget.Authorization, error) {
		fixture.authorizations = append(fixture.authorizations, request)
		want := "RPT-ABC123"
		if request.Device == "/dev/sdb1" {
			want = "RPT-123ABC"
		}
		if request.ConfirmationCode != want {
			t.Fatal("a stale or incorrect code reached authorization")
		}
		return reporttarget.Authorization{MountPoint: reporttarget.MountPoint,
			Device: reporttarget.DeviceBinding{Device: request.Device}}, nil
	}
	fixture.operations.mount = func(context.Context, reporttarget.Options, reporttarget.Authorization) (func() error, error) {
		fixture.mounts++
		return func() error {
			fixture.unmounts++
			if strings.Contains(fixture.audit.String(), "HHPT_SAVE_SAVED") {
				t.Fatal("success preceded cleanup")
			}
			return fixture.cleanupError
		}, nil
	}
	fixture.operations.persist = func(_ context.Context, _ reporttarget.Options, authorization reporttarget.Authorization,
		source *os.File, size int64, name string, digest [sha256.Size]byte) (reporttarget.Receipt, error) {
		fixture.persists++
		if fixture.source == nil {
			fixture.source = source
		} else if fixture.source != source {
			t.Fatal("retry reopened the RAM report instead of retaining its verified descriptor")
		}
		path := filepath.Join(fixture.destinations[authorization.Device.Device], name)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return reporttarget.Receipt{}, err
		}
		defer file.Close()
		if fixture.persists == 1 {
			if _, err := io.CopyN(file, io.NewSectionReader(source, 0, size), 31); err != nil {
				t.Fatal(err)
			}
			if _, err := source.Seek(size, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			return reporttarget.Receipt{}, errors.New("injected short report write")
		}
		contents, err := io.ReadAll(io.NewSectionReader(source, 0, size))
		if err != nil || int64(len(contents)) != size || sha256.Sum256(contents) != digest {
			t.Fatal("retry did not receive the complete original verified report")
		}
		if _, err := file.Write(contents); err != nil {
			t.Fatal(err)
		}
		return reporttarget.Receipt{File: name, Bytes: size, SHA256: fmt.Sprintf("sha256:%x", digest),
			FileSynced: true, ReadBackVerified: true, DirectorySynced: true}, nil
	}
	return fixture
}

func (fixture *saveRecoveryFixture) run(t *testing.T, input string) int {
	t.Helper()
	return runSaveWith(t.Context(), fixture.arguments, strings.NewReader(input), &fixture.stdout, &fixture.stderr, fixture.operations)
}

func (fixture *saveRecoveryFixture) checkFiles(t *testing.T, saved bool) {
	t.Helper()
	original, err := os.ReadFile(fixture.arguments[len(fixture.arguments)-1])
	if err != nil {
		t.Fatal(err)
	}
	partial, err := os.ReadFile(filepath.Join(fixture.destinations["/dev/sda1"], "hhpt-save-fixture.hhpcr"))
	if err != nil || !bytes.Equal(partial, original[:31]) {
		t.Fatal("retry changed or deleted the partial report")
	}
	path := filepath.Join(fixture.destinations["/dev/sdb1"], "hhpt-save-fixture.hhpcr")
	if !saved {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("an unconfirmed target received a file")
		}
		return
	}
	contents, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(contents, original) {
		t.Fatal("second target did not receive the complete unchanged report")
	}
	if _, err := bundle.VerifySelfIssued(bytes.NewReader(contents), int64(len(contents))); err != nil {
		t.Fatalf("saved report failed independent bundle verification: %v", err)
	}
}

func TestSaveRecoversToFreshTargetAndConfirmationWithOriginalReport(t *testing.T) {
	fixture := newSaveRecoveryFixture(t)
	if code := fixture.run(t, "1\nRPT-ABC123\n2\nRPT-123ABC\n"); code != 0 {
		t.Fatalf("recovery failed: %d %s", code, fixture.stderr.String())
	}
	if fixture.discoveries != 2 || fixture.mounts != 2 || fixture.persists != 2 || fixture.unmounts != 2 ||
		len(fixture.authorizations) != 2 || fixture.authorizations[1].Device != "/dev/sdb1" ||
		strings.Count(fixture.audit.String(), "HHPT_SAVE_SAVED ") != 1 ||
		strings.Count(fixture.audit.String(), "HHPT_SAVE_RETRY reason=persistence\n") != 1 {
		t.Fatal("recovery did not require a complete second authorized attempt")
	}
	fixture.checkFiles(t, true)
}

func TestSaveRetryAuditFollowsCleanupAndFailureStopsBeforeNewSelection(t *testing.T) {
	for _, auditFails := range []bool{false, true} {
		t.Run(fmt.Sprint(auditFails), func(t *testing.T) {
			fixture := newSaveRecoveryFixture(t)
			retries := 0
			fixture.operations.openAudit = func(string) (io.Writer, func(), error) {
				return recoveryAuditWriter{buffer: &fixture.audit, onWrite: func(value string) error {
					if value == "HHPT_SAVE_RETRY reason=persistence\n" {
						retries++
						if fixture.unmounts != 1 || fixture.discoveries != 1 {
							t.Fatal("retry was announced before cleanup or after rediscovery")
						}
						if auditFails {
							return errors.New("injected retry audit failure")
						}
					}
					return nil
				}}, func() {}, nil
			}
			code := fixture.run(t, "1\nRPT-ABC123\n2\nRPT-123ABC\n")
			if retries != 1 {
				t.Fatal("missing exact retry transition")
			}
			if auditFails {
				if code != 1 || fixture.discoveries != 1 || fixture.mounts != 1 || fixture.persists != 1 ||
					strings.Contains(fixture.audit.String(), "HHPT_SAVE_SAVED") {
					t.Fatal("failed retry audit permitted another selection or write")
				}
			} else if code != 0 {
				t.Fatal("successful retry audit blocked recovery")
			}
			fixture.checkFiles(t, !auditFails)
		})
	}
}

func TestSaveRetryNeverOverwritesThePreservedPartialReport(t *testing.T) {
	fixture := newSaveRecoveryFixture(t)
	if code := fixture.run(t, "1\nRPT-ABC123\n1\nRPT-ABC123\n2\nRPT-123ABC\n"); code != 0 {
		t.Fatalf("recovery after exclusive-create failure: %d %s", code, fixture.stderr.String())
	}
	if fixture.persists != 3 || fixture.unmounts != 3 || strings.Count(fixture.stdout.String(), "Save failed;") != 2 {
		t.Fatal("same-name partial was overwritten or its failure was concealed")
	}
	fixture.checkFiles(t, true)
}

func TestSaveFailedAttemptNeedsNewIntentAndTruthfulQuit(t *testing.T) {
	for _, tail := range []string{"Q\n", "", "\n", "2\nRPT-DEF456\nQ\n", "2\nRPT-000000\n", "9\nQ\n"} {
		t.Run(fmt.Sprintf("input-%x", tail), func(t *testing.T) {
			fixture := newSaveRecoveryFixture(t)
			if code := fixture.run(t, "1\nRPT-ABC123\n"+tail); code != 1 {
				t.Fatalf("failed attempt became success on quit/EOF: %d", code)
			}
			text := fixture.stdout.String() + fixture.stderr.String()
			if fixture.mounts != 1 || fixture.persists != 1 || fixture.unmounts != 1 || len(fixture.authorizations) != 1 ||
				strings.Contains(fixture.audit.String(), "HHPT_SAVE_SAVED") || strings.Contains(text, "no storage device was written") ||
				!strings.Contains(text, "Earlier target data may have changed") {
				t.Fatal("retry reused intent or hid possible partial writes")
			}
			fixture.checkFiles(t, false)
		})
	}
}

func TestSaveInitialQuitAndEOFRemainNoWriteExits(t *testing.T) {
	for _, input := range []string{"Q\n", ""} {
		fixture := newSaveRecoveryFixture(t)
		if code := fixture.run(t, input); code != 0 || fixture.mounts != 0 || fixture.persists != 0 {
			t.Fatal("initial quit/EOF changed its no-write exit behavior")
		}
		if input != "" && !strings.Contains(fixture.stdout.String(), "no storage device was written") {
			t.Fatal("initial quit lost its no-write explanation")
		}
	}
}

func TestSaveCleanupFailureIsTerminalAndKeepsBothErrors(t *testing.T) {
	fixture := newSaveRecoveryFixture(t)
	fixture.cleanupError = errors.New("injected unmount failure")
	if code := fixture.run(t, "1\nRPT-ABC123\n2\nRPT-123ABC\n"); code != 1 {
		t.Fatal("failed cleanup accepted a retry")
	}
	text := fixture.stderr.String()
	if fixture.discoveries != 1 || fixture.mounts != 1 || fixture.persists != 1 || fixture.unmounts != 1 ||
		!strings.Contains(text, "injected short report write") || !strings.Contains(text, "injected unmount failure") ||
		strings.Contains(text, "data was read-back verified") || strings.Contains(fixture.audit.String(), "HHPT_SAVE_SAVED") ||
		strings.Contains(fixture.audit.String(), "HHPT_SAVE_RETRY") {
		t.Fatal("dual failure lost cleanup ownership or made a false verification claim")
	}
	fixture.checkFiles(t, false)
}

func TestSaveMountCleanupUncertaintyIsTerminal(t *testing.T) {
	fixture := newSaveRecoveryFixture(t)
	fixture.operations.mount = func(context.Context, reporttarget.Options, reporttarget.Authorization) (func() error, error) {
		fixture.mounts++
		return nil, errors.Join(errors.New("injected mount failure"), errReportMountCleanup)
	}
	if code := fixture.run(t, "1\nRPT-ABC123\n2\nRPT-123ABC\n"); code != 1 ||
		fixture.discoveries != 1 || fixture.mounts != 1 || fixture.persists != 0 ||
		!strings.Contains(fixture.stderr.String(), "cleanup is uncertain") {
		t.Fatal("unconfirmed cleanup after mount failure permitted further writes")
	}
}

func TestSaveCancellationStopsAtEveryOperationBoundary(t *testing.T) {
	for _, phase := range []string{"before-discovery", "discovery", "authorization", "mount", "persist", "unmount"} {
		t.Run(phase, func(t *testing.T) {
			fixture := newSaveRecoveryFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if phase == "before-discovery" {
				cancel()
			}
			discover, authorize, mount, persist := fixture.operations.discover, fixture.operations.authorize, fixture.operations.mount, fixture.operations.persist
			fixture.operations.discover = func(ctx context.Context, options reporttarget.Options) reporttarget.Catalog {
				result := discover(ctx, options)
				if phase == "discovery" {
					cancel()
				}
				return result
			}
			fixture.operations.authorize = func(ctx context.Context, options reporttarget.Options, request reporttarget.AuthorizationRequest) (reporttarget.Authorization, error) {
				result, err := authorize(ctx, options, request)
				if phase == "authorization" {
					cancel()
				}
				return result, err
			}
			fixture.operations.mount = func(ctx context.Context, options reporttarget.Options, authorization reporttarget.Authorization) (func() error, error) {
				unmount, err := mount(ctx, options, authorization)
				if phase == "mount" {
					cancel()
				}
				return func() error {
					if phase == "unmount" {
						cancel()
					}
					return unmount()
				}, err
			}
			fixture.operations.persist = func(ctx context.Context, options reporttarget.Options, authorization reporttarget.Authorization, source *os.File, size int64, name string, digest [sha256.Size]byte) (reporttarget.Receipt, error) {
				result, err := persist(ctx, options, authorization, source, size, name, digest)
				if phase == "persist" {
					cancel()
				}
				return result, err
			}
			code := runSaveWith(ctx, fixture.arguments, strings.NewReader("1\nRPT-ABC123\n2\nRPT-123ABC\n"), &fixture.stdout, &fixture.stderr, fixture.operations)
			wantDiscover, wantAuthorize, wantMount, wantPersist := 1, 1, 1, 1
			switch phase {
			case "before-discovery":
				wantDiscover, wantAuthorize, wantMount, wantPersist = 0, 0, 0, 0
			case "discovery":
				wantAuthorize, wantMount, wantPersist = 0, 0, 0
			case "authorization":
				wantMount, wantPersist = 0, 0
			case "mount":
				wantPersist = 0
			}
			if code != 1 || fixture.discoveries != wantDiscover || len(fixture.authorizations) != wantAuthorize ||
				fixture.mounts != wantMount || fixture.persists != wantPersist || fixture.unmounts != wantMount ||
				strings.Contains(fixture.audit.String(), "HHPT_SAVE_SAVED") || strings.Contains(fixture.audit.String(), "HHPT_SAVE_RETRY") ||
				!strings.Contains(fixture.stderr.String(), "cancelled") {
				t.Fatalf("cancelled flow continued or skipped cleanup: code=%d discover=%d authorize=%d mount=%d persist=%d unmount=%d", code,
					fixture.discoveries, len(fixture.authorizations), fixture.mounts, fixture.persists, fixture.unmounts)
			}
		})
	}
}

func TestSaveCancellationInterruptsWaitingForRenewedConfirmation(t *testing.T) {
	fixture := newSaveRecoveryFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	confirming := make(chan struct{}, 1)
	fixture.operations.openAudit = func(string) (io.Writer, func(), error) {
		return recoveryAuditWriter{buffer: &fixture.audit, onWrite: func(value string) error {
			if value == "HHPT_SAVE_CONFIRM index=2 device=/dev/sdb1 confirmation=RPT-123ABC\n" {
				confirming <- struct{}{}
			}
			return nil
		}}, func() {}, nil
	}
	done := make(chan int, 1)
	go func() {
		done <- runSaveWith(ctx, fixture.arguments, input, &fixture.stdout, &fixture.stderr, fixture.operations)
	}()
	written := make(chan error, 1)
	go func() {
		_, err := io.WriteString(writer, "1\nRPT-ABC123\n2\n")
		written <- err
	}()
	select {
	case err := <-written:
		if err != nil {
			t.Fatal(err)
		}
	case code := <-done:
		t.Fatalf("CLI exited before consuming retry intent: %d", code)
	case <-time.After(3 * time.Second):
		t.Fatal("CLI did not consume retry intent")
	}
	// The audit transition proves the first attempt was cleaned up and the
	// second target is waiting for renewed confirmation, not buffered input.
	select {
	case <-confirming:
	case code := <-done:
		t.Fatalf("CLI exited before requesting renewed confirmation: %d", code)
	case <-time.After(3 * time.Second):
		t.Fatal("CLI did not request renewed confirmation")
	}
	cancel()
	select {
	case code := <-done:
		if code != 1 || fixture.mounts != 1 || len(fixture.authorizations) != 1 || fixture.unmounts != 1 ||
			strings.Contains(fixture.audit.String(), "HHPT_SAVE_SAVED") {
			t.Fatal("cancelled console wait authorized another write or skipped cleanup")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation remained blocked on console input")
	}
	fixture.checkFiles(t, false)
}
