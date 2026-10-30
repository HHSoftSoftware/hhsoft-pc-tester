package storagesample

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/workerexec"
)

type fixtureSampleOwner struct {
	check          func(int) error
	close          func() error
	checks, closes int
}

func (owner *fixtureSampleOwner) Check(context.Context) error {
	owner.checks++
	if owner.check != nil {
		return owner.check(owner.checks)
	}
	return nil
}

func (owner *fixtureSampleOwner) Close() error {
	owner.closes++
	if owner.close != nil {
		return owner.close()
	}
	return nil
}

func fixtureSampleParent(t *testing.T, timeout time.Duration) (*sampleBudget, *time.Time, sampleWorkerReply) {
	t.Helper()
	current := time.Unix(1700000000, 0)
	budget, err := newSampleBudget(timeout, func() time.Time { return current })
	if err != nil {
		t.Fatal(err)
	}
	return budget, &current, fixtureSampleWorkerReply()
}

func TestStorageSampleParentAcceptsOnlyCompleteOwnedLedger(t *testing.T) {
	budget, current, reply := fixtureSampleParent(t, 30*time.Second)
	*current = current.Add(3 * time.Second)
	ledger := []string{}
	owner := &fixtureSampleOwner{
		check: func(n int) error {
			if n == 1 {
				ledger = append(ledger, "initial")
			} else {
				ledger = append(ledger, "final")
			}
			return nil
		},
		close: func() error { ledger = append(ledger, "close"); return nil },
	}
	operations := sampleParentOperations{
		open: func(_ context.Context, target storagetest.Target) (sampleTargetOwner, error) {
			if !reflect.DeepEqual(target, reply.Target) {
				t.Fatal("target changed at acquisition")
			}
			ledger = append(ledger, "open")
			return owner, nil
		},
		run: func(_ context.Context, target storagetest.Target, snapshot string, remaining time.Duration) (workerexec.Result, error) {
			if !reflect.DeepEqual(target, reply.Target) || snapshot != reply.SnapshotSHA256 || remaining != 10*time.Second {
				t.Fatal("worker request or deadline changed")
			}
			ledger = append(ledger, "run")
			*current = current.Add(2 * time.Second)
			return workerexec.Result{Stdout: uncheckedSampleWorkerBytes(t, reply)}, nil
		},
	}
	out, err := parentSampleTarget(context.Background(), budget, reply.Target, reply.SnapshotSHA256, operations)
	if err != nil || out.Error != "none" || !out.CleanupConfirmed || !out.Device.Accepted || !out.Device.CloseConfirmed || !out.Device.FinalBindingConfirmed || out.Device.Status != "pass" || out.Device.StartedMilliseconds != 3000 || out.Device.FinishedMilliseconds != 5000 || out.Summary.ReadBytes != 1024 {
		t.Fatalf("complete parent ledger rejected: %+v, %v", out, err)
	}
	if !reflect.DeepEqual(ledger, []string{"open", "initial", "run", "final", "close"}) || owner.closes != 1 {
		t.Fatalf("ownership order changed: %v", ledger)
	}
}

func TestStorageSampleParentCancellationAtEverySuccessfulBoundary(t *testing.T) {
	for _, stage := range []string{"open", "initial", "run", "final", "close"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			budget, _, reply := fixtureSampleParent(t, 30*time.Second)
			runs := 0
			owner := &fixtureSampleOwner{
				check: func(n int) error {
					if n == 1 && stage == "initial" || n == 2 && stage == "final" {
						cancel()
					}
					return nil
				},
				close: func() error {
					if stage == "close" {
						cancel()
					}
					return nil
				},
			}
			operations := sampleParentOperations{
				open: func(context.Context, storagetest.Target) (sampleTargetOwner, error) {
					if stage == "open" {
						cancel()
					}
					return owner, nil
				},
				run: func(context.Context, storagetest.Target, string, time.Duration) (workerexec.Result, error) {
					runs++
					if stage == "run" {
						cancel()
					}
					return workerexec.Result{Stdout: uncheckedSampleWorkerBytes(t, reply)}, nil
				},
			}
			out, err := parentSampleTarget(ctx, budget, reply.Target, reply.SnapshotSHA256, operations)
			if err != nil || out.Error != "cancelled" || out.Device.Accepted || owner.closes != 1 || !out.Device.CloseConfirmed {
				t.Fatalf("successful return erased cancellation: %+v, %v", out, err)
			}
			expectedRuns := 1
			if stage == "open" || stage == "initial" {
				expectedRuns = 0
			}
			if runs != expectedRuns {
				t.Fatal("worker ran after observed cancellation")
			}
		})
	}
}

func TestStorageSampleParentOwnerErrorsCloseExactlyOnce(t *testing.T) {
	fault := errors.New("fixture ownership fault")
	for _, stage := range []string{"open", "initial", "final", "close", "typed_nil"} {
		t.Run(stage, func(t *testing.T) {
			budget, _, reply := fixtureSampleParent(t, 30*time.Second)
			runs := 0
			owner := &fixtureSampleOwner{
				check: func(n int) error {
					if n == 1 && stage == "initial" || n == 2 && stage == "final" {
						return fault
					}
					return nil
				},
				close: func() error {
					if stage == "close" {
						return fault
					}
					return nil
				},
			}
			operations := sampleParentOperations{
				open: func(context.Context, storagetest.Target) (sampleTargetOwner, error) {
					if stage == "typed_nil" {
						var absent *fixtureSampleOwner
						return absent, fault
					}
					if stage == "open" {
						return owner, fault
					}
					return owner, nil
				},
				run: func(context.Context, storagetest.Target, string, time.Duration) (workerexec.Result, error) {
					runs++
					return workerexec.Result{Stdout: uncheckedSampleWorkerBytes(t, reply)}, nil
				},
			}
			out, err := parentSampleTarget(context.Background(), budget, reply.Target, reply.SnapshotSHA256, operations)
			wantCloses, wantRuns := 1, 1
			if stage == "typed_nil" {
				wantCloses = 0
			}
			if stage == "open" || stage == "initial" || stage == "typed_nil" {
				wantRuns = 0
			}
			if err != nil || out.Device.Accepted || owner.closes != wantCloses || runs != wantRuns {
				t.Fatal("owner failure continued or misclosed")
			}
			if stage == "close" && (out.Error != "cleanup_unconfirmed" || out.CleanupConfirmed || out.Device.CloseConfirmed) {
				t.Fatal("close uncertainty lost")
			}
		})
	}
}

func TestStorageSampleParentRejectsChildAndLifecycleFaults(t *testing.T) {
	for _, stage := range []string{"nonzero", "protocol", "stdout_limit", "stderr_limit", "stderr", "cleanup", "child_close", "child_close_nonzero", "child_close_output", "child_close_runner", "run_late", "final_late", "close_late", "target_late", "backwards"} {
		t.Run(stage, func(t *testing.T) {
			budget, current, reply := fixtureSampleParent(t, 30*time.Second)
			owner := &fixtureSampleOwner{
				check: func(n int) error {
					if n == 2 && stage == "final_late" {
						*current = current.Add(31 * time.Second)
					}
					return nil
				},
				close: func() error {
					if stage == "close_late" {
						*current = current.Add(31 * time.Second)
					}
					return nil
				},
			}
			operations := sampleParentOperations{
				open: func(context.Context, storagetest.Target) (sampleTargetOwner, error) { return owner, nil },
				run: func(context.Context, storagetest.Target, string, time.Duration) (workerexec.Result, error) {
					if stage == "child_close" || stage == "child_close_nonzero" || stage == "child_close_output" || stage == "child_close_runner" {
						reply.CloseConfirmed = false
						reply.Error = "close_error"
					}
					value := workerexec.Result{Stdout: uncheckedSampleWorkerBytes(t, reply)}
					switch stage {
					case "nonzero", "child_close_nonzero":
						value.ExitCode = 1
					case "protocol":
						value.Stdout = append(value.Stdout, 'x')
					case "stdout_limit", "child_close_output":
						value.StdoutTruncated = true
					case "child_close_runner":
						return value, errors.New("fixture runner error")
					case "stderr_limit":
						value.StderrTruncated = true
					case "stderr":
						value.Stderr = []byte("fixture")
					case "cleanup":
						return value, errors.Join(context.Canceled, workerexec.ErrCleanupUncertain)
					case "run_late":
						*current = current.Add(31 * time.Second)
					case "target_late":
						*current = current.Add(11 * time.Second)
					case "backwards":
						*current = current.Add(-time.Nanosecond)
					}
					return value, nil
				},
			}
			out, err := parentSampleTarget(context.Background(), budget, reply.Target, reply.SnapshotSHA256, operations)
			if err != nil || out.Device.Accepted || out.Error == "none" || owner.closes != 1 {
				t.Fatalf("child/lifecycle fault accepted: %+v,%v", out, err)
			}
			if (stage == "cleanup" || stage == "child_close" || stage == "child_close_nonzero" || stage == "child_close_output" || stage == "child_close_runner") && (out.CleanupConfirmed || out.Device.CloseConfirmed || out.Error != "cleanup_unconfirmed") {
				t.Fatal("child cleanup uncertainty was overwritten by parent closure")
			}
			if stage == "backwards" && out.Error != "clock_invalid" {
				t.Fatal("clock reversal lost")
			}
		})
	}
}

func TestStorageSampleParentRetainsHardwareFailureAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	budget, _, reply := fixtureSampleParent(t, 30*time.Second)
	reply.Observation.Status, reply.Observation.Error = "fail", "mismatch"
	reply.Observation.Ranges[0].Outcome = "mismatch"
	reply.Observation.ComparedBytes, reply.Observation.MatchedRanges = 0, 0
	owner := &fixtureSampleOwner{}
	operations := sampleParentOperations{
		open: func(context.Context, storagetest.Target) (sampleTargetOwner, error) { return owner, nil },
		run: func(context.Context, storagetest.Target, string, time.Duration) (workerexec.Result, error) {
			cancel()
			return workerexec.Result{Stdout: uncheckedSampleWorkerBytes(t, reply)}, nil
		},
	}
	out, err := parentSampleTarget(ctx, budget, reply.Target, reply.SnapshotSHA256, operations)
	if err != nil || out.Device.Accepted || out.Error != "cancelled" || out.Device.Status != "fail" || out.Device.Error != "mismatch" || !out.Summary.FailureObserved || out.Summary.ReadBytes != 1024 || owner.closes != 1 {
		t.Fatal("later cancellation erased the diagnostic or granted acceptance")
	}
}

func TestStorageSampleParentRunBudgetNeverRestarts(t *testing.T) {
	budget, current, reply := fixtureSampleParent(t, 6*time.Second)
	*current = current.Add(4 * time.Second)
	remaining := []time.Duration{}
	owners := []*fixtureSampleOwner{}
	operations := sampleParentOperations{
		open: func(context.Context, storagetest.Target) (sampleTargetOwner, error) {
			owner := &fixtureSampleOwner{}
			owners = append(owners, owner)
			return owner, nil
		},
		run: func(_ context.Context, _ storagetest.Target, _ string, left time.Duration) (workerexec.Result, error) {
			remaining = append(remaining, left)
			*current = current.Add(time.Second)
			return workerexec.Result{Stdout: uncheckedSampleWorkerBytes(t, reply)}, nil
		},
	}
	first, err := parentSampleTarget(context.Background(), budget, reply.Target, reply.SnapshotSHA256, operations)
	if err != nil || !first.Device.Accepted {
		t.Fatal("within-budget first target rejected")
	}
	second, err := parentSampleTarget(context.Background(), budget, reply.Target, reply.SnapshotSHA256, operations)
	if err != nil || second.Device.Accepted || second.Error != "deadline" {
		t.Fatal("second target received a restarted run deadline")
	}
	third, err := parentSampleTarget(context.Background(), budget, reply.Target, reply.SnapshotSHA256, operations)
	if err != nil || third.Device.Status != "not_started" || third.Error != "deadline" || len(owners) != 2 || !reflect.DeepEqual(remaining, []time.Duration{2 * time.Second, time.Second}) {
		t.Fatal("expired run opened or spawned another target")
	}
	for _, owner := range owners {
		if owner.closes != 1 {
			t.Fatal("owner not closed once")
		}
	}
}

func TestStorageSampleParentRejectsInvalidBeforeAcquisition(t *testing.T) {
	for _, test := range []struct {
		name  string
		limit time.Duration
	}{{"zero", 0}, {"fractional", time.Millisecond + time.Nanosecond}, {"too_long", 180*time.Second + time.Millisecond}} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := newSampleBudget(test.limit, time.Now); !errors.Is(err, errSampleProtocol) {
				t.Fatal("invalid run budget admitted")
			}
		})
	}
	t.Run("zero_clock", func(t *testing.T) {
		if _, err := newSampleBudget(time.Second, func() time.Time { return time.Time{} }); !errors.Is(err, errSampleProtocol) {
			t.Fatal("zero clock admitted")
		}
	})
	for _, stage := range []string{"nil_context", "invalid_target", "invalid_snapshot", "cancelled", "expired"} {
		t.Run(stage, func(t *testing.T) {
			budget, current, reply := fixtureSampleParent(t, time.Second)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var input context.Context = ctx
			if stage == "nil_context" {
				input = nil
			}
			if stage == "invalid_target" {
				reply.Target.FixtureMask = 1
			}
			if stage == "invalid_snapshot" {
				reply.SnapshotSHA256 = "invalid"
			}
			if stage == "cancelled" {
				cancel()
			}
			if stage == "expired" {
				*current = current.Add(time.Second)
			}
			calls := 0
			operations := sampleParentOperations{
				open: func(context.Context, storagetest.Target) (sampleTargetOwner, error) {
					calls++
					return &fixtureSampleOwner{}, nil
				},
				run: func(context.Context, storagetest.Target, string, time.Duration) (workerexec.Result, error) {
					calls++
					return workerexec.Result{}, nil
				},
			}
			out, err := parentSampleTarget(input, budget, reply.Target, reply.SnapshotSHA256, operations)
			if calls != 0 || out.Device.Accepted {
				t.Fatal("invalid/expired admission reached acquisition")
			}
			if stage == "nil_context" || stage == "invalid_target" || stage == "invalid_snapshot" {
				if !errors.Is(err, errSampleProtocol) {
					t.Fatal("invalid configuration not rejected")
				}
			} else if err != nil || out.Device.Status != "not_started" {
				t.Fatal("stopped target disposition changed")
			}
		})
	}
}

func TestStorageSampleParentCopiesTargetAtCallbackBoundaries(t *testing.T) {
	for _, stage := range []string{"caller_open", "open_argument", "owner_initial", "run_argument", "owner_final"} {
		t.Run(stage, func(t *testing.T) {
			budget, _, original := fixtureSampleParent(t, 30*time.Second)
			expected := fixtureSampleWorkerReply()
			caller := original.Target
			var openArgument storagetest.Target
			mutated := false
			mutate := func(target *storagetest.Target) {
				mutated = true
				target.Ranges[0].Offset, target.Ranges[0].Length = 1, 1
				target.FixtureMask = 1
				target.Exclusions = append(target.Exclusions, "mounted")
			}
			owner := &fixtureSampleOwner{check: func(n int) error {
				if n == 1 && stage == "owner_initial" || n == 2 && stage == "owner_final" {
					mutate(&openArgument)
				}
				return nil
			}}
			runs := 0
			operations := sampleParentOperations{
				open: func(_ context.Context, target storagetest.Target) (sampleTargetOwner, error) {
					if !reflect.DeepEqual(target, expected.Target) {
						t.Fatal("acquisition target changed before callback")
					}
					openArgument = target
					if stage == "caller_open" {
						mutate(&caller)
					}
					if stage == "open_argument" {
						mutate(&target)
					}
					return owner, nil
				},
				run: func(_ context.Context, target storagetest.Target, digest string, remaining time.Duration) (workerexec.Result, error) {
					runs++
					if !reflect.DeepEqual(target, expected.Target) || digest != expected.SnapshotSHA256 || remaining != 10*time.Second {
						t.Fatal("prior callback changed worker authority")
					}
					if stage == "run_argument" {
						mutate(&target)
					}
					return workerexec.Result{Stdout: uncheckedSampleWorkerBytes(t, expected)}, nil
				},
			}
			out, err := parentSampleTarget(context.Background(), budget, caller, original.SnapshotSHA256, operations)
			if err != nil || !mutated || runs != 1 || owner.closes != 1 || !out.Device.Accepted || !out.CleanupConfirmed || !reflect.DeepEqual(out.Device.Observation, expected.Observation) {
				t.Fatalf("callback changed canonical parent authority: %+v, %v", out, err)
			}
		})
	}
}
