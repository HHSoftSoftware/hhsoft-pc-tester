package quick

import (
	"context"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

const memoryStressAvailabilityLimit = 65536

type memoryStressAvailabilityCompletion struct {
	evidence  MemoryStressAvailability
	available uint64
	finished  time.Time
}

func parseMemoryStressAvailable(data []byte) (uint64, bool) {
	if len(data) == 0 || len(data) > memoryStressAvailabilityLimit {
		return 0, false
	}
	for _, b := range data {
		if b != '\n' && b != '\t' && (b < 32 || b > 126) {
			return 0, false
		}
	}
	count, value := 0, ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, tail, ok := strings.Cut(line, ":")
		if !ok {
			return 0, false
		}
		name = strings.TrimSpace(name)
		if strings.EqualFold(name, "MemAvailable") {
			if name != "MemAvailable" {
				return 0, false
			}
			count++
			value = tail
		}
	}
	fields := strings.Fields(value)
	if count != 1 || len(fields) != 2 || fields[1] != "kB" || len(fields[0]) == 0 {
		return 0, false
	}
	for _, digit := range fields[0] {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil || n == 0 || n > math.MaxUint64/1024 {
		return 0, false
	}
	return n * 1024, true
}

// The sole owner closes acquired handles, even after the caller has stopped.
// A completion is immutable and has one bounded destination; no late owner can
// rewrite a returned diagnostic. Cancellation does not preempt kernel reads.
func readMemoryStressAvailability(ctx context.Context, open func() (io.ReadCloser, error), now func() time.Time) (out memoryStressAvailabilityCompletion) {
	out.evidence = MemoryStressAvailability{Started: true, CloseConfirmed: true}
	var handle io.ReadCloser
	defer func() {
		if recover() != nil {
			out.evidence.Failure = "reader_panic"
			out.evidence.Valid = false
			out.available = 0
			if handle == nil {
				out.evidence.CloseConfirmed = false
			}
		}
		if handle != nil {
			out.evidence.CloseConfirmed = false
			func() {
				defer func() {
					if recover() != nil {
						if out.evidence.Failure == "" {
							out.evidence.Failure = "close_failed"
						}
					}
				}()
				if handle.Close() == nil {
					out.evidence.CloseConfirmed = true
				} else {
					if out.evidence.Failure == "" {
						out.evidence.Failure = "close_failed"
					}
				}
			}()
		}
		if !out.evidence.CloseConfirmed {
			out.evidence.Valid = false
			out.available = 0
		}
		out.finished = now()
	}()
	if ctx.Err() != nil {
		out.evidence.Failure = "stopped"
		return out
	}
	var err error
	handle, err = open()
	out.evidence.Opened = handle != nil
	if err != nil || handle == nil {
		out.evidence.Failure = "open_failed"
		return out
	}
	data := make([]byte, memoryStressAvailabilityLimit)
	used := 0
	var extra [1]byte
	for {
		if ctx.Err() != nil {
			out.evidence.Failure = "stopped"
			return out
		}
		if out.evidence.ReadCalls >= memoryStressAvailabilityLimit+1 {
			out.evidence.Failure = "read_failed"
			return out
		}
		chunk := extra[:]
		if used < len(data) {
			chunk = data[used:min(used+4096, len(data))]
		}
		out.evidence.ReadCalls++
		n, readErr := handle.Read(chunk)
		if n < 0 || n > len(chunk) {
			out.evidence.Failure = "read_failed"
			return out
		}
		if used == len(data) && n != 0 {
			out.evidence.RawBytes = uint64(used + n)
			out.evidence.Failure = "too_large"
			return out
		}
		used += n
		out.evidence.RawBytes = uint64(used)
		if readErr != nil && readErr != io.EOF {
			out.evidence.Failure = "read_failed"
			return out
		}
		if readErr == io.EOF {
			out.evidence.EOF = true
			break
		}
		if n == 0 {
			out.evidence.Failure = "read_failed"
			return out
		}
	}
	available, ok := parseMemoryStressAvailable(data[:used])
	if !ok {
		out.evidence.Failure = "invalid"
		return out
	}
	out.available, out.evidence.Valid = available, true
	return out
}

func acquireMemoryStressAvailability(ctx context.Context, open func() (io.ReadCloser, error), clock *cpuStressClock) (memoryStressAvailabilityCompletion, bool) {
	completed := make(chan memoryStressAvailabilityCompletion, 1)
	go func() { completed <- readMemoryStressAvailability(ctx, open, clock.now) }()
	select {
	case result := <-completed:
		return result, true
	case <-ctx.Done():
		// A queued completion is collected; current stop facts and its timestamp
		// are still checked before allocation.
		select {
		case result := <-completed:
			return result, true
		default:
			return memoryStressAvailabilityCompletion{evidence: MemoryStressAvailability{Started: true, Failure: "stopped"}}, false
		}
	}
}
