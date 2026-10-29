package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/deploymentkey"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

const deploymentInputTokenID = "hmac-sha256:a5198b7fec3ea14dc8d7fe86acdee42733bae84417aafc2bf8bbfb2addf58184"
const deploymentInputDiscoveryID = "hmac-sha256:2522c2950f795d02bf30c8f3973b75b11f6ea8f6cedfbe14503e4b00efc9144a"

type deploymentInputConsole struct {
	reader io.Reader
	reads  int
}

func (r *deploymentInputConsole) Read(p []byte) (int, error) {
	r.reads++
	return r.reader.Read(p)
}

type deploymentInputErrorReader struct{}

func (deploymentInputErrorReader) Read([]byte) (int, error) {
	return 0, errors.New("fixture console failure")
}

type deploymentInputFixture struct {
	t                                     *testing.T
	ctx                                   context.Context
	key                                   []byte
	path, auditPath                       string
	args                                  []string
	input                                 *deploymentInputConsole
	stdout, stderr, audit                 bytes.Buffer
	ops                                   deploymentKeyLoadOperations
	core                                  *retestInputFixture
	coreUsed                              bool
	acquire                               retestInputReader
	afterRead, afterAudit, afterAvailable func()
	candidate                             reporttarget.Candidate
	record                                deploymentkey.Record
	events                                []string
	reads, auditOpens, auditCloses        int
	available, selections, loads, writes  int
	unmounts                              int
	auditErr, availableErr, selectErr     error
	loadErr, writeErr, unmountErr         error
}

func deploymentInputMAC(key []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("HHSoft fingerprint key identifier v1\x00"))
	return fmt.Sprintf("hmac-sha256:%x", mac.Sum(nil))
}

func newDeploymentInputFixture(t *testing.T) *deploymentInputFixture {
	t.Helper()
	h := &deploymentInputFixture{
		t: t, ctx: context.WithValue(context.Background(), struct{}{}, "deployment-original-context"),
		key:  []byte("01234567890123456789012345678901"),
		path: "literal discovery input marker.key", auditPath: "literal audit marker",
		input: &deploymentInputConsole{reader: strings.NewReader("selection\nLOAD\n")},
		core:  newRetestInputFixture(32),
		candidate: reporttarget.Candidate{
			Parent: "/dev/sdb", Device: "/dev/sdb1",
			Topology:  "/sys/devices/pci0000:00/usb1/1-2/1-2:1.0/host0/target0:0:0/0:0:0:0/block/sdb/sdb1",
			NodeReady: true, EligibleReadOnly: true, ReadOnly: true,
		},
		record: deploymentkey.Record{Epoch: "deployment-fixture-v1"},
	}
	for i := range h.record.Key {
		h.record.Key[i] = byte(i)
	}
	if deploymentInputMAC(h.record.Key[:]) != deploymentInputTokenID ||
		deploymentInputMAC(h.key) != deploymentInputDiscoveryID || deploymentInputTokenID == deploymentInputDiscoveryID {
		t.Fatal("independent fixed token and discovery HMAC prerequisites changed")
	}
	h.core.readable.data = append([]byte(nil), h.key...)
	h.args = []string{"-discovery-key", h.path, "-audit-output", h.auditPath}
	h.ops = deploymentKeyLoadOperations{
		readInput: h.read, openAudit: h.openAudit, activationAvailable: h.activationAvailable,
		selectMedia: h.selectMedia, loadMedia: h.loadMedia, writeActivation: h.writeActivation,
	}
	return h
}

func (h *deploymentInputFixture) closed() {
	h.t.Helper()
	if h.coreUsed && !reflect.DeepEqual(h.core.ledger.acquired, h.core.ledger.closed) {
		h.t.Fatal("later effect observed a live discovery-key owner")
	}
}

func (h *deploymentInputFixture) openAudit(path string) (io.Writer, func(), error) {
	h.t.Helper()
	if path != h.auditPath {
		h.t.Fatal("audit path changed")
	}
	h.auditOpens++
	h.events = append(h.events, "audit.open")
	if h.auditErr != nil {
		return nil, func() { h.t.Fatal("failed audit unexpectedly acquired a closer") }, h.auditErr
	}
	if h.afterAudit != nil {
		h.afterAudit()
	}
	return &h.audit, func() {
		h.auditCloses++
		h.events = append(h.events, "audit.close")
	}, nil
}

func (h *deploymentInputFixture) read(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
	h.t.Helper()
	if h.reads != 0 || ctx != h.ctx || path != h.path || kind != retestFingerprintKeyInput {
		h.t.Fatal("discovery reader received changed context, literal path, finite role or duplicate acquisition")
	}
	h.reads++
	h.events = append(h.events, "key")
	var data []byte
	var err error
	if h.acquire != nil {
		data, err = h.acquire(ctx, path, kind)
	} else {
		h.coreUsed = true
		data, err = readRetestInputWithOperations(ctx, path, kind, h.core.operations())
	}
	if h.afterRead != nil {
		h.afterRead()
	}
	return data, err
}

func (h *deploymentInputFixture) activationAvailable() error {
	h.t.Helper()
	h.closed()
	if h.reads != 1 {
		h.t.Fatal("availability ran before the required key acquisition")
	}
	h.available++
	h.events = append(h.events, "available")
	if h.afterAvailable != nil {
		h.afterAvailable()
	}
	return h.availableErr
}

func (h *deploymentInputFixture) options(ctx context.Context, options reporttarget.Options) {
	h.t.Helper()
	h.closed()
	if ctx != h.ctx || !reflect.DeepEqual(options, reporttarget.Options{Root: "/", FingerprintKey: h.key}) {
		h.t.Fatal("consumer received changed context, root or discovery key")
	}
}

func (h *deploymentInputFixture) selectMedia(ctx context.Context, options reporttarget.Options, reader *bufio.Reader, stdout, audit io.Writer) (reporttarget.Candidate, error) {
	h.t.Helper()
	h.options(ctx, options)
	if stdout != &h.stdout || audit != &h.audit || reader == nil || h.available != 1 {
		h.t.Fatal("selector received changed writers/reader or skipped availability")
	}
	h.selections++
	h.events = append(h.events, "select")
	if h.selectErr != nil {
		return reporttarget.Candidate{}, h.selectErr
	}
	selection, err := reader.ReadString('\n')
	if err != nil || selection != "selection\n" {
		h.t.Fatal("selector did not consume its line from the shared command reader")
	}
	return h.candidate, nil
}

func (h *deploymentInputFixture) loadMedia(ctx context.Context, options reporttarget.Options, candidate reporttarget.Candidate) (deploymentkey.Record, func() error, error) {
	h.t.Helper()
	h.options(ctx, options)
	if !reflect.DeepEqual(candidate, h.candidate) || h.selections != 1 {
		h.t.Fatal("loader received a different candidate or skipped selection")
	}
	h.loads++
	h.events = append(h.events, "load")
	if h.loadErr != nil {
		return deploymentkey.Record{}, nil, h.loadErr
	}
	return h.record, func() error {
		h.unmounts++
		h.events = append(h.events, "unmount")
		return h.unmountErr
	}, nil
}

func (h *deploymentInputFixture) expectedActivation() deploymentActivation {
	return deploymentActivation{
		Schema: "hhsoft.pc-tester.deployment-key-activation/v1", Mode: "deployment",
		Epoch: "deployment-fixture-v1", KeyID: deploymentInputTokenID, Device: "/dev/sdb1", ReadOnly: true,
		ExcludedComponents: []string{"block:sdb", "block:sdb1", "usb:1-2"},
	}
}

func (h *deploymentInputFixture) writeActivation(record deploymentkey.Record, activation deploymentActivation) error {
	h.t.Helper()
	h.closed()
	if !reflect.DeepEqual(record, h.record) || !reflect.DeepEqual(activation, h.expectedActivation()) || h.loads != 1 {
		h.t.Fatal("complete token record or independently expected activation changed")
	}
	h.writes++
	h.events = append(h.events, "write")
	return h.writeErr
}

func (h *deploymentInputFixture) run() int {
	return runDeploymentKeyLoadMediaWithOperations(h.ctx, h.args, h.input, &h.stdout, &h.stderr, h.ops)
}

func (h *deploymentInputFixture) safe() {
	h.t.Helper()
	for _, output := range [][]byte{h.stdout.Bytes(), h.stderr.Bytes(), h.audit.Bytes()} {
		for _, marker := range [][]byte{h.key, h.record.Key[:], []byte(h.path), []byte("provider-detail-marker")} {
			if bytes.Contains(output, marker) {
				h.t.Fatal("raw key, discovery path or provider detail escaped")
			}
		}
	}
}

func (h *deploymentInputFixture) rejected(code int, diagnostic string, reads int) {
	h.t.Helper()
	if code != 1 || h.stderr.String() != diagnostic+"\n" || h.reads != reads ||
		h.available != 0 || h.selections != 0 || h.loads != 0 || h.writes != 0 || h.unmounts != 0 ||
		h.input.reads != 0 || h.stdout.Len() != 0 || h.audit.Len() != 0 || h.auditCloses != 1 {
		h.t.Fatalf("input rejection changed: code=%d reads=%d effects=%d/%d/%d/%d unmount=%d console=%d auditClose=%d stderr=%q",
			code, h.reads, h.available, h.selections, h.loads, h.writes, h.unmounts, h.input.reads, h.auditCloses, h.stderr.String())
	}
	h.closed()
	h.safe()
}

func (h *deploymentInputFixture) success(code int) {
	h.t.Helper()
	if code != 0 || h.reads != 1 || h.auditOpens != 1 || h.auditCloses != 1 ||
		h.available != 1 || h.selections != 1 || h.loads != 1 || h.writes != 1 || h.unmounts != 0 ||
		h.stderr.Len() != 0 || h.input.reads == 0 {
		h.t.Fatalf("valid activation failed: code=%d events=%v stderr=%q", code, h.events, h.stderr.String())
	}
	wantEvents := []string{"audit.open", "key", "available", "select", "load", "write", "audit.close"}
	if !reflect.DeepEqual(h.events, wantEvents) {
		h.t.Fatal("command effect order changed", h.events)
	}
	wantAudit := "HHPT_KEY_MEDIA_SELECTED device=/dev/sdb1\nHHPT_KEY_MEDIA_CONFIRMED policy=read-only\n" +
		"HHPT_KEY_MEDIA_ACTIVE epoch=deployment-fixture-v1 key_id=" + deploymentInputTokenID + " read_only=true\n"
	wantStdout := "Type LOAD to mount /dev/sdb1 read-only and activate its stable fingerprint epoch: " +
		"Deployment key active: epoch=deployment-fixture-v1 key_id=" + deploymentInputTokenID + "; media remains mounted read-only.\n"
	if h.audit.String() != wantAudit || h.stdout.String() != wantStdout {
		h.t.Fatal("complete activation announcements changed")
	}
	h.closed()
	if h.coreUsed {
		assertRetestInputLedger(h.t, h.core, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
		if !reflect.DeepEqual(h.core.readable.readSize, []int{32, 1}) {
			h.t.Fatal("valid acquisition omitted the bounded terminal probe")
		}
	}
	h.safe()
}

func TestDeploymentDiscoveryKeyActivationUsesClosedInput(t *testing.T) {
	for _, test := range []struct{ name, confirmation string }{
		{"exact", "LOAD\n"}, {"trimmed_casefold", " \tLoAd \t\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newDeploymentInputFixture(t)
			h.input.reader = strings.NewReader("selection\n" + test.confirmation)
			h.success(h.run())
		})
	}
}

func TestDeploymentDiscoveryKeyUsageAndAuditPrecedence(t *testing.T) {
	for _, name := range []string{"unknown_flag", "missing_key", "positional", "missing_audit", "audit_error"} {
		t.Run(name, func(t *testing.T) {
			h := newDeploymentInputFixture(t)
			h.ctx = nil
			switch name {
			case "unknown_flag":
				h.args = []string{"-unknown"}
			case "missing_key":
				h.args = nil
			case "positional":
				h.args = append(h.args, "extra")
			case "missing_audit":
				h.ops.openAudit = nil
			case "audit_error":
				h.auditErr = errors.New("fixture audit refusal")
			}
			code := h.run()
			wantCode, wantOpen := 2, 0
			if name == "missing_audit" {
				wantCode = 1
				if h.stderr.String() != "deployment-key input acquisition failed\n" {
					t.Fatal("missing audit diagnostic changed")
				}
			} else if name == "audit_error" {
				wantCode, wantOpen = 1, 1
				if h.stderr.String() != "open deployment-key audit output: fixture audit refusal\n" {
					t.Fatal("audit failure did not retain earlier precedence")
				}
			}
			if code != wantCode || h.auditOpens != wantOpen || h.auditCloses != 0 || h.reads != 0 ||
				h.available != 0 || h.selections != 0 || h.loads != 0 || h.writes != 0 ||
				h.input.reads != 0 || h.stdout.Len() != 0 || h.audit.Len() != 0 {
				t.Fatal("usage or audit rejection performed later work", code, h.events)
			}
		})
	}
}

func TestDeploymentDiscoveryKeyAdmissionStopsBeforeAcquisition(t *testing.T) {
	for _, name := range []string{"nil_context", "typed_nil_context", "pre_cancelled", "audit_cancelled", "missing_reader", "missing_availability", "missing_selector", "missing_loader", "missing_writer"} {
		t.Run(name, func(t *testing.T) {
			h := newDeploymentInputFixture(t)
			ctx, cancel := context.WithCancel(h.ctx)
			defer cancel()
			h.ctx = ctx
			switch name {
			case "nil_context":
				h.ctx = nil
			case "typed_nil_context":
				var nilContext *retestInputStoppedContext
				h.ctx = nilContext
			case "pre_cancelled":
				cancel()
			case "audit_cancelled":
				h.afterAudit = cancel
			case "missing_reader":
				h.ops.readInput = nil
			case "missing_availability":
				h.ops.activationAvailable = nil
			case "missing_selector":
				h.ops.selectMedia = nil
			case "missing_loader":
				h.ops.loadMedia = nil
			case "missing_writer":
				h.ops.writeActivation = nil
			}
			h.rejected(h.run(), "deployment-key input acquisition failed", 0)
			if h.auditOpens != 1 || !reflect.DeepEqual(h.events, []string{"audit.open", "audit.close"}) {
				t.Fatal("context/dependency admission moved before the audit")
			}
		})
	}
}

func TestDeploymentDiscoveryKeyReturnedFaultsPrecedeCancellation(t *testing.T) {
	for _, name := range []string{"nil_data", "empty_data", "short31", "long33", "hard_error", "bytes_error", "cancel_error", "deadline_error", "empty_cancel", "bytes_cancel", "joined_cancel", "cleanup_cancel", "valid32"} {
		for _, state := range []string{"active", "cancel_before_return"} {
			t.Run(name+"/"+state, func(t *testing.T) {
				h := newDeploymentInputFixture(t)
				ctx, cancel := context.WithCancel(h.ctx)
				defer cancel()
				h.ctx = ctx
				h.acquire = func(context.Context, string, retestInputKind) ([]byte, error) {
					var data []byte
					var err error
					switch name {
					case "empty_data":
						data = []byte{}
					case "short31":
						data = bytes.Repeat([]byte{0x55}, 31)
					case "long33":
						data = bytes.Repeat([]byte{0x55}, 33)
					case "hard_error":
						err = errors.New("provider-detail-marker")
					case "bytes_error":
						data, err = append([]byte(nil), h.key...), errors.New("provider-detail-marker")
					case "cancel_error":
						err = context.Canceled
					case "deadline_error":
						err = context.DeadlineExceeded
					case "empty_cancel":
						data, err = []byte{}, context.Canceled
					case "bytes_cancel":
						data, err = append([]byte(nil), h.key...), context.Canceled
					case "joined_cancel":
						err = errors.Join(context.Canceled, errors.New("provider-detail-marker"))
					case "cleanup_cancel":
						err = errors.Join(context.Canceled, errRetestInputCleanup)
					case "valid32":
						data = append([]byte(nil), h.key...)
					}
					if state == "cancel_before_return" {
						cancel()
					}
					return data, err
				}
				code := h.run()
				if name == "valid32" && state == "active" {
					h.success(code)
				} else if name == "valid32" {
					h.rejected(code, "deployment-key input acquisition failed", 1)
				} else {
					h.rejected(code, "read media discovery key: input acquisition failed", 1)
				}
			})
		}
	}
}

func TestDeploymentDiscoveryKeyOwnerAndReadFaultsStopEffects(t *testing.T) {
	for _, name := range []string{"metadata_nil", "metadata_typed_nil", "metadata_handle_error", "metadata_stat_nil", "readable_nil", "readable_typed_nil", "readable_handle_error", "alias", "identity", "hard_read", "readable_close", "metadata_close"} {
		for _, state := range []string{"active", "cancelled"} {
			t.Run(name+"/"+state, func(t *testing.T) {
				h := newDeploymentInputFixture(t)
				ctx, cancel := context.WithCancel(h.ctx)
				defer cancel()
				h.ctx = ctx
				f := h.core
				failure := errors.New("provider-detail-marker")
				want := map[string]int{"metadata": 1, "readable": 1}
				closes := []string{"readable", "metadata"}
				var readSizes []int
				switch name {
				case "metadata_nil":
					f.metadataOut = nil
					want, closes = map[string]int{}, nil
				case "metadata_typed_nil":
					var owner *retestInputTestFile
					f.metadataOut = owner
					want, closes = map[string]int{}, nil
				case "metadata_handle_error":
					f.metadataErr = failure
					want, closes = map[string]int{"metadata": 1}, []string{"metadata"}
				case "metadata_stat_nil":
					f.metadata.stats = []retestInputTestStat{{}}
					want, closes = map[string]int{"metadata": 1}, []string{"metadata"}
				case "readable_nil":
					f.readableOut = nil
					want, closes = map[string]int{"metadata": 1}, []string{"metadata"}
				case "readable_typed_nil":
					var owner *retestInputTestFile
					f.readableOut = owner
					want, closes = map[string]int{"metadata": 1}, []string{"metadata"}
				case "readable_handle_error":
					f.readableErr = failure
				case "alias":
					f.readableOut = f.metadata
					want, closes = map[string]int{"metadata": 1}, []string{"metadata"}
				case "identity":
					info := *f.readable.info
					info.id++
					f.readable.info = &info
				case "hard_read":
					f.readable.reads = []retestInputTestRead{{0, failure}}
					readSizes = []int{32}
				case "readable_close":
					f.readable.closeErr = failure
					readSizes = []int{32, 1}
				case "metadata_close":
					f.metadata.closeErr = failure
					readSizes = []int{32, 1}
				}
				if state == "cancelled" {
					if name == "readable_close" {
						f.readable.hook = func(operation string) {
							if operation == "close" {
								cancel()
							}
						}
					} else if name == "metadata_close" {
						f.metadata.hook = func(operation string) {
							if operation == "close" {
								cancel()
							}
						}
					} else {
						h.afterRead = cancel
					}
				}
				h.rejected(h.run(), "read media discovery key: input acquisition failed", 1)
				assertRetestInputLedger(t, f, want, closes)
				if !reflect.DeepEqual(f.readable.readSize, readSizes) {
					t.Fatal("fault reached the wrong read boundary", f.readable.readSize, readSizes)
				}
			})
		}
	}
}

func TestDeploymentDiscoveryKeyTerminalProbeFaultsAreReached(t *testing.T) {
	for _, name := range []string{"extra_after_data_eof", "probe_no_progress", "probe_hard_error"} {
		t.Run(name, func(t *testing.T) {
			h := newDeploymentInputFixture(t)
			probe := retestInputTestRead{n: 1, err: io.EOF}
			if name == "probe_no_progress" {
				probe = retestInputTestRead{}
			} else if name == "probe_hard_error" {
				probe = retestInputTestRead{err: errors.New("provider-detail-marker")}
			}
			h.core.readable.reads = []retestInputTestRead{{n: 32, err: io.EOF}, probe}
			h.rejected(h.run(), "read media discovery key: input acquisition failed", 1)
			assertRetestInputLedger(t, h.core, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
			if !reflect.DeepEqual(h.core.readable.readSize, []int{32, 1}) {
				t.Fatal("terminal probe fault was not reached", h.core.readable.readSize)
			}
		})
	}
}

func TestDeploymentDiscoveryKeyCancellationKeepsBoundaryPolicy(t *testing.T) {
	for _, when := range []string{"during_read", "readable_close", "metadata_close", "after_closed_result", "consumer_entry"} {
		t.Run(when, func(t *testing.T) {
			h := newDeploymentInputFixture(t)
			ctx, cancel := context.WithCancel(h.ctx)
			defer cancel()
			h.ctx = ctx
			switch when {
			case "during_read":
				h.core.readable.hook = func(operation string) {
					if operation == "read" {
						cancel()
					}
				}
			case "readable_close":
				h.core.readable.hook = func(operation string) {
					if operation == "close" {
						cancel()
					}
				}
			case "metadata_close":
				h.core.metadata.hook = func(operation string) {
					if operation == "close" {
						cancel()
					}
				}
			case "after_closed_result":
				h.afterRead = cancel
			case "consumer_entry":
				h.afterAvailable = cancel
			}
			code := h.run()
			if when == "consumer_entry" {
				// Injected consumers deliberately ignore cancellation after entry.
				h.success(code)
			} else if when == "after_closed_result" {
				h.rejected(code, "deployment-key input acquisition failed", 1)
			} else {
				h.rejected(code, "read media discovery key: input acquisition failed", 1)
			}
			if ctx.Err() != context.Canceled {
				t.Fatal("cancellation prerequisite was not reached")
			}
			assertRetestInputLedger(t, h.core, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
			wantReads := []int{32, 1}
			if when == "during_read" {
				wantReads = []int{32}
			}
			if !reflect.DeepEqual(h.core.readable.readSize, wantReads) {
				t.Fatal("cancellation occurred at the wrong completion boundary", h.core.readable.readSize)
			}
		})
	}
}

func TestDeploymentDiscoveryKeyDownstreamFailuresRetainPolicy(t *testing.T) {
	for _, name := range []string{"availability", "selection", "wrong_confirmation", "empty_confirmation", "partial_eof", "console_error", "load", "fixture", "activation_write", "unmount_error"} {
		t.Run(name, func(t *testing.T) {
			h := newDeploymentInputFixture(t)
			want := []int{1, 1, 0, 0, 0}
			diagnostic := "deployment-key media activation was not confirmed\n"
			switch name {
			case "availability":
				h.availableErr = errors.New("fixture availability failure")
				want = []int{1, 0, 0, 0, 0}
				diagnostic = "prepare deployment-key activation: fixture availability failure\n"
			case "selection":
				h.selectErr = errors.New("fixture selection failure")
				diagnostic = "select deployment-key media: fixture selection failure\n"
			case "wrong_confirmation":
				h.input.reader = strings.NewReader("selection\nno\n")
			case "empty_confirmation":
				h.input.reader = strings.NewReader("selection\n\n")
			case "partial_eof":
				h.input.reader = strings.NewReader("selection\nLOAD")
			case "console_error":
				h.input.reader = io.MultiReader(strings.NewReader("selection\n"), deploymentInputErrorReader{})
			case "load":
				h.loadErr = errors.New("fixture load failure")
				want = []int{1, 1, 1, 0, 0}
				diagnostic = "load deployment-key media: fixture load failure\n"
			case "fixture":
				h.candidate.Topology = "/sys/devices/no-usb/block/sdb/sdb1"
				want = []int{1, 1, 1, 0, 1}
				diagnostic = "bind deployment-key fixture: selected token topology cannot be mapped to fixture component IDs\n"
			case "activation_write", "unmount_error":
				h.writeErr = errors.New("fixture activation failure")
				want = []int{1, 1, 1, 1, 1}
				diagnostic = "activate deployment key: fixture activation failure\n"
				if name == "unmount_error" {
					h.unmountErr = errors.New("fixture unmount failure")
				}
			}
			code := h.run()
			got := []int{h.available, h.selections, h.loads, h.writes, h.unmounts}
			if code != 1 || h.reads != 1 || h.auditCloses != 1 || !reflect.DeepEqual(got, want) ||
				h.stderr.String() != diagnostic || strings.Contains(h.stdout.String(), "Deployment key active:") ||
				strings.Contains(h.audit.String(), "HHPT_KEY_MEDIA_ACTIVE") {
				t.Fatal("downstream failure behavior changed", code, got, h.stderr.String(), h.events)
			}
			if (name == "availability" || name == "selection") && h.input.reads != 0 {
				t.Fatal("console was consumed before an earlier failure")
			}
			assertRetestInputLedger(t, h.core, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
			h.safe()
		})
	}
}
