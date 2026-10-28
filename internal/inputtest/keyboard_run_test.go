package inputtest

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"strconv"
	"testing"
	"time"
)

type keyboardFixtureSource struct {
	data, stale                                  []byte
	size, chunk, reads, checks, closes, neutrals int
	armed                                        bool
	capabilities                                 KeyboardCapabilities
	readError, closeError, openError             error
	check                                        func(int) error
	beforeRead                                   func(int)
	neutral                                      func(int) (bool, error)
	initialRetry                                 bool
}

func keyboardFixtureBytes(size int, events []keyboardEvent) []byte {
	data := make([]byte, size*len(events))
	for index, event := range events {
		record := data[index*size+size-8:]
		binary.NativeEndian.PutUint16(record, event.typeCode)
		binary.NativeEndian.PutUint16(record[2:], event.code)
		binary.NativeEndian.PutUint32(record[4:], uint32(event.value))
	}
	return data
}

func completeKeyboardSource(size int) *keyboardFixtureSource {
	return &keyboardFixtureSource{size: size, data: keyboardFixtureBytes(size, keyboardCompleteEvents()),
		capabilities: KeyboardCapabilities{EventKey: true, FixedKeys: []string{"left_shift", "right_shift", "up", "down", "left", "right"}}}
}

func keyboardFixtureBackend(sources ...*keyboardFixtureSource) keyboardBackend {
	names := make([]string, len(sources))
	for index := range names {
		names[index] = "event" + strconv.Itoa(index+2)
	}
	return keyboardBackend{discover: func(context.Context, string) ([]string, error) { return append([]string{}, names...), nil },
		open: func(_ context.Context, _, name string) (keyboardOpened, error) {
			for index, source := range sources {
				if name == names[index] {
					return keyboardOpened{identity: KeyboardIdentity{3, 1, uint16(index + 1), 1}, capabilities: source.capabilities, eventSize: source.size, typeOffset: source.size - 8,
						close: func() error { source.closes++; return source.closeError },
						validate: func() error {
							source.checks++
							if source.check != nil {
								return source.check(source.checks)
							}
							return nil
						},
						neutral: func() (bool, error) {
							source.neutrals++
							if source.neutral != nil {
								return source.neutral(source.neutrals)
							}
							return true, nil
						},
						read: func(buffer []byte) (int, error) {
							source.reads++
							if source.beforeRead != nil {
								source.beforeRead(source.reads)
							}
							if source.initialRetry && source.reads == 1 {
								return 0, errKeyboardRetry
							}
							data := &source.data
							if !source.armed {
								data = &source.stale
							}
							if len(*data) == 0 {
								if source.armed && source.readError != nil {
									return 0, source.readError
								}
								source.armed = true
								return 0, errKeyboardIdle
							}
							count := min(len(buffer), len(*data))
							if source.chunk > 0 {
								count = min(count, source.chunk)
							}
							copy(buffer, (*data)[:count])
							*data = (*data)[count:]
							return count, nil
						}}, source.openError
				}
			}
			return keyboardOpened{}, errors.New("fixture missing")
		}}
}

func TestKeyboardRunnerCompleteLayoutsAndIndependentDevices(t *testing.T) {
	for _, size := range []int{16, 24} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			first, second := completeKeyboardSource(size), completeKeyboardSource(size)
			first.chunk = 17
			second.chunk = 31
			result := runKeyboardWith(t.Context(), KeyboardOptions{Duration: time.Second}, keyboardFixtureBackend(first, second))
			if result.Status != StatusPass || result.StopReason != "completed" || !result.CleanupConfirmed || len(result.Devices) != 2 || first.closes != 1 || second.closes != 1 {
				t.Fatalf("complete keyboards did not pass: %#v", result)
			}
			for _, device := range result.Devices {
				if !device.BaselineNeutral || !device.FinalNeutral || !device.CleanupConfirmed {
					t.Fatal("missing lifetime boundary")
				}
			}
			first, second = completeKeyboardSource(size), completeKeyboardSource(size)
			first.data = keyboardFixtureBytes(size, keyboardCompleteEvents()[:12])
			second.data = keyboardFixtureBytes(size, keyboardCompleteEvents()[12:])
			result = runKeyboardWith(t.Context(), KeyboardOptions{Duration: 30 * time.Millisecond}, keyboardFixtureBackend(first, second))
			if result.Status == StatusPass || result.StopReason != "observation_timeout" || first.closes != 1 || second.closes != 1 {
				t.Fatal("complementary devices supplied one combined pass")
			}
		})
	}
}

func TestKeyboardRunnerBaselineNeverCreditsOldOrHeldInput(t *testing.T) {
	for _, condition := range []string{"stale complete", "held", "retry", "lost baseline", "malformed baseline", "partial baseline", "oversized baseline"} {
		t.Run(condition, func(t *testing.T) {
			source := completeKeyboardSource(24)
			switch condition {
			case "stale complete":
				source.stale = source.data
				source.data = nil
			case "held":
				source.neutral = func(int) (bool, error) { return false, nil }
			case "retry":
				source.initialRetry = true
				source.stale = source.data
				source.data = nil
			case "lost baseline":
				source.stale = keyboardFixtureBytes(24, []keyboardEvent{{0, 3, 0}, {0, 0, 0}})
			case "malformed baseline":
				source.stale = keyboardFixtureBytes(24, []keyboardEvent{{0, 2, 0}, {0, 0, 0}})
			case "partial baseline":
				source.stale = []byte{1}
			case "oversized baseline":
				source.stale = make([]byte, (64<<10)+24)
			}
			result := runKeyboardWith(t.Context(), KeyboardOptions{Duration: 150 * time.Millisecond}, keyboardFixtureBackend(source))
			if result.Status == StatusPass || source.closes != 1 {
				t.Fatal("baseline data supplied current completion or leaked ownership")
			}
			for _, action := range result.Devices[0].Actions {
				if action.PairCompleted || action.PressObserved {
					t.Fatal("baseline transition was credited")
				}
			}
			if condition == "lost baseline" && !result.Devices[0].EvidenceLoss {
				t.Fatal("baseline loss not latched")
			}
			if condition == "retry" && source.neutrals != 1 {
				t.Fatal("EINTR incorrectly supplied an idle baseline")
			}
		})
	}
}

func TestKeyboardRunnerRejectsTailsIdentityAndCleanupUncertainty(t *testing.T) {
	for _, condition := range []string{"eof", "loss", "partial record", "partial frame", "late replacement", "close failure", "metadata open failure", "metadata close failure", "nonneutral final", "neutral error"} {
		t.Run(condition, func(t *testing.T) {
			source := completeKeyboardSource(24)
			switch condition {
			case "eof":
				source.readError = io.EOF
			case "loss":
				source.data = append(source.data, keyboardFixtureBytes(24, []keyboardEvent{{0, 3, 0}})...)
			case "partial record":
				source.data = append(source.data, 1)
			case "partial frame":
				source.data = append(source.data, keyboardFixtureBytes(24, []keyboardEvent{{1, 42, 1}})...)
			case "late replacement":
				source.check = func(count int) error {
					if count >= 4 {
						return errors.New("changed")
					}
					return nil
				}
			case "close failure":
				source.closeError = errors.New("not confirmed")
			case "metadata open failure":
				source.openError = errors.New("no identity")
			case "metadata close failure":
				source.openError = errors.New("no identity")
				source.closeError = errors.New("not confirmed")
			case "nonneutral final":
				source.neutral = func(count int) (bool, error) { return count == 1, nil }
			case "neutral error":
				source.neutral = func(count int) (bool, error) {
					if count > 1 {
						return false, errors.New("unknown")
					}
					return true, nil
				}
			}
			result := runKeyboardWith(t.Context(), KeyboardOptions{Duration: 35 * time.Millisecond}, keyboardFixtureBackend(source))
			if result.Status == StatusPass || source.closes != 1 {
				t.Fatalf("uncertain lifetime passed or wrong close count: %#v", result)
			}
			uncertain := condition == "close failure" || condition == "metadata close failure"
			if result.CleanupConfirmed == uncertain || uncertain && (result.StopReason != "cleanup_unconfirmed" || result.Devices[0].CleanupConfirmed) {
				t.Fatal("lost cleanup uncertainty")
			}
			if condition == "partial record" || condition == "partial frame" {
				if result.Devices[0].Detail != "malformed_events" {
					t.Fatal("partial tail not retained")
				}
			}
		})
	}
}

func TestKeyboardRunnerFinalRegistryAndCancellationBoundaries(t *testing.T) {
	for _, condition := range []string{"registry added", "registry error", "cancel read", "cancel close", "cancel failed close", "expired read"} {
		t.Run(condition, func(t *testing.T) {
			source := completeKeyboardSource(24)
			backend := keyboardFixtureBackend(source)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			discovers := 0
			original := backend.discover
			backend.discover = func(ctx context.Context, root string) ([]string, error) {
				discovers++
				names, err := original(ctx, root)
				if discovers > 1 {
					if condition == "registry added" {
						names = append(names, "event8")
					}
					if condition == "registry error" {
						err = errors.New("gone")
					}
				}
				return names, err
			}
			if condition == "cancel read" {
				source.beforeRead = func(count int) {
					if count == 2 {
						cancel()
					}
				}
			}
			if condition == "expired read" {
				source.beforeRead = func(count int) {
					if count == 2 {
						time.Sleep(30 * time.Millisecond)
					}
				}
			}
			if condition == "cancel close" || condition == "cancel failed close" {
				open := backend.open
				backend.open = func(ctx context.Context, root, name string) (keyboardOpened, error) {
					owned, err := open(ctx, root, name)
					close := owned.close
					owned.close = func() error { cancel(); return close() }
					return owned, err
				}
			}
			if condition == "cancel failed close" {
				source.closeError = errors.New("unknown")
			}
			result := runKeyboardWith(ctx, KeyboardOptions{Duration: 25 * time.Millisecond}, backend)
			if result.Status == StatusPass || source.closes != 1 {
				t.Fatal("late boundary supplied pass or leaked resource")
			}
			if condition == "cancel failed close" && result.StopReason != "cleanup_unconfirmed" {
				t.Fatal("cancellation erased cleanup failure")
			}
			if condition == "cancel close" && result.Status != StatusAborted {
				t.Fatal("post-close cancellation ignored")
			}
		})
	}
}

func TestKeyboardRunnerUnavailableUnsupportedAndInvalidRegistry(t *testing.T) {
	for _, condition := range []string{"none", "duplicate", "noncanonical", "limit", "unsupported", "invalid options"} {
		t.Run(condition, func(t *testing.T) {
			source := completeKeyboardSource(24)
			backend := keyboardFixtureBackend(source)
			duration := time.Second
			switch condition {
			case "none":
				backend = keyboardFixtureBackend()
			case "duplicate":
				backend.discover = func(context.Context, string) ([]string, error) { return []string{"event2", "event2"}, nil }
			case "noncanonical":
				backend.discover = func(context.Context, string) ([]string, error) { return []string{"event02"}, nil }
			case "limit":
				backend.discover = func(context.Context, string) ([]string, error) { return make([]string, 129), nil }
			case "unsupported":
				source.capabilities.FixedKeys = []string{"left_shift"}
			case "invalid options":
				duration = time.Nanosecond
			}
			result := runKeyboardWith(t.Context(), KeyboardOptions{Duration: duration}, backend)
			if result.Status == StatusPass || !result.CleanupConfirmed {
				t.Fatal("unavailable/invalid source passed")
			}
			wantedCloses := 0
			if condition == "unsupported" {
				wantedCloses = 1
			}
			if source.closes != wantedCloses {
				t.Fatal("unexpected descriptor lifetime")
			}
		})
	}
}

func TestKeyboardTemporaryCleanupFailureNeverBecomesOrdinaryAbsence(t *testing.T) {
	for _, phase := range []string{"initial registry", "open metadata", "live identity", "final registry", "final identity"} {
		t.Run(phase, func(t *testing.T) {
			source := completeKeyboardSource(24)
			backend := keyboardFixtureBackend(source)
			original := backend.discover
			discovers := 0
			backend.discover = func(ctx context.Context, root string) ([]string, error) {
				discovers++
				if phase == "initial registry" || phase == "final registry" && discovers > 1 {
					return nil, errInputClose
				}
				return original(ctx, root)
			}
			if phase == "open metadata" {
				source.openError = errInputClose
			}
			if phase == "live identity" {
				source.check = func(int) error { return errInputClose }
			}
			if phase == "final identity" {
				source.check = func(count int) error {
					if count == 5 {
						return errInputClose
					}
					return nil
				}
			}
			result := runKeyboardWith(t.Context(), KeyboardOptions{Duration: time.Second}, backend)
			if result.Status != StatusError || result.StopReason != "cleanup_unconfirmed" || result.CleanupConfirmed {
				t.Fatalf("temporary ownership uncertainty was lost: %#v", result)
			}
			wantCloses := 1
			if phase == "initial registry" {
				wantCloses = 0
			}
			if source.closes != wantCloses {
				t.Fatal("evdev cleanup not completed exactly once")
			}
		})
	}
}

func TestKeyboardHeldBaselineRequiresNewPostNeutralPairs(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		source := completeKeyboardSource(24)
		source.stale = keyboardFixtureBytes(24, []keyboardEvent{{1, 42, 1}, {0, 0, 0}})
		source.data = keyboardFixtureBytes(24, []keyboardEvent{{1, 42, 0}, {0, 0, 0}})
		source.neutral = func(count int) (bool, error) { return count > 1, nil }
		source.beforeRead = func(count int) {
			if fresh && count == 5 {
				source.data = keyboardFixtureBytes(24, keyboardCompleteEvents())
			}
		}
		result := runKeyboardWith(t.Context(), KeyboardOptions{Duration: 60 * time.Millisecond}, keyboardFixtureBackend(source))
		if (result.Status == StatusPass) != fresh || source.closes != 1 {
			t.Fatal("held baseline did not require fresh complete actions")
		}
		if !fresh {
			for _, action := range result.Devices[0].Actions {
				if action.PressObserved || action.PairCompleted {
					t.Fatal("held baseline release became evidence")
				}
			}
		}
	}
}

func TestKeyboardUnreturnedSourceStorageCannotMutateEvidence(t *testing.T) {
	source := completeKeyboardSource(24)
	result := runKeyboardWith(t.Context(), KeyboardOptions{Duration: time.Second}, keyboardFixtureBackend(source))
	if result.Status != StatusPass {
		t.Fatal("fixture did not complete")
	}
	source.capabilities.FixedKeys[0] = "changed"
	if result.Devices[0].Capabilities.FixedKeys[0] != "left_shift" {
		t.Fatal("returned capabilities alias backend storage")
	}
	result.Devices[0].Actions[0].Name = "changed"
	second := runKeyboardWith(t.Context(), KeyboardOptions{Duration: time.Second}, keyboardFixtureBackend(completeKeyboardSource(24)))
	if second.Devices[0].Actions[0].Name != "left_shift" {
		t.Fatal("returned action mutation changed later runs")
	}
}

func TestKeyboardInvalidOptionsAndPreCancelledContextOwnNothing(t *testing.T) {
	for _, condition := range []string{"nil context", "zero duration", "excess duration", "cancelled"} {
		t.Run(condition, func(t *testing.T) {
			ctx := t.Context()
			duration := time.Second
			switch condition {
			case "nil context":
				ctx = nil
			case "zero duration":
				duration = 0
			case "excess duration":
				duration = 91 * time.Second
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			calls := 0
			backend := keyboardBackend{discover: func(context.Context, string) ([]string, error) { calls++; return []string{}, nil }, open: func(context.Context, string, string) (keyboardOpened, error) { calls++; return keyboardOpened{}, nil }}
			result := runKeyboardWith(ctx, KeyboardOptions{Duration: duration}, backend)
			if calls != 0 || !result.CleanupConfirmed || len(result.Devices) != 0 || result.Status == StatusPass {
				t.Fatal("invalid/cancelled invocation acquired resources")
			}
			if condition == "cancelled" {
				if result.Status != StatusAborted {
					t.Fatal("cancelled context lost")
				}
			} else if result.LimitMilliseconds != 0 || result.DurationMilliseconds != 0 || result.Detail != "invalid_options" {
				t.Fatal("invalid option diagnostic shape changed")
			}
		})
	}
}

func TestKeyboardFinalRediscoveryCannotRetainStalePass(t *testing.T) {
	for _, condition := range []string{"held control", "queued complete actions", "queued partial record", "replacement", "interrupted read"} {
		t.Run(condition, func(t *testing.T) {
			source := completeKeyboardSource(24)
			backend := keyboardFixtureBackend(source)
			discover := backend.discover
			calls := 0
			backend.discover = func(ctx context.Context, root string) ([]string, error) {
				calls++
				if calls == 2 {
					switch condition {
					case "held control":
						source.neutral = func(int) (bool, error) { return false, nil }
					case "queued complete actions":
						source.data = keyboardFixtureBytes(24, keyboardCompleteEvents())
					case "queued partial record":
						source.data = []byte{1}
					case "replacement":
						source.check = func(count int) error {
							if count >= 6 {
								return errors.New("replaced")
							}
							return nil
						}
					case "interrupted read":
						source.readError = errKeyboardRetry
					}
				}
				return discover(ctx, root)
			}
			result := runKeyboardWith(t.Context(), KeyboardOptions{Duration: time.Second}, backend)
			if calls != 2 || result.Status == StatusPass || result.Devices[0].Status == StatusPass || result.Devices[0].FinalNeutral || source.closes != 1 {
				t.Fatal("rediscovery race retained a stale completed device")
			}
			for _, action := range result.Devices[0].Actions {
				if !action.PairCompleted {
					t.Fatal("final recheck erased earlier fixed-control evidence")
				}
			}
		})
	}
}

func TestKeyboardCancellationRechecksPriorCompletedDeviceWithoutCreditingNewActions(t *testing.T) {
	for _, condition := range []string{"held", "queued", "replacement", "still neutral"} {
		t.Run(condition, func(t *testing.T) {
			first, second := completeKeyboardSource(24), completeKeyboardSource(24)
			second.data = nil
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			second.beforeRead = func(count int) {
				if count == 3 {
					if condition == "held" {
						first.neutral = func(int) (bool, error) { return false, nil }
					}
					if condition == "queued" {
						first.data = keyboardFixtureBytes(24, []keyboardEvent{{1, 42, 1}, {0, 0, 0}})
					}
					if condition == "replacement" {
						first.check = func(int) error { return errors.New("replaced") }
					}
					cancel()
				}
			}
			result := runKeyboardWith(ctx, KeyboardOptions{Duration: time.Second}, keyboardFixtureBackend(first, second))
			want := StatusAborted
			if condition == "replacement" {
				want = StatusError
			}
			if result.Status != want || first.closes != 1 || second.closes != 1 {
				t.Fatal("cancelled ownership or hard-identity precedence did not finish")
			}
			if (result.Devices[0].Status == StatusPass) != (condition == "still neutral") {
				t.Fatal("prior completed device escaped its final recheck")
			}
			for _, action := range result.Devices[0].Actions {
				if !action.PairCompleted {
					t.Fatal("post-stop recheck lost earlier pairs")
				}
			}
			if result.Devices[1].Actions[0].PairCompleted {
				t.Fatal("post-stop data supplied another device")
			}
		})
	}
}
