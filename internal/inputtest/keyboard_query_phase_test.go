package inputtest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strconv"
	"testing"
	"time"
)

func TestKeyboardActiveQueryCannotEraseMalformedPacket(t *testing.T) {
	for _, size := range []int{16, 24} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			first, second := completeKeyboardSource(size), completeKeyboardSource(size)
			second.data = nil
			// Independent fixed packet: both transitions before one report are
			// forbidden by the worker. A same-client key query would remove the
			// keys and leave this first report, hiding that fault from the parser.
			packet := keyboardFixtureBytes(size, []keyboardEvent{{1, 42, 1}, {1, 42, 0}, {0, 0, 0}})
			report := keyboardFixtureBytes(size, []keyboardEvent{{0, 0, 0}})
			injected, compacted := false, 0
			first.neutral = func(count int) (bool, error) {
				if count > 1 && bytes.Equal(first.data, packet) {
					first.data = append([]byte{}, report...)
					compacted++
				}
				return true, nil
			}
			second.beforeRead = func(count int) {
				if count == 4 {
					second.data = keyboardFixtureBytes(size, keyboardCompleteEvents())
				}
			}
			backend := keyboardFixtureBackend(first, second)
			open := backend.open
			backend.open = func(ctx context.Context, root, name string) (keyboardOpened, error) {
				owned, err := open(ctx, root, name)
				if name == "event2" {
					read := owned.read
					owned.read = func(buffer []byte) (int, error) {
						count, err := read(buffer)
						if first.reads == 3 && count == 0 && err == errKeyboardIdle {
							first.data = append([]byte{}, packet...)
							injected = true
						}
						return count, err
					}
				}
				return owned, err
			}
			result := runKeyboardWith(t.Context(), KeyboardOptions{Duration: time.Second}, backend)
			if !injected {
				t.Fatal("the fixture did not reach the idle-to-packet race")
			}
			if result.Status != StatusError || result.Detail != "malformed_events" || result.Devices[0].Detail != "malformed_events" || result.Devices[0].FinalNeutral {
				t.Fatal("a completed keyboard hid a malformed packet while another was unfinished")
			}
			if compacted != 0 || first.neutrals != 1 || second.neutrals != 1 {
				t.Fatal("an active query crossed the event-observation boundary")
			}
			if first.closes != 1 || second.closes != 1 || !result.CleanupConfirmed || result.Devices[1].Status == StatusPass {
				t.Fatal("hard packet failure lost ownership or supplied another device's pass")
			}
		})
	}
}

func TestKeyboardQueryPacketWitnessUsesRealDecoder(t *testing.T) {
	for _, compacted := range []bool{false, true} {
		state, _ := keyboardStateFixture()
		for _, event := range keyboardCompleteEvents() {
			state.consume(event)
		}
		if !compacted {
			state.consume(keyboardEvent{1, 42, 1})
			state.consume(keyboardEvent{1, 42, 0})
		}
		state.consume(keyboardEvent{0, 0, 0})
		if state.complete() != compacted || state.failed == compacted {
			t.Fatal("the original malformed packet and its compacted report were not distinguished")
		}
	}
}

func TestKeyboardBaselineQueryStillExcludesRacingOldActions(t *testing.T) {
	source := completeKeyboardSource(24)
	source.data = nil
	old := keyboardFixtureBytes(24, keyboardCompleteEvents())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	flushes := 0
	source.neutral = func(count int) (bool, error) {
		if count == 1 && bytes.Equal(source.data, old) {
			// State sampling establishes the original baseline and removes the
			// old keys. No action from this interval may be credited afterward.
			source.data = keyboardFixtureBytes(24, []keyboardEvent{{0, 0, 0}})
			flushes++
		}
		return true, nil
	}
	source.beforeRead = func(count int) {
		if count == 4 {
			cancel()
		}
	}
	backend := keyboardFixtureBackend(source)
	open := backend.open
	backend.open = func(ctx context.Context, root, name string) (keyboardOpened, error) {
		owned, err := open(ctx, root, name)
		read := owned.read
		owned.read = func(buffer []byte) (int, error) {
			count, err := read(buffer)
			if source.reads == 1 && count == 0 && err == errKeyboardIdle {
				source.data = append([]byte{}, old...)
			}
			return count, err
		}
		return owned, err
	}
	result := runKeyboardWith(ctx, KeyboardOptions{Duration: time.Second}, backend)
	if flushes != 1 || source.neutrals != 1 || source.closes != 1 || result.Status != StatusAborted || result.Devices[0].FinalNeutral {
		t.Fatal("the original baseline/stop boundary changed")
	}
	for _, action := range result.Devices[0].Actions {
		if action.PressObserved || action.ReleaseObserved || action.PairCompleted {
			t.Fatal("pre-baseline actions supplied fresh evidence")
		}
	}
}

func TestKeyboardTerminalProofAloneSetsFinalNeutral(t *testing.T) {
	for _, condition := range []string{"success", "first identity", "last identity", "queued", "partial record", "retry", "eof", "nil read", "negative count", "oversized count", "bytes with error", "nonneutral", "query error", "pending bytes", "partial frame", "incomplete state"} {
		t.Run(condition, func(t *testing.T) {
			state, result := keyboardStateFixture()
			for _, event := range keyboardCompleteEvents() {
				state.consume(event)
			}
			result.Status, result.Detail, result.FinalNeutral = StatusPass, "", false
			device := observedKeyboard{state: state}
			var order []string
			checks, reads, queries := 0, 0, 0
			failure := errors.New("fixture terminal failure")
			device.source.finalValidate = func() error {
				if result.FinalNeutral {
					t.Fatal("neutrality was published before the terminal proof ended")
				}
				order = append(order, "identity")
				checks++
				if condition == "first identity" && checks == 1 || condition == "last identity" && checks == 2 {
					return failure
				}
				return nil
			}
			device.source.validate = func() error { t.Fatal("terminal identity callback was bypassed"); return failure }
			device.source.read = func(buffer []byte) (int, error) {
				order = append(order, "read")
				reads++
				switch condition {
				case "queued":
					return copy(buffer, keyboardFixtureBytes(24, keyboardCompleteEvents())), nil
				case "partial record":
					buffer[0] = 1
					return 1, nil
				case "retry":
					return 0, errKeyboardRetry
				case "eof":
					return 0, io.EOF
				case "nil read":
					return 0, nil
				case "negative count":
					return -1, errKeyboardIdle
				case "oversized count":
					return len(buffer) + 1, nil
				case "bytes with error":
					return 1, io.EOF
				}
				return 0, errKeyboardIdle
			}
			device.source.neutral = func() (bool, error) {
				if result.FinalNeutral {
					t.Fatal("candidate supplied an unproved neutral fact")
				}
				order = append(order, "neutral")
				queries++
				if condition == "query error" {
					return false, failure
				}
				return condition != "nonneutral", nil
			}
			switch condition {
			case "pending bytes":
				device.pending = []byte{1}
			case "partial frame":
				state.consume(keyboardEvent{1, 42, 1})
			case "incomplete state":
				result.Actions[5].PairCompleted = false
			}
			before := append([]KeyboardAction{}, result.Actions...)
			err := recheckKeyboardPass(&device)
			if condition == "success" {
				if err != nil || result.Status != StatusPass || !result.FinalNeutral || !slices.Equal(order, []string{"identity", "read", "neutral", "identity"}) {
					t.Fatal("complete terminal proof did not publish final neutrality in order")
				}
			} else if result.Status == StatusPass || result.FinalNeutral || !state.failed {
				t.Fatal("an incomplete terminal proof retained a candidate pass")
			}
			if reads > 1 || queries > 1 || !slices.Equal(before, result.Actions) {
				t.Fatal("terminal proof retried or interpreted queued action evidence")
			}
			if (condition == "retry" || condition == "nonneutral" || condition == "queued" || condition == "partial record") && err != nil {
				t.Fatal("a non-error revocation was changed into a hard error")
			}
			if (condition == "first identity" || condition == "last identity" || condition == "query error") && !errors.Is(err, failure) {
				t.Fatal("terminal failure identity was lost")
			}
		})
	}
}

func TestKeyboardTerminalTrafficDoesNotResumeObservation(t *testing.T) {
	for _, condition := range []string{"queued held press", "nonneutral bitmap"} {
		t.Run(condition, func(t *testing.T) {
			source := completeKeyboardSource(24)
			backend := keyboardFixtureBackend(source)
			open := backend.open
			injected := false
			backend.open = func(ctx context.Context, root, name string) (keyboardOpened, error) {
				owned, err := open(ctx, root, name)
				read := owned.read
				owned.read = func(buffer []byte) (int, error) {
					count, err := read(buffer)
					if source.reads == 3 && count == 0 && err == errKeyboardIdle {
						injected = true
						if condition == "queued held press" {
							source.data = keyboardFixtureBytes(24, []keyboardEvent{{1, 42, 1}, {0, 0, 0}})
						} else {
							source.neutral = func(int) (bool, error) { return false, nil }
						}
					}
					return count, err
				}
				return owned, err
			}
			result := runKeyboardWith(t.Context(), KeyboardOptions{Duration: time.Second}, backend)
			wantQueries := 1
			if condition == "nonneutral bitmap" {
				wantQueries = 2
			}
			if !injected || source.reads != 4 || source.neutrals != wantQueries || source.closes != 1 || result.Status != StatusInconclusive || result.Devices[0].FinalNeutral {
				t.Fatal("racing terminal traffic resumed the loop or retained a pass")
			}
			for _, action := range result.Devices[0].Actions {
				if !action.PairCompleted {
					t.Fatal("terminal revocation erased earlier completed actions")
				}
			}
		})
	}
}

func TestKeyboardCandidateStopPreservesFinalProofAndCleanup(t *testing.T) {
	for _, condition := range []string{"cancel", "later read error", "later event loss", "cancel and close error"} {
		t.Run(condition, func(t *testing.T) {
			first, second := completeKeyboardSource(24), completeKeyboardSource(24)
			second.data = nil
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if condition == "cancel and close error" {
				first.closeError = errors.New("fixture close failure")
			}
			stopped := false
			second.beforeRead = func(count int) {
				if count == 3 {
					if first.neutrals != 1 {
						t.Fatal("A's active candidate queried state while B was unfinished")
					}
					stopped = true
					switch condition {
					case "cancel", "cancel and close error":
						cancel()
					case "later read error":
						second.readError = io.EOF
					case "later event loss":
						second.data = keyboardFixtureBytes(24, []keyboardEvent{{0, 3, 0}})
					}
				}
			}
			result := runKeyboardWith(ctx, KeyboardOptions{Duration: time.Second}, keyboardFixtureBackend(first, second))
			want := StatusError
			if condition == "cancel" {
				want = StatusAborted
			}
			if !stopped || result.Status != want || first.neutrals != 2 || first.closes != 1 || second.closes != 1 || !result.Devices[0].FinalNeutral {
				t.Fatal("stopping lost the prior candidate's bounded proof or ownership")
			}
			if condition == "cancel and close error" {
				if result.CleanupConfirmed || result.StopReason != "cleanup_unconfirmed" || result.Devices[0].Status != StatusError {
					t.Fatal("cancellation masked close uncertainty")
				}
			} else if !result.CleanupConfirmed || result.Devices[0].Status != StatusPass {
				t.Fatal("successful terminal diagnostic proof was discarded")
			}
			if condition == "later event loss" && !result.Devices[1].EvidenceLoss {
				t.Fatal("another device's event loss was not retained")
			}
			for _, action := range result.Devices[1].Actions {
				if action.PairCompleted {
					t.Fatal("terminal handling supplied the unfinished device's actions")
				}
			}
		})
	}
}
