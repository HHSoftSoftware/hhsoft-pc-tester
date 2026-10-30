package storagesample

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
)

var errSampleProtocol = errors.New("storage sample accounting is invalid")

type sampleReader interface {
	ReadAt([]byte, int64) (int, error)
}

func nilSampleValue(value any) bool {
	if value == nil {
		return true
	}
	switch reflect.ValueOf(value).Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflect.ValueOf(value).IsNil()
	default:
		return false
	}
}

// sampleReads consumes an already admitted, owned target. It never opens or
// closes a device and grants no lifecycle credit. Its caller retains the owner,
// the original run deadline and final binding/closure responsibilities.
func sampleReads(ctx context.Context, target storagetest.Target, reader sampleReader, stop func() string) (storagetest.SampleObservation, error) {
	if nilSampleValue(ctx) || nilSampleValue(reader) || stop == nil {
		return storagetest.SampleObservation{}, errSampleProtocol
	}
	value, err := prepareSampleObservation(target)
	if err != nil {
		return storagetest.SampleObservation{}, err
	}
	// No callback can change the admitted offsets or lengths.
	target = copyAdmittedSampleTarget(target)
	first, second := make([]byte, storagetest.MaximumSampleBytes), make([]byte, storagetest.MaximumSampleBytes)
	defer clear(first)
	defer clear(second)
	matched := []storagetest.Range{}
	finish := func() (storagetest.SampleObservation, error) {
		coverage, err := storagetest.RangeCoverage(matched)
		if err != nil {
			return storagetest.SampleObservation{}, errSampleProtocol
		}
		value.ComparedBytes = uint64(coverage)
		if _, err := storagetest.ValidateObservation(target, value); err != nil {
			return storagetest.SampleObservation{}, errSampleProtocol
		}
		return value, nil
	}
	stopped := func() (bool, error) {
		reason := ""
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				reason = "deadline"
			} else {
				reason = "cancelled"
			}
		} else {
			reason = stop()
			if reason == "" && ctx.Err() != nil {
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					reason = "deadline"
				} else {
					reason = "cancelled"
				}
			}
		}
		switch reason {
		case "":
			return false, nil
		case "cancelled":
			value.Status, value.Error = "aborted", reason
		case "deadline", "clock_invalid":
			value.Status, value.Error = "error", reason
		default:
			return false, errSampleProtocol
		}
		return true, nil
	}
	for index, planned := range target.Ranges {
		row := &value.Ranges[index]
		if halted, err := stopped(); err != nil {
			return storagetest.SampleObservation{}, err
		} else if halted {
			return finish()
		}
		for readIndex, buffer := range [][]byte{first[:planned.Length], second[:planned.Length]} {
			n, readErr := reader.ReadAt(buffer, planned.Offset)
			if n < 0 || n > len(buffer) {
				// An impossible count cannot be represented as confirmed bytes.
				// Reject the observation rather than fabricating a read ledger.
				return storagetest.SampleObservation{}, errSampleProtocol
			}
			row.ReadCount++
			if readIndex == 0 {
				row.FirstBytes = uint32(n)
			} else {
				row.SecondBytes = uint32(n)
			}
			value.ReadBytes += uint64(n)
			if readErr != nil && !(errors.Is(readErr, io.EOF) && n < len(buffer)) {
				row.Outcome = "read_error"
				value.Status, value.Error = "fail", "read_error"
				return finish()
			}
			if n != len(buffer) {
				row.Outcome = "short_read"
				value.Status, value.Error = "fail", "short_read"
				return finish()
			}
			// A known read fault above outranks cancellation accompanying the
			// return. Successful full bytes are not compared after a stop.
			if halted, err := stopped(); err != nil {
				return storagetest.SampleObservation{}, err
			} else if halted {
				row.Outcome = "interrupted"
				return finish()
			}
		}
		if !bytes.Equal(first[:planned.Length], second[:planned.Length]) {
			row.Outcome = "mismatch"
			value.Status, value.Error = "fail", "mismatch"
			return finish()
		}
		row.Outcome = "matched"
		value.MatchedRanges++
		matched = append(matched, planned)
	}
	value.Status, value.Error = "pass", "none"
	return finish()
}
