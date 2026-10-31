package inputtest

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"strconv"
	"testing"
	"time"
)

type touchpadFixture struct {
	metadata                           touchpadMetadata
	size, firstCut                     int
	stale, data                        []byte
	armed, retryOnce                   bool
	reads, neutrals, snapshots, closes int
	openErr, closeErr, readErr         error
	beforeRead                         func(*touchpadFixture, []byte)
	onIdle                             func(*touchpadFixture)
	query                              func(int) (bool, error)
	check                              func(int) (touchpadMetadata, error)
	closeHook                          func()
	override                           func([]byte) (int, error)
}

func touchpadFixtureBytes(size int, events []touchpadEvent) []byte {
	raw := make([]byte, size*len(events))
	for i, e := range events {
		record := raw[i*size+size-8 : (i+1)*size]
		binary.NativeEndian.PutUint16(record, e.typeCode)
		binary.NativeEndian.PutUint16(record[2:], e.code)
		binary.NativeEndian.PutUint32(record[4:], uint32(e.value))
	}
	return raw
}

func newTouchpadFixture(size int) *touchpadFixture {
	return &touchpadFixture{size: size, metadata: touchpadMetadata{identity: TouchpadIdentity{Bus: 3, Vendor: 1, Product: 2, Version: 1},
		capabilities: touchpadTestCapabilities()}, data: touchpadFixtureBytes(size, touchpadTestCompleteEvents())}
}

func (f *touchpadFixture) Close() error {
	f.closes++
	if f.closeHook != nil {
		f.closeHook()
	}
	return f.closeErr
}

func (f *touchpadFixture) opened() touchpadOpened {
	read := func(dst []byte) (int, error) {
		f.reads++
		if f.beforeRead != nil {
			f.beforeRead(f, dst)
		}
		if f.override != nil {
			return f.override(dst)
		}
		if f.retryOnce {
			f.retryOnce = false
			return 0, errTouchpadRetry
		}
		data := &f.data
		if !f.armed {
			data = &f.stale
		}
		if len(*data) == 0 {
			if f.armed && f.readErr != nil {
				return 0, f.readErr
			}
			f.armed = true
			if f.onIdle != nil {
				f.onIdle(f)
			}
			return 0, errTouchpadIdle
		}
		n := min(len(dst), len(*data))
		if f.armed && f.firstCut > 0 {
			n = min(n, f.firstCut)
			f.firstCut = 0
		}
		copy(dst, (*data)[:n])
		*data = (*data)[n:]
		return n, nil
	}
	snapshot := func() (touchpadMetadata, error) {
		f.snapshots++
		if f.check != nil {
			return f.check(f.snapshots)
		}
		return f.metadata, nil
	}
	return touchpadOpened{owner: f, metadata: f.metadata, eventSize: f.size, typeOffset: f.size - 8,
		read: read, snapshot: snapshot, finalSnapshot: snapshot,
		neutral: func() (bool, error) {
			f.neutrals++
			if f.query != nil {
				return f.query(f.neutrals)
			}
			return true, nil
		}}
}

func touchpadFixtureBackend(fixtures ...*touchpadFixture) touchpadBackend {
	return touchpadBackend{discover: func(context.Context, string) ([]string, error) {
		names := []string{}
		for i := range fixtures {
			names = append(names, "event"+strconv.Itoa(i))
		}
		return names, nil
	}, open: func(_ context.Context, _ string, name string) (touchpadOpened, error) {
		for i, f := range fixtures {
			if name == "event"+strconv.Itoa(i) {
				return f.opened(), f.openErr
			}
		}
		return touchpadOpened{}, errInputUnavailable
	}}
}

func TestTouchpadRunnerCompleteFragmentedLayoutsAndReverseClosure(t *testing.T) {
	for _, size := range []int{16, 24} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			for cut := 1; cut < size; cut++ {
				first, second := newTouchpadFixture(size), newTouchpadFixture(size)
				first.firstCut = cut
				second.firstCut = size - cut
				order := []int{}
				first.closeHook = func() { order = append(order, 0) }
				second.closeHook = func() { order = append(order, 1) }
				result := runTouchpadWith(t.Context(), TouchpadOptions{Duration: time.Second}, touchpadFixtureBackend(first, second))
				if result.Status != StatusPass || result.StopReason != "completed" || !result.CleanupConfirmed ||
					!reflect.DeepEqual(order, []int{1, 0}) || first.closes != 1 || second.closes != 1 {
					t.Fatal("fragmented complete devices or reverse closure failed")
				}
				for _, row := range result.Devices {
					if !row.BaselineNeutral || !row.FinalNeutral || !row.CleanupConfirmed || row.QualifyingContacts != 1 ||
						row.XPositive != 1 || row.XNegative != 1 || row.YPositive != 1 || row.YNegative != 1 {
						t.Fatal("wrong completed criteria")
					}
				}
				if first.neutrals != 2 || second.neutrals != 2 {
					t.Fatal("query outside baseline and terminal phases")
				}
			}
		})
	}
}

func TestTouchpadRunnerBaselineCannotCreditStaleOrHeldInput(t *testing.T) {
	for _, name := range []string{"stale_complete", "held", "retry", "partial", "partial_frame", "lost", "unknown_sync", "byte_limit", "frame_limit"} {
		t.Run(name, func(t *testing.T) {
			f := newTouchpadFixture(8)
			switch name {
			case "stale_complete":
				f.stale, f.data = f.data, nil
			case "held":
				f.query = func(int) (bool, error) { return false, nil }
			case "retry":
				f.retryOnce = true
				f.stale, f.data = f.data, nil
			case "partial":
				f.stale = []byte{1}
			case "partial_frame":
				f.stale = touchpadFixtureBytes(8, []touchpadEvent{{1, 0x14a, 1}})
			case "lost":
				f.stale = touchpadFixtureBytes(8, []touchpadEvent{{0, 3, 0}})
			case "unknown_sync":
				f.stale = touchpadFixtureBytes(8, []touchpadEvent{{0, 2, -2147483648}})
			case "byte_limit":
				f.stale = make([]byte, 65544) // Complete empty reports, then one overflow record.
			case "frame_limit":
				events := make([]touchpadEvent, 4097)
				for i := range events {
					events[i] = touchpadEvent{4, 4, 0}
				}
				f.stale = touchpadFixtureBytes(8, events)
			}
			duration := 120 * time.Millisecond
			if name == "byte_limit" || name == "frame_limit" {
				duration = time.Second
			}
			result := runTouchpadWith(t.Context(), TouchpadOptions{Duration: duration}, touchpadFixtureBackend(f))
			if result.Status == StatusPass || f.closes != 1 || result.Devices[0].QualifyingContacts != 0 {
				t.Fatal("baseline supplied action credit or leaked owner")
			}
			if name == "lost" && !result.Devices[0].EvidenceLoss {
				t.Fatal("baseline loss not retained")
			}
			if name == "frame_limit" && result.Detail != "frame_limit" {
				t.Fatal("baseline frame cap missing")
			}
			if name == "byte_limit" && result.Detail != "baseline_unavailable" {
				t.Fatal("baseline byte cap missing")
			}
		})
	}
}

func TestTouchpadRunnerContinuesCompletedAWhileBIsUnfinished(t *testing.T) {
	first, second := newTouchpadFixture(24), newTouchpadFixture(24)
	second.data = nil
	bad := touchpadFixtureBytes(24, []touchpadEvent{{1, 0x110, 1}, {1, 0x110, 0}, {0, 0, 0}})
	first.onIdle = func(f *touchpadFixture) {
		if f.reads == 3 {
			f.data = append([]byte{}, bad...)
		}
	}
	first.query = func(n int) (bool, error) {
		if n > 1 {
			first.data = nil
		} // Model the destructive same-client key query.
		return true, nil
	}
	second.beforeRead = func(f *touchpadFixture, _ []byte) {
		if f.reads == 4 {
			f.data = touchpadFixtureBytes(24, touchpadTestCompleteEvents())
		}
	}
	result := runTouchpadWith(t.Context(), TouchpadOptions{Duration: time.Second}, touchpadFixtureBackend(first, second))
	if result.Status != StatusError || result.Detail != "malformed_events" || first.neutrals != 1 ||
		first.closes != 1 || second.closes != 1 || result.Devices[0].FinalNeutral {
		t.Fatal("completed device escaped active error observation")
	}
	witness := newTouchpadFixture(24)
	witness.data = append([]byte{}, bad...)
	witness.query = func(int) (bool, error) { witness.data = nil; return true, nil }
	opened := witness.opened()
	if _, err := opened.neutral(); err != nil || len(witness.data) != 0 {
		t.Fatal("destructive-query fixture lacks its counterexample")
	}
}

func TestTouchpadTerminalProofAloneAssignsFinalNeutral(t *testing.T) {
	for _, name := range []string{"success", "queued", "partial", "retry", "nonneutral", "query_error", "first_identity", "last_identity", "positive_error", "zero_nil"} {
		t.Run(name, func(t *testing.T) {
			f := newTouchpadFixture(24)
			f.armed = true
			f.data = nil
			s := touchpadTestState()
			for _, e := range touchpadTestCompleteEvents() {
				s.consume(e)
			}
			m, _ := copyTouchpadMetadata(f.metadata)
			d := observedTouchpad{source: f.opened(), expected: m, state: s, candidate: true}
			switch name {
			case "queued":
				f.data = touchpadFixtureBytes(24, []touchpadEvent{{0, 0, 0}})
			case "partial":
				d.pending = []byte{1}
			case "retry":
				f.readErr = errTouchpadRetry
			case "nonneutral":
				f.query = func(int) (bool, error) { return false, nil }
			case "query_error":
				f.query = func(int) (bool, error) { return false, io.ErrUnexpectedEOF }
			case "first_identity":
				f.check = func(int) (touchpadMetadata, error) { return touchpadMetadata{}, errInputIdentity }
			case "last_identity":
				f.check = func(n int) (touchpadMetadata, error) {
					if n == 2 {
						return touchpadMetadata{}, errInputIdentity
					}
					return f.metadata, nil
				}
			case "positive_error":
				f.override = func(dst []byte) (int, error) { dst[0] = 1; return 1, io.EOF }
			case "zero_nil":
				f.override = func([]byte) (int, error) { return 0, nil }
			}
			stops := 0
			recheckTouchpadPass(&d, func() {
				stops++
				if s.result.FinalNeutral {
					t.Fatal("neutrality assigned before last boundary")
				}
			})
			if s.result.FinalNeutral != (name == "success") || f.closes != 0 || stops == 0 {
				t.Fatal("terminal proof or borrowed ownership boundary incorrect")
			}
			if s.result.QualifyingContacts != 1 || s.result.Buttons[0].Pairs != 1 {
				t.Fatal("terminal observations changed earlier action evidence")
			}
		})
	}
}

func TestTouchpadRunnerTerminalRacesDoNotResume(t *testing.T) {
	for _, name := range []string{"queued", "partial", "retry", "nonneutral", "identity", "close"} {
		t.Run(name, func(t *testing.T) {
			f := newTouchpadFixture(24)
			backend := touchpadFixtureBackend(f)
			discover := backend.discover
			calls := 0
			backend.discover = func(ctx context.Context, root string) ([]string, error) {
				calls++
				if calls == 2 {
					switch name {
					case "queued":
						f.data = touchpadFixtureBytes(24, touchpadTestCompleteEvents())
					case "partial":
						f.data = []byte{1}
					case "retry":
						f.readErr = errTouchpadRetry
					case "nonneutral":
						f.query = func(int) (bool, error) { return false, nil }
					case "identity":
						f.metadata.identity.Product++
					case "close":
						f.closeErr = io.ErrUnexpectedEOF
					}
				}
				return discover(ctx, root)
			}
			result := runTouchpadWith(t.Context(), TouchpadOptions{Duration: time.Second}, backend)
			if calls != 2 || result.Status == StatusPass || f.closes != 1 || f.reads > 4 {
				t.Fatal("terminal race resumed observation or retained pass")
			}
			if name != "close" && result.Devices[0].FinalNeutral {
				t.Fatal("failed final proof retained neutrality")
			}
			if name == "close" && (result.CleanupConfirmed || result.StopReason != "cleanup_unconfirmed") {
				t.Fatal("uncertain close retained pass")
			}
		})
	}
}

type touchpadSliceOwner []int

func (s touchpadSliceOwner) Close() error { s[0]++; return nil }

func TestTouchpadRunnerOwnershipTransferErrorsAndAliases(t *testing.T) {
	for _, name := range []string{"owner_error", "owner_cleanup_error", "duplicate_owner", "nonpointer_owner", "missing_owner", "typed_nil_owner", "missing_callback"} {
		t.Run(name, func(t *testing.T) {
			first, second := newTouchpadFixture(24), newTouchpadFixture(24)
			backend := touchpadFixtureBackend(first, second)
			open := backend.open
			sliceOwner := touchpadSliceOwner{0}
			backend.open = func(ctx context.Context, root, path string) (touchpadOpened, error) {
				o, e := open(ctx, root, path)
				if path == "event0" {
					switch name {
					case "owner_error":
						return o, io.ErrUnexpectedEOF
					case "owner_cleanup_error":
						first.closeErr = io.EOF
						return o, errInputClose
					case "nonpointer_owner":
						o.owner = sliceOwner
					case "missing_owner":
						o.owner = nil
					case "typed_nil_owner":
						var absent *touchpadFixture
						o.owner = absent
					case "missing_callback":
						o.snapshot = nil
					}
				}
				if name == "duplicate_owner" && path == "event1" {
					o.owner = first
				}
				return o, e
			}
			result := runTouchpadWith(t.Context(), TouchpadOptions{Duration: time.Second}, backend)
			if result.Status != StatusError || second.closes != 0 {
				t.Fatal("invalid transfer was admitted")
			}
			want := 1
			if name == "nonpointer_owner" || name == "missing_owner" || name == "typed_nil_owner" {
				want = 0
			}
			if first.closes != want || name == "nonpointer_owner" && sliceOwner[0] != 1 {
				t.Fatal("wrong concrete Close count")
			}
			if name == "duplicate_owner" && result.Detail != "ownership_alias" {
				t.Fatal("duplicate owner not rejected")
			}
			uncertain := name == "missing_owner" || name == "typed_nil_owner" || name == "owner_cleanup_error"
			if result.CleanupConfirmed == uncertain || uncertain && result.Devices[0].CleanupConfirmed {
				t.Fatal("ownership uncertainty lost")
			}
		})
	}
}

func TestTouchpadRunnerStopAndErrorPrecedence(t *testing.T) {
	for _, name := range []string{"cancel", "cancel_hard_read", "cancel_cleanup", "cancel_close", "spurious_cancel", "real_cancel_error"} {
		t.Run(name, func(t *testing.T) {
			f := newTouchpadFixture(24)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f.beforeRead = func(f *touchpadFixture, _ []byte) {
				if f.reads == 2 {
					switch name {
					case "cancel", "cancel_cleanup":
						cancel()
					case "cancel_hard_read":
						cancel()
						f.override = func([]byte) (int, error) { return 0, io.EOF }
					case "spurious_cancel":
						f.override = func([]byte) (int, error) { return 0, context.Canceled }
					case "real_cancel_error":
						cancel()
						f.override = func([]byte) (int, error) { return 0, context.Canceled }
					}
				}
			}
			if name == "cancel_cleanup" {
				f.closeErr = io.ErrUnexpectedEOF
			}
			if name == "cancel_close" {
				f.closeHook = cancel
			}
			result := runTouchpadWith(ctx, TouchpadOptions{Duration: time.Second}, touchpadFixtureBackend(f))
			want := StatusAborted
			if name == "cancel_hard_read" || name == "spurious_cancel" || name == "cancel_cleanup" {
				want = StatusError
			}
			if result.Status != want || f.closes != 1 {
				t.Fatal("cancellation/error precedence or Close count wrong")
			}
			if name == "cancel_cleanup" && result.StopReason != "cleanup_unconfirmed" {
				t.Fatal("cancellation erased cleanup uncertainty")
			}
		})
	}
}

func TestTouchpadRunnerReadSentinelsAndStaticMetadata(t *testing.T) {
	for _, name := range []string{"negative_count", "oversized_count", "zero_nil", "positive_idle", "positive_retry", "positive_error", "joined_idle", "eof", "changed_range", "changed_keys", "changed_identity"} {
		t.Run(name, func(t *testing.T) {
			f := newTouchpadFixture(24)
			f.beforeRead = func(f *touchpadFixture, _ []byte) {
				if f.reads != 2 {
					return
				}
				switch name {
				case "negative_count":
					f.override = func([]byte) (int, error) { return -1, nil }
				case "oversized_count":
					f.override = func(b []byte) (int, error) { return len(b) + 1, nil }
				case "zero_nil":
					f.override = func([]byte) (int, error) { return 0, nil }
				case "positive_idle":
					f.override = func([]byte) (int, error) { return 1, errTouchpadIdle }
				case "positive_retry":
					f.override = func([]byte) (int, error) { return 1, errTouchpadRetry }
				case "positive_error":
					f.override = func([]byte) (int, error) { return 1, io.EOF }
				case "joined_idle":
					f.override = func([]byte) (int, error) { return 0, errors.Join(errTouchpadIdle, io.EOF) }
				case "eof":
					f.override = func([]byte) (int, error) { return 0, io.EOF }
				case "changed_range":
					f.metadata.capabilities.Axes[0].Maximum++
				case "changed_keys":
					f.metadata.capabilities.Keys[0] = 0x111
				case "changed_identity":
					f.metadata.identity.Bus++
				}
			}
			result := runTouchpadWith(t.Context(), TouchpadOptions{Duration: time.Second}, touchpadFixtureBackend(f))
			if result.Status != StatusError || f.closes != 1 || result.Devices[0].FinalNeutral {
				t.Fatal("uncertain stream or replacement admitted")
			}
		})
	}
}

type touchpadMutationContext struct {
	context.Context
	hook func()
}

func (c *touchpadMutationContext) Err() error {
	if c.hook != nil {
		hook := c.hook
		c.hook = nil
		hook()
	}
	return c.Context.Err()
}

func TestTouchpadRunnerCopiesBeforeContextAndProviderCallbacks(t *testing.T) {
	for _, name := range []string{"registry", "metadata", "read_buffer"} {
		t.Run(name, func(t *testing.T) {
			f := newTouchpadFixture(24)
			backend := touchpadFixtureBackend(f)
			ctx := &touchpadMutationContext{Context: t.Context()}
			switch name {
			case "registry":
				discover := backend.discover
				calls := 0
				backend.discover = func(c context.Context, root string) ([]string, error) {
					names, err := discover(c, root)
					calls++
					if calls == 1 {
						ctx.hook = func() { names[0] = "event999" }
					}
					return names, err
				}
			case "metadata":
				initial, _ := copyTouchpadMetadata(f.metadata)
				f.check = func(int) (touchpadMetadata, error) { return initial, nil }
				open := backend.open
				backend.open = func(c context.Context, root, path string) (touchpadOpened, error) {
					o, e := open(c, root, path)
					ctx.hook = func() { f.metadata.capabilities.Axes[0].Minimum = 101; f.metadata.capabilities.Keys[0] = 0 }
					return o, e
				}
			case "read_buffer":
				f.beforeRead = func(f *touchpadFixture, b []byte) {
					if f.reads == 2 {
						ctx.hook = func() {
							for i := range b {
								b[i] = 0
							}
						}
					}
				}
			}
			result := runTouchpadWith(ctx, TouchpadOptions{Duration: time.Second}, backend)
			if result.Status != StatusPass || f.closes != 1 || result.Devices[0].Capabilities.Axes[0].Minimum != 50 {
				t.Fatal("callback alias changed admitted authority or bytes")
			}
		})
	}
}

func TestTouchpadRunnerExactRegistryAndUnsupportedRows(t *testing.T) {
	for _, name := range []string{"none", "duplicate", "noncanonical", "too_many", "unsupported_pair", "unreadable_pair", "added_final", "removed_final", "registry_error"} {
		t.Run(name, func(t *testing.T) {
			first, second := newTouchpadFixture(24), newTouchpadFixture(24)
			backend := touchpadFixtureBackend(first, second)
			discover := backend.discover
			calls := 0
			backend.discover = func(ctx context.Context, root string) ([]string, error) {
				calls++
				names, err := discover(ctx, root)
				switch name {
				case "none":
					return nil, nil
				case "duplicate":
					return []string{"event0", "event0"}, nil
				case "noncanonical":
					return []string{"event00"}, nil
				case "too_many":
					return make([]string, 129), nil
				case "added_final":
					if calls > 1 {
						names = append(names, "event2")
					}
				case "removed_final":
					if calls > 1 {
						names = names[:1]
					}
				case "registry_error":
					if calls > 1 {
						err = io.EOF
					}
				}
				return names, err
			}
			if name == "unsupported_pair" {
				second.metadata.capabilities.Properties = []uint16{1}
			}
			if name == "unreadable_pair" {
				open := backend.open
				backend.open = func(ctx context.Context, root, path string) (touchpadOpened, error) {
					if path == "event1" {
						return touchpadOpened{}, errInputUnavailable
					}
					return open(ctx, root, path)
				}
			}
			result := runTouchpadWith(t.Context(), TouchpadOptions{Duration: time.Second}, backend)
			if result.Status == StatusPass {
				t.Fatal("incomplete registry or mixed support supplied aggregate pass")
			}
			if name == "unsupported_pair" || name == "unreadable_pair" {
				if len(result.Devices) != 2 || result.Devices[0].Status != StatusPass || result.Devices[1].Status == StatusPass {
					t.Fatal("unsupported/unreadable row omitted")
				}
			}
			for _, f := range []*touchpadFixture{first, second} {
				if f.closes > 1 {
					t.Fatal("owner closed twice")
				}
			}
		})
	}
}

func TestTouchpadInvalidOptionsOwnNothing(t *testing.T) {
	for _, name := range []string{"nil_context", "zero", "too_short", "too_long", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			duration := time.Second
			switch name {
			case "nil_context":
				ctx = nil
			case "zero":
				duration = 0
			case "too_short":
				duration = 9 * time.Millisecond
			case "too_long":
				duration = 91 * time.Second
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			calls := 0
			b := touchpadBackend{discover: func(context.Context, string) ([]string, error) { calls++; return nil, nil }, open: func(context.Context, string, string) (touchpadOpened, error) { calls++; return touchpadOpened{}, nil }}
			r := runTouchpadWith(ctx, TouchpadOptions{Duration: duration}, b)
			if r.Status == StatusPass || calls != 0 || !r.CleanupConfirmed || len(r.Devices) != 0 {
				t.Fatal("invalid invocation acquired ownership")
			}
		})
	}
}

type touchpadDeadlineContext struct {
	context.Context
	stopped bool
}

func (c *touchpadDeadlineContext) Err() error {
	if c.stopped {
		return context.DeadlineExceeded
	}
	return c.Context.Err()
}

// These fixtures stop after a valid partial record or a valid unfinished
// packet. Neither condition is an observed EOF or malformed event.
func TestTouchpadInterruptedFragmentsDoNotInventProtocolFaults(t *testing.T) {
	for _, c := range []struct {
		name                                        string
		frame, deadline, eof, badRead, closeFailure bool
	}{
		{"partial_cancel", false, false, false, false, false},
		{"frame_cancel", true, false, false, false, false},
		{"partial_deadline", false, true, false, false, false},
		{"frame_deadline", true, true, false, false, false},
		{"partial_eof_cancel", false, false, true, false, false},
		{"frame_eof_deadline", true, true, true, false, false},
		{"partial_bad_read_cancel", false, false, false, true, false},
		{"frame_bad_read_deadline", true, true, false, true, false},
		{"partial_cleanup_cancel", false, false, false, false, true},
		{"frame_cleanup_deadline", true, true, false, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			base, cancel := context.WithCancel(t.Context())
			defer cancel()
			deadline := &touchpadDeadlineContext{Context: base}
			var ctx context.Context = base
			if c.deadline {
				ctx = deadline
			}
			f := newTouchpadFixture(24)
			frame := []touchpadEvent{{1, 0x145, 1}, {1, 0x14a, 1}, {3, 0, 100}, {3, 1, 100}}
			f.data = touchpadFixtureBytes(24, frame)
			if !c.frame {
				f.data = f.data[:7]
			}
			f.beforeRead = func(f *touchpadFixture, _ []byte) {
				if f.reads == 3 {
					if c.deadline {
						deadline.stopped = true
					} else {
						cancel()
					}
					if c.eof {
						f.override = func([]byte) (int, error) { return 0, io.EOF }
					}
					if c.badRead {
						f.override = func([]byte) (int, error) { return 1, io.EOF }
					}
				}
			}
			if c.closeFailure {
				f.closeErr = io.ErrUnexpectedEOF
			}
			r := runTouchpadWith(ctx, TouchpadOptions{Duration: time.Second}, touchpadFixtureBackend(f))
			want, reason := StatusAborted, "operator_cancelled"
			if c.deadline {
				want, reason = StatusError, "deadline"
			}
			if c.eof || c.badRead {
				want, reason = StatusError, "error"
			}
			if c.closeFailure {
				want, reason = StatusError, "cleanup_unconfirmed"
			}
			if r.Status != want || r.StopReason != reason || f.closes != 1 || len(r.Devices) != 1 ||
				r.Devices[0].FinalNeutral || r.Devices[0].QualifyingContacts != 0 || r.Devices[0].ContactStarts != 0 {
				t.Fatalf("interrupted fragment precedence changed: %#v", r)
			}
			if !c.eof && !c.badRead && !c.closeFailure && r.Devices[0].Detail != "incomplete_events" {
				t.Fatal("valid unfinished input was called malformed")
			}
			if r.CleanupConfirmed == c.closeFailure {
				t.Fatal("fragment interruption lost checked cleanup")
			}
		})
	}
	f := newTouchpadFixture(24)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.data = touchpadFixtureBytes(24, []touchpadEvent{{1, 0x145, 1}, {0, 3, 0}})
	f.closeHook = cancel
	r := runTouchpadWith(ctx, TouchpadOptions{Duration: time.Second}, touchpadFixtureBackend(f))
	if r.Status != StatusError || r.Detail != "event_loss" || !r.Devices[0].EvidenceLoss || f.closes != 1 {
		t.Fatal("later cancellation erased observed event loss")
	}
}

type touchpadCountdownContext struct {
	context.Context
	remaining int
	cancel    context.CancelFunc
}

func (c *touchpadCountdownContext) Err() error {
	if c.remaining > 0 {
		c.remaining--
		if c.remaining == 0 {
			c.cancel()
		}
	}
	return c.Context.Err()
}

func TestTouchpadEveryCallbackBoundaryPreservesInterruption(t *testing.T) {
	for _, phase := range []string{"initial_registry", "open", "baseline_query", "active_snapshot", "final_registry", "terminal_query", "terminal_snapshot", "close"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f := newTouchpadFixture(24)
			backend := touchpadFixtureBackend(f)
			discover := backend.discover
			calls := 0
			backend.discover = func(ctx context.Context, root string) ([]string, error) {
				calls++
				names, err := discover(ctx, root)
				if phase == "initial_registry" && calls == 1 || phase == "final_registry" && calls == 2 {
					cancel()
				}
				return names, err
			}
			open := backend.open
			backend.open = func(ctx context.Context, root, path string) (touchpadOpened, error) {
				o, e := open(ctx, root, path)
				if phase == "open" {
					cancel()
				}
				return o, e
			}
			f.query = func(n int) (bool, error) {
				if phase == "baseline_query" && n == 1 || phase == "terminal_query" && n == 2 {
					cancel()
				}
				return true, nil
			}
			f.check = func(n int) (touchpadMetadata, error) {
				if phase == "active_snapshot" && n == 2 || phase == "terminal_snapshot" && n == 6 {
					cancel()
				}
				return f.metadata, nil
			}
			if phase == "close" {
				f.closeHook = cancel
			}
			r := runTouchpadWith(ctx, TouchpadOptions{Duration: time.Second}, backend)
			wantCloses := 1
			if phase == "initial_registry" {
				wantCloses = 0
			}
			if r.Status != StatusAborted || r.StopReason != "operator_cancelled" || !r.CleanupConfirmed || f.closes != wantCloses {
				t.Fatal("callback cancellation bypassed stop or ownership")
			}
			if phase == "baseline_query" && r.Devices[0].BaselineNeutral {
				t.Fatal("cancelled baseline was admitted")
			}
		})
	}
	base, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := &touchpadCountdownContext{Context: base, cancel: cancel}
	f := newTouchpadFixture(24)
	f.beforeRead = func(f *touchpadFixture, _ []byte) {
		if f.reads == 2 {
			ctx.remaining = 4
		}
	}
	r := runTouchpadWith(ctx, TouchpadOptions{Duration: time.Second}, touchpadFixtureBackend(f))
	if r.Status != StatusAborted || r.Devices[0].Frames != 0 || r.Devices[0].ContactStarts != 0 ||
		r.Devices[0].Detail != "incomplete_events" || f.closes != 1 {
		t.Fatal("cancellation between valid staged fields committed a packet or invented a fault")
	}
}

func TestTouchpadMetadataCleanupUncertaintyRemainsExplicit(t *testing.T) {
	for _, phase := range []string{"initial_registry", "open", "baseline_query", "active_snapshot", "final_registry", "terminal_query", "terminal_snapshot"} {
		t.Run(phase, func(t *testing.T) {
			f := newTouchpadFixture(24)
			backend := touchpadFixtureBackend(f)
			discover := backend.discover
			calls := 0
			backend.discover = func(ctx context.Context, root string) ([]string, error) {
				calls++
				if phase == "initial_registry" && calls == 1 || phase == "final_registry" && calls == 2 {
					return nil, errInputClose
				}
				return discover(ctx, root)
			}
			if phase == "open" {
				f.openErr = errInputClose
			}
			f.query = func(n int) (bool, error) {
				if phase == "baseline_query" && n == 1 || phase == "terminal_query" && n == 2 {
					return false, errInputClose
				}
				return true, nil
			}
			f.check = func(n int) (touchpadMetadata, error) {
				if phase == "active_snapshot" && n == 2 || phase == "terminal_snapshot" && n == 6 {
					return touchpadMetadata{}, errInputClose
				}
				return f.metadata, nil
			}
			r := runTouchpadWith(t.Context(), TouchpadOptions{Duration: time.Second}, backend)
			wantCloses := 1
			if phase == "initial_registry" {
				wantCloses = 0
			}
			if r.Status != StatusError || r.StopReason != "cleanup_unconfirmed" || r.CleanupConfirmed || f.closes != wantCloses {
				t.Fatal("temporary metadata ownership was downgraded")
			}
			if phase != "initial_registry" && phase != "final_registry" && r.Devices[0].CleanupConfirmed {
				t.Fatal("readable Close hid another unconfirmed device-owned Close")
			}
		})
	}
}
