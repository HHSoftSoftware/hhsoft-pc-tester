package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

func reportTargetFingerprintCatalog() reporttarget.Catalog {
	return reporttarget.Catalog{
		Schema: reporttarget.CatalogSchema, Status: reporttarget.StatusPass,
		Candidates: []reporttarget.Candidate{{
			Device: "/dev/sdb1", Parent: "/dev/sdb", Topology: "/sys/devices/pci/usb1/1-1/block/sdb/sdb1",
			DeviceMajorMinor: "8:17", ParentMajorMinor: "8:16", SizeBytes: 33554432, ParentSizeBytes: 67108864,
			FingerprintKeyID: "hmac-sha256:" + strings.Repeat("1", 64),
			MediaIdentity:    "hmac-sha256:" + strings.Repeat("2", 64), MediaIdentitySource: "serial",
			ConfirmationCode: "RPT-15B6C5", Eligible: true, EligibleReadOnly: true, NodeReady: true,
		}},
	}
}

func reportTargetFingerprintAuthorization() reporttarget.Authorization {
	// The fixed HMAC-SHA256 vector is independent of NewAuthorization.
	return reporttarget.Authorization{
		Schema: reporttarget.AuthorizationSchema,
		ID:     "hmac-sha256:7def6eead564e30c7e65c48bf31e45046e98b3b07350823133a81e467b355b3a",
		BootID: "11111111-1111-4111-8111-111111111111", IssuedBootSeconds: 12, ExpiresBootSeconds: 912,
		Device: reporttarget.DeviceBinding{
			Device: "/dev/sdb1", Parent: "/dev/sdb", Topology: "/sys/devices/pci/usb1/1-1/block/sdb/sdb1",
			DeviceMajorMinor: "8:17", ParentMajorMinor: "8:16", SizeBytes: 33554432, ParentSizeBytes: 67108864,
			FingerprintKeyID: "hmac-sha256:" + strings.Repeat("1", 64),
			MediaIdentity:    "hmac-sha256:" + strings.Repeat("2", 64), MediaIdentitySource: "serial",
		},
		MountPoint: "/run/hhpt/report-target", RequiredMountOptions: []string{"nodev", "noexec", "nosuid", "rw"},
		AllowedFileSystemTypes: []string{"exfat", "ext4", "vfat"},
	}
}

type reportTargetFingerprintFixture struct {
	t                                           *testing.T
	ctx                                         context.Context
	key                                         []byte
	keyPath, outputPath                         string
	authorization                               bool
	input                                       *retestInputFixture
	catalog                                     reporttarget.Catalog
	authorizationErr                            error
	consumerHook                                func()
	reads, discoveries, authorizations, outputs int
	stdout, stderr                              bytes.Buffer
}

func newReportTargetFingerprintFixture(t *testing.T, authorization bool) *reportTargetFingerprintFixture {
	t.Helper()
	h := &reportTargetFingerprintFixture{
		t: t, ctx: context.WithValue(context.Background(), struct{}{}, "command-context"),
		key: []byte("12345678901234567890123456789012"), keyPath: "private-fingerprint-fixture", outputPath: "-",
		authorization: authorization, input: newRetestInputFixture(32), catalog: reportTargetFingerprintCatalog(),
	}
	h.input.readable.data = append([]byte(nil), h.key...)
	// Every negative authorization case starts with a structurally valid literal.
	encoded, err := json.Marshal(reportTargetFingerprintAuthorization())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := reporttarget.DecodeAuthorization(encoded)
	if err != nil || !reflect.DeepEqual(decoded, reportTargetFingerprintAuthorization()) {
		t.Fatal("invalid authorization prerequisite", err)
	}
	return h
}

func (h *reportTargetFingerprintFixture) arguments() []string {
	arguments := []string{"--fingerprint-key", h.keyPath, "--output", h.outputPath}
	if h.authorization {
		arguments = append(arguments, "--device", "/dev/sdb1", "--confirm", "RPT-15B6C5")
	}
	return arguments
}

func (h *reportTargetFingerprintFixture) read(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
	h.t.Helper()
	h.reads++
	if ctx != h.ctx || path != h.keyPath || kind != retestFingerprintKeyInput {
		h.t.Fatal("fingerprint reader arguments changed")
	}
	return readRetestInputWithOperations(ctx, path, kind, h.input.operations())
}

func (h *reportTargetFingerprintFixture) checkConsumer(ctx context.Context, options reporttarget.Options) {
	h.t.Helper()
	if ctx != h.ctx || options.Root != "/" || !bytes.Equal(options.FingerprintKey, h.key) {
		h.t.Fatal("consumer context, root or fingerprint changed")
	}
	assertRetestInputLedger(h.t, h.input, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
	if h.consumerHook != nil {
		h.consumerHook()
	}
}

func (h *reportTargetFingerprintFixture) discover(ctx context.Context, options reporttarget.Options) reporttarget.Catalog {
	h.discoveries++
	if h.authorization {
		h.t.Fatal("authorization entered catalog command branch")
	}
	h.checkConsumer(ctx, options)
	return h.catalog
}

func (h *reportTargetFingerprintFixture) authorize(ctx context.Context, options reporttarget.Options, request reporttarget.AuthorizationRequest) (reporttarget.Authorization, error) {
	h.authorizations++
	if !h.authorization || request != (reporttarget.AuthorizationRequest{Device: "/dev/sdb1", ConfirmationCode: "RPT-15B6C5"}) {
		h.t.Fatal("authorization branch or request changed")
	}
	h.checkConsumer(ctx, options)
	if h.authorizationErr != nil {
		return reporttarget.Authorization{}, h.authorizationErr
	}
	return reporttarget.NewAuthorization(reportTargetFingerprintCatalog().Candidates[0], "11111111-1111-4111-8111-111111111111", 12, options.FingerprintKey)
}

func (h *reportTargetFingerprintFixture) destination(path string, stdout io.Writer, write func(io.Writer) error) error {
	h.outputs++
	if path != h.outputPath {
		h.t.Fatal("destination path changed")
	}
	return writeDestination(path, stdout, write)
}

func (h *reportTargetFingerprintFixture) run(reader retestInputReader, destination destinationWriter) int {
	return runReportTargetWith(h.ctx, h.arguments(), &h.stdout, &h.stderr, reader, h.discover, h.authorize, destination)
}

func (h *reportTargetFingerprintFixture) noLaterEffects(code int, diagnostic string) {
	h.t.Helper()
	if code != 1 || h.discoveries != 0 || h.authorizations != 0 || h.outputs != 0 || h.stdout.Len() != 0 || h.stderr.String() != diagnostic+"\n" {
		h.t.Fatalf("rejection code=%d discover=%d authorize=%d output=%d stdout=%d stderr=%q", code, h.discoveries, h.authorizations, h.outputs, h.stdout.Len(), h.stderr.String())
	}
	for _, forbidden := range [][]byte{h.key, []byte(h.keyPath), []byte("provider-detail-marker")} {
		if bytes.Contains(h.stderr.Bytes(), forbidden) {
			h.t.Fatal("input diagnostic disclosed input data")
		}
	}
}

func assertReportTargetFingerprintJSON(t *testing.T, encoded []byte, authorization bool, catalog reporttarget.Catalog) {
	t.Helper()
	if len(encoded) == 0 || encoded[len(encoded)-1] != '\n' {
		t.Fatal("missing newline-terminated result")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if authorization {
		var got reporttarget.Authorization
		if err := decoder.Decode(&got); err != nil || !reflect.DeepEqual(got, reportTargetFingerprintAuthorization()) {
			t.Fatalf("authorization differs from fixed vector: %+v / %v", got, err)
		}
	} else {
		var got reporttarget.Catalog
		if err := decoder.Decode(&got); err != nil || !reflect.DeepEqual(got, catalog) {
			t.Fatalf("catalog changed: %+v / %v", got, err)
		}
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatal("extra output after result", err)
	}
}

func (h *reportTargetFingerprintFixture) success(code int) {
	h.t.Helper()
	discoveries, authorizations := 1, 0
	if h.authorization {
		discoveries, authorizations = 0, 1
	}
	if code != 0 || h.reads != 1 || h.discoveries != discoveries || h.authorizations != authorizations || h.outputs != 1 || h.stderr.Len() != 0 {
		h.t.Fatalf("completion code=%d read=%d discover=%d authorize=%d output=%d stderr=%q", code, h.reads, h.discoveries, h.authorizations, h.outputs, h.stderr.String())
	}
	assertReportTargetFingerprintJSON(h.t, h.stdout.Bytes(), h.authorization, h.catalog)
	if bytes.Contains(h.stdout.Bytes(), h.key) || bytes.Contains(h.stdout.Bytes(), []byte(h.keyPath)) {
		h.t.Fatal("result disclosed the raw fingerprint or its path")
	}
}

func TestReportTargetFingerprintClosedBeforeBothConsumers(t *testing.T) {
	for _, mode := range []string{"catalog", "authorization"} {
		t.Run(mode, func(t *testing.T) {
			h := newReportTargetFingerprintFixture(t, mode == "authorization")
			h.success(h.run(h.read, h.destination))
			if !reflect.DeepEqual(h.input.readable.readSize, []int{32, 1}) {
				t.Fatal("finite fingerprint capture omitted its overflow probe", h.input.readable.readSize)
			}
		})
	}
}

func TestReportTargetFingerprintPreservesCatalogAndAuthorizationPolicies(t *testing.T) {
	for _, status := range []reporttarget.Status{reporttarget.StatusNotPresent, reporttarget.StatusInconclusive, reporttarget.StatusError, reporttarget.StatusAborted} {
		t.Run(string(status), func(t *testing.T) {
			h := newReportTargetFingerprintFixture(t, false)
			h.catalog.Status, h.catalog.Detail = status, "synthetic policy result"
			h.success(h.run(h.read, h.destination))
		})
	}
	t.Run("authorization_error", func(t *testing.T) {
		h := newReportTargetFingerprintFixture(t, true)
		h.authorizationErr = errors.New("synthetic authorization policy rejection")
		code := h.run(h.read, h.destination)
		if code != 1 || h.reads != 1 || h.discoveries != 0 || h.authorizations != 1 || h.outputs != 0 || h.stdout.Len() != 0 || h.stderr.String() != "authorize report target: synthetic authorization policy rejection\n" {
			t.Fatal("authorization error policy changed", code, h.stderr.String())
		}
	})
}

func TestReportTargetFingerprintCloseFaultStopsConsumers(t *testing.T) {
	for _, mode := range []string{"catalog", "authorization"} {
		for _, owner := range []string{"readable", "metadata"} {
			for _, timing := range []string{"live", "cancel_on_close"} {
				t.Run(mode+"/"+owner+"/"+timing, func(t *testing.T) {
					h := newReportTargetFingerprintFixture(t, mode == "authorization")
					ctx, cancel := context.WithCancel(h.ctx)
					defer cancel()
					h.ctx = ctx
					file := h.input.readable
					if owner == "metadata" {
						file = h.input.metadata
					}
					file.closeErr = errors.New("provider-detail-marker")
					if timing == "cancel_on_close" {
						file.hook = func(operation string) {
							if operation == "close" {
								cancel()
							}
						}
					}
					reader := func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
						data, err := h.read(ctx, path, kind)
						if data != nil || !errors.Is(err, errRetestInput) || !errors.Is(err, errRetestInputCleanup) || errors.Is(err, context.Canceled) {
							t.Fatal("checked Close uncertainty lost before command boundary", err)
						}
						return data, err
					}
					h.noLaterEffects(h.run(reader, h.destination), "read fingerprint key: input acquisition failed")
					if h.reads != 1 {
						t.Fatal("reader not reached")
					}
					assertRetestInputLedger(t, h.input, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
				})
			}
		}
	}
}

func TestReportTargetFingerprintAcquiredOwnerAndReadFaults(t *testing.T) {
	for _, mode := range []string{"catalog", "authorization"} {
		for _, fault := range []string{"metadata_error", "readable_error", "nil_metadata", "typed_nil_metadata", "nil_readable", "non_regular", "wrong_identity", "hard_read", "overflow"} {
			t.Run(mode+"/"+fault, func(t *testing.T) {
				h := newReportTargetFingerprintFixture(t, mode == "authorization")
				acquired := map[string]int{"metadata": 1, "readable": 1}
				closes := []string{"readable", "metadata"}
				witness := "open.readable"
				switch fault {
				case "metadata_error":
					h.input.metadataErr = errors.New("provider-detail-marker")
					acquired, closes, witness = map[string]int{"metadata": 1}, []string{"metadata"}, "open.metadata"
				case "readable_error":
					h.input.readableErr = errors.New("provider-detail-marker")
				case "nil_metadata":
					h.input.metadataOut = nil
					acquired, closes, witness = map[string]int{}, nil, "open.metadata"
				case "typed_nil_metadata":
					var absent *retestInputTestFile
					h.input.metadataOut = absent
					acquired, closes, witness = map[string]int{}, nil, "open.metadata"
				case "nil_readable":
					h.input.readableOut = nil
					acquired, closes = map[string]int{"metadata": 1}, []string{"metadata"}
				case "non_regular":
					h.input.metadata.info.mode = os.ModeNamedPipe
					acquired, closes, witness = map[string]int{"metadata": 1}, []string{"metadata"}, "metadata.stat"
				case "wrong_identity":
					info := *h.input.readable.info
					info.id++
					h.input.readable.info, witness = &info, "readable.stat"
				case "hard_read":
					h.input.readable.reads = []retestInputTestRead{{0, errors.New("provider-detail-marker")}}
					witness = "readable.read"
				case "overflow":
					h.input.readable.data = append(h.input.readable.data, 1)
					witness = "readable.read"
				}
				h.noLaterEffects(h.run(h.read, h.destination), "read fingerprint key: input acquisition failed")
				if h.reads != 1 {
					t.Fatal("reader not reached")
				}
				assertRetestInputLedger(t, h.input, acquired, closes)
				if !strings.Contains("|"+strings.Join(h.input.ledger.calls, "|")+"|", "|"+witness+"|") {
					t.Fatal("fault prerequisite was not reached", h.input.ledger.calls)
				}
				if fault == "overflow" && !reflect.DeepEqual(h.input.readable.readSize, []int{32, 1}) {
					t.Fatal("overflow fault did not reach the bounded probe")
				}
			})
		}
	}
}

func TestReportTargetFingerprintShapePrecedesLateCancellation(t *testing.T) {
	for _, mode := range []string{"catalog", "authorization"} {
		for _, fault := range []string{"nil", "short", "long", "bytes_and_error", "short_and_cancel", "hard_and_cancel"} {
			t.Run(mode+"/"+fault, func(t *testing.T) {
				h := newReportTargetFingerprintFixture(t, mode == "authorization")
				ctx, cancel := context.WithCancel(h.ctx)
				defer cancel()
				h.ctx = ctx
				reader := func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
					data, err := h.read(ctx, path, kind)
					if err != nil || !bytes.Equal(data, h.key) {
						t.Fatal("valid capture prerequisite failed", err)
					}
					switch fault {
					case "nil":
						return nil, nil
					case "short":
						return data[:31], nil
					case "long":
						return append(data, 1), nil
					case "bytes_and_error":
						return data, errors.New("provider-detail-marker")
					case "short_and_cancel":
						cancel()
						return data[:31], nil
					case "hard_and_cancel":
						cancel()
						return data, errors.New("provider-detail-marker")
					}
					panic("unknown test fault")
				}
				h.noLaterEffects(h.run(reader, h.destination), "read fingerprint key: input acquisition failed")
				assertRetestInputLedger(t, h.input, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
			})
		}
	}
}

func TestReportTargetFingerprintUsageAndRequiredDependencies(t *testing.T) {
	for _, test := range []struct {
		name      string
		arguments []string
	}{
		{"unknown_flag", []string{"--unknown"}},
		{"missing_key", nil},
		{"positional", []string{"--fingerprint-key", "fixture", "extra"}},
		{"device_only", []string{"--fingerprint-key", "fixture", "--device", "/dev/sdb1"}},
		{"confirm_only", []string{"--fingerprint-key", "fixture", "--confirm", "RPT-15B6C5"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := runReportTargetWith(nil, test.arguments, &stdout, &stderr, nil, nil, nil, nil); code != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatal("usage no longer precedes dependency admission", code)
			}
		})
	}
	for _, mode := range []string{"catalog", "authorization"} {
		for _, missing := range []string{"nil_context", "typed_nil_context", "reader", "discover", "authorize", "destination"} {
			t.Run(mode+"/"+missing, func(t *testing.T) {
				h := newReportTargetFingerprintFixture(t, mode == "authorization")
				reader, discover, authorize, destination := h.read, h.discover, h.authorize, h.destination
				switch missing {
				case "nil_context":
					h.ctx = nil
				case "typed_nil_context":
					var ctx *retestInputStoppedContext
					h.ctx = ctx
				case "reader":
					reader = nil
				case "discover":
					discover = nil
				case "authorize":
					authorize = nil
				case "destination":
					destination = nil
				}
				code := runReportTargetWith(h.ctx, h.arguments(), &h.stdout, &h.stderr, reader, discover, authorize, destination)
				h.noLaterEffects(code, "report-target input acquisition failed")
				if h.reads != 0 || len(h.input.ledger.calls) != 0 {
					t.Fatal("missing required dependency allowed acquisition")
				}
			})
		}
	}
}

func TestReportTargetFingerprintPreservesLiteralArguments(t *testing.T) {
	h := newReportTargetFingerprintFixture(t, true)
	h.keyPath = " fingerprint fixture "
	arguments := []string{"--fingerprint-key", h.keyPath, "--device", " /dev/sdb1 ", "--confirm", " RPT-15B6C5 "}
	authorize := func(ctx context.Context, options reporttarget.Options, request reporttarget.AuthorizationRequest) (reporttarget.Authorization, error) {
		h.authorizations++
		h.checkConsumer(ctx, options)
		if request.Device != " /dev/sdb1 " || request.ConfirmationCode != " RPT-15B6C5 " {
			t.Fatal("arguments were trimmed")
		}
		return reporttarget.Authorization{}, errors.New("literal request policy")
	}
	code := runReportTargetWith(h.ctx, arguments, &h.stdout, &h.stderr, h.read, h.discover, authorize, h.destination)
	if code != 1 || h.reads != 1 || h.authorizations != 1 || h.discoveries != 0 || h.outputs != 0 || h.stdout.Len() != 0 || h.stderr.String() != "authorize report target: literal request policy\n" {
		t.Fatal("literal request behavior changed", code, h.stderr.String())
	}
}

func TestReportTargetFingerprintCancellationBoundaries(t *testing.T) {
	for _, mode := range []string{"catalog", "authorization"} {
		for _, timing := range []string{"before", "deadline", "during_read", "after_closed_read", "after_consumer"} {
			t.Run(mode+"/"+timing, func(t *testing.T) {
				h := newReportTargetFingerprintFixture(t, mode == "authorization")
				ctx, cancel := context.WithCancel(h.ctx)
				defer cancel()
				h.ctx = ctx
				reader := h.read
				switch timing {
				case "before":
					cancel()
				case "deadline":
					deadline, end := context.WithDeadline(h.ctx, time.Unix(1, 0))
					defer end()
					h.ctx = deadline
				case "during_read":
					h.input.metadataHook = cancel
					reader = func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
						data, err := h.read(ctx, path, kind)
						if data != nil || err != context.Canceled {
							t.Fatal("pure interruption was rewritten", err)
						}
						return data, err
					}
				case "after_closed_read":
					reader = func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
						data, err := h.read(ctx, path, kind)
						if err != nil || !bytes.Equal(data, h.key) {
							t.Fatal("capture prerequisite failed", err)
						}
						assertRetestInputLedger(t, h.input, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
						cancel()
						return data, nil
					}
				case "after_consumer":
					h.consumerHook = cancel
					if !h.authorization {
						h.catalog.Status = reporttarget.StatusAborted
					}
				}
				code := h.run(reader, h.destination)
				switch timing {
				case "before", "deadline":
					h.noLaterEffects(code, "report-target input acquisition failed")
					if h.reads != 0 {
						t.Fatal("stopped context reached reader")
					}
					assertRetestInputLedger(t, h.input, map[string]int{}, nil)
				case "during_read":
					h.noLaterEffects(code, "read fingerprint key: input acquisition failed")
					assertRetestInputLedger(t, h.input, map[string]int{"metadata": 1}, []string{"metadata"})
				case "after_closed_read":
					h.noLaterEffects(code, "report-target input acquisition failed")
				case "after_consumer":
					h.success(code)
				}
			})
		}
	}
}

func TestReportTargetFingerprintOutputCompletionAndOwnership(t *testing.T) {
	for _, mode := range []string{"catalog", "authorization"} {
		for _, fault := range []string{"success", "write", "short", "close", "open_with_owner"} {
			t.Run(mode+"/"+fault, func(t *testing.T) {
				h := newReportTargetFingerprintFixture(t, mode == "authorization")
				h.outputPath = "new-result.json"
				file := &outputFaultFile{limit: -1}
				var openErr error
				want := ""
				switch fault {
				case "write":
					file.writeErr = errors.New("synthetic write fault")
					want = "synthetic write fault"
				case "short":
					file.shortFinal = true
					want = "short write"
				case "close":
					file.closeErr = errors.New("synthetic close fault")
					want = "close output: synthetic close fault"
				case "open_with_owner":
					openErr = errors.New("synthetic open fault")
					want = "open output: synthetic open fault"
				}
				opens := 0
				destination := func(path string, stdout io.Writer, write func(io.Writer) error) error {
					h.outputs++
					return writeDestinationWith(path, stdout, write, func(path string, flags int, mode os.FileMode) (io.WriteCloser, error) {
						opens++
						if path != h.outputPath || flags != os.O_WRONLY|os.O_CREATE|os.O_EXCL || mode != 0600 {
							t.Fatal("exclusive output policy changed")
						}
						return file, openErr
					})
				}
				code := h.run(h.read, destination)
				writes := 1
				if openErr != nil {
					writes = 0
				}
				if opens != 1 || file.writes != writes || file.closes != 1 || h.outputs != 1 || h.reads != 1 || h.discoveries+h.authorizations != 1 || h.stdout.Len() != 0 {
					t.Fatal("output ownership or branch counts changed", opens, file.writes, file.closes)
				}
				if fault == "success" {
					if code != 0 || h.stderr.Len() != 0 {
						t.Fatal(code, h.stderr.String())
					}
					assertReportTargetFingerprintJSON(t, file.Bytes(), h.authorization, h.catalog)
				} else if code != 1 || h.stderr.String() != "write report-target "+mode+" result: "+want+"\n" {
					t.Fatal("output diagnostic policy changed", code, h.stderr.String())
				}
			})
		}
	}
}

func TestReportTargetFingerprintPreservesExistingOutputAndStdout(t *testing.T) {
	for _, mode := range []string{"catalog", "authorization"} {
		t.Run(mode+"/existing", func(t *testing.T) {
			h := newReportTargetFingerprintFixture(t, mode == "authorization")
			h.outputPath = filepath.Join(t.TempDir(), "existing.json")
			original := []byte("existing owned result")
			if err := os.WriteFile(h.outputPath, original, 0600); err != nil {
				t.Fatal(err)
			}
			code := h.run(h.read, h.destination)
			got, err := os.ReadFile(h.outputPath)
			if code != 1 || err != nil || !bytes.Equal(got, original) || h.outputs != 1 || h.discoveries+h.authorizations != 1 || h.stdout.Len() != 0 || !strings.HasPrefix(h.stderr.String(), "write report-target "+mode+" result: open output: ") {
				t.Fatal("existing output changed", code, err)
			}
		})
		t.Run(mode+"/stdout", func(t *testing.T) {
			h := newReportTargetFingerprintFixture(t, mode == "authorization")
			stdout := &outputFaultFile{limit: -1, closeErr: errors.New("must not close caller stdout")}
			code := runReportTargetWith(h.ctx, h.arguments(), stdout, &h.stderr, h.read, h.discover, h.authorize, h.destination)
			if code != 0 || stdout.writes != 1 || stdout.closes != 0 || h.outputs != 1 || h.discoveries+h.authorizations != 1 || h.stderr.Len() != 0 {
				t.Fatal("stdout ownership changed", code)
			}
			assertReportTargetFingerprintJSON(t, stdout.Bytes(), h.authorization, h.catalog)
		})
	}
}
