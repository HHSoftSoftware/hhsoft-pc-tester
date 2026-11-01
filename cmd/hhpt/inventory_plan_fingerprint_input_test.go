package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

type inventoryPlanFingerprintCommand struct {
	name         string
	run          func(context.Context, []string, io.Writer, io.Writer, snapshotCollector, destinationWriter, retestInputReader) int
	defaultInput func(context.Context, []string, io.Writer, io.Writer, snapshotCollector, destinationWriter) int
	dispatch     func(context.Context, []string, io.Writer, io.Writer) int
}

func inventoryPlanFingerprintCommands() []inventoryPlanFingerprintCommand {
	return []inventoryPlanFingerprintCommand{
		{"inventory", runInventoryWithInput, runInventoryWithDestination, runInventory},
		{"plan", runPlanWithInput, runPlanWithDestination, runPlan},
	}
}

type inventoryPlanFingerprintHarness struct {
	t                                                  *testing.T
	command                                            inventoryPlanFingerprintCommand
	ctx                                                context.Context
	args                                               []string
	keyPath                                            string
	fixture                                            *retestInputFixture
	readInput                                          retestInputReader
	collect                                            snapshotCollector
	writeOutput                                        destinationWriter
	afterRead                                          func()
	readCalls, collectCalls, writeCalls, callbackCalls int
	readData                                           []byte
	readError                                          error
	stdout, stderr                                     bytes.Buffer
}

func newInventoryPlanFingerprintHarness(t *testing.T, command inventoryPlanFingerprintCommand) *inventoryPlanFingerprintHarness {
	t.Helper()
	h := &inventoryPlanFingerprintHarness{
		t: t, command: command, ctx: t.Context(), keyPath: " synthetic fingerprint path ",
		fixture: newRetestInputFixture(32),
	}
	h.args = []string{"-fingerprint-key", h.keyPath}
	if command.name == "plan" {
		h.args = append(h.args, "-architecture", "x86_64")
	}
	h.readInput = func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
		return readRetestInputWithOperations(ctx, path, kind, h.fixture.operations())
	}
	h.collect = func(context.Context) (inventory.Snapshot, error) { return inventoryPlanFingerprintSnapshot(), nil }
	h.writeOutput = func(path string, output io.Writer, callback func(io.Writer) error) error {
		return writeDestinationWith(path, output, callback, func(string, int, os.FileMode) (io.WriteCloser, error) {
			t.Error("fingerprint test unexpectedly attempted an output-file open")
			return nil, errors.New("unexpected output open")
		})
	}
	return h
}

func (h *inventoryPlanFingerprintHarness) run() int {
	h.t.Helper()
	readInput := h.readInput
	if readInput != nil {
		readInput = func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
			h.readCalls++
			if ctx != h.ctx || path != h.keyPath || kind != retestFingerprintKeyInput {
				h.t.Fatal("fingerprint context, literal path or requested kind changed")
			}
			data, err := h.readInput(ctx, path, kind)
			h.readData, h.readError = data, err
			if h.afterRead != nil {
				h.afterRead()
			}
			return data, err
		}
	}
	collect := h.collect
	if collect != nil {
		collect = func(ctx context.Context) (inventory.Snapshot, error) {
			h.collectCalls++
			if ctx != h.ctx || h.readCalls != 1 {
				h.t.Fatal("collection preceded the single key acquisition")
			}
			h.requireClosed()
			return h.collect(ctx)
		}
	}
	writeOutput := h.writeOutput
	if writeOutput != nil {
		writeOutput = func(path string, output io.Writer, callback func(io.Writer) error) error {
			h.writeCalls++
			if h.collectCalls != 1 || path != "-" || output != &h.stdout || callback == nil {
				h.t.Fatal("destination preceded collection or changed output arguments")
			}
			h.requireClosed()
			return h.writeOutput(path, output, func(writer io.Writer) error {
				h.callbackCalls++
				return callback(writer)
			})
		}
	}
	return h.command.run(h.ctx, h.args, &h.stdout, &h.stderr, collect, writeOutput, readInput)
}

func (h *inventoryPlanFingerprintHarness) requireClosed() {
	h.t.Helper()
	if !reflect.DeepEqual(h.fixture.ledger.acquired, h.fixture.ledger.closed) {
		h.t.Fatal("fingerprint owner remained open when a consumer was reached")
	}
}

func (h *inventoryPlanFingerprintHarness) requireFailure(code int, message string, reads, collects, writes int) {
	h.t.Helper()
	if code != 1 || h.stderr.String() != message || h.stdout.Len() != 0 ||
		h.readCalls != reads || h.collectCalls != collects || h.writeCalls != writes {
		h.t.Fatalf("fingerprint failure boundary changed: code=%d reads=%d collect=%d output=%d",
			code, h.readCalls, h.collectCalls, h.writeCalls)
	}
	h.requireClosed()
}

func (h *inventoryPlanFingerprintHarness) requireCoreFailure(code int) {
	h.t.Helper()
	h.requireFailure(code, "read fingerprint key: input acquisition failed\n", 1, 0, 0)
	if h.readData != nil || h.readError == nil || strings.Contains(h.readError.Error(), "sensitive-input-marker") {
		h.t.Fatal("checked reader retained bytes or provider diagnostics on failure")
	}
}

func (h *inventoryPlanFingerprintHarness) requireSuccess(code int) {
	h.t.Helper()
	if code != 0 || h.stderr.Len() != 0 || h.stdout.Len() == 0 ||
		h.readCalls != 1 || h.collectCalls != 1 || h.writeCalls != 1 || h.callbackCalls != 1 {
		h.t.Fatalf("fingerprint success boundary changed: code=%d reads=%d collect=%d output=%d callback=%d",
			code, h.readCalls, h.collectCalls, h.writeCalls, h.callbackCalls)
	}
	h.requireClosed()
}

func inventoryPlanFingerprintSnapshot() inventory.Snapshot {
	return inventory.Snapshot{SchemaVersion: 1, Components: []inventory.Component{
		{Type: inventory.ComponentBlock, ID: "block:oracle-fixture", Attributes: []inventory.Attribute{{Name: "kind", Value: "device"}}},
		{Type: inventory.ComponentCPU, ID: "cpu:0", Attributes: []inventory.Attribute{{Name: "model", Value: "Oracle CPU"}}},
		{Type: inventory.ComponentMemory, ID: "memory:system", Attributes: []inventory.Attribute{{Name: "total_kib", Value: "8388608"}}},
		{Type: inventory.ComponentMemoryDevice, ID: "memory_device:1", Attributes: []inventory.Attribute{
			{Name: "capacity_bytes", Value: "8589934592"}, {Name: "capacity_bytes_state", Value: "reported"},
			{Name: "form_factor", Value: "dimm"}, {Name: "form_factor_state", Value: "reported"},
			{Name: "memory_type", Value: "ddr4"}, {Name: "memory_type_state", Value: "reported"},
			{Name: "observation_source", Value: "smbios_type17"},
		}},
		{Type: inventory.ComponentSystem, ID: "system0", Attributes: []inventory.Attribute{
			{Name: "product_uuid", Value: "11223344-5566-7788-99aa-bbccddeeff00"}, {Name: "vendor", Value: "Oracle Fixture"},
		}},
	}}
}

func inventoryPlanFingerprintKey(offset byte) []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = offset + byte(i)
	}
	return key
}

func TestInventoryPlanFingerprintFixedOutputs(t *testing.T) {
	// These digests bind independently checked command bytes for literal
	// synthetic fixtures, including formatting and the final newline. Plan
	// bytes include the current catalog; the inventory goldens are unchanged.
	cases := []struct {
		name, command, digest        string
		size                         int
		filtered, selected, standard bool
	}{
		{"inventory-default", "inventory", "eed5302c9ef92a0eafb9df1a5f0b378196ddbf95503697852af07fb03f3d635b", 2588, false, false, false},
		{"inventory-filtered", "inventory", "e686df6afb7f98552411b6b5b8f01cfee513ce1e129e163fa67716508926836e", 2289, true, false, false},
		{"plan-default", "plan", "bafc819f70e8b8e0a09504b657247e3147e9f8508a97139cc85db453bc264dda", 7135, false, false, false},
		{"plan-selected", "plan", "83dc1015c516c0f4ec29e2a18836dcee5205c4580de36c83347a785b5714450d", 6774, true, true, false},
		{"plan-standard", "plan", "fb220ba46b64fc7791b1b11c1aecb41f20a15d6d7652c03e6a4dab9cd5a02a2e", 7139, false, false, true},
	}
	for _, command := range inventoryPlanFingerprintCommands() {
		for _, test := range cases {
			if command.name != test.command {
				continue
			}
			t.Run(test.name, func(t *testing.T) {
				h := newInventoryPlanFingerprintHarness(t, command)
				key := inventoryPlanFingerprintKey(0)
				if test.filtered {
					key = inventoryPlanFingerprintKey(32)
					h.args = append(h.args, "-key-epoch", " oracle-v1 ", "-output", "-")
					path := writeTestKey(t, t.TempDir(), "components.fixture", []byte("block:oracle-fixture\n"))
					h.args = append(h.args, "-fixture-components", path)
				}
				if test.selected {
					path := writeTestKey(t, t.TempDir(), "selection.fixture", []byte(`{"excluded_tests":["usb.readiness.quick","cpu.quick"]}`))
					h.args = append(h.args, "-selection-file", path, "-profile", " QuIcK ")
				}
				if test.standard {
					h.args = append(h.args, "-profile", " STANDARD ", "-architecture", " x86_64 ")
				}
				h.fixture.readable.data = append([]byte(nil), key...)
				h.requireSuccess(h.run())
				if !bytes.Equal(h.readData, key) || h.readError != nil {
					t.Fatal("fingerprint bytes changed before use")
				}
				assertRetestInputLedger(t, h.fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
				if !reflect.DeepEqual(h.fixture.readable.readSize, []int{32, 1}) {
					t.Fatal("fingerprint read lost its exact bound or EOF probe")
				}
				digest := fmt.Sprintf("%x", sha256.Sum256(h.stdout.Bytes()))
				if h.stdout.Len() != test.size || digest != test.digest {
					t.Fatal("successful command bytes changed")
				}
			})
		}
	}
}

func TestInventoryPlanFingerprintCallerAdmission(t *testing.T) {
	private := errors.New("sensitive-input-marker")
	cases := []struct {
		name     string
		data     []byte
		err      error
		accepted bool
	}{
		{"nil-data", nil, nil, false},
		{"empty-data", []byte{}, nil, false},
		{"short-31", bytes.Repeat([]byte{0x31}, 31), nil, false},
		{"long-33", bytes.Repeat([]byte{0x33}, 33), nil, false},
		{"signing-size-64", bytes.Repeat([]byte{0x64}, 64), nil, false},
		{"larger-128", bytes.Repeat([]byte{0x28}, 128), nil, false},
		{"error-without-bytes", nil, private, false},
		{"complete-bytes-and-error", inventoryPlanFingerprintKey(0), private, false},
		{"short-bytes-and-error", bytes.Repeat([]byte{0x31}, 31), private, false},
		{"zero-key-accepted", make([]byte, 32), nil, true},
		{"literal-key-accepted", inventoryPlanFingerprintKey(0), nil, true},
	}
	for _, command := range inventoryPlanFingerprintCommands() {
		for _, test := range cases {
			t.Run(command.name+"/"+test.name, func(t *testing.T) {
				h := newInventoryPlanFingerprintHarness(t, command)
				h.readInput = func(context.Context, string, retestInputKind) ([]byte, error) { return test.data, test.err }
				code := h.run()
				if test.accepted {
					h.requireSuccess(code)
				} else {
					h.requireFailure(code, "read fingerprint key: input acquisition failed\n", 1, 0, 0)
				}
				if len(h.fixture.ledger.calls) != 0 {
					t.Fatal("caller-only fixture unexpectedly exercised owner operations")
				}
			})
		}
	}
}

type inventoryPlanFingerprintNilContext struct{ context.Context }

func TestInventoryPlanFingerprintDependencyAdmission(t *testing.T) {
	for _, command := range inventoryPlanFingerprintCommands() {
		for _, name := range []string{"nil-context", "typed-nil-context", "nil-reader", "nil-collector", "nil-writer", "canceled", "deadline"} {
			t.Run(command.name+"/"+name, func(t *testing.T) {
				h := newInventoryPlanFingerprintHarness(t, command)
				switch name {
				case "nil-context":
					h.ctx = nil
				case "typed-nil-context":
					var missing *inventoryPlanFingerprintNilContext
					h.ctx = missing
				case "nil-reader":
					h.readInput = nil
				case "nil-collector":
					h.collect = nil
				case "nil-writer":
					h.writeOutput = nil
				case "canceled":
					ctx, cancel := context.WithCancel(t.Context())
					cancel()
					h.ctx = ctx
				case "deadline":
					ctx, cancel := context.WithDeadline(t.Context(), time.Unix(1, 0))
					defer cancel()
					h.ctx = ctx
				}
				h.requireFailure(h.run(), command.name+" input acquisition failed\n", 0, 0, 0)
				if len(h.fixture.ledger.calls) != 0 {
					t.Fatal("dependency failure acquired an owner")
				}
			})
		}
	}
}

func TestInventoryPlanFingerprintUsageBeforeInput(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"missing-key", nil},
		{"output-without-key", []string{"-output", "must-not-be-created"}},
		{"invalid-flag", []string{"-not-a-supported-flag"}},
	}
	for _, command := range inventoryPlanFingerprintCommands() {
		for _, test := range cases {
			t.Run(command.name+"/"+test.name, func(t *testing.T) {
				h := newInventoryPlanFingerprintHarness(t, command)
				h.args = test.args
				h.ctx, h.readInput, h.collect, h.writeOutput = nil, nil, nil, nil
				if code := h.run(); code != 2 || h.stderr.Len() == 0 || h.stdout.Len() != 0 ||
					h.readCalls != 0 || h.collectCalls != 0 || h.writeCalls != 0 ||
					strings.Contains(h.stderr.String(), "input acquisition failed") {
					t.Fatal("usage validation lost precedence over input dependency admission")
				}
			})
		}
	}
}

func TestPlanFingerprintSelectionBeforeInput(t *testing.T) {
	cases := []struct{ name, contents, prefix string }{
		{"missing", "", "read quick-test selection:"},
		{"directory", "", "read quick-test selection:"},
		{"invalid-json", "{", "validate quick-test selection:"},
		{"unknown-test", `{"excluded_tests":["unknown"]}`, "validate quick-test selection:"},
		{"null-array", `{"excluded_tests":null}`, "validate quick-test selection:"},
		{"valid-before-nil-context", `{"excluded_tests":[]}`, "plan input acquisition failed\n"},
	}
	for _, command := range inventoryPlanFingerprintCommands() {
		if command.name != "plan" {
			continue
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				h := newInventoryPlanFingerprintHarness(t, command)
				directory := t.TempDir()
				path := directory + string(os.PathSeparator) + "selection.fixture"
				if test.name == "directory" {
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal("selection directory fixture failed")
					}
				} else if test.name != "missing" {
					path = writeTestKey(t, directory, "selection.fixture", []byte(test.contents))
				}
				h.args = append(h.args, "-selection-file", path)
				h.ctx, h.readInput, h.collect, h.writeOutput = nil, nil, nil, nil
				if code := h.run(); code != 1 || !strings.HasPrefix(h.stderr.String(), test.prefix) ||
					h.stdout.Len() != 0 || h.readCalls != 0 || h.collectCalls != 0 || h.writeCalls != 0 {
					t.Fatal("selection validation lost its existing precedence")
				}
			})
		}
	}
}

func TestPlanFingerprintPositionalBeforeInput(t *testing.T) {
	for _, command := range inventoryPlanFingerprintCommands() {
		if command.name != "plan" {
			continue
		}
		h := newInventoryPlanFingerprintHarness(t, command)
		h.args = append(h.args, "stray")
		h.ctx, h.readInput, h.collect, h.writeOutput = nil, nil, nil, nil
		if code := h.run(); code != 2 || h.stderr.String() != "plan accepts no positional arguments\n" ||
			h.stdout.Len() != 0 || h.readCalls != 0 || h.collectCalls != 0 || h.writeCalls != 0 {
			t.Fatal("plan positional validation reached input acquisition")
		}
	}
}

func TestInventoryFingerprintRetainsPositionalTail(t *testing.T) {
	for _, command := range inventoryPlanFingerprintCommands() {
		if command.name != "inventory" {
			continue
		}
		h := newInventoryPlanFingerprintHarness(t, command)
		h.args = append(h.args, "existing-positional-tail")
		h.requireSuccess(h.run())
		if fmt.Sprintf("%x", sha256.Sum256(h.stdout.Bytes())) != "eed5302c9ef92a0eafb9df1a5f0b378196ddbf95503697852af07fb03f3d635b" {
			t.Fatal("inventory positional behavior changed")
		}
	}
}

func inventoryPlanFingerprintOperationCount(fixture *retestInputFixture, wanted string) int {
	count := 0
	for _, call := range fixture.ledger.calls {
		if call == wanted {
			count++
		}
	}
	return count
}

func TestInventoryPlanFingerprintOwnerAcquisition(t *testing.T) {
	private := errors.New("sensitive-input-marker")
	for _, command := range inventoryPlanFingerprintCommands() {
		for _, stage := range []string{"metadata", "readable"} {
			for _, shape := range []string{"nil", "nil-error", "typed-nil", "typed-nil-error", "owner-error"} {
				t.Run(command.name+"/"+stage+"/"+shape, func(t *testing.T) {
					h := newInventoryPlanFingerprintHarness(t, command)
					var missing *retestInputTestFile
					var failure error
					if strings.HasSuffix(shape, "error") {
						failure = private
					}
					if stage == "metadata" {
						h.fixture.metadataErr = failure
						switch shape {
						case "nil", "nil-error":
							h.fixture.metadataOut = nil
						case "typed-nil", "typed-nil-error":
							h.fixture.metadataOut = missing
						}
					} else {
						h.fixture.readableErr = failure
						switch shape {
						case "nil", "nil-error":
							h.fixture.readableOut = nil
						case "typed-nil", "typed-nil-error":
							h.fixture.readableOut = missing
						}
					}
					h.requireCoreFailure(h.run())
					want := map[string]int{}
					var closes []string
					metadataStats, readableOpens := 0, 0
					if stage == "readable" || shape == "owner-error" {
						want["metadata"] = 1
						closes = []string{"metadata"}
					}
					if stage == "readable" {
						metadataStats, readableOpens = 1, 1
						if shape == "owner-error" {
							want["readable"] = 1
							closes = []string{"readable", "metadata"}
						}
					}
					assertRetestInputLedger(t, h.fixture, want, closes)
					if inventoryPlanFingerprintOperationCount(h.fixture, "open.metadata") != 1 ||
						inventoryPlanFingerprintOperationCount(h.fixture, "metadata.stat") != metadataStats ||
						inventoryPlanFingerprintOperationCount(h.fixture, "open.readable") != readableOpens ||
						inventoryPlanFingerprintOperationCount(h.fixture, "readable.stat") != 0 ||
						len(h.fixture.readable.readSize) != 0 {
						t.Fatal("owner admission reached a later operation")
					}
				})
			}
		}
	}
}

func TestInventoryPlanFingerprintDuplicateOwner(t *testing.T) {
	for _, command := range inventoryPlanFingerprintCommands() {
		for _, name := range []string{"same-owner", "same-owner-with-error"} {
			t.Run(command.name+"/"+name, func(t *testing.T) {
				h := newInventoryPlanFingerprintHarness(t, command)
				h.fixture.readableOut = h.fixture.metadata
				if name == "same-owner-with-error" {
					h.fixture.readableErr = errors.New("sensitive-input-marker")
				}
				h.requireCoreFailure(h.run())
				assertRetestInputLedger(t, h.fixture, map[string]int{"metadata": 1}, []string{"metadata"})
				if inventoryPlanFingerprintOperationCount(h.fixture, "open.readable") != 1 ||
					inventoryPlanFingerprintOperationCount(h.fixture, "metadata.stat") != 1 ||
					inventoryPlanFingerprintOperationCount(h.fixture, "readable.stat") != 0 ||
					len(h.fixture.metadata.readSize) != 0 || len(h.fixture.readable.readSize) != 0 {
					t.Fatal("duplicate owner was read or closed more than once")
				}
			})
		}
	}
}

func TestInventoryPlanFingerprintMetadataAdmission(t *testing.T) {
	private := errors.New("sensitive-input-marker")
	cases := []struct {
		name     string
		change   func(*retestInputFixture)
		accepted bool
	}{
		{"zero-size", func(f *retestInputFixture) { f.metadata.info.size = 0 }, false},
		{"short-31", func(f *retestInputFixture) { f.metadata.info.size = 31 }, false},
		{"long-33", func(f *retestInputFixture) { f.metadata.info.size = 33 }, false},
		{"large-size", func(f *retestInputFixture) { f.metadata.info.size = 1 << 40 }, false},
		{"directory", func(f *retestInputFixture) { f.metadata.info.mode = os.ModeDir | 0600 }, false},
		{"fifo", func(f *retestInputFixture) { f.metadata.info.mode = os.ModeNamedPipe | 0600 }, false},
		{"device", func(f *retestInputFixture) { f.metadata.info.mode = os.ModeDevice | 0600 }, false},
		{"link", func(f *retestInputFixture) { f.metadata.info.mode = os.ModeSymlink | 0600 }, false},
		{"nil-info", func(f *retestInputFixture) { f.metadata.stats = []retestInputTestStat{{}} }, false},
		{"typed-nil-info", func(f *retestInputFixture) {
			var missing *retestInputTestInfo
			f.metadata.stats = []retestInputTestStat{{info: missing}}
		}, false},
		{"stat-error", func(f *retestInputFixture) { f.metadata.stats = []retestInputTestStat{{err: private}} }, false},
		{"info-and-error", func(f *retestInputFixture) {
			f.metadata.stats = []retestInputTestStat{{info: f.metadata.info, err: private}}
		}, false},
		{"regular-permissions", func(f *retestInputFixture) { f.metadata.info.mode = 0644 }, true},
	}
	for _, command := range inventoryPlanFingerprintCommands() {
		for _, test := range cases {
			t.Run(command.name+"/"+test.name, func(t *testing.T) {
				h := newInventoryPlanFingerprintHarness(t, command)
				test.change(h.fixture)
				code := h.run()
				if test.accepted {
					h.requireSuccess(code)
					assertRetestInputLedger(t, h.fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
					if !reflect.DeepEqual(h.fixture.readable.readSize, []int{32, 1}) {
						t.Fatal("regular file lost bounded completion")
					}
				} else {
					h.requireCoreFailure(code)
					assertRetestInputLedger(t, h.fixture, map[string]int{"metadata": 1}, []string{"metadata"})
					if inventoryPlanFingerprintOperationCount(h.fixture, "open.readable") != 0 || len(h.fixture.readable.readSize) != 0 {
						t.Fatal("ineligible metadata acquired a readable owner")
					}
				}
			})
		}
	}
}

func TestInventoryPlanFingerprintDescriptorStability(t *testing.T) {
	private := errors.New("sensitive-input-marker")
	for _, command := range inventoryPlanFingerprintCommands() {
		for _, stage := range []string{"opened", "readable-final", "metadata-final"} {
			for _, field := range []string{"identity", "mode", "size", "modified", "nil-info", "typed-nil-info", "error", "info-and-error"} {
				t.Run(command.name+"/"+stage+"/"+field, func(t *testing.T) {
					h := newInventoryPlanFingerprintHarness(t, command)
					changed := *h.fixture.metadata.info
					bad := retestInputTestStat{info: &changed}
					switch field {
					case "identity":
						changed.id++
					case "mode":
						changed.mode = 0644
					case "size":
						changed.size++
					case "modified":
						changed.modified = changed.modified.Add(time.Nanosecond)
					case "nil-info":
						bad.info = nil
					case "typed-nil-info":
						var missing *retestInputTestInfo
						bad.info = missing
					case "error":
						bad.info, bad.err = nil, private
					case "info-and-error":
						bad.err = private
					}
					good := retestInputTestStat{info: h.fixture.metadata.info}
					switch stage {
					case "opened":
						h.fixture.readable.stats = []retestInputTestStat{bad}
					case "readable-final":
						h.fixture.readable.stats = []retestInputTestStat{good, bad}
					case "metadata-final":
						h.fixture.metadata.stats = []retestInputTestStat{good, bad}
					}
					h.requireCoreFailure(h.run())
					assertRetestInputLedger(t, h.fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
					wantMetadataStats, wantReadableStats := 1, 1
					if stage != "opened" {
						wantReadableStats = 2
					}
					if stage == "metadata-final" {
						wantMetadataStats = 2
					}
					if inventoryPlanFingerprintOperationCount(h.fixture, "metadata.stat") != wantMetadataStats ||
						inventoryPlanFingerprintOperationCount(h.fixture, "readable.stat") != wantReadableStats {
						t.Fatal("stability failure reached the wrong stat boundary")
					}
					if stage == "opened" {
						if len(h.fixture.readable.readSize) != 0 {
							t.Fatal("read occurred before descriptor stability")
						}
					} else if !reflect.DeepEqual(h.fixture.readable.readSize, []int{32, 1}) {
						t.Fatal("final stability case did not reach a complete bounded read")
					}
				})
			}
		}
	}
}

func TestInventoryPlanFingerprintReadCompletion(t *testing.T) {
	private := errors.New("sensitive-input-marker")
	cases := []struct {
		name                      string
		reads                     []retestInputTestRead
		requests                  []int
		accepted, mutateAfterRead bool
	}{
		{"complete", nil, []int{32, 1}, true, false},
		{"split-progress", []retestInputTestRead{{7, nil}, {25, nil}, {0, io.EOF}}, []int{32, 25, 1}, true, false},
		{"complete-with-eof", []retestInputTestRead{{32, io.EOF}, {0, io.EOF}}, []int{32, 1}, true, false},
		{"owned-bytes-after-close", nil, []int{32, 1}, true, true},
		{"negative-count", []retestInputTestRead{{-1, private}}, []int{32}, false, false},
		{"excessive-count", []retestInputTestRead{{33, io.EOF}}, []int{32}, false, false},
		{"no-progress", []retestInputTestRead{{0, nil}}, []int{32}, false, false},
		{"empty-eof", []retestInputTestRead{{0, io.EOF}}, []int{32}, false, false},
		{"short-eof", []retestInputTestRead{{31, io.EOF}}, []int{32}, false, false},
		{"partial-hard-error", []retestInputTestRead{{5, private}}, []int{32}, false, false},
		{"complete-hard-error", []retestInputTestRead{{32, private}}, []int{32}, false, false},
		{"zero-hard-error", []retestInputTestRead{{0, private}}, []int{32}, false, false},
		{"wrapped-eof", []retestInputTestRead{{32, fmt.Errorf("sensitive-input-marker: %w", io.EOF)}}, []int{32}, false, false},
		{"joined-eof", []retestInputTestRead{{32, errors.Join(io.EOF, private)}}, []int{32}, false, false},
		{"progress-then-stall", []retestInputTestRead{{7, nil}, {0, nil}}, []int{32, 25}, false, false},
		{"probe-extra-byte", []retestInputTestRead{{32, nil}, {1, io.EOF}}, []int{32, 1}, false, false},
		{"probe-excessive", []retestInputTestRead{{32, nil}, {2, io.EOF}}, []int{32, 1}, false, false},
		{"probe-negative", []retestInputTestRead{{32, nil}, {-1, private}}, []int{32, 1}, false, false},
		{"probe-no-eof", []retestInputTestRead{{32, nil}, {0, nil}}, []int{32, 1}, false, false},
		{"probe-hard-error", []retestInputTestRead{{32, nil}, {0, private}}, []int{32, 1}, false, false},
		{"probe-wrapped-eof", []retestInputTestRead{{32, nil}, {0, fmt.Errorf("sensitive-input-marker: %w", io.EOF)}}, []int{32, 1}, false, false},
		{"exact-eof-then-extra", []retestInputTestRead{{32, io.EOF}, {1, io.EOF}}, []int{32, 1}, false, false},
		{"exact-eof-then-error", []retestInputTestRead{{32, io.EOF}, {0, private}}, []int{32, 1}, false, false},
		{"exact-eof-then-stall", []retestInputTestRead{{32, io.EOF}, {0, nil}}, []int{32, 1}, false, false},
	}
	for _, command := range inventoryPlanFingerprintCommands() {
		for _, test := range cases {
			t.Run(command.name+"/"+test.name, func(t *testing.T) {
				h := newInventoryPlanFingerprintHarness(t, command)
				h.fixture.readable.reads = test.reads
				if test.mutateAfterRead {
					h.afterRead = func() { h.fixture.readable.data[0] ^= 0xff }
				}
				code := h.run()
				if test.accepted {
					h.requireSuccess(code)
					if h.readError != nil || !bytes.Equal(h.readData, inventoryPlanFingerprintKey(0)) {
						t.Fatal("completed fingerprint bytes changed or retained provider storage")
					}
					want := "eed5302c9ef92a0eafb9df1a5f0b378196ddbf95503697852af07fb03f3d635b"
					if command.name == "plan" {
						want = "bafc819f70e8b8e0a09504b657247e3147e9f8508a97139cc85db453bc264dda"
					}
					if fmt.Sprintf("%x", sha256.Sum256(h.stdout.Bytes())) != want {
						t.Fatal("completed key changed command output")
					}
				} else {
					h.requireCoreFailure(code)
				}
				assertRetestInputLedger(t, h.fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
				if !reflect.DeepEqual(h.fixture.readable.readSize, test.requests) {
					t.Fatal("fingerprint read request ledger changed")
				}
				statCount := 1
				if test.accepted {
					statCount = 2
				}
				if inventoryPlanFingerprintOperationCount(h.fixture, "metadata.stat") != statCount ||
					inventoryPlanFingerprintOperationCount(h.fixture, "readable.stat") != statCount {
					t.Fatal("read completion reached the wrong final-stability boundary")
				}
			})
		}
	}
}

func TestInventoryPlanFingerprintCloseFailures(t *testing.T) {
	for _, command := range inventoryPlanFingerprintCommands() {
		for _, name := range []string{"readable", "metadata", "both"} {
			t.Run(command.name+"/"+name, func(t *testing.T) {
				h := newInventoryPlanFingerprintHarness(t, command)
				private := errors.New("sensitive-input-marker")
				if name == "readable" || name == "both" {
					h.fixture.readable.closeErr = private
				}
				if name == "metadata" || name == "both" {
					h.fixture.metadata.closeErr = private
				}
				h.requireCoreFailure(h.run())
				assertRetestInputLedger(t, h.fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
				if !errors.Is(h.readError, errRetestInputCleanup) || !reflect.DeepEqual(h.fixture.readable.readSize, []int{32, 1}) ||
					inventoryPlanFingerprintOperationCount(h.fixture, "metadata.stat") != 2 ||
					inventoryPlanFingerprintOperationCount(h.fixture, "readable.stat") != 2 {
					t.Fatal("checked-close failure did not follow complete acquisition and stability checks")
				}
			})
		}
	}
}

type inventoryPlanFingerprintBoundaryContext struct {
	context.Context
	armed  bool
	checks int
	cancel context.CancelFunc
}

func (ctx *inventoryPlanFingerprintBoundaryContext) Err() error {
	if ctx.armed {
		ctx.checks++
		if ctx.checks == 2 {
			ctx.cancel()
		}
	}
	return ctx.Context.Err()
}

func TestInventoryPlanFingerprintCancellation(t *testing.T) {
	cases := []struct {
		name                         string
		metadataStats, readableStats int
		requests                     []int
		readable, completedRead      bool
		collects                     int
	}{
		{"metadata-open", 0, 0, nil, false, false, 0},
		{"metadata-stat", 1, 0, nil, false, false, 0},
		{"readable-open", 1, 0, nil, true, false, 0},
		{"readable-stat", 1, 1, nil, true, false, 0},
		{"data-read", 1, 1, []int{32}, true, false, 0},
		{"probe-read", 1, 1, []int{32, 1}, true, false, 0},
		{"readable-final-stat", 1, 2, []int{32, 1}, true, false, 0},
		{"metadata-final-stat", 2, 2, []int{32, 1}, true, false, 0},
		{"readable-close", 2, 2, []int{32, 1}, true, false, 0},
		{"metadata-close", 2, 2, []int{32, 1}, true, false, 0},
		{"after-read", 2, 2, []int{32, 1}, true, true, 0},
		{"after-collection", 2, 2, []int{32, 1}, true, true, 1},
		{"before-destination", 2, 2, []int{32, 1}, true, true, 1},
		{"collection-error-priority", 2, 2, []int{32, 1}, true, true, 1},
	}
	for _, command := range inventoryPlanFingerprintCommands() {
		for _, test := range cases {
			t.Run(command.name+"/"+test.name, func(t *testing.T) {
				h := newInventoryPlanFingerprintHarness(t, command)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				h.ctx = ctx
				on := func(wanted string, occurrence int) func(string) {
					count := 0
					return func(operation string) {
						if operation == wanted {
							count++
							if count == occurrence {
								cancel()
							}
						}
					}
				}
				var gate *inventoryPlanFingerprintBoundaryContext
				switch test.name {
				case "metadata-open":
					h.fixture.metadataHook = cancel
				case "metadata-stat":
					h.fixture.metadata.hook = on("stat", 1)
				case "readable-open":
					h.fixture.readableHook = cancel
				case "readable-stat":
					h.fixture.readable.hook = on("stat", 1)
				case "data-read":
					h.fixture.readable.hook = on("read", 1)
				case "probe-read":
					h.fixture.readable.hook = on("read", 2)
				case "readable-final-stat":
					h.fixture.readable.hook = on("stat", 2)
				case "metadata-final-stat":
					h.fixture.metadata.hook = on("stat", 2)
				case "readable-close":
					h.fixture.readable.hook = on("close", 1)
				case "metadata-close":
					h.fixture.metadata.hook = on("close", 1)
				case "after-read":
					h.afterRead = cancel
				case "after-collection":
					h.collect = func(context.Context) (inventory.Snapshot, error) {
						cancel()
						return inventoryPlanFingerprintSnapshot(), nil
					}
				case "before-destination":
					// After collection, the first observation is still live;
					// the next observation cancels the underlying real context.
					gate = &inventoryPlanFingerprintBoundaryContext{Context: ctx, cancel: cancel}
					h.ctx = gate
					h.collect = func(context.Context) (inventory.Snapshot, error) {
						gate.armed = true
						return inventoryPlanFingerprintSnapshot(), nil
					}
				case "collection-error-priority":
					h.collect = func(context.Context) (inventory.Snapshot, error) {
						cancel()
						return inventory.Snapshot{}, errors.New("collection-marker")
					}
				}
				code := h.run()
				if test.completedRead {
					message := command.name + " input acquisition failed\n"
					if test.name == "collection-error-priority" {
						message = "collect inventory: collection-marker\n"
					}
					h.requireFailure(code, message, 1, test.collects, 0)
					if h.readError != nil || !bytes.Equal(h.readData, inventoryPlanFingerprintKey(0)) {
						t.Fatal("caller cancellation did not follow a successfully closed read")
					}
				} else {
					h.requireCoreFailure(code)
					if !errors.Is(h.readError, context.Canceled) {
						t.Fatal("reader cancellation hook was not observed")
					}
				}
				if ctx.Err() != context.Canceled || (gate != nil && gate.checks != 2) {
					t.Fatal("cancellation fixture did not reach its declared boundary")
				}
				want := map[string]int{"metadata": 1}
				closes := []string{"metadata"}
				readableOpens := 0
				if test.readable {
					want["readable"] = 1
					closes = []string{"readable", "metadata"}
					readableOpens = 1
				}
				assertRetestInputLedger(t, h.fixture, want, closes)
				if inventoryPlanFingerprintOperationCount(h.fixture, "open.metadata") != 1 ||
					inventoryPlanFingerprintOperationCount(h.fixture, "open.readable") != readableOpens ||
					inventoryPlanFingerprintOperationCount(h.fixture, "metadata.stat") != test.metadataStats ||
					inventoryPlanFingerprintOperationCount(h.fixture, "readable.stat") != test.readableStats ||
					!reflect.DeepEqual(h.fixture.readable.readSize, test.requests) {
					t.Fatal("cancellation advanced beyond its declared owner/read boundary")
				}
			})
		}
	}
}

type inventoryPlanFingerprintShortWriter struct{}

func (inventoryPlanFingerprintShortWriter) Write(raw []byte) (int, error) {
	if len(raw) == 0 {
		return 0, nil
	}
	return len(raw) - 1, nil
}

func TestInventoryPlanFingerprintOutputFailureAfterClose(t *testing.T) {
	for _, command := range inventoryPlanFingerprintCommands() {
		for _, name := range []string{"writer-error", "short-write"} {
			t.Run(command.name+"/"+name, func(t *testing.T) {
				h := newInventoryPlanFingerprintHarness(t, command)
				suffix := "write-marker"
				callbacks := 0
				if name == "writer-error" {
					h.writeOutput = func(string, io.Writer, func(io.Writer) error) error { return errors.New("write-marker") }
				} else {
					suffix, callbacks = "short write", 1
					h.writeOutput = func(path string, _ io.Writer, callback func(io.Writer) error) error {
						return writeDestinationWith(path, inventoryPlanFingerprintShortWriter{}, callback, nil)
					}
				}
				prefix := "write inventory: "
				if command.name == "plan" {
					prefix = "write test plan: "
				}
				h.requireFailure(h.run(), prefix+suffix+"\n", 1, 1, 1)
				if h.callbackCalls != callbacks {
					t.Fatal("destination callback completion changed")
				}
				assertRetestInputLedger(t, h.fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
			})
		}
	}
}

type inventoryPlanFingerprintDeadlineContext struct {
	context.Context
	expired    context.Context
	useExpired bool
}

func (ctx *inventoryPlanFingerprintDeadlineContext) Err() error {
	if ctx.useExpired {
		return ctx.expired.Err()
	}
	return ctx.Context.Err()
}

func TestInventoryPlanFingerprintObservedDeadline(t *testing.T) {
	for _, command := range inventoryPlanFingerprintCommands() {
		for _, stage := range []string{"read", "close", "after-read", "after-collection"} {
			t.Run(command.name+"/"+stage, func(t *testing.T) {
				h := newInventoryPlanFingerprintHarness(t, command)
				expired, cancel := context.WithDeadline(t.Context(), time.Unix(1, 0))
				defer cancel()
				ctx := &inventoryPlanFingerprintDeadlineContext{Context: t.Context(), expired: expired}
				h.ctx = ctx
				switch stage {
				case "read":
					h.fixture.readable.hook = func(operation string) {
						if operation == "read" {
							ctx.useExpired = true
						}
					}
				case "close":
					h.fixture.readable.hook = func(operation string) {
						if operation == "close" {
							ctx.useExpired = true
						}
					}
				case "after-read":
					h.afterRead = func() { ctx.useExpired = true }
				case "after-collection":
					h.collect = func(context.Context) (inventory.Snapshot, error) {
						ctx.useExpired = true
						return inventoryPlanFingerprintSnapshot(), nil
					}
				}
				code := h.run()
				requests, stats := []int{32, 1}, 2
				if stage == "read" {
					requests, stats = []int{32}, 1
				}
				if stage == "read" || stage == "close" {
					h.requireCoreFailure(code)
					if h.readError != context.DeadlineExceeded {
						t.Fatal("reader did not preserve the observed deadline category")
					}
				} else {
					collects := 0
					if stage == "after-collection" {
						collects = 1
					}
					h.requireFailure(code, command.name+" input acquisition failed\n", 1, collects, 0)
					if h.readError != nil || !bytes.Equal(h.readData, inventoryPlanFingerprintKey(0)) {
						t.Fatal("caller deadline did not follow a successfully closed read")
					}
				}
				if !ctx.useExpired || ctx.Err() != context.DeadlineExceeded {
					t.Fatal("deadline fixture was not observed")
				}
				assertRetestInputLedger(t, h.fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
				if !reflect.DeepEqual(h.fixture.readable.readSize, requests) ||
					inventoryPlanFingerprintOperationCount(h.fixture, "metadata.stat") != stats ||
					inventoryPlanFingerprintOperationCount(h.fixture, "readable.stat") != stats {
					t.Fatal("deadline advanced beyond its declared read or close boundary")
				}
			})
		}
	}
}

func TestInventoryPlanFingerprintKeyFailureBeforeCancellation(t *testing.T) {
	for _, command := range inventoryPlanFingerprintCommands() {
		for _, test := range []struct {
			name string
			data []byte
			err  error
		}{
			{"nil-data", nil, nil},
			{"short-31", make([]byte, 31), nil},
			{"long-33", make([]byte, 33), nil},
			{"complete-with-error", inventoryPlanFingerprintKey(0), errors.New("sensitive-input-marker")},
		} {
			t.Run(command.name+"/"+test.name, func(t *testing.T) {
				h := newInventoryPlanFingerprintHarness(t, command)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				h.ctx = ctx
				h.readInput = func(context.Context, string, retestInputKind) ([]byte, error) { return test.data, test.err }
				h.afterRead = cancel
				h.requireFailure(h.run(), "read fingerprint key: input acquisition failed\n", 1, 0, 0)
				if ctx.Err() != context.Canceled || len(h.fixture.ledger.calls) != 0 {
					t.Fatal("caller-only fixture did not combine key rejection with observed cancellation")
				}
			})
		}
	}
}

func TestInventoryPlanFingerprintCanceledValidationPrecedence(t *testing.T) {
	cases := []struct {
		name, command, mode, diagnostic string
		code                            int
	}{
		{"inventory-usage", "inventory", "usage", "inventory requires -fingerprint-key;", 2},
		{"plan-usage", "plan", "usage", "plan requires -fingerprint-key;", 2},
		{"plan-selection", "plan", "selection", "validate quick-test selection:", 1},
	}
	for _, command := range inventoryPlanFingerprintCommands() {
		for _, test := range cases {
			if command.name != test.command {
				continue
			}
			t.Run(test.name, func(t *testing.T) {
				h := newInventoryPlanFingerprintHarness(t, command)
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				h.ctx = ctx
				if test.mode == "usage" {
					h.args = nil
				} else {
					path := writeTestKey(t, t.TempDir(), "selection.fixture", []byte("{"))
					h.args = append(h.args, "-selection-file", path)
				}
				code := h.run()
				if code != test.code || !strings.HasPrefix(h.stderr.String(), test.diagnostic) ||
					strings.Contains(h.stderr.String(), "input acquisition failed") || h.stdout.Len() != 0 ||
					h.readCalls != 0 || h.collectCalls != 0 || h.writeCalls != 0 ||
					len(h.fixture.ledger.calls) != 0 || ctx.Err() != context.Canceled {
					t.Fatal("observed cancellation changed usage or selection precedence")
				}
			})
		}
	}
}
