package quick

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

type memoryStressFixture struct {
	clock                            *stressFixtureClock
	operations                       memoryStressOperations
	monitor                          *safetyMonitor
	owner                            *memoryAvailabilityReader
	opens, allocations, monitorCalls int
	observe                          thermalObserver
}

func newMemoryStressFixture(t *testing.T) *memoryStressFixture {
	t.Helper()
	h := &memoryStressFixture{clock: &stressFixtureClock{base: time.Now()}}
	h.observe = func(context.Context, string) ThermalSnapshot {
		return ThermalSnapshot{Coverage: ThermalAvailable, Sensors: 1, PeakMilliCelsius: 42000}
	}
	h.operations = memoryStressOperations{
		now: h.clock.now,
		open: func(root string) (io.ReadCloser, error) {
			h.opens++
			if root != "/fixture" {
				t.Error("changed root")
			}
			input := strings.NewReader("MemAvailable: 262144 kB\n")
			h.owner = &memoryAvailabilityReader{read: input.Read}
			return h.owner, nil
		},
		allocate: func(n int) []byte { h.allocations++; return make([]byte, n) },
		monitor: func(ctx context.Context, root string, timeout time.Duration, limit int) *safetyMonitor {
			h.monitorCalls++
			if root != "/fixture" || timeout != 1860*time.Second || limit != 90000 {
				t.Fatal("changed fixed monitor controls")
			}
			h.monitor = startSafetyMonitorWithPolicy(ctx, root, timeout, limit, h.observe, time.Hour, time.Second, time.Second, true)
			return h.monitor
		},
	}
	return h
}

func requireMemoryStressClosed(t *testing.T, h *memoryStressFixture, v MemoryStressResult) {
	t.Helper()
	if !v.Safety.CleanupConfirmed || !v.Availability.CloseConfirmed {
		t.Fatal("owned cleanup not confirmed")
	}
	if h.owner != nil && h.owner.closed.Load() != 1 {
		t.Fatal("availability not closed exactly once")
	}
	if h.monitor != nil {
		select {
		case <-h.monitor.done:
		default:
			t.Fatal("monitor owner not joined")
		}
		if !h.monitor.frozen {
			t.Fatal("monitor receipt not frozen")
		}
	}
}

func TestMemoryStressOwnsOneBufferAndSeparatesLoadFromTerminal(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int64
	}{{"exact_chunks", 4194304}, {"short_tail", 4194305}} {
		t.Run(tc.name, func(t *testing.T) {
			h := newMemoryStressFixture(t)
			var terminalChunks int
			h.operations.afterChunk = func(b []byte, cycle uint64, phase, first, count int, terminal bool) {
				if terminal {
					terminalChunks++
					return
				}
				if cycle == 0 && phase == 9 && first+count == len(b) {
					h.clock.nanos.Store(int64(1799 * time.Second))
				}
				if cycle == 1 && phase == 0 && first == 0 {
					h.clock.nanos.Store(int64(1800*time.Second + time.Millisecond))
				}
			}
			v, err := runMemoryStressWith(context.Background(), MemoryStressOptions{Bytes: tc.size, Root: "/fixture"}, h.operations)
			if err != nil || v.Status != StatusPass || v.StopReason != "completed" || h.opens != 1 || h.allocations != 1 || h.monitorCalls != 1 {
				t.Fatalf("valid load failed: %+v %v", v, err)
			}
			want := MemoryStressCoverage{CompletedPhases: 20, CompletedCycles: 2, LoadCycles: 1, ReadBytes: uint64(tc.size) * 12, WrittenBytes: uint64(tc.size) * 12, TerminalReadBytes: uint64(tc.size)}
			if v.Coverage != want || terminalChunks != (int(tc.size)+262143)/262144 || v.ObservedLoadMilliseconds != 1800000 || v.FinishedMilliseconds != 1800001 {
				t.Fatalf("load/finish/terminal mixed: %+v", v.Coverage)
			}
			requireMemoryStressClosed(t, h, v)
		})
	}
}

func TestMemoryStressFreshCycleRechecksLoadBoundaryBeforeKernel(t *testing.T) {
	for _, offset := range []struct {
		name string
		at   time.Duration
	}{{"equal", 1800 * time.Second}, {"after", 1800*time.Second + time.Millisecond}} {
		t.Run(offset.name, func(t *testing.T) {
			h := newMemoryStressFixture(t)
			boundaryReached, terminalReached := false, false
			h.operations.afterChunk = func(b []byte, cycle uint64, phase, first, count int, terminal bool) {
				if cycle == 0 && phase == 9 && first+count == len(b) && !terminal {
					h.clock.nanos.Store(int64(1799 * time.Second))
				}
			}
			h.operations.beforeChunk = func(b []byte, cycle uint64, phase, first, count int, terminal bool) {
				if terminal {
					terminalReached = true
					return
				}
				if cycle == 1 && phase == 0 && first == 0 {
					if h.clock.nanos.Load() != int64(1799*time.Second) {
						t.Fatal("outer pre-boundary prerequisite")
					}
					for _, value := range b {
						if value != 170 {
							t.Fatal("completed first-cycle prerequisite")
						}
					}
					boundaryReached = true
					h.clock.nanos.Store(int64(offset.at))
				}
			}
			v, err := runMemoryStressWith(context.Background(), MemoryStressOptions{Bytes: 4194304, Root: "/fixture"}, h.operations)
			if err != nil || !boundaryReached || !terminalReached || v.Status != StatusPass {
				t.Fatalf("boundary did not reach final read: %+v %v", v, err)
			}
			want := MemoryStressCoverage{CompletedPhases: 10, CompletedCycles: 1, LoadCycles: 1, ReadBytes: 25165824, WrittenBytes: 25165824, TerminalReadBytes: 4194304}
			if v.Coverage != want {
				t.Fatalf("fresh cycle wrote after load boundary: %+v", v.Coverage)
			}
			requireMemoryStressClosed(t, h, v)
		})
	}
}

func TestMemoryStressAdmissionAndFreshAvailability(t *testing.T) {
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	v, err := RunMemoryStress(stopped, MemoryStressOptions{})
	if err != nil || v.StopReason != "parent_cancelled" || v.Availability.Started || v.AllocatedBytes != 0 {
		t.Fatal("real wrapper bypassed stopped admission")
	}
	var typedNil *nilStressContext
	for _, tc := range []struct {
		name string
		ctx  context.Context
		size int64
	}{
		{"nil", nil, 4194304}, {"typed_nil", typedNil, 4194304}, {"negative", context.Background(), -1},
		{"too_small", context.Background(), 4194303}, {"too_large", context.Background(), 67108865},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newMemoryStressFixture(t)
			v, err := runMemoryStressWith(tc.ctx, MemoryStressOptions{Bytes: tc.size, Root: "/fixture"}, h.operations)
			if !errors.Is(err, ErrMemoryStressOptions) || v != (MemoryStressResult{}) || h.monitorCalls != 0 || h.opens != 0 || h.allocations != 0 {
				t.Fatal("invalid options acquired an owner")
			}
		})
	}
	for _, tc := range []struct {
		name, available string
		request         int64
		want            uint64
	}{
		{"automatic_floor", "20479", 0, 4194304},
		{"automatic_cap", "262144", 0, 67108864},
		{"explicit_tail", "16385", 4194305, 4194305},
		{"below_minimum", "16383", 0, 0},
		{"over_quarter", "16384", 4194305, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newMemoryStressFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h.operations.open = func(string) (io.ReadCloser, error) {
				h.opens++
				r := strings.NewReader("MemAvailable: " + tc.available + " kB\n")
				h.owner = &memoryAvailabilityReader{read: r.Read}
				return h.owner, nil
			}
			h.operations.allocate = func(n int) []byte { h.allocations++; cancel(); return make([]byte, n) }
			v, err := runMemoryStressWith(ctx, MemoryStressOptions{Bytes: tc.request, Root: "/fixture"}, h.operations)
			if err != nil || h.opens != 1 || v.AllocatedBytes != tc.want || v.LoadStartedMilliseconds != nil {
				t.Fatalf("selection/fresh read failed: %+v %v", v, err)
			}
			if tc.want == 0 && (h.allocations != 0 || v.StopReason != "insufficient_availability") {
				t.Fatal("low availability allocated")
			}
			if tc.want != 0 && (h.allocations != 1 || v.StopReason != "parent_cancelled") {
				t.Fatal("post-allocation cancellation lost")
			}
			requireMemoryStressClosed(t, h, v)
		})
	}
}

func TestMemoryStressPreparationConsumesOriginalBudget(t *testing.T) {
	for _, stage := range []string{"availability", "allocation"} {
		t.Run(stage, func(t *testing.T) {
			h := newMemoryStressFixture(t)
			open, allocate := h.operations.open, h.operations.allocate
			h.operations.open = func(root string) (io.ReadCloser, error) {
				r, e := open(root)
				if stage == "availability" {
					h.clock.nanos.Store(int64(60 * time.Second))
				}
				return r, e
			}
			h.operations.allocate = func(n int) []byte {
				b := allocate(n)
				if stage == "allocation" {
					h.clock.nanos.Store(int64(60 * time.Second))
				}
				return b
			}
			v, err := runMemoryStressWith(context.Background(), MemoryStressOptions{Bytes: 4194304, Root: "/fixture"}, h.operations)
			if err != nil || v.StopReason != "insufficient_budget" || v.EffectiveDeadlineMilliseconds != 1860000 || !v.StopFacts.InsufficientBudget || v.LoadStartedMilliseconds != nil || v.Coverage != (MemoryStressCoverage{}) {
				t.Fatalf("preparation got a new load budget: %+v", v)
			}
			if stage == "availability" && h.allocations != 0 {
				t.Fatal("late acquisition allocated")
			}
			requireMemoryStressClosed(t, h, v)
		})
	}
}

func TestMemoryStressMismatchSurvivesCancelledHotAndLatePostchecks(t *testing.T) {
	for _, tc := range []struct {
		name  string
		phase int
		stop  string
	}{
		{"forward_cancel", 1, "cancel"}, {"reverse_cancel", 8, "cancel"}, {"reverse_heat", 8, "heat"}, {"reverse_watchdog", 8, "watchdog"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newMemoryStressFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			injected, stopped := false, false
			h.operations.beforeChunk = func(b []byte, cycle uint64, phase, first, count int, terminal bool) {
				target := first == 0
				if tc.phase == 8 {
					target = first+count == len(b)
				}
				if !terminal && cycle == 0 && phase == tc.phase && target {
					expected := byte(0)
					if phase == 8 {
						expected = 85
					}
					if b[0] != expected {
						t.Fatal("valid pre-fault byte prerequisite")
					}
					b[0] ^= 1
					injected = true
				}
			}
			h.operations.afterChunk = func(_ []byte, _ uint64, phase, _, _ int, terminal bool) {
				if terminal || phase != tc.phase || !injected {
					return
				}
				stopped = true
				switch tc.stop {
				case "cancel":
					cancel()
				case "watchdog":
					h.clock.nanos.Store(int64(1860 * time.Second))
				case "heat":
					at := time.Now()
					h.monitor.complete(thermalCompletion{snapshot: ThermalSnapshot{Coverage: ThermalAvailable, Sensors: 1, PeakMilliCelsius: 95000}, finished: at, invoked: true}, at.Add(time.Second))
				}
			}
			v, err := runMemoryStressWith(ctx, MemoryStressOptions{Bytes: 4194304, Root: "/fixture"}, h.operations)
			if err != nil || !injected || !stopped || v.Status != StatusFail || v.StopReason != "memory_mismatch" || v.Mismatch == nil || v.Mismatch.Offset != 0 || v.Mismatch.Phase != tc.phase {
				t.Fatalf("late mismatch lost: %+v %v", v, err)
			}
			if tc.phase == 1 && (v.Coverage.ReadBytes != 0 || v.Coverage.WrittenBytes != 4194304 || v.Coverage.UncreditedBytes != 0) {
				t.Fatal("offset-zero prefix credited")
			}
			if tc.phase == 8 && (v.Coverage.ReadBytes != 20709376 || v.Coverage.WrittenBytes != 24903680 || v.Coverage.PhaseBytes != 3932160 || v.Coverage.UncreditedBytes != 262143) {
				t.Fatalf("reverse late prefix changed: %+v", v.Coverage)
			}
			if tc.stop == "cancel" && !v.StopFacts.ParentCancelled || tc.stop == "heat" && (!v.StopFacts.ThermalLimit || v.Safety.PeakMilliCelsius != 95000) || tc.stop == "watchdog" && !v.StopFacts.WatchdogDeadline {
				t.Fatal("independent adverse fact lost")
			}
			requireMemoryStressClosed(t, h, v)
		})
	}
}

func TestMemoryStressTerminalMismatchAndNoVerifiedLoadCycle(t *testing.T) {
	for _, mode := range []string{"terminal_mismatch", "first_cycle_late"} {
		t.Run(mode, func(t *testing.T) {
			h := newMemoryStressFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h.operations.afterChunk = func(b []byte, cycle uint64, phase, first, count int, terminal bool) {
				if terminal {
					if mode == "terminal_mismatch" {
						cancel()
					}
					return
				}
				if mode == "first_cycle_late" && phase == 0 && first == 0 {
					h.clock.nanos.Store(int64(1800*time.Second + time.Millisecond))
				}
				if mode == "terminal_mismatch" && cycle == 0 && phase == 9 && first+count == len(b) {
					h.clock.nanos.Store(int64(1800 * time.Second))
				}
			}
			h.operations.beforeChunk = func(b []byte, _ uint64, _ int, first, _ int, terminal bool) {
				if terminal && first == 0 && mode == "terminal_mismatch" {
					if b[0] != 170 {
						t.Fatal("terminal prerequisite")
					}
					b[0] = 171
				}
			}
			v, err := runMemoryStressWith(ctx, MemoryStressOptions{Bytes: 4194304, Root: "/fixture"}, h.operations)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "terminal_mismatch" {
				if v.Status != StatusFail || v.Mismatch == nil || v.Mismatch.Stage != "terminal" || v.Mismatch.ExpectedByte != 170 || v.Coverage.CompletedCycles != 1 || v.Coverage.TerminalReadBytes != 0 || !v.StopFacts.ParentCancelled {
					t.Fatal("terminal mismatch became cycle credit or cancellation")
				}
			} else if v.Status != StatusInconclusive || v.StopReason != "incomplete_work" || v.Coverage.LoadCycles != 0 || v.Coverage.CompletedCycles != 1 || v.Coverage.TerminalReadBytes != 4194304 {
				t.Fatal("cycle completed only after load window qualified")
			}
			requireMemoryStressClosed(t, h, v)
		})
	}
}

func TestMemoryStressInitialThermalAndAllocationFailures(t *testing.T) {
	for _, mode := range []string{"unavailable", "partial", "hot", "panic", "wrong_capacity"} {
		t.Run(mode, func(t *testing.T) {
			h := newMemoryStressFixture(t)
			switch mode {
			case "unavailable":
				h.observe = func(context.Context, string) ThermalSnapshot { return ThermalSnapshot{Coverage: ThermalUnavailable} }
			case "partial":
				h.observe = func(context.Context, string) ThermalSnapshot {
					return ThermalSnapshot{Coverage: ThermalPartial, Sensors: 1, PeakMilliCelsius: 42000, UnavailableReadings: 1}
				}
			case "hot":
				h.observe = func(context.Context, string) ThermalSnapshot {
					return ThermalSnapshot{Coverage: ThermalAvailable, Sensors: 1, PeakMilliCelsius: 95000}
				}
			case "panic":
				h.operations.allocate = func(int) []byte { h.allocations++; panic("private") }
			case "wrong_capacity":
				h.operations.allocate = func(n int) []byte { h.allocations++; return make([]byte, n, n+1) }
			}
			v, err := runMemoryStressWith(context.Background(), MemoryStressOptions{Bytes: 4194304, Root: "/fixture"}, h.operations)
			if v.LoadStartedMilliseconds != nil || v.AllocatedBytes != 0 || v.Coverage != (MemoryStressCoverage{}) {
				t.Fatal("bad preparation started load")
			}
			if mode == "panic" || mode == "wrong_capacity" {
				if !errors.Is(err, ErrMemoryStressAccounting) || v.Status != StatusError || h.allocations != 1 {
					t.Fatal("allocation fault escaped")
				}
			} else if err != nil || h.opens != 0 || h.allocations != 0 || v.Status == StatusPass {
				t.Fatal("adverse initial observation acquired memory")
			}
			requireMemoryStressClosed(t, h, v)
		})
	}
}

func TestMemoryStressAbsoluteMonitorJoinAndLegacyStop(t *testing.T) {
	for _, mode := range []string{"absolute", "legacy", "zero"} {
		t.Run(mode, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			monitor := startSafetyMonitorWithPolicy(context.Background(), "/fixture", time.Hour, 90000, func(context.Context, string) ThermalSnapshot {
				close(entered)
				<-release
				return ThermalSnapshot{Coverage: ThermalAvailable, Sensors: 1, PeakMilliCelsius: 42000}
			}, time.Hour, time.Millisecond, time.Second, true)
			<-entered
			if mode == "absolute" {
				start := time.Now()
				monitor.stopBefore(start.Add(10 * time.Millisecond))
				frozen := monitor.evidence()
				if frozen.CleanupConfirmed || time.Since(start) > 500*time.Millisecond {
					close(release)
					t.Fatal("join ignored original boundary")
				}
				close(release)
				select {
				case <-monitor.done:
				case <-time.After(time.Second):
					t.Fatal("late owner did not close")
				}
				monitor.stop()
				if !reflect.DeepEqual(frozen, monitor.evidence()) {
					t.Fatal("late owner rewrote frozen receipt")
				}
			} else {
				close(release)
				if mode == "legacy" {
					monitor.stop()
				} else {
					monitor.stopBefore(time.Time{})
				}
				if !monitor.evidence().CleanupConfirmed {
					t.Fatal("legacy stop window changed")
				}
			}
		})
	}
}

func TestMemoryStressExpiredBeforeFirstCycleNeverReadsUninitializedMemory(t *testing.T) {
	h := newMemoryStressFixture(t)
	pre, kernels := 0, 0
	h.operations.beforeChunk = func(_ []byte, cycle uint64, phase, first, _ int, terminal bool) {
		if terminal || cycle != 0 || phase != 0 || first != 0 {
			t.Fatal("unexpected kernel request")
		}
		pre++
		h.clock.nanos.Store(int64(1800 * time.Second))
	}
	h.operations.afterChunk = func([]byte, uint64, int, int, int, bool) { kernels++ }
	v, err := runMemoryStressWith(context.Background(), MemoryStressOptions{Bytes: 4194304, Root: "/fixture"}, h.operations)
	if err != nil || pre != 1 || kernels != 0 || v.Status != StatusInconclusive || v.StopReason != "incomplete_work" || v.Coverage != (MemoryStressCoverage{}) || v.Mismatch != nil || v.TerminalStartedMilliseconds != nil {
		t.Fatalf("uninitialized terminal verification: %+v %v", v, err)
	}
	requireMemoryStressClosed(t, h, v)
}
