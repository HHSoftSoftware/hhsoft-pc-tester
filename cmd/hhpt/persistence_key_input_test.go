package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

// Fixed fields and standard-library HMAC keep the oracle independent of NewAuthorization.
const persistKeyAuthorizationBody = "{\"device\":{\"device\":\"/dev/sdb1\",\"parent\":\"/dev/sdb\",\"topology\":\"/sys/devices/fixture\",\"device_major_minor\":\"8:17\",\"parent_major_minor\":\"8:16\",\"size_bytes\":33554432,\"parent_size_bytes\":67108864,\"fingerprint_key_id\":\"hmac-sha256:1111111111111111111111111111111111111111111111111111111111111111\",\"media_identity\":\"hmac-sha256:2222222222222222222222222222222222222222222222222222222222222222\",\"media_identity_source\":\"serial\"},\"boot_id\":\"11111111-1111-4111-8111-111111111111\",\"issued_boot_seconds\":123,\"expires_boot_seconds\":1023}"

func persistKeyAuthorization() reporttarget.Authorization {
	return reporttarget.Authorization{
		Schema: "hhsoft.pc-tester.report-target-authorization/v1",
		ID:     "hmac-sha256:ca71508887c6a29432aab64fc9068f67b06bf7f8be3a7ebcf85c2e0f927d636a",
		BootID: "11111111-1111-4111-8111-111111111111", IssuedBootSeconds: 123, ExpiresBootSeconds: 1023,
		Device: reporttarget.DeviceBinding{
			Device: "/dev/sdb1", Parent: "/dev/sdb", Topology: "/sys/devices/fixture",
			DeviceMajorMinor: "8:17", ParentMajorMinor: "8:16", SizeBytes: 33554432, ParentSizeBytes: 67108864,
			FingerprintKeyID: "hmac-sha256:" + strings.Repeat("1", 64),
			MediaIdentity:    "hmac-sha256:" + strings.Repeat("2", 64), MediaIdentitySource: "serial",
		},
		MountPoint:             "/run/hhpt/report-target",
		RequiredMountOptions:   []string{"nodev", "noexec", "nosuid", "rw"},
		AllowedFileSystemTypes: []string{"exfat", "ext4", "vfat"},
	}
}

func persistKeyMAC(key []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("HHSoft report-target authorization v1\x00"))
	mac.Write([]byte(persistKeyAuthorizationBody))
	return fmt.Sprintf("hmac-sha256:%x", mac.Sum(nil))
}

type persistKeyFixture struct {
	t                                                        *testing.T
	ctx                                                      context.Context
	args                                                     []string
	key, public, original                                    []byte
	keyPath, publicPath, sourcePath, authorizationPath, name string
	digest                                                   [sha256.Size]byte
	signed                                                   comparisonInputSigned
	authorization                                            reporttarget.Authorization
	order                                                    []string
	kinds                                                    []retestInputKind
	inputs                                                   []*retestInputFixture
	coreUsed                                                 []bool
	reader                                                   retestInputReader
	acquire                                                  func(int, context.Context, string, retestInputKind) ([]byte, error)
	afterRead                                                func(int)
	consumer                                                 reportPersister
	afterPersist                                             func()
	persistError                                             error
	changeReceipt                                            func(*reporttarget.Receipt)
	reads, persists                                          int
	source                                                   *os.File
	stdout, stderr                                           bytes.Buffer
	output                                                   io.Writer
}

func newPersistKeyFixture(t *testing.T, self, linked bool) *persistKeyFixture {
	t.Helper()
	parent := comparisonInputReport(t, true, 0x71, 0x28, "persist-key-fixture", nil)
	var previous *report.PreviousReport
	if linked {
		previous = &report.PreviousReport{ManifestDigest: parent.artifact.Envelope.ManifestDigest, BundleSHA256: fmt.Sprintf("sha256:%x", parent.artifact.BundleSHA256)}
	}
	signed := comparisonInputReport(t, self, 0x72, 0x28, "persist-key-fixture", previous)
	digest := sha256.Sum256(signed.encoded)
	var verified bundle.Artifact
	var err error
	if self {
		verified, err = bundle.VerifySelfIssued(bytes.NewReader(signed.encoded), int64(len(signed.encoded)))
	} else {
		verified, err = bundle.Verify(bytes.NewReader(signed.encoded), int64(len(signed.encoded)), report.Keyring{"comparison-fixture-key": signed.public})
	}
	if err != nil || verified.BundleSHA256 != digest || !bytes.Equal(verified.InventoryJSON, signed.artifact.InventoryJSON) {
		t.Fatal("independently verified report prerequisite failed")
	}
	wantSchema := bundle.ManifestSchema
	if linked {
		wantSchema = bundle.RetestManifestSchema
		if !reflect.DeepEqual(verified.Manifest.PreviousReport, previous) || verified.Envelope.SessionID == parent.artifact.Envelope.SessionID {
			t.Fatal("linked fixture lost its exact parent binding or new identity")
		}
	} else if verified.Manifest.PreviousReport != nil {
		t.Fatal("legacy fixture unexpectedly links a parent")
	}
	if verified.Manifest.Schema != wantSchema {
		t.Fatal("report fixture has the wrong manifest profile")
	}
	directory := t.TempDir()
	h := &persistKeyFixture{
		t: t, ctx: context.WithValue(context.Background(), struct{}{}, "persist-original-context"),
		key: []byte("12345678901234567890123456789012"), public: append([]byte(nil), signed.public...),
		original: append([]byte(nil), signed.encoded...), signed: signed, digest: digest,
		keyPath: filepath.Join(directory, "fingerprint input marker.key"), publicPath: filepath.Join(directory, "public input marker.key"),
		sourcePath: filepath.Join(directory, "source.hhpcr"), authorizationPath: filepath.Join(directory, "authorization.json"),
		name: "persist-input-fixture.hhpcr", authorization: persistKeyAuthorization(),
		inputs: []*retestInputFixture{newRetestInputFixture(32), newRetestInputFixture(32)}, coreUsed: make([]bool, 2),
	}
	if persistKeyMAC(h.key) != h.authorization.ID {
		t.Fatal("independent fixed authorization HMAC changed")
	}
	h.writeAuthorization()
	if err := os.WriteFile(h.sourcePath, h.original, 0600); err != nil {
		t.Fatal(err)
	}
	h.args = []string{"-authorization", h.authorizationPath, "-fingerprint-key", h.keyPath, "-name", h.name}
	h.order, h.kinds = []string{h.keyPath}, []retestInputKind{retestFingerprintKeyInput}
	if self {
		h.args = append(h.args, "-self-issued")
	} else {
		h.args = append(h.args, "-self-issued=false", "-public-key", h.publicPath, "-key-id", " \tcomparison-fixture-key \t")
		h.order, h.kinds = append(h.order, h.publicPath), append(h.kinds, retestPublicKeyInput)
	}
	h.args = append(h.args, h.sourcePath)
	h.inputs[0].readable.data = append([]byte(nil), h.key...)
	h.inputs[1].readable.data = append([]byte(nil), h.public...)
	h.reader, h.consumer, h.output = h.read, h.persist, &h.stdout
	return h
}

func (h *persistKeyFixture) writeAuthorization() {
	h.t.Helper()
	encoded, err := json.Marshal(h.authorization)
	if err != nil {
		h.t.Fatal(err)
	}
	decoded, err := reporttarget.DecodeAuthorization(encoded)
	if err != nil || !reflect.DeepEqual(decoded, h.authorization) {
		h.t.Fatal("valid decoded authorization prerequisite failed", err)
	}
	if err := os.WriteFile(h.authorizationPath, encoded, 0600); err != nil {
		h.t.Fatal(err)
	}
}

func (h *persistKeyFixture) argument(name, value string) {
	h.t.Helper()
	for i := 0; i+1 < len(h.args); i++ {
		if h.args[i] == name {
			h.args[i+1] = value
			return
		}
	}
	h.t.Fatal("fixture argument is missing", name)
}

func (h *persistKeyFixture) closed() {
	h.t.Helper()
	for i, used := range h.coreUsed {
		if used && !reflect.DeepEqual(h.inputs[i].ledger.acquired, h.inputs[i].ledger.closed) {
			h.t.Fatal("later work observed a live key owner")
		}
	}
}

func (h *persistKeyFixture) core(index int, ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
	h.coreUsed[index] = true
	return readRetestInputWithOperations(ctx, path, kind, h.inputs[index].operations())
}

func (h *persistKeyFixture) read(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
	h.t.Helper()
	index := h.reads
	if index >= len(h.order) || ctx != h.ctx || path != h.order[index] || kind != h.kinds[index] {
		h.t.Fatal("unexpected context, literal key path, role or later acquisition")
	}
	h.closed()
	h.reads++
	var data []byte
	var err error
	if h.acquire == nil {
		data, err = h.core(index, ctx, path, kind)
	} else {
		data, err = h.acquire(index, ctx, path, kind)
	}
	if h.afterRead != nil {
		h.afterRead(index)
	}
	return data, err
}

func (h *persistKeyFixture) receipt() reporttarget.Receipt {
	return reporttarget.Receipt{
		Schema: "hhsoft.pc-tester.report-persist-receipt/v1", AuthorizationID: h.authorization.ID, Device: "/dev/sdb1",
		File: h.name, Bytes: int64(len(h.original)), SHA256: fmt.Sprintf("sha256:%x", h.digest),
		FileSynced: true, ReadBackVerified: true, DirectorySynced: true,
	}
}

func (h *persistKeyFixture) persist(ctx context.Context, options reporttarget.Options, authorization reporttarget.Authorization, source *os.File, size int64, name string, digest [sha256.Size]byte) (reporttarget.Receipt, error) {
	h.t.Helper()
	h.closed()
	h.persists++
	if ctx != h.ctx || options.Root != "/" || !bytes.Equal(options.FingerprintKey, h.key) || h.reads != len(h.order) ||
		!reflect.DeepEqual(authorization, h.authorization) || source == nil || size != int64(len(h.original)) || name != h.name || digest != h.digest {
		h.t.Fatal("persistence received changed context, authorization, key, source or naming")
	}
	raw, err := io.ReadAll(io.NewSectionReader(source, 0, size+1))
	if err != nil || !bytes.Equal(raw, h.original) || sha256.Sum256(raw) != h.digest {
		h.t.Fatal("persistence did not receive the exact verified current report")
	}
	h.source = source
	receipt := h.receipt()
	if h.changeReceipt != nil {
		h.changeReceipt(&receipt)
	}
	if h.afterPersist != nil {
		h.afterPersist()
	}
	return receipt, h.persistError
}

func (h *persistKeyFixture) run() int {
	return runPersistWith(h.ctx, h.args, h.output, &h.stderr, h.reader, h.consumer)
}

func (h *persistKeyFixture) preserved() {
	h.t.Helper()
	raw, err := os.ReadFile(h.sourcePath)
	if err != nil || !bytes.Equal(raw, h.original) {
		h.t.Fatal("fixture source changed")
	}
	h.closed()
	if h.source != nil {
		if n, err := h.source.Read(make([]byte, 1)); n != 0 || !errors.Is(err, os.ErrClosed) {
			h.t.Fatal("retained live-source deferred closer did not run")
		}
	}
}

func (h *persistKeyFixture) rejected(code int, diagnostic string, reads int) {
	h.t.Helper()
	if code != 1 || h.reads != reads || h.persists != 0 || h.stdout.Len() != 0 || h.stderr.String() != diagnostic+"\n" {
		h.t.Fatalf("key rejection changed: code=%d reads=%d persists=%d stdout=%q stderr=%q", code, h.reads, h.persists, h.stdout.String(), h.stderr.String())
	}
	for _, marker := range [][]byte{h.key, []byte(h.keyPath), []byte(h.publicPath), []byte("provider-detail-marker")} {
		if bytes.Contains(h.stderr.Bytes(), marker) {
			h.t.Fatal("key acquisition detail leaked")
		}
	}
	h.preserved()
}

func (h *persistKeyFixture) success(code int, encoded []byte) {
	h.t.Helper()
	if code != 0 || h.persists != 1 || h.reads != len(h.order) || h.stderr.Len() != 0 {
		h.t.Fatalf("valid persistence rejected: code=%d reads=%d persists=%d error=%s", code, h.reads, h.persists, h.stderr.String())
	}
	var got reporttarget.Receipt
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&got); err != nil || !reflect.DeepEqual(got, h.receipt()) {
		h.t.Fatal("complete receipt differs from independent expected data", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		h.t.Fatal("receipt output contains trailing data")
	}
	h.preserved()
}

func persistKeyDiagnostic(index int) string {
	if index == 0 {
		return "read fingerprint key: input acquisition failed"
	}
	return "verify source report: input acquisition failed"
}

func TestPersistKeysCloseBeforeLegacyLinkedAndTrustedConsumers(t *testing.T) {
	for _, profile := range []string{"legacy", "linked"} {
		for _, trust := range []string{"self", "trusted"} {
			for _, naming := range []string{"explicit", "default"} {
				t.Run(profile+"/"+trust+"/"+naming, func(t *testing.T) {
					h := newPersistKeyFixture(t, trust == "self", profile == "linked")
					if naming == "default" {
						h.argument("-name", "")
						h.name = "hhpt-comparison-legacy-session.hhpcr"
						if profile == "linked" {
							h.name = "hhpt-bbbbbbbb-cccc-4ddd-8eee-ffffffffffff.hhpcr"
						}
					}
					code := h.run()
					h.success(code, h.stdout.Bytes())
					for i := range h.order {
						assertRetestInputLedger(t, h.inputs[i], map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
						if !reflect.DeepEqual(h.inputs[i].readable.readSize, []int{32, 1}) {
							t.Fatal("positive key did not finish its bounded probe")
						}
					}
				})
			}
		}
	}
}

func TestPersistKeysPreserveLiteralPathsAndSeparateSamePathRoles(t *testing.T) {
	for _, mode := range []string{"literal_paths", "same_path"} {
		t.Run(mode, func(t *testing.T) {
			h := newPersistKeyFixture(t, false, true)
			h.keyPath, h.publicPath = "literal/segment/../fingerprint key", "./literal public key"
			if mode == "same_path" {
				h.publicPath = h.keyPath
				h.key = append([]byte(nil), h.public...)
				h.inputs[0].readable.data = append([]byte(nil), h.key...)
				h.authorization.ID = persistKeyMAC(h.key)
				h.writeAuthorization()
			}
			h.argument("-fingerprint-key", h.keyPath)
			h.argument("-public-key", h.publicPath)
			h.order = []string{h.keyPath, h.publicPath}
			code := h.run()
			h.success(code, h.stdout.Bytes())
		})
	}
}

func TestPersistKeyCloseFaultsPrecedeLaterKeysAndCancellation(t *testing.T) {
	for index, role := range []string{"fingerprint", "public"} {
		for _, owner := range []string{"readable", "metadata"} {
			for _, timing := range []string{"live", "cancel"} {
				t.Run(role+"/"+owner+"/"+timing, func(t *testing.T) {
					h := newPersistKeyFixture(t, false, false)
					ctx, cancel := context.WithCancel(h.ctx)
					defer cancel()
					h.ctx = ctx
					file := h.inputs[index].readable
					if owner == "metadata" {
						file = h.inputs[index].metadata
					}
					file.closeErr = errors.New("provider-detail-marker")
					if timing == "cancel" {
						file.hook = func(operation string) {
							if operation == "close" {
								cancel()
							}
						}
					}
					h.rejected(h.run(), persistKeyDiagnostic(index), index+1)
					for i := 0; i <= index; i++ {
						assertRetestInputLedger(t, h.inputs[i], map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
						if !reflect.DeepEqual(h.inputs[i].readable.readSize, []int{32, 1}) {
							t.Fatal("Close fault preceded the otherwise valid capture")
						}
					}
				})
			}
		}
	}
}

func TestPersistKeyOwnerAndReadFaultsStopTheConsumer(t *testing.T) {
	for index, role := range []string{"fingerprint", "public"} {
		for _, fault := range []string{"metadata_error", "readable_error", "nil_metadata", "typed_nil_metadata", "nil_readable", "typed_nil_readable", "alias", "identity", "hard_read"} {
			t.Run(role+"/"+fault, func(t *testing.T) {
				h := newPersistKeyFixture(t, false, false)
				f := h.inputs[index]
				want, closes := map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"}
				switch fault {
				case "metadata_error":
					f.metadataErr = errors.New("provider-detail-marker")
					want, closes = map[string]int{"metadata": 1}, []string{"metadata"}
				case "readable_error":
					f.readableErr = errors.New("provider-detail-marker")
				case "nil_metadata", "typed_nil_metadata":
					f.metadataOut = nil
					if fault == "typed_nil_metadata" {
						f.metadataOut = (*retestInputTestFile)(nil)
					}
					want, closes = map[string]int{}, nil
				case "nil_readable", "typed_nil_readable", "alias":
					f.readableOut = nil
					if fault == "typed_nil_readable" {
						f.readableOut = (*retestInputTestFile)(nil)
					}
					if fault == "alias" {
						f.readableOut = f.metadata
					}
					want, closes = map[string]int{"metadata": 1}, []string{"metadata"}
				case "identity":
					info := *f.readable.info
					info.id++
					f.readable.info = &info
				case "hard_read":
					f.readable.reads = []retestInputTestRead{{0, errors.New("provider-detail-marker")}}
				}
				h.rejected(h.run(), persistKeyDiagnostic(index), index+1)
				assertRetestInputLedger(t, f, want, closes)
				if fault == "hard_read" {
					if !reflect.DeepEqual(f.readable.readSize, []int{32}) {
						t.Fatal("hard-read fault did not reach the bounded read")
					}
				} else if len(f.readable.readSize) != 0 {
					t.Fatal("owner failure reached a read")
				}
				if index == 1 {
					assertRetestInputLedger(t, h.inputs[0], map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
				}
			})
		}
	}
}

func TestPersistKeyTerminalProbeFaultsReachTheProbe(t *testing.T) {
	for index, role := range []string{"fingerprint", "public"} {
		for _, fault := range []string{"extra", "no_progress", "hard"} {
			t.Run(role+"/"+fault, func(t *testing.T) {
				h := newPersistKeyFixture(t, false, false)
				probe := retestInputTestRead{1, io.EOF}
				if fault == "no_progress" {
					probe = retestInputTestRead{0, nil}
				}
				if fault == "hard" {
					probe = retestInputTestRead{0, errors.New("provider-detail-marker")}
				}
				h.inputs[index].readable.reads = []retestInputTestRead{{32, nil}, probe}
				h.rejected(h.run(), persistKeyDiagnostic(index), index+1)
				assertRetestInputLedger(t, h.inputs[index], map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
				if !reflect.DeepEqual(h.inputs[index].readable.readSize, []int{32, 1}) {
					t.Fatal("negative did not reach a one-byte probe after 32 bytes")
				}
			})
		}
	}
}

func TestPersistKeyShapesAndErrorsPrecedeLateCancellation(t *testing.T) {
	for index, role := range []string{"fingerprint", "public"} {
		for _, fault := range []string{"nil", "short", "long", "bytes_error", "pure_cancel", "bytes_cancel", "joined_hard", "joined_cleanup"} {
			for _, timing := range []string{"live", "cancel"} {
				t.Run(role+"/"+fault+"/"+timing, func(t *testing.T) {
					h := newPersistKeyFixture(t, false, false)
					ctx, cancel := context.WithCancel(h.ctx)
					defer cancel()
					h.ctx = ctx
					h.acquire = func(i int, ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
						if i != index {
							return h.core(i, ctx, path, kind)
						}
						if timing == "cancel" {
							cancel()
						}
						switch fault {
						case "nil":
							return nil, nil
						case "short":
							return make([]byte, 31), nil
						case "long":
							return make([]byte, 33), nil
						case "bytes_error":
							return make([]byte, 32), errors.New("provider-detail-marker")
						case "pure_cancel":
							return nil, context.Canceled
						case "bytes_cancel":
							return make([]byte, 32), context.Canceled
						case "joined_hard":
							return nil, errors.Join(context.Canceled, errors.New("provider-detail-marker"))
						default:
							return nil, errors.Join(context.Canceled, errRetestInputCleanup)
						}
					}
					h.rejected(h.run(), persistKeyDiagnostic(index), index+1)
				})
			}
		}
	}
}

type persistKeyNilContext struct{ context.Context }

func (*persistKeyNilContext) Err() error { panic("typed nil context method called") }

type persistKeyBoundaryContext struct {
	context.Context
	calls int
}

func (ctx *persistKeyBoundaryContext) Err() error {
	ctx.calls++
	if ctx.calls >= 3 {
		return context.Canceled
	}
	return nil
}

func TestPersistKeyUsageAuthorizationAndDependenciesKeepOrder(t *testing.T) {
	for _, fault := range []string{"unknown_flag", "missing_report", "extra_report", "missing_fingerprint", "mixed_trust", "missing_trusted"} {
		t.Run("usage/"+fault, func(t *testing.T) {
			h := newPersistKeyFixture(t, false, false)
			switch fault {
			case "unknown_flag":
				h.args = []string{"-unknown-persist-key-flag"}
			case "missing_report":
				h.args = h.args[:len(h.args)-1]
			case "extra_report":
				h.args = append(h.args, "extra")
			case "missing_fingerprint":
				h.argument("-fingerprint-key", "")
			case "mixed_trust":
				h.args[6] = "-self-issued"
			case "missing_trusted":
				h.argument("-public-key", "")
			}
			if code := h.run(); code != 2 || h.reads != 0 || h.persists != 0 || h.stdout.Len() != 0 {
				t.Fatal("usage did not precede input/consumer work")
			}
			h.preserved()
		})
	}
	for _, dependency := range []string{"nil_context", "typed_nil_context", "nil_reader", "nil_persister", "stopped_context"} {
		for _, auth := range []string{"valid", "invalid"} {
			t.Run(dependency+"/"+auth, func(t *testing.T) {
				h := newPersistKeyFixture(t, false, false)
				switch dependency {
				case "nil_context":
					h.ctx = nil
				case "typed_nil_context":
					h.ctx = (*persistKeyNilContext)(nil)
				case "nil_reader":
					h.reader = nil
				case "nil_persister":
					h.consumer = nil
				case "stopped_context":
					ctx, cancel := context.WithCancel(h.ctx)
					cancel()
					h.ctx = ctx
				}
				if auth == "invalid" {
					if err := os.WriteFile(h.authorizationPath, []byte("{}"), 0600); err != nil {
						t.Fatal(err)
					}
					if code := h.run(); code != 1 || h.reads != 0 || h.persists != 0 || h.stdout.Len() != 0 || !strings.HasPrefix(h.stderr.String(), "validate report-target authorization: ") {
						t.Fatal("dependency admission displaced authorization failure")
					}
					h.preserved()
				} else {
					h.rejected(h.run(), "persist input acquisition failed", 0)
				}
			})
		}
	}
	for _, auth := range []string{"missing", "trailing", "unknown"} {
		t.Run("authorization/"+auth, func(t *testing.T) {
			h := newPersistKeyFixture(t, true, false)
			encoded, err := os.ReadFile(h.authorizationPath)
			if err != nil {
				t.Fatal(err)
			}
			switch auth {
			case "missing":
				h.argument("-authorization", h.authorizationPath+".missing")
			case "trailing":
				encoded = append(encoded, []byte(" {}")...)
			case "unknown":
				encoded = append([]byte("{\"unexpected\":true,"), encoded[1:]...)
			}
			if auth != "missing" {
				if err := os.WriteFile(h.authorizationPath, encoded, 0600); err != nil {
					t.Fatal(err)
				}
			}
			prefix := "validate report-target authorization: "
			if auth == "missing" {
				prefix = "read report-target authorization: "
			}
			if code := h.run(); code != 1 || h.reads != 0 || h.persists != 0 || h.stdout.Len() != 0 || !strings.HasPrefix(h.stderr.String(), prefix) {
				t.Fatal("authorization prerequisite changed")
			}
			h.preserved()
		})
	}
}

func TestPersistKeyCancellationStopsAtEachAcquisitionBoundary(t *testing.T) {
	for index, role := range []string{"fingerprint", "public"} {
		for _, timing := range []string{"read", "close", "after_closed"} {
			t.Run(role+"/"+timing, func(t *testing.T) {
				h := newPersistKeyFixture(t, false, false)
				ctx, cancel := context.WithCancel(h.ctx)
				defer cancel()
				h.ctx = ctx
				diagnostic := persistKeyDiagnostic(index)
				if timing == "after_closed" {
					h.afterRead = func(i int) {
						if i == index {
							cancel()
						}
					}
					diagnostic = "persist input acquisition failed"
				} else {
					h.inputs[index].readable.hook = func(operation string) {
						if operation == timing {
							cancel()
						}
					}
				}
				h.rejected(h.run(), diagnostic, index+1)
				assertRetestInputLedger(t, h.inputs[index], map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
				if timing != "read" && !reflect.DeepEqual(h.inputs[index].readable.readSize, []int{32, 1}) {
					t.Fatal("cancellation did not follow complete key capture")
				}
			})
		}
	}
	t.Run("before_public", func(t *testing.T) {
		h := newPersistKeyFixture(t, false, false)
		h.ctx = &persistKeyBoundaryContext{Context: h.ctx}
		// Isolate the command's three Err checks; actual-core owners are covered above.
		h.acquire = func(int, context.Context, string, retestInputKind) ([]byte, error) {
			return append([]byte(nil), h.key...), nil
		}
		h.rejected(h.run(), "persist input acquisition failed", 1)
	})
	t.Run("after_persistence_entry", func(t *testing.T) {
		h := newPersistKeyFixture(t, false, true)
		ctx, cancel := context.WithCancel(h.ctx)
		defer cancel()
		h.ctx, h.afterPersist = ctx, cancel
		code := h.run()
		h.success(code, h.stdout.Bytes())
	})
}

func TestPersistKeyFailuresKeepSourceAndVerifierPrecedence(t *testing.T) {
	for _, source := range []string{"valid", "missing"} {
		t.Run("fingerprint/"+source, func(t *testing.T) {
			h := newPersistKeyFixture(t, false, false)
			if source == "missing" {
				h.args[len(h.args)-1] = h.sourcePath + ".missing"
			}
			h.inputs[0].readable.closeErr = errors.New("provider-detail-marker")
			h.rejected(h.run(), persistKeyDiagnostic(0), 1)
		})
	}
	t.Run("missing_report_precedes_public", func(t *testing.T) {
		h := newPersistKeyFixture(t, false, false)
		h.args[len(h.args)-1] = h.sourcePath + ".missing"
		if code := h.run(); code != 1 || h.reads != 1 || h.persists != 0 || h.stdout.Len() != 0 || !strings.HasPrefix(h.stderr.String(), "open source report: ") {
			t.Fatal("public key acquisition displaced the earlier report open")
		}
		h.preserved()
	})
	for _, fault := range []string{"wrong_public", "wrong_id", "invalid_signature"} {
		t.Run("verification/"+fault, func(t *testing.T) {
			h := newPersistKeyFixture(t, false, false)
			switch fault {
			case "wrong_public":
				h.inputs[1].readable.data = bytes.Repeat([]byte{0x15}, 32)
			case "wrong_id":
				h.argument("-key-id", "wrong-id")
			case "invalid_signature":
				envelope := h.signed.artifact.Envelope
				envelope.Signature = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
				var raw bytes.Buffer
				if err := bundle.Write(&raw, h.signed.artifact.InventoryJSON, envelope, nil); err != nil {
					t.Fatal("invalid-signature archive shape failed", err)
				}
				h.original = append([]byte(nil), raw.Bytes()...)
				if _, err := bundle.Verify(bytes.NewReader(h.original), int64(len(h.original)), report.Keyring{"comparison-fixture-key": h.public}); err == nil {
					t.Fatal("invalid-signature precondition did not fail")
				}
				if err := os.WriteFile(h.sourcePath, h.original, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if code := h.run(); code != 1 || h.reads != 2 || h.persists != 0 || h.stdout.Len() != 0 || !strings.HasPrefix(h.stderr.String(), "verify source report: ") || h.stderr.String() == persistKeyDiagnostic(1)+"\n" {
				t.Fatal("real verification rejection or retained diagnostic changed")
			}
			h.preserved()
		})
	}
}

type persistKeyOutput struct {
	bytes.Buffer
	mode   string
	closes int
}

func (out *persistKeyOutput) Write(data []byte) (int, error) {
	if out.mode == "error" {
		return 0, errors.New("fixture receipt writer")
	}
	if out.mode == "short" {
		return len(data) - 1, nil
	}
	return out.Buffer.Write(data)
}
func (out *persistKeyOutput) Close() error { out.closes++; return nil }

func TestPersistKeysPreserveReceiptAndOutputFailurePolicies(t *testing.T) {
	for _, fault := range []string{"persister", "file_sync", "read_back", "directory_sync"} {
		t.Run("receipt/"+fault, func(t *testing.T) {
			h := newPersistKeyFixture(t, false, true)
			if fault == "persister" {
				h.persistError = errors.New("fixture persistence error")
			} else {
				h.changeReceipt = func(receipt *reporttarget.Receipt) {
					switch fault {
					case "file_sync":
						receipt.FileSynced = false
					case "read_back":
						receipt.ReadBackVerified = false
					default:
						receipt.DirectorySynced = false
					}
				}
			}
			want := "persist report: persistence did not complete verification and synchronization\n"
			if fault == "persister" {
				want = "persist report: fixture persistence error\n"
			}
			if code := h.run(); code != 1 || h.persists != 1 || h.stdout.Len() != 0 || h.stderr.String() != want {
				t.Fatal("retained persistence/receipt failure policy changed")
			}
			h.preserved()
		})
	}
	for _, mode := range []string{"success", "short", "error"} {
		t.Run("stdout/"+mode, func(t *testing.T) {
			h := newPersistKeyFixture(t, true, false)
			writer := &persistKeyOutput{mode: mode}
			h.output = writer
			code := h.run()
			if writer.closes != 0 || h.persists != 1 {
				t.Fatal("caller stdout ownership or prior persistence changed")
			}
			if mode == "success" {
				h.success(code, writer.Bytes())
			} else {
				want := "write persistence receipt result: short write\n"
				if mode == "error" {
					want = "write persistence receipt result: fixture receipt writer\n"
				}
				if code != 1 || h.stderr.String() != want {
					t.Fatal("output diagnostic changed", h.stderr.String())
				}
				h.preserved()
			}
		})
	}
	for _, mode := range []string{"new", "collision"} {
		t.Run("file/"+mode, func(t *testing.T) {
			h := newPersistKeyFixture(t, false, false)
			path := filepath.Join(t.TempDir(), "receipt.json")
			if mode == "collision" {
				if err := os.WriteFile(path, []byte("existing fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			h.args = append([]string{"-output", path}, h.args...)
			code := h.run()
			encoded, err := os.ReadFile(path)
			if err != nil || h.stdout.Len() != 0 {
				t.Fatal("receipt destination did not retain exclusive file behavior")
			}
			if mode == "new" {
				h.success(code, encoded)
			} else if code != 1 || h.persists != 1 || string(encoded) != "existing fixture" || !strings.HasPrefix(h.stderr.String(), "write persistence receipt result: open output: ") {
				t.Fatal("existing output changed or persistence was incorrectly rolled back")
			}
			h.preserved()
		})
	}
}
