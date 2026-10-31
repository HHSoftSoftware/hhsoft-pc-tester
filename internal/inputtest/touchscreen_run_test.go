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

type touchscreenFixture struct {
	metadata                           touchscreenMetadata
	size, firstCut                     int
	stale, data                        []byte
	armed, retryOnce                   bool
	reads, neutrals, snapshots, closes int
	openErr, closeErr, readErr         error
	beforeRead                         func(*touchscreenFixture, []byte)
	onIdle                             func(*touchscreenFixture)
	query                              func(int) (bool, error)
	check                              func(int) (touchscreenMetadata, error)
	closeHook                          func()
	override                           func([]byte) (int, error)
}

func touchscreenFixtureBytes(size int, events []touchscreenEvent) []byte {
	raw := make([]byte, size*len(events))
	for i, e := range events {
		record := raw[i*size+size-8 : (i+1)*size]
		binary.NativeEndian.PutUint16(record, e.typeCode)
		binary.NativeEndian.PutUint16(record[2:], e.code)
		binary.NativeEndian.PutUint32(record[4:], uint32(e.value))
	}
	return raw
}

func newTouchscreenFixture(size int) *touchscreenFixture {
	return &touchscreenFixture{size: size, metadata: touchscreenMetadata{identity: TouchscreenIdentity{Bus: 3, Vendor: 1, Product: 2, Version: 1},
		capabilities: touchscreenTestCapabilities()}, data: touchscreenFixtureBytes(size, touchscreenTestCompleteEvents())}
}

func (f *touchscreenFixture) Close() error {
	f.closes++
	if f.closeHook != nil {
		f.closeHook()
	}
	return f.closeErr
}

func (f *touchscreenFixture) opened() touchscreenOpened {
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
			return 0, errTouchscreenRetry
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
			return 0, errTouchscreenIdle
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
	snapshot := func() (touchscreenMetadata, error) {
		f.snapshots++
		if f.check != nil {
			return f.check(f.snapshots)
		}
		return f.metadata, nil
	}
	return touchscreenOpened{owner: f, metadata: f.metadata, eventSize: f.size, typeOffset: f.size - 8,
		read: read, snapshot: snapshot, finalSnapshot: snapshot,
		neutral: func() (bool, error) {
			f.neutrals++
			if f.query != nil {
				return f.query(f.neutrals)
			}
			return true, nil
		}}
}

func touchscreenFixtureBackend(fixtures ...*touchscreenFixture) touchscreenBackend {
	return touchscreenBackend{discover: func(context.Context, string) ([]string, error) {
		names := []string{}
		for i := range fixtures {
			names = append(names, "event"+strconv.Itoa(i))
		}
		return names, nil
	}, open: func(_ context.Context, _ string, name string) (touchscreenOpened, error) {
		for i, f := range fixtures {
			if name == "event"+strconv.Itoa(i) {
				return f.opened(), f.openErr
			}
		}
		return touchscreenOpened{}, errInputUnavailable
	}}
}

func TestTouchscreenRunnerFragmentationAndOwnerClosure(t *testing.T) {
	for _, size := range []int{16, 24} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			for cut := 1; cut < size; cut++ {
				first, second := newTouchscreenFixture(size), newTouchscreenFixture(size)
				first.firstCut, second.firstCut = cut, size-cut
				order := []int{}
				first.closeHook = func() { order = append(order, 0) }
				second.closeHook = func() { order = append(order, 1) }
				result := runTouchscreenWith(t.Context(), TouchscreenOptions{Duration: time.Second}, touchscreenFixtureBackend(first, second))
				if result.Status != StatusPass || result.StopReason != "completed" || !result.CleanupConfirmed ||
					!reflect.DeepEqual(order, []int{1, 0}) || first.closes != 1 || second.closes != 1 {
					t.Fatal("fragmented contact or reverse owner cleanup failed")
				}
				for _, row := range result.Devices {
					if !row.BaselineNeutral || !row.FinalNeutral || !row.CleanupConfirmed ||
						row.QualifyingContacts != 1 || row.ContactStarts != 1 || row.ContactReleases != 1 ||
						row.XPositive != 1 || row.XNegative != 1 || row.YPositive != 1 || row.YNegative != 1 {
						t.Fatal("runner did not preserve the independent four-frame gesture")
					}
				}
				if first.neutrals != 2 || second.neutrals != 2 {
					t.Fatal("key getter escaped startup/terminal phases")
				}
			}
		})
	}
}

func TestTouchscreenStartupEvidenceNeverSuppliesCredit(t *testing.T) {
	for _, name := range []string{"stale_complete", "held", "partial_record", "partial_frame", "lost"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f := newTouchscreenFixture(24)
			switch name {
			case "stale_complete":
				f.stale, f.data = f.data, nil
				f.onIdle = func(f *touchscreenFixture) {
					if f.reads >= 3 {
						cancel()
					}
				}
			case "held":
				f.query = func(int) (bool, error) { return false, nil }
				f.onIdle = func(f *touchscreenFixture) {
					if f.reads >= 3 {
						cancel()
					}
				}
			case "partial_record":
				f.stale = []byte{1}
			case "partial_frame":
				f.stale = touchscreenFixtureBytes(24, []touchscreenEvent{{1, 0x14a, 1}})
			case "lost":
				f.stale = touchscreenFixtureBytes(24, []touchscreenEvent{{0, 3, 0}})
			}
			result := runTouchscreenWith(ctx, TouchscreenOptions{Duration: time.Second}, touchscreenFixtureBackend(f))
			if result.Status == StatusPass || f.closes != 1 || len(result.Devices) != 1 ||
				result.Devices[0].QualifyingContacts != 0 || result.Devices[0].FinalNeutral {
				t.Fatal("startup records or held state supplied action credit")
			}
			if name == "held" {
				row := result.Devices[0]
				if f.neutrals != 1 || row.BaselineNeutral || row.Frames != 0 ||
					row.ContactStarts != 0 || row.ContactReleases != 0 || row.XYBaselines != 0 ||
					row.XPositive != 0 || row.XNegative != 0 || row.YPositive != 0 || row.YNegative != 0 {
					t.Fatal("held startup query did not prevent complete-gesture credit")
				}
			}
			if name == "lost" && !result.Devices[0].EvidenceLoss {
				t.Fatal("startup event loss was discarded")
			}
		})
	}
}

func TestTouchscreenTerminalProofRejectsQueuedRetryAndIdentity(t *testing.T) {
	for _, name := range []string{"queued", "retry", "partial", "read_error", "zero_nil", "held", "query_error", "identity"} {
		t.Run(name, func(t *testing.T) {
			f := newTouchscreenFixture(24)
			f.beforeRead = func(f *touchscreenFixture, dst []byte) {
				if f.reads != 4 {
					return
				}
				switch name {
				case "queued":
					f.override = func(dst []byte) (int, error) {
						return copy(dst, touchscreenFixtureBytes(24, []touchscreenEvent{{0, 0, 0}})), nil
					}
				case "retry":
					f.override = func([]byte) (int, error) { return 0, errTouchscreenRetry }
				case "partial":
					f.override = func(dst []byte) (int, error) { dst[0] = 1; return 1, nil }
				case "read_error":
					f.override = func([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
				case "zero_nil":
					f.override = func([]byte) (int, error) { return 0, nil }
				}
			}
			f.query = func(n int) (bool, error) {
				if n == 2 {
					switch name {
					case "held":
						return false, nil
					case "query_error":
						return false, errors.New("unexported query detail")
					case "identity":
						f.metadata.identity.Version++
					}
				}
				return true, nil
			}
			result := runTouchscreenWith(t.Context(), TouchscreenOptions{Duration: time.Second}, touchscreenFixtureBackend(f))
			if result.Status == StatusPass || result.Devices[0].FinalNeutral || f.closes != 1 ||
				result.Devices[0].QualifyingContacts != 1 {
				t.Fatal("terminal failure lost diagnostic motion or retained a pass")
			}
			if f.reads != 4 {
				t.Fatal("terminal uncertainty restarted observation")
			}
		})
	}
}

func TestTouchscreenRunnerContinuesObservingEarlierCandidate(t *testing.T) {
	first, second := newTouchscreenFixture(24), newTouchscreenFixture(24)
	second.data = nil
	first.onIdle = func(f *touchscreenFixture) {
		if f.reads == 3 {
			f.data = touchscreenFixtureBytes(24, []touchscreenEvent{{1, 0x14a, 1}, {1, 0x14a, 0}, {0, 0, 0}})
		}
	}
	second.beforeRead = func(f *touchscreenFixture, _ []byte) {
		if f.reads == 4 {
			f.data = touchscreenFixtureBytes(24, touchscreenTestCompleteEvents())
		}
	}
	result := runTouchscreenWith(t.Context(), TouchscreenOptions{Duration: time.Second}, touchscreenFixtureBackend(first, second))
	if result.Status != StatusError || result.Detail != "malformed_events" ||
		first.neutrals != 1 || first.closes != 1 || second.closes != 1 || result.Devices[0].FinalNeutral {
		t.Fatal("candidate escaped observation while another device was unfinished")
	}
}

func TestTouchscreenOwnershipErrorAndCancellationPrecedence(t *testing.T) {
	for _, name := range []string{"open_with_owner", "duplicate_owner", "cancel_after_open", "close_failure", "cancel_and_close"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			f := newTouchscreenFixture(24)
			backend := touchscreenFixtureBackend(f)
			switch name {
			case "open_with_owner":
				f.openErr = errors.New("unexported open detail")
			case "duplicate_owner":
				backend = touchscreenBackend{
					discover: func(context.Context, string) ([]string, error) { return []string{"event0", "event1"}, nil },
					open:     func(context.Context, string, string) (touchscreenOpened, error) { return f.opened(), nil },
				}
			case "cancel_after_open":
				backend.open = func(context.Context, string, string) (touchscreenOpened, error) { cancel(); return f.opened(), nil }
			case "close_failure":
				f.closeErr = errors.New("unexported close detail")
			case "cancel_and_close":
				backend.open = func(context.Context, string, string) (touchscreenOpened, error) { cancel(); return f.opened(), nil }
				f.closeErr = errors.New("unexported close detail")
			}
			result := runTouchscreenWith(ctx, TouchscreenOptions{Duration: time.Second}, backend)
			if result.Status == StatusPass || f.closes != 1 {
				t.Fatal("failed or interrupted acquisition passed or closed an owner twice")
			}
			if name == "cancel_after_open" && result.Status != StatusAborted {
				t.Fatal("clean cancellation lost its disposition")
			}
			if name == "close_failure" || name == "cancel_and_close" {
				if result.Status != StatusError || result.CleanupConfirmed || result.Detail != "cleanup_unconfirmed" {
					t.Fatal("cleanup uncertainty lost precedence")
				}
			}
		})
	}
}

func TestTouchscreenUnsupportedOrUnreadableDevicePreventsAggregatePass(t *testing.T) {
	for _, name := range []string{"multitouch", "stylus", "unavailable"} {
		t.Run(name, func(t *testing.T) {
			first, second := newTouchscreenFixture(24), newTouchscreenFixture(24)
			switch name {
			case "multitouch":
				second.metadata.capabilities.Axes = append(second.metadata.capabilities.Axes, TouchscreenAxis{Code: 47})
			case "stylus":
				second.metadata.capabilities.Keys = []uint16{0x140, 0x14a}
			}
			backend := touchscreenFixtureBackend(first, second)
			if name == "unavailable" {
				backend.open = func(_ context.Context, _ string, name string) (touchscreenOpened, error) {
					if name == "event1" {
						return touchscreenOpened{}, errInputUnavailable
					}
					return first.opened(), nil
				}
			}
			result := runTouchscreenWith(t.Context(), TouchscreenOptions{Duration: time.Second}, backend)
			if result.Status == StatusPass || len(result.Devices) != 2 || first.closes != 1 ||
				result.Devices[1].Status == StatusPass || result.Devices[1].Supported {
				t.Fatal("unsupported or unreadable registry candidate was hidden")
			}
			if name != "unavailable" && second.closes != 1 {
				t.Fatal("unsupported descriptor owner was not closed")
			}
		})
	}
}
