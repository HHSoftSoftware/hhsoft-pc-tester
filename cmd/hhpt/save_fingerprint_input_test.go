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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

type saveFingerprintConsole struct {
	reader *strings.Reader
	reads  int
}

func (input *saveFingerprintConsole) Read(data []byte) (int, error) {
	input.reads++
	return input.reader.Read(data)
}

type saveFingerprintFixture struct {
	t                                                       *testing.T
	ctx                                                     context.Context
	args                                                    []string
	key, original, saved                                    []byte
	keyPath, sourcePath, name                               string
	digest                                                  [sha256.Size]byte
	input                                                   *retestInputFixture
	acquire                                                 retestInputReader
	ops                                                     saveOperations
	console                                                 *saveFingerprintConsole
	stdout, stderr, audit                                   bytes.Buffer
	reads, auditOpens, auditCloses                          int
	discoveries, authorizations, mounts, persists, unmounts int
	order                                                   []string
	source                                                  *os.File
	failFirstPersist                                        bool
}

func newSaveFingerprintFixture(t *testing.T, linked bool) *saveFingerprintFixture {
	t.Helper()
	parent := comparisonInputReport(t, true, 0x61, 0x28, "save-fixture", nil)
	signed := parent
	if linked {
		previous := report.PreviousReport{
			ManifestDigest: parent.artifact.Envelope.ManifestDigest,
			BundleSHA256:   fmt.Sprintf("sha256:%x", parent.artifact.BundleSHA256),
		}
		signed = comparisonInputReport(t, true, 0x62, 0x28, "save-fixture", &previous)
	}
	original := append([]byte(nil), signed.encoded...)
	digest := sha256.Sum256(original)
	verified, err := bundle.VerifySelfIssued(bytes.NewReader(original), int64(len(original)))
	if err != nil || verified.BundleSHA256 != digest || verified.Envelope.ManifestDigest != signed.artifact.Envelope.ManifestDigest {
		t.Fatal("valid independently verified source prerequisite failed")
	}
	if linked {
		want := &report.PreviousReport{ManifestDigest: parent.artifact.Envelope.ManifestDigest, BundleSHA256: fmt.Sprintf("sha256:%x", parent.artifact.BundleSHA256)}
		if !reflect.DeepEqual(verified.Manifest.PreviousReport, want) || verified.Envelope.SessionID == parent.artifact.Envelope.SessionID {
			t.Fatal("linked fixture lost its exact parent reference or distinct identity")
		}
	} else if verified.Manifest.PreviousReport != nil {
		t.Fatal("legacy fixture unexpectedly contains a parent reference")
	}
	directory := t.TempDir()
	h := &saveFingerprintFixture{
		t: t, ctx: context.WithValue(context.Background(), struct{}{}, "save-context"),
		key: []byte("12345678901234567890123456789012"), original: original, digest: digest,
		keyPath:    filepath.Join(directory, "fingerprint input marker.key"),
		sourcePath: filepath.Join(directory, "source.hhpcr"), name: "save-input-fixture.hhpcr",
		input: newRetestInputFixture(32),
	}
	if err := os.WriteFile(h.sourcePath, original, 0600); err != nil {
		t.Fatal(err)
	}
	h.args = []string{"-fingerprint-key", h.keyPath, "-name", h.name, "-audit-output", "/dev/console", h.sourcePath}
	h.input.readable.data = append([]byte(nil), h.key...)
	h.acquire = func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
		return readRetestInputWithOperations(ctx, path, kind, h.input.operations())
	}
	h.ops = saveOperations{
		readInput: h.read, discover: h.discover, authorize: h.authorize,
		mount: h.mount, persist: h.persist, openAudit: h.openAudit,
	}
	return h
}

func (h *saveFingerprintFixture) openAudit(path string) (io.Writer, func(), error) {
	h.t.Helper()
	h.auditOpens++
	h.order = append(h.order, "audit.open")
	if path != "/dev/console" {
		h.t.Fatal("audit path changed")
	}
	writer := recoveryAuditWriter{buffer: &h.audit, onWrite: func(value string) error {
		if strings.HasPrefix(value, "HHPT_SAVE_SAVED ") {
			if h.persists == 0 || h.unmounts != h.persists {
				h.t.Fatal("saved audit preceded persistence or unmount")
			}
			h.order = append(h.order, "audit.saved")
		}
		return nil
	}}
	return writer, func() {
		h.auditCloses++
		h.order = append(h.order, "audit.close")
	}, nil
}

func (h *saveFingerprintFixture) read(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
	h.t.Helper()
	h.reads++
	h.order = append(h.order, "key.read")
	if ctx != h.ctx || path != h.keyPath || kind != retestFingerprintKeyInput || h.auditOpens != 1 || h.auditCloses != 0 {
		h.t.Fatal("key reader lost original context, path, kind or audit precedence")
	}
	return h.acquire(ctx, path, kind)
}

func (h *saveFingerprintFixture) checkConsumer(ctx context.Context, options reporttarget.Options) {
	h.t.Helper()
	if ctx != h.ctx || options.Root != "/" || !bytes.Equal(options.FingerprintKey, h.key) || h.reads != 1 || h.auditCloses != 0 {
		h.t.Fatal("consumer received incorrect context/key or acquisition order")
	}
	if h.input != nil {
		assertRetestInputLedger(h.t, h.input, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
	}
}

func (h *saveFingerprintFixture) discover(ctx context.Context, options reporttarget.Options) reporttarget.Catalog {
	h.checkConsumer(ctx, options)
	h.discoveries++
	h.order = append(h.order, "discover")
	return reportTargetFingerprintCatalog()
}

func (h *saveFingerprintFixture) authorize(ctx context.Context, options reporttarget.Options, request reporttarget.AuthorizationRequest) (reporttarget.Authorization, error) {
	h.checkConsumer(ctx, options)
	h.authorizations++
	h.order = append(h.order, "authorize")
	if request != (reporttarget.AuthorizationRequest{Device: "/dev/sdb1", ConfirmationCode: "RPT-15B6C5"}) {
		h.t.Fatal("authorization did not receive the exact selected confirmation")
	}
	authorization, err := reporttarget.NewAuthorization(reportTargetFingerprintCatalog().Candidates[0], "11111111-1111-4111-8111-111111111111", 12, options.FingerprintKey)
	if err != nil || !reflect.DeepEqual(authorization, reportTargetFingerprintAuthorization()) {
		h.t.Fatal("authorization differs from the independent fixed HMAC vector")
	}
	return authorization, nil
}

func (h *saveFingerprintFixture) mount(ctx context.Context, options reporttarget.Options, authorization reporttarget.Authorization) (func() error, error) {
	h.checkConsumer(ctx, options)
	if !reflect.DeepEqual(authorization, reportTargetFingerprintAuthorization()) {
		h.t.Fatal("mount authorization changed")
	}
	h.mounts++
	h.order = append(h.order, "mount")
	return func() error {
		if strings.Contains(h.audit.String(), "HHPT_SAVE_SAVED") {
			h.t.Fatal("success preceded unmount")
		}
		h.unmounts++
		h.order = append(h.order, "unmount")
		return nil
	}, nil
}

func (h *saveFingerprintFixture) persist(ctx context.Context, options reporttarget.Options, authorization reporttarget.Authorization, source *os.File, size int64, name string, digest [sha256.Size]byte) (reporttarget.Receipt, error) {
	h.checkConsumer(ctx, options)
	h.persists++
	h.order = append(h.order, "persist")
	if source == nil || size != int64(len(h.original)) || name != h.name || digest != h.digest || !reflect.DeepEqual(authorization, reportTargetFingerprintAuthorization()) {
		h.t.Fatal("persistence arguments changed")
	}
	if h.source == nil {
		h.source = source
	} else if h.source != source {
		h.t.Fatal("retry replaced the original source descriptor")
	}
	encoded, err := io.ReadAll(io.NewSectionReader(source, 0, size+1))
	if err != nil || !bytes.Equal(encoded, h.original) || sha256.Sum256(encoded) != h.digest {
		h.t.Fatal("persistence did not receive the exact verified report bytes")
	}
	if h.failFirstPersist && h.persists == 1 {
		return reporttarget.Receipt{}, errors.New("injected save fixture failure")
	}
	h.saved = append([]byte(nil), encoded...)
	return reporttarget.Receipt{
		File: name, Bytes: size, SHA256: fmt.Sprintf("sha256:%x", digest),
		AuthorizationID: authorization.ID, Device: authorization.Device.Device,
		FileSynced: true, ReadBackVerified: true, DirectorySynced: true,
	}, nil
}

func (h *saveFingerprintFixture) run(input string) int {
	h.console = &saveFingerprintConsole{reader: strings.NewReader(input)}
	return runSaveWith(h.ctx, h.args, h.console, &h.stdout, &h.stderr, h.ops)
}

func (h *saveFingerprintFixture) preserved() {
	h.t.Helper()
	current, err := os.ReadFile(h.sourcePath)
	if err != nil || !bytes.Equal(current, h.original) {
		h.t.Fatal("source report changed")
	}
	for _, output := range [][]byte{h.stdout.Bytes(), h.stderr.Bytes(), h.audit.Bytes()} {
		if bytes.Contains(output, h.key) || bytes.Contains(output, []byte(h.keyPath)) || bytes.Contains(output, []byte("provider-detail-marker")) {
			h.t.Fatal("key acquisition detail leaked")
		}
	}
}

func (h *saveFingerprintFixture) rejected(code int, diagnostic string, reads, auditOpens, auditCloses int) {
	h.t.Helper()
	if code != 1 || h.reads != reads || h.auditOpens != auditOpens || h.auditCloses != auditCloses {
		h.t.Fatalf("rejection counts: code=%d reads=%d audit=%d/%d", code, h.reads, h.auditOpens, h.auditCloses)
	}
	if h.discoveries != 0 || h.authorizations != 0 || h.mounts != 0 || h.persists != 0 || h.unmounts != 0 || h.console.reads != 0 {
		h.t.Fatal("key failure reached later effects")
	}
	if h.stdout.Len() != 0 || h.audit.Len() != 0 || h.stderr.String() != diagnostic+"\n" {
		h.t.Fatalf("unexpected failure output: stdout=%q stderr=%q audit=%q", h.stdout.String(), h.stderr.String(), h.audit.String())
	}
	h.preserved()
}

func (h *saveFingerprintFixture) savedSuccessfully(code, attempts int) {
	h.t.Helper()
	if code != 0 || h.reads != 1 || h.auditOpens != 1 || h.auditCloses != 1 || h.discoveries != attempts || h.authorizations != attempts || h.mounts != attempts || h.persists != attempts || h.unmounts != attempts || !bytes.Equal(h.saved, h.original) {
		h.t.Fatalf("save counts/bytes: code=%d reads=%d audit=%d/%d effects=%d/%d/%d/%d/%d", code, h.reads, h.auditOpens, h.auditCloses, h.discoveries, h.authorizations, h.mounts, h.persists, h.unmounts)
	}
	order := []string{"audit.open", "key.read"}
	audit := ""
	for attempt := 0; attempt < attempts; attempt++ {
		order = append(order, "discover", "authorize", "mount", "persist", "unmount")
		audit += "HHPT_SAVE_CANDIDATE index=1 device=/dev/sdb1 confirmation=RPT-15B6C5\n"
		audit += "HHPT_SAVE_CONFIRM index=1 device=/dev/sdb1 confirmation=RPT-15B6C5\n"
		if attempt+1 < attempts {
			audit += "HHPT_SAVE_RETRY reason=persistence\n"
		}
	}
	order = append(order, "audit.saved", "audit.close")
	audit += fmt.Sprintf("HHPT_SAVE_SAVED file=%s bytes=%d sha256=%x\n", h.name, len(h.original), h.digest)
	if !reflect.DeepEqual(h.order, order) || h.audit.String() != audit {
		h.t.Fatalf("save order/audit mismatch: %v / %q", h.order, h.audit.String())
	}
	ending := fmt.Sprintf("Saved and read-back verified: /run/hhpt/report-target/%s (%d bytes, sha256:%x)\n", h.name, len(h.original), h.digest)
	wantError := ""
	if attempts == 2 {
		wantError = "save report: injected save fixture failure\n"
	}
	if !strings.HasSuffix(h.stdout.String(), ending) || h.stderr.String() != wantError {
		h.t.Fatal("save announcement or retained error changed")
	}
	artifact, err := bundle.VerifySelfIssued(bytes.NewReader(h.saved), int64(len(h.saved)))
	if err != nil || artifact.BundleSHA256 != h.digest {
		h.t.Fatal("copied self-issued report no longer verifies")
	}
	h.preserved()
}

func TestSaveFingerprintClosedBeforeLegacyAndLinkedConsumers(t *testing.T) {
	for _, profile := range []string{"legacy", "linked"} {
		for _, outcome := range []string{"quit", "eof", "save"} {
			t.Run(profile+"/"+outcome, func(t *testing.T) {
				h := newSaveFingerprintFixture(t, profile == "linked")
				input := map[string]string{"quit": "Q\n", "eof": "", "save": "1\nRPT-15B6C5\n"}[outcome]
				code := h.run(input)
				if outcome == "save" {
					h.savedSuccessfully(code, 1)
				} else {
					if code != 0 || h.reads != 1 || h.discoveries != 1 || h.authorizations != 0 || h.mounts != 0 || h.persists != 0 || h.unmounts != 0 || h.auditCloses != 1 || h.stderr.Len() != 0 {
						t.Fatal("initial quit/EOF changed")
					}
					if h.audit.String() != "HHPT_SAVE_CANDIDATE index=1 device=/dev/sdb1 confirmation=RPT-15B6C5\n" {
						t.Fatal("initial quit/EOF audit changed")
					}
					if outcome == "quit" && !strings.Contains(h.stdout.String(), "no storage device was written") {
						t.Fatal("initial quit omitted the no-write result")
					}
					h.preserved()
				}
				assertRetestInputLedger(t, h.input, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
				if !reflect.DeepEqual(h.input.readable.readSize, []int{32, 1}) {
					t.Fatal("successful key omitted exact read and terminal probe")
				}
			})
		}
	}
}

func TestSaveFingerprintCloseFaultPrecedesConsumersAndCancellation(t *testing.T) {
	for _, owner := range []string{"readable", "metadata"} {
		for _, timing := range []string{"live", "cancel"} {
			t.Run(owner+"/"+timing, func(t *testing.T) {
				h := newSaveFingerprintFixture(t, false)
				ctx, cancel := context.WithCancel(h.ctx)
				defer cancel()
				h.ctx = ctx
				file := h.input.readable
				if owner == "metadata" {
					file = h.input.metadata
				}
				file.closeErr = errors.New("provider-detail-marker")
				if timing == "cancel" {
					file.hook = func(operation string) {
						if operation == "close" {
							cancel()
						}
					}
				}
				h.rejected(h.run("1\nRPT-15B6C5\n"), "read fingerprint key: input acquisition failed", 1, 1, 1)
				assertRetestInputLedger(t, h.input, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
				if !reflect.DeepEqual(h.input.readable.readSize, []int{32, 1}) {
					t.Fatal("Close fault did not follow successful bounded acquisition")
				}
			})
		}
	}
}

func TestSaveFingerprintAcquiredOwnersIdentityAndReadFaults(t *testing.T) {
	for _, fault := range []string{"metadata_error", "readable_error", "nil_metadata", "typed_nil_metadata", "nil_readable", "typed_nil_readable", "aliased_owner", "non_regular", "identity", "hard_read", "hard_read_cancel"} {
		t.Run(fault, func(t *testing.T) {
			h := newSaveFingerprintFixture(t, false)
			want := map[string]int{"metadata": 1, "readable": 1}
			closes := []string{"readable", "metadata"}
			switch fault {
			case "metadata_error":
				h.input.metadataErr = errors.New("provider-detail-marker")
				want, closes = map[string]int{"metadata": 1}, []string{"metadata"}
			case "readable_error":
				h.input.readableErr = errors.New("provider-detail-marker")
			case "nil_metadata", "typed_nil_metadata":
				h.input.metadataOut = nil
				if fault == "typed_nil_metadata" {
					h.input.metadataOut = (*retestInputTestFile)(nil)
				}
				want, closes = map[string]int{}, nil
			case "nil_readable", "typed_nil_readable", "aliased_owner":
				h.input.readableOut = nil
				if fault == "typed_nil_readable" {
					h.input.readableOut = (*retestInputTestFile)(nil)
				}
				if fault == "aliased_owner" {
					h.input.readableOut = h.input.metadata
				}
				want, closes = map[string]int{"metadata": 1}, []string{"metadata"}
			case "non_regular":
				h.input.metadata.info.mode = os.ModeNamedPipe
				want, closes = map[string]int{"metadata": 1}, []string{"metadata"}
			case "identity":
				info := *h.input.readable.info
				info.id++
				h.input.readable.info = &info
			case "hard_read", "hard_read_cancel":
				h.input.readable.reads = []retestInputTestRead{{0, errors.New("provider-detail-marker")}}
				if fault == "hard_read_cancel" {
					ctx, cancel := context.WithCancel(h.ctx)
					defer cancel()
					h.ctx = ctx
					h.input.readable.hook = func(operation string) {
						if operation == "read" {
							cancel()
						}
					}
				}
			}
			h.rejected(h.run("1\nRPT-15B6C5\n"), "read fingerprint key: input acquisition failed", 1, 1, 1)
			assertRetestInputLedger(t, h.input, want, closes)
			if !strings.HasPrefix(fault, "hard_read") && len(h.input.readable.readSize) != 0 {
				t.Fatal("ownership/identity failure reached a readable operation")
			}
			if fault == "metadata_error" || fault == "nil_metadata" || fault == "typed_nil_metadata" || fault == "non_regular" {
				for _, call := range h.input.ledger.calls {
					if call == "open.readable" {
						t.Fatal("metadata failure acquired a later owner")
					}
				}
			}
		})
	}
}

func TestSaveFingerprintTerminalProbeFaults(t *testing.T) {
	for _, fault := range []string{"extra_byte", "no_progress", "hard_error"} {
		t.Run(fault, func(t *testing.T) {
			h := newSaveFingerprintFixture(t, false)
			probe := retestInputTestRead{1, io.EOF}
			if fault == "no_progress" {
				probe = retestInputTestRead{0, nil}
			}
			if fault == "hard_error" {
				probe = retestInputTestRead{0, errors.New("provider-detail-marker")}
			}
			h.input.readable.reads = []retestInputTestRead{{32, nil}, probe}
			h.rejected(h.run("1\nRPT-15B6C5\n"), "read fingerprint key: input acquisition failed", 1, 1, 1)
			assertRetestInputLedger(t, h.input, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
			if !reflect.DeepEqual(h.input.readable.readSize, []int{32, 1}) {
				t.Fatal("fault did not reach the terminal one-byte probe")
			}
		})
	}
}

func TestSaveFingerprintShapeAndHardErrorsPrecedeLateCancellation(t *testing.T) {
	for _, fault := range []string{"nil", "short", "long", "hard", "hard_bytes", "joined", "cleanup", "active_context_error", "cancel_empty", "cancel_bytes"} {
		for _, timing := range []string{"live", "cancel"} {
			t.Run(fault+"/"+timing, func(t *testing.T) {
				h := newSaveFingerprintFixture(t, false)
				ctx, cancel := context.WithCancel(h.ctx)
				defer cancel()
				h.ctx = ctx
				h.acquire = func(context.Context, string, retestInputKind) ([]byte, error) {
					if timing == "cancel" || fault == "cancel_empty" || fault == "cancel_bytes" {
						cancel()
					}
					switch fault {
					case "nil":
						return nil, nil
					case "short":
						return append([]byte(nil), h.key[:31]...), nil
					case "long":
						return append(append([]byte(nil), h.key...), 1), nil
					case "hard":
						return nil, errors.New("provider-detail-marker")
					case "hard_bytes":
						return h.key, errors.New("provider-detail-marker")
					case "joined":
						return nil, errors.Join(context.Canceled, errors.New("provider-detail-marker"))
					case "cleanup":
						return nil, errors.Join(context.Canceled, errRetestInputCleanup)
					case "active_context_error":
						return nil, context.DeadlineExceeded
					case "cancel_empty":
						return []byte{}, context.Canceled
					default:
						return h.key, context.Canceled
					}
				}
				h.rejected(h.run("1\nRPT-15B6C5\n"), "read fingerprint key: input acquisition failed", 1, 1, 1)
				assertRetestInputLedger(t, h.input, map[string]int{}, nil)
			})
		}
	}
}

type saveFingerprintNilContext struct{ context.Context }

func (*saveFingerprintNilContext) Err() error { panic("typed nil context method called") }

func TestSaveFingerprintUsageAuditAndRequiredDependencyOrder(t *testing.T) {
	for _, usage := range []string{"unknown_flag", "missing_key", "extra_argument"} {
		t.Run(usage, func(t *testing.T) {
			h := newSaveFingerprintFixture(t, false)
			switch usage {
			case "unknown_flag":
				h.args = []string{"-unknown-save-fixture-flag"}
			case "missing_key":
				h.args = []string{h.sourcePath}
			case "extra_argument":
				h.args = append(h.args, "extra")
			}
			if code := h.run("1\n"); code != 2 || h.auditOpens != 0 || h.reads != 0 || h.discoveries != 0 || h.console.reads != 0 || h.stdout.Len() != 0 {
				t.Fatal("usage did not precede acquisition")
			}
			h.preserved()
		})
	}
	for _, fault := range []string{"nil_context", "typed_nil_context", "nil_reader"} {
		for _, audit := range []string{"success", "failure"} {
			t.Run(fault+"/"+audit, func(t *testing.T) {
				h := newSaveFingerprintFixture(t, false)
				switch fault {
				case "nil_context":
					h.ctx = nil
				case "typed_nil_context":
					h.ctx = (*saveFingerprintNilContext)(nil)
				case "nil_reader":
					h.ops.readInput = nil
				}
				diagnostic, closes := "save input acquisition failed", 1
				if audit == "failure" {
					h.ops.openAudit = func(string) (io.Writer, func(), error) {
						h.auditOpens++
						return nil, func() { h.auditCloses++ }, errors.New("fixture audit unavailable")
					}
					diagnostic, closes = "open report-save audit output: fixture audit unavailable", 0
				}
				h.rejected(h.run("1\n"), diagnostic, 0, 1, closes)
			})
		}
	}
}

func TestSaveFingerprintCleanCancellationBoundaries(t *testing.T) {
	for _, stage := range []string{"before", "deadline", "audit", "during_read", "during_deadline", "core_read", "after_closed_read"} {
		t.Run(stage, func(t *testing.T) {
			h := newSaveFingerprintFixture(t, false)
			ctx, cancel := context.WithCancel(h.ctx)
			defer cancel()
			h.ctx = ctx
			wantReads := 0
			switch stage {
			case "before":
				cancel()
			case "deadline":
				expired, stop := context.WithDeadline(h.ctx, time.Unix(1, 0))
				defer stop()
				h.ctx = expired
			case "audit":
				openAudit := h.ops.openAudit
				h.ops.openAudit = func(path string) (io.Writer, func(), error) {
					writer, closeAudit, err := openAudit(path)
					cancel()
					return writer, closeAudit, err
				}
			case "during_read":
				wantReads = 1
				h.acquire = func(context.Context, string, retestInputKind) ([]byte, error) {
					cancel()
					return nil, context.Canceled
				}
			case "during_deadline":
				wantReads = 1
				deadline := &retestInputDeadlineContext{Context: h.ctx}
				h.ctx = deadline
				h.acquire = func(context.Context, string, retestInputKind) ([]byte, error) {
					deadline.stopped = true
					return nil, context.DeadlineExceeded
				}
			case "core_read":
				wantReads = 1
				h.input.readable.hook = func(operation string) {
					if operation == "read" {
						cancel()
					}
				}
			case "after_closed_read":
				wantReads = 1
				acquire := h.acquire
				h.acquire = func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
					data, err := acquire(ctx, path, kind)
					cancel()
					return data, err
				}
			}
			h.rejected(h.run("1\nRPT-15B6C5\n"), "Report saving cancelled; the report remains in volatile memory.", wantReads, 1, 1)
			if stage == "after_closed_read" || stage == "core_read" {
				assertRetestInputLedger(t, h.input, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
			} else {
				assertRetestInputLedger(t, h.input, map[string]int{}, nil)
			}
		})
	}
}

func TestSaveFingerprintLiteralPathAndKeyFailureBeforeSource(t *testing.T) {
	for _, path := range []string{"relative/key", " relative/./key with spaces ", "key/../literal"} {
		t.Run("literal/"+path, func(t *testing.T) {
			h := newSaveFingerprintFixture(t, false)
			h.keyPath, h.args[1] = path, path
			h.savedSuccessfully(h.run("1\nRPT-15B6C5\n"), 1)
		})
	}
	for _, source := range []string{"verified", "missing"} {
		t.Run(source, func(t *testing.T) {
			h := newSaveFingerprintFixture(t, false)
			if source == "missing" {
				h.args[len(h.args)-1] = filepath.Join(t.TempDir(), "absent.hhpcr")
			}
			h.input.readable.closeErr = errors.New("provider-detail-marker")
			h.rejected(h.run("1\nRPT-15B6C5\n"), "read fingerprint key: input acquisition failed", 1, 1, 1)
			assertRetestInputLedger(t, h.input, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
		})
	}
}

func TestSaveFingerprintIsAcquiredOnceAcrossRetry(t *testing.T) {
	for _, profile := range []string{"legacy", "linked"} {
		t.Run(profile, func(t *testing.T) {
			h := newSaveFingerprintFixture(t, profile == "linked")
			h.failFirstPersist = true
			h.savedSuccessfully(h.run("1\nRPT-15B6C5\n1\nRPT-15B6C5\n"), 2)
			assertRetestInputLedger(t, h.input, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
		})
	}
}
