package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/touchpadevidence"
)

// Literal producer-phase claims test the command boundary. They do not replace
// the inputtest frame, terminal-query, descriptor or real-kernel requirements.
func TestTouchpadCommandPhaseDiagnostics(t *testing.T) {
	for _, name := range []string{
		"terminal_not_idle", "terminal_nonneutral", "late_identity", "late_registry",
		"event_loss", "cancel_retained_pass", "deadline_retained_pass", "cancel_hard_error",
		"partial_frame", "unsupported_mt_marker", "unreadable_prestate", "initialization_diagnostic",
	} {
		t.Run(name, func(t *testing.T) {
			result := touchpadCommandLiteral(t)
			row := &result.Devices[0]
			result.Status, result.StopReason, result.Detail = inputtest.StatusInconclusive, "error", "incomplete_actions"
			row.Status, row.Detail, row.FinalNeutral = inputtest.StatusInconclusive, "terminal_not_idle", false
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var parent context.Context = ctx
			deadline := &touchpadCommandDeadlineContext{Context: ctx, deadline: time.Now().Add(time.Minute)}
			switch name {
			case "terminal_not_idle":
			case "terminal_nonneutral":
				row.Detail = "terminal_not_neutral"
			case "late_identity":
				result.Status, result.Detail, row.Status, row.Detail = inputtest.StatusError, "identity_changed", inputtest.StatusError, "identity_changed"
			case "late_registry":
				result.Status, result.Detail, row.Detail = inputtest.StatusError, "registry_changed", "registry_changed"
			case "event_loss":
				result.Status, result.Detail, row.Status, row.Detail, row.EvidenceLoss = inputtest.StatusError, "event_loss", inputtest.StatusError, "event_loss", true
			case "cancel_retained_pass", "deadline_retained_pass":
				result = touchpadCommandLiteral(t)
				parent = deadline
			case "cancel_hard_error":
				result.Status, result.Detail, row.Status, row.Detail = inputtest.StatusError, "disconnected", inputtest.StatusError, "disconnected"
			case "partial_frame":
				row.Detail = "incomplete_events"
			case "unsupported_mt_marker":
				metadata := row.Capabilities
				metadata.Axes = append(metadata.Axes, inputtest.TouchpadAxis{Code: 47})
				*row = inputtest.TouchpadDeviceResult{
					Interface: "event4", Status: inputtest.StatusInconclusive, Readable: true,
					Identity:     &inputtest.TouchpadIdentity{Bus: 3, Vendor: 4660, Product: 22136, Version: 1},
					Capabilities: metadata, Buttons: []inputtest.PointerButtonResult{},
					CleanupConfirmed: true, Detail: "unsupported_capabilities",
				}
				result.Detail = "unsupported_or_unreadable_device"
			case "unreadable_prestate":
				*row = inputtest.TouchpadDeviceResult{
					Interface: "event4", Status: inputtest.StatusInconclusive,
					Buttons: []inputtest.PointerButtonResult{}, CleanupConfirmed: true, Detail: "identity_unavailable",
				}
				result.Detail = "unsupported_or_unreadable_device"
			case "initialization_diagnostic":
				row.XThreshold, row.YThreshold = 0, 0
				row.Frames, row.ContactStarts, row.ContactReleases, row.XYBaselines, row.QualifyingContacts = 0, 0, 0, 0, 0
				row.XPositive, row.XNegative, row.YPositive, row.YNegative = 0, 0, 0, 0
				row.Buttons = []inputtest.PointerButtonResult{}
				row.BaselineNeutral, row.FinalNeutral, row.Detail = false, false, "incomplete_actions"
			}
			expected := result
			if name == "cancel_retained_pass" {
				expected.Status, expected.StopReason, expected.Detail = inputtest.StatusAborted, "operator_cancelled", "operator_cancelled"
			}
			if name == "deadline_retained_pass" {
				expected.Status, expected.StopReason, expected.Detail = inputtest.StatusError, "deadline", "deadline"
			}
			var out, diagnostic bytes.Buffer
			calls := 0
			code := runTouchpadWith(parent, nil, &out, &diagnostic,
				func(context.Context, inputtest.TouchpadOptions) inputtest.TouchpadResult {
					calls++
					if name == "cancel_retained_pass" || name == "cancel_hard_error" {
						cancel()
					}
					if name == "deadline_retained_pass" {
						deadline.deadline = time.Now().Add(-time.Second)
					}
					return result
				})
			actual, err := touchpadevidence.Decode(out.Bytes())
			if code != 0 || calls != 1 || err != nil || !reflect.DeepEqual(actual, expected) {
				t.Fatalf("phase-compatible diagnostic changed: code=%d decode=%v", code, err)
			}
			if strings.Contains(out.String(), "canary") || strings.Contains(diagnostic.String(), "canary") {
				t.Fatal("diagnostic exposed non-schema information")
			}
		})
	}
}

// Exercise the exported production collector through the command's real
// collector seam, with only temporary regular registry files and no event node.
// Event production, ioctl behavior and physical devices are not simulated here.
func TestTouchpadCommandProductionCollectorOwnedRoot(t *testing.T) {
	for _, name := range []string{"empty_registry", "missing_registry", "missing_candidate_anchor"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if name != "missing_registry" {
				directory := filepath.Join(root, "proc", "bus", "input")
				if err := os.MkdirAll(directory, 0700); err != nil {
					t.Fatal(err)
				}
				registry := ""
				if name == "missing_candidate_anchor" {
					registry = "N: Name=\"Touchpad canary-owned-fixture\"\nH: Handlers=event0\nB: ABS=3\n\n"
				}
				if err := os.WriteFile(filepath.Join(directory, "devices"), []byte(registry), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var out, diagnostic bytes.Buffer
			calls := 0
			code := runTouchpadWith(t.Context(), nil, &out, &diagnostic,
				func(ctx context.Context, options inputtest.TouchpadOptions) inputtest.TouchpadResult {
					calls++
					if options.Root != "/" || options.Duration != 30*time.Second {
						t.Fatal("command allowed caller-selected acquisition")
					}
					options.Root = root // Test-owned root only; production keeps "/".
					return inputtest.RunTouchpad(ctx, options)
				})
			actual, err := touchpadevidence.Decode(out.Bytes())
			if code != 0 || calls != 1 || err != nil || !actual.CleanupConfirmed || actual.Status == inputtest.StatusPass {
				t.Fatalf("production diagnostic did not survive strict command boundary: code=%d decode=%v", code, err)
			}
			supportedABI := runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "386" || runtime.GOARCH == "arm64")
			if !supportedABI {
				if actual.Status != inputtest.StatusInconclusive || actual.StopReason != "unsupported_platform" || len(actual.Devices) != 0 {
					t.Fatal("unsupported target claimed Linux acquisition")
				}
			} else {
				switch name {
				case "empty_registry":
					if actual.Status != inputtest.StatusInconclusive || actual.StopReason != "no_touchpads" || len(actual.Devices) != 0 {
						t.Fatal("empty registry was not an honest no-candidate result")
					}
				case "missing_registry":
					if actual.Status != inputtest.StatusError || actual.Detail != "registry_unavailable" || len(actual.Devices) != 0 {
						t.Fatal("missing registry was promoted to absence")
					}
				case "missing_candidate_anchor":
					if actual.Status != inputtest.StatusError || actual.Detail != "identity_unavailable" || len(actual.Devices) != 1 ||
						actual.Devices[0].Interface != "event0" || actual.Devices[0].Readable {
						t.Fatal("missing candidate anchor was treated as a readable device")
					}
				}
			}
			if strings.Contains(out.String()+diagnostic.String(), root) || strings.Contains(out.String()+diagnostic.String(), "canary") {
				t.Fatal("temporary registry name/path escaped the producer projection")
			}
		})
	}
}

// A parent with a deliberately non-signalling deadline exercises the command's
// monotonic deadline check independently of timer delivery.
type touchpadCommandDeadlineContext struct {
	context.Context
	deadline time.Time
	observe  func()
}

func (ctx *touchpadCommandDeadlineContext) Deadline() (time.Time, bool) {
	if ctx.observe != nil {
		ctx.observe()
	}
	return ctx.deadline, true
}

func TestTouchpadCommandFreezesBeforeContextObservation(t *testing.T) {
	result := touchpadCommandLiteral(t)
	expected := touchpadCommandLiteral(t)
	ctx := &touchpadCommandDeadlineContext{Context: context.Background(), deadline: time.Now().Add(time.Minute)}
	observed := 0
	var out, diagnostic bytes.Buffer
	collect := func(context.Context, inputtest.TouchpadOptions) inputtest.TouchpadResult {
		ctx.observe = func() {
			observed++
			result.Devices[0].Interface = "canary-context-alias"
			result.Devices[0].Capabilities.Axes[0].Maximum = 3
			result.Devices[0].Buttons[0].Pairs = 0
			result.Devices[0].CleanupConfirmed = false
		}
		return result
	}
	if code := runTouchpadWith(ctx, nil, &out, &diagnostic, collect); code != 0 || observed == 0 {
		t.Fatal("context observation did not cross the owned-result boundary")
	}
	actual, err := touchpadevidence.Decode(out.Bytes())
	if err != nil || !reflect.DeepEqual(actual, expected) || strings.Contains(out.String()+diagnostic.String(), "canary") {
		t.Fatal("context callback mutated an admitted collector result")
	}
}

func TestTouchpadCommandDeadlineDuringPublication(t *testing.T) {
	for _, name := range []string{"normal_result", "cleanup_uncertain"} {
		t.Run(name, func(t *testing.T) {
			result := touchpadCommandLiteral(t)
			want := 1
			if name == "cleanup_uncertain" {
				result.Status, result.StopReason, result.Detail = inputtest.StatusError, "cleanup_unconfirmed", "cleanup_unconfirmed"
				result.CleanupConfirmed = false
				result.Devices = []inputtest.TouchpadDeviceResult{}
				want = workerCleanupUncertainExitCode
			}
			ctx := &touchpadCommandDeadlineContext{Context: context.Background(), deadline: time.Now().Add(time.Minute)}
			var out, diagnostic bytes.Buffer
			destination := func(_ string, _ io.Writer, write func(io.Writer) error) error {
				err := write(&out)
				ctx.deadline = time.Now().Add(-time.Second)
				return err
			}
			code := runTouchpadWithDestination(ctx, nil, &out, &diagnostic,
				func(context.Context, inputtest.TouchpadOptions) inputtest.TouchpadResult { return result }, destination)
			if code != want || out.Len() == 0 {
				t.Fatal("late deadline returned success or erased written evidence")
			}
			actual, err := touchpadevidence.Decode(out.Bytes())
			if name == "normal_result" {
				if err != nil || actual.Status != inputtest.StatusPass {
					t.Fatal("publication failure rewrote the already emitted claim")
				}
			} else if !errors.Is(err, touchpadevidence.ErrCleanupUnconfirmed) {
				t.Fatal("cleanup uncertainty lost its typed diagnostic")
			}
		})
	}
}
