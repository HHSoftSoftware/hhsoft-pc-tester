package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

type launchPlanHarness struct {
	arguments      []string
	launch         []byte
	launchErr      error
	afterLaunch    func()
	stdout, stderr bytes.Buffer
	events         []string
	read           retestInputReader
	collect        snapshotCollector
	write          destinationWriter
}

func newLaunchPlanHarness(t *testing.T) *launchPlanHarness {
	t.Helper()
	h := &launchPlanHarness{
		arguments: []string{"-fingerprint-key", "fixture-key", "-architecture", "x86_64", "-launch-selection-file", "fixture-launch"},
		launch:    []byte(`{"schema_version":1,"profile":"quick","architecture":"x86_64","excluded_tests":[]}`),
	}
	h.read = func(_ context.Context, path string, kind retestInputKind) ([]byte, error) {
		switch kind {
		case retestLaunchSelectionInput:
			if path != "fixture-launch" {
				t.Fatal("wrong launch path")
			}
			h.events = append(h.events, "launch")
			if h.afterLaunch != nil {
				h.afterLaunch()
			}
			return bytes.Clone(h.launch), h.launchErr
		case retestFingerprintKeyInput:
			if path != "fixture-key" {
				t.Fatal("wrong fingerprint path")
			}
			h.events = append(h.events, "key")
			return bytes.Repeat([]byte{0x25}, 32), nil
		default:
			t.Fatal("unexpected input role")
			return nil, errors.New("unexpected input role")
		}
	}
	h.collect = func(context.Context) (inventory.Snapshot, error) {
		h.events = append(h.events, "collect")
		return inventory.NewSnapshot([]inventory.Component{inventory.NewComponent(inventory.ComponentCPU, "cpu:0", nil)}), nil
	}
	h.write = func(path string, output io.Writer, encode func(io.Writer) error) error {
		h.events = append(h.events, "output")
		if path != "-" {
			t.Fatal("unexpected output destination")
		}
		return encode(output)
	}
	return h
}

func (h *launchPlanHarness) run(ctx context.Context) int {
	return runPlanWithInput(ctx, h.arguments, &h.stdout, &h.stderr, h.collect, h.write, h.read)
}

func TestPlanLaunchSelectionBindsTargetBeforeEffects(t *testing.T) {
	for _, test := range []struct {
		name, profile, architecture, exclusions string
		want                                    []string
	}{
		{"quick_selected", "quick", "x86_64", `["usb.readiness.quick","cpu.quick"]`, []string{"cpu.quick", "usb.readiness.quick"}},
		{"standard_modern", "standard", "x86_64", "[]", []string{}},
		{"standard_legacy", "standard", "legacy-x86", "[]", []string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newLaunchPlanHarness(t)
			h.arguments[3] = test.architecture
			h.arguments = append(h.arguments, "-profile", test.profile)
			h.launch = []byte(fmt.Sprintf(`{"schema_version":1,"profile":%q,"architecture":%q,"excluded_tests":%s}`, test.profile, test.architecture, test.exclusions))
			if code := h.run(t.Context()); code != 0 || h.stderr.Len() != 0 || !slices.Equal(h.events, []string{"launch", "key", "collect", "output"}) {
				t.Fatal("valid handoff did not precede downstream effects")
			}
			got, err := plan.Decode(h.stdout.Bytes())
			if err != nil || got.Profile != plan.Profile(test.profile) || got.Architecture != test.architecture || !slices.Equal(got.ExcludedTests, test.want) || plan.Validate(got) != nil {
				t.Fatal("frozen plan did not bind the expected target and exclusions")
			}
		})
	}
}

func TestPlanLaunchSelectionRejectsBeforeKey(t *testing.T) {
	const valid = `{"schema_version":1,"profile":"quick","architecture":"x86_64","excluded_tests":[]}`
	for _, test := range []struct {
		name, wire string
		err        error
	}{
		{"malformed", "{", nil},
		{"old_unbound_format", `{"excluded_tests":[]}`, nil},
		{"wrong_profile", strings.Replace(valid, "quick", "standard", 1), nil},
		{"wrong_architecture", strings.Replace(valid, "x86_64", "aarch64", 1), nil},
		{"duplicate_profile", strings.TrimSuffix(valid, "}") + `,"profile":"quick"}`, nil},
		{"null_exclusions", strings.Replace(valid, "[]", "null", 1), nil},
		{"unknown_exclusion", strings.Replace(valid, "[]", `["unknown"]`, 1), nil},
		{"empty_bytes", "", nil},
		{"oversize", strings.Repeat(" ", 16385), nil},
		{"bytes_with_error", valid, errors.New("sensitive provider detail")},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newLaunchPlanHarness(t)
			h.launch, h.launchErr = []byte(test.wire), test.err
			if code := h.run(t.Context()); code != 1 || h.stdout.Len() != 0 || h.stderr.String() != "read launch selection: input acquisition failed\n" || !slices.Equal(h.events, []string{"launch"}) {
				t.Fatal("invalid launch reached downstream effects or exposed input details")
			}
		})
	}
}

func TestPlanLaunchSelectionAdmitsDependenciesBeforeRead(t *testing.T) {
	for _, name := range []string{"nil_context", "typed_nil_context", "cancelled", "nil_reader", "nil_collector", "nil_output"} {
		t.Run(name, func(t *testing.T) {
			h := newLaunchPlanHarness(t)
			ctx := context.Context(t.Context())
			switch name {
			case "nil_context":
				ctx = nil
			case "typed_nil_context":
				var stopped *retestInputStoppedContext
				ctx = stopped
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			case "nil_reader":
				h.read = nil
			case "nil_collector":
				h.collect = nil
			case "nil_output":
				h.write = nil
			}
			if code := h.run(ctx); code != 1 || h.stderr.String() != "plan input acquisition failed\n" || h.stdout.Len() != 0 || len(h.events) != 0 {
				t.Fatal("invalid dependency reached launch acquisition")
			}
		})
	}
}

func TestPlanLaunchSelectionCancellationBeforeKey(t *testing.T) {
	h := newLaunchPlanHarness(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	h.afterLaunch = cancel
	if code := h.run(ctx); code != 1 || h.stderr.String() != "plan input acquisition failed\n" || h.stdout.Len() != 0 || !slices.Equal(h.events, []string{"launch"}) {
		t.Fatal("cancelled handoff reached key acquisition")
	}
}

func TestPlanLaunchSelectionUsageStopsBeforeInput(t *testing.T) {
	for _, test := range []struct {
		name       string
		arguments  []string
		diagnostic string
	}{
		{"both_modes", []string{"-selection-file", "must-not-open", "-launch-selection-file", "fixture-launch"}, "plan selection modes are mutually exclusive\n"},
		{"empty_old_with_new", []string{"-selection-file=", "-launch-selection-file", "fixture-launch"}, "plan selection modes are mutually exclusive\n"},
		{"empty_launch", []string{"-launch-selection-file="}, "plan requires a nonempty -launch-selection-file\n"},
		{"positional", []string{"-launch-selection-file", "fixture-launch", "stray"}, "plan accepts no positional arguments\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newLaunchPlanHarness(t)
			h.arguments = append([]string{"-fingerprint-key", "fixture-key", "-architecture", "x86_64"}, test.arguments...)
			if code := h.run(t.Context()); code != 2 || h.stderr.String() != test.diagnostic || h.stdout.Len() != 0 || len(h.events) != 0 {
				t.Fatal("usage failure reached an input or output effect")
			}
		})
	}
}

func TestPlanLaunchSelectionCloseFailureStopsBeforeKey(t *testing.T) {
	for _, owner := range []string{"readable", "metadata"} {
		t.Run(owner, func(t *testing.T) {
			h := newLaunchPlanHarness(t)
			fixture := newRetestInputFixture(len(h.launch))
			fixture.readable.data = bytes.Clone(h.launch)
			if owner == "readable" {
				fixture.readable.closeErr = errors.New("private close detail")
			} else {
				fixture.metadata.closeErr = errors.New("private close detail")
			}
			h.read = func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
				if kind != retestLaunchSelectionInput || path != "fixture-launch" {
					t.Fatal("close failure reached key input")
				}
				h.events = append(h.events, "launch")
				return readRetestInputWithOperations(ctx, path, kind, fixture.operations())
			}
			if code := h.run(t.Context()); code != 1 || h.stderr.String() != "read launch selection: input acquisition failed\n" || h.stdout.Len() != 0 || !slices.Equal(h.events, []string{"launch"}) {
				t.Fatal("unchecked close released a launch selection")
			}
			assertRetestInputLedger(t, fixture, map[string]int{"metadata": 1, "readable": 1}, []string{"readable", "metadata"})
		})
	}
}

func TestPlanLaunchSelectionPreservesDefaultPlanBytes(t *testing.T) {
	for _, profile := range []string{"quick", "standard"} {
		t.Run(profile, func(t *testing.T) {
			defaults := newLaunchPlanHarness(t)
			defaults.arguments = append(defaults.arguments[:4], "-profile", profile)
			if code := defaults.run(t.Context()); code != 0 {
				t.Fatal("default plan failed")
			}
			launch := newLaunchPlanHarness(t)
			launch.arguments = append(launch.arguments, "-profile", profile)
			launch.launch = []byte(fmt.Sprintf(`{"schema_version":1,"profile":%q,"architecture":"x86_64","excluded_tests":[]}`, profile))
			if code := launch.run(t.Context()); code != 0 || !bytes.Equal(defaults.stdout.Bytes(), launch.stdout.Bytes()) {
				t.Fatal("empty launch selection changed default frozen bytes")
			}
			if profile == "quick" {
				old := newLaunchPlanHarness(t)
				path := filepath.Join(t.TempDir(), "quick-selection.json")
				if err := os.WriteFile(path, []byte(`{"excluded_tests":[]}`), 0600); err != nil {
					t.Fatal(err)
				}
				old.arguments = append(old.arguments[:4], "-selection-file", path)
				if code := old.run(t.Context()); code != 0 || !bytes.Equal(defaults.stdout.Bytes(), old.stdout.Bytes()) || bytes.Contains(old.stdout.Bytes(), []byte("excluded_tests")) {
					t.Fatal("old quick-selection byte contract changed")
				}
			}
		})
	}
}

func TestPlanLaunchSelectionPreservesLegacyValidationPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old-selection.json")
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	h := newLaunchPlanHarness(t)
	h.arguments = append(h.arguments[:4], "-selection-file", path)
	if code := h.run(nil); code != 1 || !strings.HasPrefix(h.stderr.String(), "validate quick-test selection: ") || h.stdout.Len() != 0 || len(h.events) != 0 {
		t.Fatal("old selection validation precedence changed")
	}
}
