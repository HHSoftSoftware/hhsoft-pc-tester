package inputtest

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"
)

func TestKeyboardInterruptionDistinguishesInitialStateFromHardFailure(t *testing.T) {
	for _, stop := range []error{context.Canceled, context.DeadlineExceeded} {
		initial := newKeyboardResult(KeyboardOptions{Duration: time.Second})
		initial.MarkInterrupted(stop)
		if initial.Detail == "" || initial.StopReason == "error" {
			t.Fatal("initial empty error masked interruption")
		}
		for _, detail := range []string{"registry_unavailable", "identity_changed", "disconnected", "malformed_events", "console_unavailable"} {
			value := newKeyboardResult(KeyboardOptions{Duration: time.Second})
			value.Detail = detail
			before := value
			value.MarkInterrupted(stop)
			if !reflect.DeepEqual(value, before) {
				t.Fatal("late interruption replaced a recorded hard failure")
			}
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if value := RunKeyboard(ctx, KeyboardOptions{Duration: time.Second}); value.Status != StatusAborted || value.StopReason != "operator_cancelled" || !value.CleanupConfirmed || len(value.Devices) != 0 {
		t.Fatal("public platform entry changed pre-cancel semantics")
	}
}

type keyboardReadStopContext struct {
	context.Context
	stopped error
}

func (ctx *keyboardReadStopContext) Err() error { return ctx.stopped }

func TestKeyboardReadSeparatesExactStoppedContextFromHardProtocol(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		for _, mode := range []string{"clean", "not_stopped", "positive", "negative", "oversize", "joined"} {
			t.Run(cause.Error()+"/"+mode, func(t *testing.T) {
				ctx := &keyboardReadStopContext{Context: t.Context()}
				source := completeKeyboardSource(24)
				backend := keyboardFixtureBackend(source)
				open := backend.open
				reads := 0
				backend.open = func(ctx context.Context, root, name string) (keyboardOpened, error) {
					owned, err := open(ctx, root, name)
					owned.read = func(p []byte) (int, error) {
						reads++
						if reads == 1 {
							return 0, errKeyboardIdle
						}
						if mode != "not_stopped" {
							ctx.(*keyboardReadStopContext).stopped = cause
						}
						switch mode {
						case "positive":
							copy(p, keyboardFixtureBytes(24, keyboardCompleteEvents()))
							return 24, cause
						case "negative":
							return -1, cause
						case "oversize":
							return len(p) + 1, cause
						case "joined":
							return 0, errors.Join(cause, io.ErrUnexpectedEOF)
						}
						return 0, cause
					}
					return owned, err
				}
				result := runKeyboardWith(ctx, KeyboardOptions{Duration: time.Second}, backend)
				if reads != 2 || source.closes != 1 || !result.CleanupConfirmed {
					t.Fatal("context read ownership changed")
				}
				if mode == "clean" {
					wantStatus, wantReason := StatusAborted, "operator_cancelled"
					if cause == context.DeadlineExceeded {
						wantStatus, wantReason = StatusError, "deadline"
					}
					if result.Status != wantStatus || result.StopReason != wantReason {
						t.Fatal("clean stopped context became a hard I/O failure")
					}
				} else if result.Status != StatusError || result.StopReason != "error" {
					t.Fatal("hard protocol became clean cancellation")
				}
				for _, a := range result.Devices[0].Actions {
					if a.PressObserved || a.ReleaseObserved || a.PairCompleted {
						t.Fatal("context-associated bytes supplied evidence")
					}
				}
			})
		}
	}
	var typedNil *keyboardReadStopContext
	calls := 0
	backend := keyboardBackend{discover: func(context.Context, string) ([]string, error) { calls++; return nil, nil }, open: func(context.Context, string, string) (keyboardOpened, error) { calls++; return keyboardOpened{}, nil }}
	if result := runKeyboardWith(typedNil, KeyboardOptions{Duration: time.Second}, backend); calls != 0 || result.Detail != "invalid_options" {
		t.Fatal("typed-nil context reached acquisition")
	}
}

func TestKeyboardAcquisitionErrorsOwnReturnedHandlesAndStopLaterOpens(t *testing.T) {
	for _, mode := range []string{"hard", "hard_cancel", "joined_cancel", "close", "close_cancel", "input_close", "absence_with_owner", "missing_read", "missing_validate", "missing_neutral", "missing_close", "error_missing_close", "empty_provider"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			opens, closes, calls := 0, 0, 0
			backend := keyboardBackend{discover: func(context.Context, string) ([]string, error) { return []string{"event0", "event1"}, nil }, open: func(context.Context, string, string) (keyboardOpened, error) {
				opens++
				if opens != 1 {
					t.Fatal("later interface acquired after hard fault")
				}
				source := keyboardOpened{read: func([]byte) (int, error) { calls++; return 0, errKeyboardIdle }, validate: func() error { calls++; return nil }, neutral: func() (bool, error) { calls++; return true, nil }, close: func() error {
					closes++
					if mode == "close" || mode == "close_cancel" {
						return errors.New("unpublished owner failure")
					}
					return nil
				}}
				err := errors.New("unpublished provider path")
				switch mode {
				case "hard_cancel", "close_cancel":
					cancel()
				case "joined_cancel":
					cancel()
					err = errors.Join(context.Canceled, err)
				case "input_close":
					err = errInputClose
				case "absence_with_owner":
					err = errInputUnavailable
				case "missing_read":
					source.read = nil
					err = nil
				case "missing_validate":
					source.validate = nil
					err = nil
				case "missing_neutral":
					source.neutral = nil
					err = nil
				case "missing_close":
					source.close = nil
					err = nil
				case "error_missing_close":
					source.close = nil
				case "empty_provider":
					source = keyboardOpened{}
					err = nil
				}
				return source, err
			}}
			result := runKeyboardWith(ctx, KeyboardOptions{Duration: time.Second}, backend)
			uncertain := mode == "close" || mode == "close_cancel" || mode == "input_close" || mode == "missing_close" || mode == "error_missing_close"
			wantCloses := 1
			if mode == "missing_close" || mode == "error_missing_close" || mode == "empty_provider" {
				wantCloses = 0
			}
			if result.Status != StatusError || result.CleanupConfirmed == uncertain || opens != 1 || closes != wantCloses || calls != 0 || result.Devices[0].Readable || result.Devices[1].Readable {
				t.Fatalf("bad acquisition result: %#v", result)
			}
			if uncertain && result.StopReason != "cleanup_unconfirmed" || !uncertain && result.Detail != "identity_unavailable" {
				t.Fatal("wrong ownership precedence")
			}
		})
	}
}

func TestKeyboardHardReadProtocolPrecedesCancellationAndCreditsNoFaultBytes(t *testing.T) {
	for _, mode := range []string{"zero", "negative", "oversize", "partial_error", "partial_idle", "partial_retry", "eof", "joined_idle", "joined_cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			source := completeKeyboardSource(24)
			backend := keyboardFixtureBackend(source)
			open := backend.open
			reads := 0
			backend.open = func(ctx context.Context, root, name string) (keyboardOpened, error) {
				owned, err := open(ctx, root, name)
				owned.read = func(p []byte) (int, error) {
					reads++
					if reads == 1 {
						return 0, errKeyboardIdle
					}
					cancel()
					copy(p, keyboardFixtureBytes(24, keyboardCompleteEvents()))
					switch mode {
					case "negative":
						return -1, nil
					case "oversize":
						return len(p) + 1, nil
					case "partial_error":
						return 24, errors.New("unpublished read error")
					case "partial_idle":
						return 24, errKeyboardIdle
					case "partial_retry":
						return 24, errKeyboardRetry
					case "eof":
						return 0, io.EOF
					case "joined_idle":
						return 0, errors.Join(errKeyboardIdle, io.ErrUnexpectedEOF)
					case "joined_cancel":
						return 0, errors.Join(context.Canceled, io.ErrUnexpectedEOF)
					}
					return 0, nil
				}
				return owned, err
			}
			result := runKeyboardWith(ctx, KeyboardOptions{Duration: time.Second}, backend)
			if result.Status != StatusError || result.StopReason != "error" || !result.CleanupConfirmed || source.closes != 1 || reads != 2 || result.Devices[0].FinalNeutral {
				t.Fatalf("hard read was hidden: %#v", result)
			}
			for _, action := range result.Devices[0].Actions {
				if action.PressObserved || action.ReleaseObserved || action.PairCompleted {
					t.Fatal("error-associated bytes supplied action credit")
				}
			}
		})
	}
}

func TestKeyboardHardFailureRetainsEarlierCompletedDeviceAndStopsWork(t *testing.T) {
	for _, mode := range []string{"read", "identity", "close_cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			first, second, third := completeKeyboardSource(24), completeKeyboardSource(24), completeKeyboardSource(24)
			second.data = nil
			third.data = nil
			third.beforeRead = func(n int) {
				if n > 2 {
					t.Fatal("later already-owned source processed after a hard failure")
				}
			}
			backend := keyboardFixtureBackend(first, second, third)
			open := backend.open
			secondReads := 0
			backend.open = func(ctx context.Context, root, name string) (keyboardOpened, error) {
				owned, err := open(ctx, root, name)
				if name == "event3" {
					read := owned.read
					owned.read = func(p []byte) (int, error) {
						secondReads++
						if secondReads == 3 {
							cancel()
							return 0, io.ErrUnexpectedEOF
						}
						return read(p)
					}
					if mode == "identity" {
						check := owned.validate
						owned.validate = func() error {
							if secondReads == 2 {
								cancel()
								return errors.New("changed")
							}
							return check()
						}
					}
					if mode == "close_cancel" {
						second.closeError = errors.New("close not confirmed")
					}
				}
				return owned, err
			}
			result := runKeyboardWith(ctx, KeyboardOptions{Duration: time.Second}, backend)
			if result.Status != StatusError || first.closes != 1 || second.closes != 1 || third.closes != 1 || third.reads != 2 || result.Devices[0].Status != StatusPass || !result.Devices[0].FinalNeutral {
				t.Fatalf("earlier independently completed evidence lost: %#v", result)
			}
			want := []KeyboardAction{{"left_shift", true, true, true}, {"right_shift", true, true, true}, {"up", true, true, true}, {"down", true, true, true}, {"left", true, true, true}, {"right", true, true, true}}
			if !reflect.DeepEqual(result.Devices[0].Actions, want) || result.Devices[0].Interface != "event2" || *result.Devices[0].Identity != (KeyboardIdentity{3, 1, 1, 1}) {
				t.Fatal("literal earlier row changed")
			}
			for _, a := range result.Devices[1].Actions {
				if a.PairCompleted || a.PressObserved {
					t.Fatal("later fault supplied credit")
				}
			}
			for _, a := range result.Devices[2].Actions {
				if a.PairCompleted || a.PressObserved || a.ReleaseObserved {
					t.Fatal("later unopened action window supplied credit")
				}
			}
			if (result.StopReason == "cleanup_unconfirmed") != (mode == "close_cancel") {
				t.Fatal("cleanup precedence changed")
			}
		})
	}
}

func TestKeyboardRegistryAndValidationHardErrorsSurviveLateCancellation(t *testing.T) {
	for _, mode := range []string{"initial_registry", "final_registry", "live_identity", "final_neutral"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			source := completeKeyboardSource(24)
			backend := keyboardFixtureBackend(source)
			discover := backend.discover
			calls := 0
			backend.discover = func(ctx context.Context, root string) ([]string, error) {
				calls++
				if mode == "initial_registry" || mode == "final_registry" && calls == 2 {
					cancel()
					return nil, errors.New("private fixture path")
				}
				return discover(ctx, root)
			}
			if mode == "live_identity" {
				source.check = func(int) error { cancel(); return errors.New("changed") }
			}
			if mode == "final_neutral" {
				source.neutral = func(n int) (bool, error) {
					if n > 1 {
						cancel()
						return false, errors.New("unknown")
					}
					return true, nil
				}
			}
			result := runKeyboardWith(ctx, KeyboardOptions{Duration: time.Second}, backend)
			if result.Status != StatusError || result.StopReason != "error" || !result.CleanupConfirmed {
				t.Fatalf("hard cause masked: %#v", result)
			}
			wantCloses := 1
			if mode == "initial_registry" {
				wantCloses = 0
			}
			if source.closes != wantCloses {
				t.Fatal("ownership mismatch")
			}
		})
	}
}

func TestKeyboardCancelledFinalProofPreservesHardAndCleanupFailures(t *testing.T) {
	for _, mode := range []string{"clean", "identity", "metadata_close", "queued", "retry", "neutral"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			first, second := completeKeyboardSource(24), completeKeyboardSource(24)
			second.data = nil
			second.beforeRead = func(n int) {
				if n == 3 {
					cancel()
				}
			}
			backend := keyboardFixtureBackend(first, second)
			open := backend.open
			proofs := 0
			backend.open = func(ctx context.Context, root, name string) (keyboardOpened, error) {
				owned, err := open(ctx, root, name)
				if name == "event2" {
					owned.finalValidate = func() error {
						proofs++
						if ctx.Err() != context.Canceled {
							t.Fatal("closing proof did not reach cancelled boundary")
						}
						switch mode {
						case "identity":
							return errInputIdentity
						case "metadata_close":
							return errInputClose
						}
						return nil
					}
					read := owned.read
					owned.read = func(p []byte) (int, error) {
						if ctx.Err() != nil {
							if mode == "retry" {
								return 0, errKeyboardRetry
							}
							if mode == "queued" {
								copy(p, keyboardFixtureBytes(24, keyboardCompleteEvents()))
								return 24, nil
							}
						}
						return read(p)
					}
					neutral := owned.neutral
					owned.neutral = func() (bool, error) {
						if ctx.Err() != nil && mode == "neutral" {
							return false, nil
						}
						return neutral()
					}
				}
				return owned, err
			}
			result := runKeyboardWith(ctx, KeyboardOptions{Duration: time.Second}, backend)
			want := StatusAborted
			if mode == "identity" || mode == "metadata_close" {
				want = StatusError
			}
			if result.Status != want || proofs == 0 || first.closes != 1 || second.closes != 1 || (result.Devices[0].Status == StatusPass) != (mode == "clean") {
				t.Fatalf("bad closing proof %#v", result)
			}
			if result.CleanupConfirmed != (mode != "metadata_close") {
				t.Fatal("temporary metadata cleanup lost")
			}
			for _, a := range result.Devices[0].Actions {
				if !a.PairCompleted {
					t.Fatal("post-stop proof erased prior action pairs")
				}
			}
		})
	}
}
