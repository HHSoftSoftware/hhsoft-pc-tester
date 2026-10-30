package inputtest

import (
	"strconv"
	"testing"
	"time"
)

func TestKeyboardReportValuesPreserveBaselineAndActiveFrames(t *testing.T) {
	for _, size := range []int{16, 24} {
		for _, value := range []int32{0, 1, -1, -2147483648, 2147483647} {
			t.Run(strconv.Itoa(size)+"_"+strconv.FormatInt(int64(value), 10), func(t *testing.T) {
				events := keyboardCompleteEvents()
				for index := range events {
					if events[index].typeCode == 0 && events[index].code == 0 {
						events[index].value = value
					}
				}
				source := completeKeyboardSource(size)
				source.stale = keyboardFixtureBytes(size, events)
				source.data = keyboardFixtureBytes(size, events)
				result := runKeyboardWith(t.Context(), KeyboardOptions{Duration: time.Second}, keyboardFixtureBackend(source))
				if result.Status != StatusPass || !result.CleanupConfirmed || source.closes != 1 {
					t.Fatal("an undefined report value changed baseline or active framing")
				}
				device := result.Devices[0]
				if !device.BaselineNeutral || !device.FinalNeutral || !device.CleanupConfirmed {
					t.Fatal("report-value compatibility bypassed a lifetime boundary")
				}
				for _, action := range device.Actions {
					if !action.PressObserved || !action.ReleaseObserved || !action.PairCompleted {
						t.Fatal("fresh transitions did not supply each fixed control")
					}
				}
			})
		}
	}
}

func TestKeyboardReportValuesAloneNeverSupplyPairs(t *testing.T) {
	for _, value := range []int32{0, 1, -1, -2147483648, 2147483647} {
		t.Run(strconv.FormatInt(int64(value), 10), func(t *testing.T) {
			state, result := keyboardStateFixture()
			for count := 0; count < 32; count++ {
				state.consume(keyboardEvent{0, 0, value})
			}
			if state.failed || state.complete() || state.frameEvents != 0 {
				t.Fatal("empty report values supplied completion or invalid framing")
			}
			for _, action := range result.Actions {
				if action.PressObserved || action.ReleaseObserved || action.PairCompleted {
					t.Fatal("report payload became action evidence")
				}
			}
		})
	}
}

func TestKeyboardReportValuesKeepBaselineLossAndUnknownSyncFatal(t *testing.T) {
	for _, value := range []int32{0, 1, -1, -2147483648, 2147483647} {
		for _, code := range []uint16{2, 3} {
			t.Run(strconv.Itoa(int(code))+"_"+strconv.FormatInt(int64(value), 10), func(t *testing.T) {
				source := completeKeyboardSource(24)
				source.stale = keyboardFixtureBytes(24, []keyboardEvent{{0, code, value}, {0, 0, value}})
				result := runKeyboardWith(t.Context(), KeyboardOptions{Duration: time.Second}, keyboardFixtureBackend(source))
				if result.Status != StatusError || source.closes != 1 || source.neutrals != 0 || result.Devices[0].BaselineNeutral || result.Devices[0].EvidenceLoss != (code == 3) {
					t.Fatal("report-value acceptance admitted loss or an unknown synchronization code")
				}
			})
		}
	}
}

func TestKeyboardReportValuesKeepActiveFaultsLatched(t *testing.T) {
	for _, value := range []int32{0, 1, -1, -2147483648, 2147483647} {
		for _, condition := range []string{"loss", "unknown sync", "two transitions"} {
			t.Run(condition+"_"+strconv.FormatInt(int64(value), 10), func(t *testing.T) {
				state, result := keyboardStateFixture()
				switch condition {
				case "loss":
					state.consume(keyboardEvent{0, 3, value})
				case "unknown sync":
					state.consume(keyboardEvent{0, 2, value})
				case "two transitions":
					state.consume(keyboardEvent{1, 42, 1})
					state.consume(keyboardEvent{1, 42, 0})
				}
				state.consume(keyboardEvent{0, 0, value})
				for _, event := range keyboardCompleteEvents() {
					state.consume(event)
				}
				if !state.failed || state.complete() || result.FinalNeutral || result.EvidenceLoss != (condition == "loss") {
					t.Fatal("later reports recovered a malformed or lost observation")
				}
			})
		}
	}
}

func TestKeyboardReportValuesDoNotFinishUnreportedTransitions(t *testing.T) {
	for _, value := range []int32{0, 1, -1, -2147483648, 2147483647} {
		t.Run(strconv.FormatInt(int64(value), 10), func(t *testing.T) {
			state, result := keyboardStateFixture()
			state.consume(keyboardEvent{1, 42, 1})
			if result.Actions[0].PressObserved || state.complete() {
				t.Fatal("an unreported press supplied evidence")
			}
			state.consume(keyboardEvent{0, 0, value})
			state.consume(keyboardEvent{1, 42, 0})
			if !result.Actions[0].PressObserved || result.Actions[0].PairCompleted || state.complete() {
				t.Fatal("an unreported release completed a pair")
			}
			state.consume(keyboardEvent{0, 0, value})
			if state.failed || !result.Actions[0].PairCompleted || state.complete() {
				t.Fatal("a report supplied another control or failed to commit its own pair")
			}
		})
	}
}
