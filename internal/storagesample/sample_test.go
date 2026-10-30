package storagesample

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
)

type sampleCall struct {
	offset int64
	length int
}

type fixtureSampleReader struct {
	calls    []sampleCall
	buffers  [][]byte
	returned func(int, []byte) (int, error)
}

func (reader *fixtureSampleReader) ReadAt(buffer []byte, offset int64) (int, error) {
	reader.calls = append(reader.calls, sampleCall{offset, len(buffer)})
	reader.buffers = append(reader.buffers, buffer)
	for index := range buffer {
		buffer[index] = byte((offset + int64(index)) % 251)
	}
	if reader.returned != nil {
		return reader.returned(len(reader.calls), buffer)
	}
	return len(buffer), nil
}

func fixtureSampleTarget(capacity uint64, offsets ...int64) storagetest.Target {
	ranges := []storagetest.Range{}
	width := 65536
	if capacity < uint64(width) {
		width = int(capacity)
	}
	for _, offset := range offsets {
		ranges = append(ranges, storagetest.Range{Offset: offset, Length: width})
	}
	return storagetest.Target{
		ID: "block:fixture0", Number: "8:0", CapacityBytes: capacity,
		LogicalBlockBytes: 512, Exclusions: []string{}, Ranges: ranges,
	}
}

func assertSampleBuffersCleared(t *testing.T, reader *fixtureSampleReader) {
	t.Helper()
	for _, buffer := range reader.buffers {
		for _, value := range buffer {
			if value != 0 {
				t.Fatal("sample contents retained in a read buffer")
			}
		}
	}
}

func TestStorageSampleIndependentSchedules(t *testing.T) {
	for _, test := range []struct {
		name                     string
		capacity                 uint64
		offsets                  []int64
		readBytes, comparedBytes uint64
	}{
		{"one", 65536, []int64{0}, 131072, 65536},
		{"two", 66048, []int64{0, 512}, 262144, 66048},
		{"three", 262144, []int64{0, 98304, 196608}, 393216, 196608},
		{"overlap", 131072, []int64{0, 32768, 65536}, 393216, 131072},
		{"small", 512, []int64{0}, 1024, 512},
		{"eight_gib", 8589934592, []int64{0, 4294934528, 8589869056}, 393216, 196608},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := fixtureSampleTarget(test.capacity, test.offsets...)
			reader := &fixtureSampleReader{}
			value, err := sampleReads(context.Background(), target, reader, func() string { return "" })
			if err != nil || value.Status != "pass" || value.Error != "none" || value.ReadBytes != test.readBytes || value.ComparedBytes != test.comparedBytes || int(value.MatchedRanges) != len(test.offsets) {
				t.Fatalf("unexpected finite accounting: %+v, %v", value, err)
			}
			expected := []sampleCall{}
			for _, planned := range target.Ranges {
				expected = append(expected, sampleCall{planned.Offset, planned.Length}, sampleCall{planned.Offset, planned.Length})
			}
			if !reflect.DeepEqual(reader.calls, expected) {
				t.Fatalf("read schedule changed: %+v", reader.calls)
			}
			for _, row := range value.Ranges {
				if row.ReadCount != 2 || row.FirstBytes != row.Length || row.SecondBytes != row.Length || row.Outcome != "matched" {
					t.Fatalf("incomplete repeated read: %+v", row)
				}
			}
			assertSampleBuffersCleared(t, reader)
		})
	}
}

func TestStorageSampleFaultPrecedenceAndNoRetry(t *testing.T) {
	fault := errors.New("fixture read failure")
	for _, test := range []struct {
		name                     string
		call, count              int
		readErr                  error
		cancel                   bool
		corrupt                  bool
		outcome                  string
		readBytes, comparedBytes uint64
		matched                  uint8
	}{
		{"zero_nil", 1, 0, nil, false, false, "short_read", 0, 0, 0},
		{"short_nil", 1, 65535, nil, false, false, "short_read", 65535, 0, 0},
		{"short_eof", 1, 65535, io.EOF, false, false, "short_read", 65535, 0, 0},
		{"short_error", 1, 5, fault, false, false, "read_error", 5, 0, 0},
		{"full_error", 1, 65536, fault, false, false, "read_error", 65536, 0, 0},
		{"full_eof", 1, 65536, io.EOF, false, false, "read_error", 65536, 0, 0},
		{"second_short", 2, 5, nil, false, false, "short_read", 65541, 0, 0},
		{"second_full_error", 2, 65536, fault, false, false, "read_error", 131072, 0, 0},
		{"first_changed", 1, 65536, nil, false, true, "mismatch", 131072, 0, 0},
		{"second_changed", 2, 65536, nil, false, true, "mismatch", 131072, 0, 0},
		{"later_error", 3, 0, fault, false, false, "read_error", 131072, 65536, 1},
		{"short_and_cancel", 1, 3, nil, true, false, "short_read", 3, 0, 0},
		{"error_and_cancel", 1, 65536, fault, true, false, "read_error", 65536, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reader := &fixtureSampleReader{returned: func(call int, buffer []byte) (int, error) {
				if call != test.call {
					return len(buffer), nil
				}
				if test.corrupt {
					buffer[0] ^= 0xff
				}
				if test.cancel {
					cancel()
				}
				return test.count, test.readErr
			}}
			value, err := sampleReads(ctx, fixtureSampleTarget(262144, 0, 98304, 196608), reader, func() string { return "" })
			expectedCalls := test.call
			if test.corrupt {
				expectedCalls = 2
			}
			faultRange := (expectedCalls - 1) / 2
			if err != nil || value.Status != "fail" || value.Error != test.outcome || len(reader.calls) != expectedCalls || value.Ranges[faultRange].Outcome != test.outcome || value.ReadBytes != test.readBytes || value.ComparedBytes != test.comparedBytes || value.MatchedRanges != test.matched {
				t.Fatalf("fault lost, retried or miscounted: %+v, calls=%d, err=%v", value, len(reader.calls), err)
			}
			for _, row := range value.Ranges[faultRange+1:] {
				if row.ReadCount != 0 || row.Outcome != "not_started" {
					t.Fatal("read continued after a fault")
				}
			}
			assertSampleBuffersCleared(t, reader)
		})
	}
}

func TestStorageSampleSuccessfulReturnCancellation(t *testing.T) {
	for _, test := range []struct {
		name  string
		after int
	}{{"first", 1}, {"second", 2}} {
		t.Run(test.name, func(t *testing.T) {
			after := test.after
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reader := &fixtureSampleReader{returned: func(call int, buffer []byte) (int, error) {
				if call == after {
					cancel()
				}
				return len(buffer), nil
			}}
			value, err := sampleReads(ctx, fixtureSampleTarget(262144, 0, 98304, 196608), reader, func() string { return "" })
			if err != nil || value.Status != "aborted" || value.Error != "cancelled" || len(reader.calls) != after || value.Ranges[0].Outcome != "interrupted" || int(value.Ranges[0].ReadCount) != after || value.ReadBytes != uint64(after*65536) || value.ComparedBytes != 0 || value.MatchedRanges != 0 {
				t.Fatalf("full cancelled return was accepted: %+v, %v", value, err)
			}
			assertSampleBuffersCleared(t, reader)
		})
	}
	t.Run("stop_callback_cancels", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		reader := &fixtureSampleReader{}
		value, err := sampleReads(ctx, fixtureSampleTarget(65536, 0), reader, func() string { cancel(); return "" })
		if err != nil || value.Status != "aborted" || value.Error != "cancelled" || len(reader.calls) != 0 {
			t.Fatal("successful stop callback erased cancellation")
		}
	})
}

func TestStorageSampleStopsAndPreservedComparisons(t *testing.T) {
	for _, test := range []struct {
		name, reason string
		call         int
		matched      uint8
		reads        uint64
	}{
		{"before", "deadline", 1, 0, 0},
		{"after_matched_range", "deadline", 4, 1, 131072},
		{"clock", "clock_invalid", 1, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			checks := 0
			reader := &fixtureSampleReader{}
			value, err := sampleReads(context.Background(), fixtureSampleTarget(262144, 0, 98304, 196608), reader, func() string {
				checks++
				if checks == test.call {
					return test.reason
				}
				return ""
			})
			if err != nil || value.Status != "error" || value.Error != test.reason || value.MatchedRanges != test.matched || value.ReadBytes != test.reads || value.ComparedBytes != uint64(test.matched)*65536 {
				t.Fatalf("stop accounting changed: %+v, %v", value, err)
			}
			if len(reader.calls) != int(test.matched)*2 {
				t.Fatal("read continued after stop")
			}
			assertSampleBuffersCleared(t, reader)
		})
	}
}

func TestStorageSampleRejectsInvalidAuthorityBeforeRead(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*storagetest.Target)
	}{
		{"fixture_mask", func(v *storagetest.Target) { v.FixtureMask = 1 }},
		{"exclusion", func(v *storagetest.Target) { v.Exclusions = []string{"mounted"} }},
		{"nil_exclusions", func(v *storagetest.Target) { v.Exclusions = nil }},
		{"noncanonical_number", func(v *storagetest.Target) { v.Number = "08:0" }},
		{"invalid_id", func(v *storagetest.Target) { v.ID = "block:../fixture" }},
		{"wrong_offset", func(v *storagetest.Target) { v.Ranges[0].Offset = 512 }},
		{"wrong_length", func(v *storagetest.Target) { v.Ranges[0].Length = 65535 }},
		{"extra_window", func(v *storagetest.Target) { v.Ranges = append(v.Ranges, v.Ranges[0]) }},
		{"capacity_overflow", func(v *storagetest.Target) { v.CapacityBytes = ^uint64(0) }},
		{"block_geometry", func(v *storagetest.Target) { v.LogicalBlockBytes = 513 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := fixtureSampleTarget(65536, 0)
			test.mutate(&target)
			reader := &fixtureSampleReader{}
			_, err := sampleReads(context.Background(), target, reader, func() string { return "" })
			if !errors.Is(err, errSampleProtocol) || len(reader.calls) != 0 {
				t.Fatal("invalid target reached media reader")
			}
		})
	}
	t.Run("nil_context", func(t *testing.T) {
		reader := &fixtureSampleReader{}
		_, err := sampleReads(nil, fixtureSampleTarget(65536, 0), reader, func() string { return "" })
		if !errors.Is(err, errSampleProtocol) || len(reader.calls) != 0 {
			t.Fatal("nil context accepted")
		}
	})
	t.Run("typed_nil_reader", func(t *testing.T) {
		var reader *fixtureSampleReader
		_, err := sampleReads(context.Background(), fixtureSampleTarget(65536, 0), reader, func() string { return "" })
		if !errors.Is(err, errSampleProtocol) {
			t.Fatal("typed nil reader accepted")
		}
	})
	t.Run("nil_stop", func(t *testing.T) {
		reader := &fixtureSampleReader{}
		_, err := sampleReads(context.Background(), fixtureSampleTarget(65536, 0), reader, nil)
		if !errors.Is(err, errSampleProtocol) || len(reader.calls) != 0 {
			t.Fatal("nil stop accepted")
		}
	})
	t.Run("unknown_stop", func(t *testing.T) {
		reader := &fixtureSampleReader{}
		_, err := sampleReads(context.Background(), fixtureSampleTarget(65536, 0), reader, func() string { return "fixture_private_text" })
		if !errors.Is(err, errSampleProtocol) || len(reader.calls) != 0 {
			t.Fatal("unknown stop accepted")
		}
	})
}

func TestStorageSampleImpossibleCountsRejectLedger(t *testing.T) {
	for _, test := range []struct {
		name  string
		count int
	}{{"negative", -1}, {"oversized", 65537}} {
		t.Run(test.name, func(t *testing.T) {
			reader := &fixtureSampleReader{returned: func(_ int, _ []byte) (int, error) { return test.count, nil }}
			value, err := sampleReads(context.Background(), fixtureSampleTarget(262144, 0, 98304, 196608), reader, func() string { return "" })
			if !errors.Is(err, errSampleProtocol) || !reflect.DeepEqual(value, storagetest.SampleObservation{}) || len(reader.calls) != 1 {
				t.Fatal("impossible count became accepted accounting")
			}
			assertSampleBuffersCleared(t, reader)
		})
	}
}

func TestStorageSampleCallbacksCannotChangeAdmittedRanges(t *testing.T) {
	for _, stage := range []string{"read", "stop"} {
		for _, field := range []string{"offset", "length"} {
			t.Run(stage+"_"+field, func(t *testing.T) {
				target := fixtureSampleTarget(262144, 0, 98304, 196608)
				retained := target.Ranges
				mutated := false
				mutate := func() {
					mutated = true
					if field == "offset" {
						retained[1].Offset, retained[2].Offset = 1, -1
					} else {
						retained[1].Length, retained[2].Length = 65537, -1
					}
				}
				reader := &fixtureSampleReader{returned: func(call int, buffer []byte) (int, error) {
					if stage == "read" && call == 1 {
						mutate()
					}
					return len(buffer), nil
				}}
				value, err := sampleReads(context.Background(), target, reader, func() string {
					if stage == "stop" && !mutated {
						mutate()
					}
					return ""
				})
				expected := []sampleCall{{0, 65536}, {0, 65536}, {98304, 65536}, {98304, 65536}, {196608, 65536}, {196608, 65536}}
				if err != nil || !mutated || value.Status != "pass" || value.ReadBytes != 393216 || value.ComparedBytes != 196608 || !reflect.DeepEqual(reader.calls, expected) {
					t.Fatalf("callback changed admitted schedule: %+v, %v", reader.calls, err)
				}
				assertSampleBuffersCleared(t, reader)
			})
		}
	}
}
