package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/keyboardevidence"
)

type keyboardHandoffFixture struct {
	discards, reads int
	err             error
}

func (fixture *keyboardHandoffFixture) Read([]byte) (int, error) {
	fixture.reads++
	return 0, errors.New("competing reader")
}

func (fixture *keyboardHandoffFixture) DiscardUnpromptedInput(context.Context) error {
	fixture.discards++
	return fixture.err
}

type keyboardOutputFixture struct {
	bytes.Buffer
	finished *bool
	fail     bool
}

func (fixture *keyboardOutputFixture) Write(data []byte) (int, error) {
	if !*fixture.finished || fixture.fail {
		return 0, errors.New("output rejected")
	}
	return fixture.Buffer.Write(data)
}

func incompleteKeyboardFixture(options inputtest.KeyboardOptions) inputtest.KeyboardResult {
	return inputtest.KeyboardResult{Schema: inputtest.KeyboardSchema, Status: inputtest.StatusInconclusive,
		StopReason: "observation_timeout", LimitMilliseconds: options.Duration.Milliseconds(),
		DurationMilliseconds: options.Duration.Milliseconds(), CleanupConfirmed: true,
		Devices: []inputtest.KeyboardDeviceResult{}, Detail: "observation_timeout"}
}

func TestKeyboardCommandValidatesBeforeOwningConsole(t *testing.T) {
	for _, arguments := range [][]string{{"-duration", "0"}, {"-duration", "91s"}, {"-timeout", "9ms"},
		{"-timeout", "91s"}, {"-duration", "1s", "-timeout", "10ms"}, {"extra"}, {"-unknown"}} {
		t.Run(strings.Join(arguments, "_"), func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			prepare := func(context.Context, io.Reader) (context.Context, io.Reader, func() error, error) {
				t.Fatal("invalid command acquired input")
				return nil, nil, nil, nil
			}
			collect := func(context.Context, inputtest.KeyboardOptions) inputtest.KeyboardResult {
				t.Fatal("invalid command started a keyboard worker")
				return inputtest.KeyboardResult{}
			}
			if code := runKeyboardWith(t.Context(), arguments, strings.NewReader(""), &out, &diagnostic, collect, prepare); code != 2 || out.Len() != 0 {
				t.Fatal("invalid command was accepted")
			}
		})
	}
}

func TestKeyboardCommandOwnsHandoffAndCleanupBeforeOutput(t *testing.T) {
	fixture := &keyboardHandoffFixture{}
	finished := false
	output := &keyboardOutputFixture{finished: &finished}
	var diagnostic bytes.Buffer
	prepares, finishes, collections := 0, 0, 0
	prepare := func(ctx context.Context, _ io.Reader) (context.Context, io.Reader, func() error, error) {
		prepares++
		return ctx, fixture, func() error { finishes++; finished = true; return nil }, nil
	}
	collect := func(ctx context.Context, options inputtest.KeyboardOptions) inputtest.KeyboardResult {
		collections++
		if options.Root != "/" || options.Duration != 10*time.Millisecond || ctx.Err() != nil {
			t.Fatal("command changed the bounded worker inputs")
		}
		return incompleteKeyboardFixture(options)
	}
	if code := runKeyboardWith(t.Context(), []string{"-duration", "10ms", "-timeout", "1s"}, strings.NewReader(""), output, &diagnostic, collect, prepare); code != 0 {
		t.Fatalf("keyboard command failed: code=%d diagnostic=%s", code, diagnostic.String())
	}
	if prepares != 1 || finishes != 1 || collections != 1 || fixture.discards != 1 || fixture.reads != 0 {
		t.Fatal("console had missing or repeated owners, reads, handoffs or cleanup")
	}
	result, err := keyboardevidence.Decode(output.Bytes())
	if err != nil || result.Status != inputtest.StatusInconclusive {
		t.Fatal("ordinary incomplete evidence was changed or rejected")
	}
	if !strings.Contains(diagnostic.String(), "Do not type text") || !strings.Contains(diagnostic.String(), "Only these six controls") {
		t.Fatal("operator guidance omitted the bounded scope")
	}
}

func TestKeyboardCleanupUncertaintyDominatesOutput(t *testing.T) {
	for _, cause := range []string{"setup", "handoff", "finish", "worker"} {
		for _, writeFails := range []bool{false, true} {
			t.Run(cause+"/"+map[bool]string{false: "saved", true: "write_failed"}[writeFails], func(t *testing.T) {
				fixture := &keyboardHandoffFixture{}
				if cause == "handoff" {
					fixture.err = errSessionInput
				}
				finished, finishes := false, 0
				output := &keyboardOutputFixture{finished: &finished, fail: writeFails}
				var diagnostic bytes.Buffer
				prepare := func(ctx context.Context, _ io.Reader) (context.Context, io.Reader, func() error, error) {
					if cause == "setup" {
						finished = true
						return ctx, fixture, nil, errors.Join(errSessionInput, errSessionInputCleanup)
					}
					return ctx, fixture, func() error {
						finishes++
						finished = true
						if cause == "finish" {
							return errSessionInputCleanup
						}
						return nil
					}, nil
				}
				collect := func(_ context.Context, options inputtest.KeyboardOptions) inputtest.KeyboardResult {
					if cause == "setup" {
						t.Fatal("worker started without input ownership")
					}
					result := incompleteKeyboardFixture(options)
					if cause == "worker" {
						result.Status, result.StopReason, result.CleanupConfirmed = inputtest.StatusError, "cleanup_unconfirmed", false
						result.Detail = "cleanup_unconfirmed"
					}
					return result
				}
				if code := runKeyboardWith(t.Context(), []string{"-duration", "10ms", "-timeout", "1s"}, strings.NewReader(""), output, &diagnostic, collect, prepare); code != workerCleanupUncertainExitCode {
					t.Fatalf("cleanup uncertainty lost terminal exit: %d", code)
				}
				if cause != "setup" && finishes != 1 {
					t.Fatal("cleanup was missing or repeated")
				}
				if !writeFails {
					result, err := keyboardevidence.Decode(output.Bytes())
					if !errors.Is(err, keyboardevidence.ErrCleanupUnconfirmed) || result.CleanupConfirmed || result.StopReason != "cleanup_unconfirmed" {
						t.Fatal("cleanup-uncertain diagnostic was lost or made signable")
					}
				}
			})
		}
	}
}

func TestKeyboardCancellationDuringCleanupPreventsCompletion(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var output, diagnostic bytes.Buffer
	prepare := func(ctx context.Context, input io.Reader) (context.Context, io.Reader, func() error, error) {
		return ctx, input, func() error { cancel(); return nil }, nil
	}
	collect := func(_ context.Context, options inputtest.KeyboardOptions) inputtest.KeyboardResult {
		return incompleteKeyboardFixture(options)
	}
	if code := runKeyboardWith(ctx, []string{"-duration", "10ms", "-timeout", "1s"}, strings.NewReader(""), &output, &diagnostic, collect, prepare); code != 0 {
		t.Fatal("interruption diagnostic was not retained")
	}
	result, err := keyboardevidence.Decode(output.Bytes())
	if err != nil || result.Status != inputtest.StatusAborted || result.StopReason != "operator_cancelled" {
		t.Fatal("late input cancellation did not change the final result")
	}
}

func TestKeyboardCommandRejectsMalformedOrMismatchedEvidence(t *testing.T) {
	for _, defect := range []string{"schema", "limit", "empty_pass", "invalid_cleanup"} {
		t.Run(defect, func(t *testing.T) {
			var output, diagnostic bytes.Buffer
			collect := func(_ context.Context, options inputtest.KeyboardOptions) inputtest.KeyboardResult {
				result := incompleteKeyboardFixture(options)
				switch defect {
				case "schema":
					result.Schema = "unknown"
				case "limit":
					result.LimitMilliseconds++
				case "empty_pass":
					result.Status, result.StopReason = inputtest.StatusPass, "completed"
				case "invalid_cleanup":
					result.Schema, result.CleanupConfirmed = "unknown", false
				}
				return result
			}
			code := runKeyboardWith(t.Context(), []string{"-duration", "10ms", "-timeout", "1s"}, strings.NewReader(""), &output, &diagnostic, collect, prepareSessionInput)
			want := 1
			if defect == "invalid_cleanup" {
				want = workerCleanupUncertainExitCode
			}
			if code != want || output.Len() != 0 {
				t.Fatal("invalid producer evidence reached output or lost terminal cleanup exit")
			}
		})
	}
}

func TestKeyboardCommandAlreadyCanceledDoesNotAcquireInput(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var output, diagnostic bytes.Buffer
	prepare := func(context.Context, io.Reader) (context.Context, io.Reader, func() error, error) {
		t.Fatal("canceled command acquired console input")
		return nil, nil, nil, nil
	}
	collect := func(context.Context, inputtest.KeyboardOptions) inputtest.KeyboardResult {
		t.Fatal("canceled command started keyboard collection")
		return inputtest.KeyboardResult{}
	}
	if code := runKeyboardWith(ctx, []string{"-duration", "10ms", "-timeout", "1s"}, strings.NewReader(""), &output, &diagnostic, collect, prepare); code != 0 {
		t.Fatal("cancellation diagnostic was lost")
	}
	result, err := keyboardevidence.Decode(output.Bytes())
	if err != nil || result.Status != inputtest.StatusAborted || !result.CleanupConfirmed {
		t.Fatal("canceled no-work result was changed or malformed")
	}
}

func TestKeyboardOrdinaryInputFailureDoesNotClaimCleanupUncertainty(t *testing.T) {
	for _, phase := range []string{"setup", "finish", "finish_canceled_owner"} {
		t.Run(phase, func(t *testing.T) {
			var output, diagnostic bytes.Buffer
			prepare := func(ctx context.Context, input io.Reader) (context.Context, io.Reader, func() error, error) {
				if phase == "setup" {
					return ctx, input, nil, errSessionInput
				}
				owned, stop := context.WithCancel(ctx)
				t.Cleanup(stop)
				return owned, input, func() error {
					if phase == "finish_canceled_owner" {
						stop()
					}
					return errSessionInput
				}, nil
			}
			collect := func(_ context.Context, options inputtest.KeyboardOptions) inputtest.KeyboardResult {
				if phase == "setup" {
					t.Fatal("worker started after rejected input")
				}
				return incompleteKeyboardFixture(options)
			}
			if code := runKeyboardWith(t.Context(), []string{"-duration", "10ms", "-timeout", "1s"}, strings.NewReader(""), &output, &diagnostic, collect, prepare); code != 1 {
				t.Fatal("ordinary input failure was changed into cleanup uncertainty")
			}
			if phase == "setup" {
				if output.Len() != 0 {
					t.Fatal("rejected input fabricated worker evidence")
				}
			} else {
				result, err := keyboardevidence.Decode(output.Bytes())
				if err != nil || !result.CleanupConfirmed || result.Status != inputtest.StatusError || result.Detail != "console_unavailable" {
					t.Fatal("confirmed-cleanup input failure lost its diagnostic")
				}
			}
		})
	}
}
