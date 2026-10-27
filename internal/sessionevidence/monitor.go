package sessionevidence

import (
	"errors"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
)

func ValidateMonitor(result safemode.MonitorResult, elapsed int64) error {
	interval := safemode.MonitorInterval.Milliseconds()
	if result.Schema != safemode.MonitorSchema || result.Status != "pass" ||
		result.IntervalMilliseconds != interval || result.ObservationTimeoutMilliseconds != safemode.MonitorObservationTimeout.Milliseconds() ||
		result.MaximumAllowedGapMilliseconds != safemode.MonitorMaximumGap.Milliseconds() ||
		elapsed < 0 || elapsed > 60*60*1000 || result.ElapsedMilliseconds != elapsed ||
		!result.CleanupConfirmed || result.FirstFailure != nil || result.AttemptedObservations != result.CompletedObservations ||
		result.CompletedObservations > uint64(elapsed/interval) || result.MaximumGapMilliseconds < 0 ||
		result.MaximumGapMilliseconds > result.MaximumAllowedGapMilliseconds || result.MaximumGapMilliseconds > elapsed {
		return errors.New("session monitoring was unsafe, incomplete, or inconsistent")
	}
	count := result.CompletedObservations
	first, last, gap := result.FirstCompletedMilliseconds, result.LastCompletedMilliseconds, result.MaximumGapMilliseconds
	if count == 0 {
		if elapsed >= interval || first != 0 || last != 0 || gap != elapsed || result.LastObservation != nil {
			return errors.New("session monitoring omitted required observations")
		}
		return nil
	}
	if first < interval || last < first || last > elapsed || result.LastObservation == nil || !safe(*result.LastObservation) {
		return errors.New("session monitoring has invalid observation coverage")
	}
	leadingAndTrailing := max(first, elapsed-last)
	if count == 1 {
		if first != last || gap != leadingAndTrailing {
			return errors.New("session monitoring has inconsistent single-sample coverage")
		}
		return nil
	}
	// Counts were bounded above before conversion or arithmetic. Completion
	// timestamps may share a millisecond when a queued tick finishes quickly.
	span, intervals := last-first, int64(count-1)
	minimumGap := max(leadingAndTrailing, (span+intervals-1)/intervals)
	maximumGap := max(leadingAndTrailing, span)
	if last < int64(count)*interval || gap < minimumGap || gap > maximumGap {
		return errors.New("session monitoring count and coverage cannot agree")
	}
	return nil
}
