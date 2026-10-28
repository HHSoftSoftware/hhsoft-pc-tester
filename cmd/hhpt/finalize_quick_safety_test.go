package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
)

func quickSafetyFixture(id string, status quick.Status, safety quick.SafetyEvidence) any {
	if id == "cpu.quick" {
		result := completeCPUEvidence()
		result.Status, result.Safety = status, safety
		if status == quick.StatusFail {
			setCPUFixtureFailure(&result)
		}
		if safety.ThermalObservations == 0 {
			result.Iterations, result.StateAggregate, result.DurationMilliseconds = 0, 0, 0
			result.Scalar.ChecksPassed, result.Scalar.WorkersCompleted = 0, 0
			result.Scalar.Workers = []quick.CPUScalarWorker{}
		}
		return result
	}
	result := completeMemoryEvidence()
	result.Status, result.Safety = status, safety
	if status == quick.StatusFail {
		setMemoryFixtureFailure(&result, 0)
	}
	if safety.ThermalObservations == 0 {
		result.AllocatedBytes, result.ExaminedBytes, result.DurationMilliseconds = 0, 0, 0
		result.Coverage = quick.MemoryCoverage{Suite: quick.MemorySuite}
	}
	return result
}

func TestQuickSafetyAcceptsHonestCoverageAndJoinedStops(t *testing.T) {
	base := completeCPUEvidence().Safety
	cases := map[string]struct {
		status quick.Status
		safety quick.SafetyEvidence
	}{}
	add := func(name string, status quick.Status, change func(*quick.SafetyEvidence)) {
		safety := base
		change(&safety)
		cases[name] = struct {
			status quick.Status
			safety quick.SafetyEvidence
		}{status, safety}
	}
	add("unavailable", quick.StatusPass, func(*quick.SafetyEvidence) {})
	add("disabled", quick.StatusPass, func(s *quick.SafetyEvidence) { s.ThermalUnavailableReadings = 2 })
	add("available-zero-celsius", quick.StatusPass, func(s *quick.SafetyEvidence) {
		s.ThermalCoverage = "available"
		s.ThermalSensors, s.ThermalSamples = 2, 2
	})
	add("available-changing-counts", quick.StatusPass, func(s *quick.SafetyEvidence) {
		s.ThermalCoverage = "available"
		s.ThermalObservations, s.ThermalSensors, s.ThermalSamples = 3, 2, 4
	})
	add("partial-without-valid", quick.StatusPass, func(s *quick.SafetyEvidence) { s.ThermalCoverage = "partial"; s.ThermalRejectedReadings = 1 })
	add("partial-enumeration", quick.StatusPass, func(s *quick.SafetyEvidence) { s.ThermalCoverage = "partial"; s.ThermalEnumerationErrors = 1 })
	add("partial-unavailable", quick.StatusPass, func(s *quick.SafetyEvidence) {
		s.ThermalCoverage = "partial"
		s.ThermalSensors, s.ThermalSamples, s.ThermalUnavailableReadings = 1, 1, 1
	})
	add("partial-mixed-empty", quick.StatusPass, func(s *quick.SafetyEvidence) {
		s.ThermalCoverage = "partial"
		s.ThermalObservations, s.ThermalSensors, s.ThermalSamples = 3, 2, 3
	})
	add("cancel-after-observation", quick.StatusAborted, func(*quick.SafetyEvidence) {})
	add("cancel-before-observation", quick.StatusAborted, func(s *quick.SafetyEvidence) { s.MonitorStatus, s.ThermalObservations = "not_started", 0 })
	add("insufficient-memory", quick.StatusInconclusive, func(s *quick.SafetyEvidence) { s.MonitorStatus, s.ThermalObservations = "not_started", 0 })
	add("invalid-options", quick.StatusError, func(s *quick.SafetyEvidence) {
		s.MonitorStatus, s.ThermalObservations = "not_started", 0
		s.GlobalTimeoutMilliseconds, s.ThermalLimitMilliCelsius = 0, 0
	})
	for _, reason := range []string{"observation_timeout", "observer_panic", "invalid_observation"} {
		add(reason, quick.StatusError, func(s *quick.SafetyEvidence) {
			s.MonitorStatus, s.MonitorFailure, s.ThermalObservations = "failed", reason, 0
		})
		add(reason+"-after-work", quick.StatusFail, func(s *quick.SafetyEvidence) { s.MonitorStatus, s.MonitorFailure = "failed", reason })
	}
	for _, peak := range []int{90000, 185000, 250000} {
		for _, status := range []quick.Status{quick.StatusAborted, quick.StatusFail} {
			add(string(status)+"-heat-"+strconv.Itoa(peak), status, func(s *quick.SafetyEvidence) {
				s.MonitorStatus, s.MonitorFailure, s.ThermalCoverage = "failed", "thermal_limit", "available"
				s.ThermalSensors, s.ThermalSamples, s.PeakMilliCelsius = 1, 1, peak
			})
		}
	}
	for name, item := range cases {
		for _, id := range []string{"cpu.quick", "memory.quick"} {
			t.Run(id+"/"+name, func(t *testing.T) {
				evidence := quickSafetyFixture(id, item.status, item.safety)
				if _, err := newValidatedQuickTestResult(id, currentQuickFixtureVersion(id), app.TestStatus(item.status), evidence); err != nil {
					t.Fatalf("honest joined observation rejected: %v", err)
				}
			})
		}
	}
}

func TestQuickSafetyRejectsContradictoryAggregates(t *testing.T) {
	for name, change := range map[string]func(*quick.SafetyEvidence){
		"negative-observations":  func(s *quick.SafetyEvidence) { s.ThermalObservations = -1 },
		"negative-rejected":      func(s *quick.SafetyEvidence) { s.ThermalRejectedReadings = -1 },
		"negative-unavailable":   func(s *quick.SafetyEvidence) { s.ThermalUnavailableReadings = -1 },
		"negative-enumeration":   func(s *quick.SafetyEvidence) { s.ThermalEnumerationErrors = -1 },
		"no-observation":         func(s *quick.SafetyEvidence) { s.ThermalObservations = 0 },
		"samples-without-sensor": func(s *quick.SafetyEvidence) { s.ThermalSamples = 1 },
		"peak-without-sensor":    func(s *quick.SafetyEvidence) { s.PeakMilliCelsius = 1 },
		"impossible-sample-product": func(s *quick.SafetyEvidence) {
			s.ThermalCoverage = "available"
			s.ThermalObservations, s.ThermalSensors, s.ThermalSamples = 2, 2, 5
		},
		"missing-available-sample": func(s *quick.SafetyEvidence) {
			s.ThermalCoverage = "available"
			s.ThermalObservations, s.ThermalSensors, s.ThermalSamples = 3, 2, 3
		},
		"available-with-error": func(s *quick.SafetyEvidence) {
			s.ThermalCoverage = "available"
			s.ThermalSensors, s.ThermalSamples, s.ThermalEnumerationErrors = 1, 1, 1
		},
		"unavailable-with-valid": func(s *quick.SafetyEvidence) { s.ThermalSensors, s.ThermalSamples = 1, 1 },
		"partial-without-cause":  func(s *quick.SafetyEvidence) { s.ThermalCoverage = "partial" },
		"partial-only-disabled":  func(s *quick.SafetyEvidence) { s.ThermalCoverage = "partial"; s.ThermalUnavailableReadings = 1 },
		"partial-mixed-with-no-empty-scan": func(s *quick.SafetyEvidence) {
			s.ThermalCoverage = "partial"
			s.ThermalObservations, s.ThermalSensors, s.ThermalSamples = 2, 2, 4
		},
		"unknown-coverage":           func(s *quick.SafetyEvidence) { s.ThermalCoverage = "good" },
		"completed-with-failure":     func(s *quick.SafetyEvidence) { s.MonitorFailure = "thermal_limit" },
		"unknown-monitor":            func(s *quick.SafetyEvidence) { s.MonitorStatus = "running" },
		"unknown-failure":            func(s *quick.SafetyEvidence) { s.MonitorStatus, s.MonitorFailure = "failed", "sampling_gap" },
		"thermal-limit-without-heat": func(s *quick.SafetyEvidence) { s.MonitorStatus, s.MonitorFailure = "failed", "thermal_limit" },
		"completed-with-heat": func(s *quick.SafetyEvidence) {
			s.ThermalCoverage = "available"
			s.ThermalSensors, s.ThermalSamples, s.PeakMilliCelsius = 1, 1, 90000
		},
		"outside-temperature-bound": func(s *quick.SafetyEvidence) {
			s.ThermalCoverage = "available"
			s.ThermalSensors, s.ThermalSamples, s.PeakMilliCelsius = 1, 1, 250001
		},
		"zero-configured-limits": func(s *quick.SafetyEvidence) { s.GlobalTimeoutMilliseconds, s.ThermalLimitMilliCelsius = 0, 0 },
	} {
		for _, id := range []string{"cpu.quick", "memory.quick"} {
			t.Run(id+"/"+name, func(t *testing.T) {
				safety := completeCPUEvidence().Safety
				change(&safety)
				if _, err := newValidatedQuickTestResult(id, currentQuickFixtureVersion(id), app.TestError, quickSafetyFixture(id, quick.StatusError, safety)); !errors.Is(err, errInvalidQuickSafety) {
					t.Fatalf("contradictory aggregate accepted or wrong error: %v", err)
				}
			})
		}
	}
}

func TestFinalizeRejectsExistingInvalidQuickSafetyBeforeFallback(t *testing.T) {
	for _, id := range []string{"cpu.quick", "memory.quick"} {
		original := encodeEvidenceFixture(t, quickSafetyFixture(id, quick.StatusPass, completeCPUEvidence().Safety))
		for name, change := range map[string]func([]byte) []byte{
			"old-version":     historicalQuickSchemaFixture,
			"truncated":       func(b []byte) []byte { return b[:len(b)-1] },
			"missing-cleanup": func(b []byte) []byte { return bytes.Replace(b, []byte(`"cleanup_confirmed":true,`), nil, 1) },
			"duplicate-cleanup": func(b []byte) []byte {
				return bytes.Replace(b, []byte(`"cleanup_confirmed":true`), []byte(`"cleanup_confirmed":false,"cleanup_confirmed":true`), 1)
			},
			"case-alias-cleanup": func(b []byte) []byte {
				return bytes.Replace(b, []byte(`"cleanup_confirmed":true`), []byte(`"Cleanup_Confirmed":false,"cleanup_confirmed":true`), 1)
			},
			"null-cleanup": func(b []byte) []byte {
				return bytes.Replace(b, []byte(`"cleanup_confirmed":true`), []byte(`"cleanup_confirmed":null`), 1)
			},
			"false-cleanup": func(b []byte) []byte {
				return bytes.Replace(b, []byte(`"cleanup_confirmed":true`), []byte(`"cleanup_confirmed":false`), 1)
			},
			"unjoined-state": func(b []byte) []byte {
				return bytes.Replace(b, []byte(`"monitor_status":"completed"`), []byte(`"monitor_status":"cleanup_unconfirmed"`), 1)
			},
			"unknown-safety-member": func(b []byte) []byte {
				return bytes.Replace(b, []byte(`"safety":{`), []byte(`"safety":{"ignored":true,`), 1)
			},
		} {
			for _, outcome := range []string{"pass", "fail", "error", "aborted"} {
				t.Run(id+"/"+name+"/"+outcome, func(t *testing.T) {
					encoded := change(append([]byte(nil), original...))
					if bytes.Equal(encoded, original) {
						t.Fatal("invalid fixture did not change")
					}
					assertQuickSafetyFinalizationRejected(t, id, json.RawMessage(encoded), outcome)
				})
			}
		}
	}
}

func historicalQuickSchemaFixture(encoded []byte) []byte {
	for _, schema := range []string{quick.CPUSchema, quick.MemorySchema} {
		if bytes.Contains(encoded, []byte(schema)) {
			prefix, _, _ := strings.Cut(schema, "/")
			return bytes.Replace(encoded, []byte(schema), []byte(prefix+"/v1"), 1)
		}
	}
	return encoded
}

func assertQuickSafetyFinalizationRejected(t *testing.T, id string, evidence any, outcome string) {
	t.Helper()
	// Raw malformed JSON is deliberately written without Marshal, which refuses it.
	args, snapshot, reportPath := prepareQuickFinalization(t, id, completeCPUEvidence(), outcome)
	for index, value := range args {
		if value == "-evidence-dir" {
			for _, definition := range quickResultDefinitions {
				if definition.id == id {
					encoded, ok := evidence.(json.RawMessage)
					if !ok {
						encoded = encodeEvidenceFixture(t, evidence)
					}
					if err := os.WriteFile(filepath.Join(args[index+1], definition.filename), encoded, 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
	var stdout, stderr bytes.Buffer
	random := &journalForbiddenRandom{}
	collections, clocks := 0, 0
	code := runFinalizeTestWith(t, t.Context(), args, &stdout, &stderr,
		func(context.Context) (inventory.Snapshot, error) { collections++; return snapshot, nil }, random,
		func() time.Time { clocks++; return time.Unix(1, 0) })
	if code != 1 || collections != 0 || random.calls != 0 || clocks != 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "validate evidence "+id) {
		t.Fatalf("invalid safety reached finalization: code=%d collections=%d random=%d clocks=%d stderr=%s", code, collections, random.calls, clocks, &stderr)
	}
	if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
		t.Fatal("invalid safety produced report")
	}
}

func TestSessionJournalCannotAuthorizeUnjoinedQuickEvidence(t *testing.T) {
	for _, id := range []string{"cpu.quick", "memory.quick"} {
		for _, state := range []string{"completed", "interrupted"} {
			t.Run(id+"/"+state, func(t *testing.T) {
				fixture := newJournalFixture(t, id)
				safety := completeCPUEvidence().Safety
				safety.MonitorStatus, safety.MonitorFailure, safety.CleanupConfirmed = "cleanup_unconfirmed", "cleanup_unconfirmed", false
				outcome := "error"
				if state == "interrupted" {
					outcome = "aborted"
				}
				fixture.worker(t, id, state, outcome, quickSafetyFixture(id, quick.Status(outcome), safety))
				collections, observations := 0, 0
				random := &journalForbiddenRandom{}
				var stdout, stderr bytes.Buffer
				code := fixture.finalize(t.Context(), fixture.args(t), &stdout, &stderr,
					func(context.Context) (inventory.Snapshot, error) { collections++; return fixture.snapshot, nil }, random, time.Now,
					func(context.Context) safemode.Result { observations++; return journalSafeObservation() })
				if code != 1 || collections != 0 || observations != 0 || random.calls != 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "cleanup was not confirmed") {
					t.Fatalf("journal bypassed cleanup: code=%d collect=%d observe=%d stderr=%s", code, collections, observations, &stderr)
				}
				if _, err := os.Stat(fixture.reportPath); !os.IsNotExist(err) {
					t.Fatal("uncertain session produced a report")
				}
			})
		}
	}
}

func TestFinalizePreservesHotJoinedQuickEvidence(t *testing.T) {
	for _, id := range []string{"cpu.quick", "memory.quick"} {
		for _, status := range []quick.Status{quick.StatusAborted, quick.StatusFail} {
			t.Run(id+"/"+string(status), func(t *testing.T) {
				safety := completeCPUEvidence().Safety
				safety.MonitorStatus, safety.MonitorFailure, safety.ThermalCoverage = "failed", "thermal_limit", "available"
				safety.ThermalSensors, safety.ThermalSamples, safety.PeakMilliCelsius = 1, 1, 250000
				evidence := quickSafetyFixture(id, status, safety)
				args, snapshot, reportPath := prepareQuickFinalization(t, id, evidence, string(status))
				var stdout, stderr bytes.Buffer
				if code := runFinalizeTestWith(t, t.Context(), args, &stdout, &stderr, func(context.Context) (inventory.Snapshot, error) { return snapshot, nil }, bytes.NewReader(bytes.Repeat([]byte{0x2a}, 256)), func() time.Time { return time.Unix(1, 0) }); code != 0 {
					t.Fatalf("joined thermal result rejected: %d %s", code, &stderr)
				}
				encoded, err := os.ReadFile(reportPath)
				if err != nil {
					t.Fatal(err)
				}
				verified, err := bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
				if err != nil {
					t.Fatal(err)
				}
				var payload app.InventoryPayload
				if err := json.Unmarshal(verified.InventoryJSON, &payload); err != nil {
					t.Fatal(err)
				}
				found := false
				for _, result := range payload.TestResults {
					if result.ID == id {
						found = true
						if string(result.Status) != string(status) || !bytes.Equal(result.Evidence, encodeEvidenceFixture(t, evidence)) {
							t.Fatal("thermal failure evidence was rewritten")
						}
					}
				}
				if !found {
					t.Fatal("thermal worker result omitted")
				}
			})
		}
	}
}

func TestQuickCleanupSentinelSurvivesDirectValidation(t *testing.T) {
	for _, id := range []string{"cpu.quick", "memory.quick"} {
		safety := completeCPUEvidence().Safety
		safety.CleanupConfirmed = false
		if _, err := newValidatedQuickTestResult(id, currentQuickFixtureVersion(id), app.TestFail, quickSafetyFixture(id, quick.StatusFail, safety)); !errors.Is(err, errQuickCleanupUnconfirmed) {
			t.Fatalf("cleanup sentinel lost for %s: %v", id, err)
		}
	}
}

func TestQuickSafetyRejectsWorkBeforeAnyThermalObservation(t *testing.T) {
	for _, state := range []string{"not_started", "failed"} {
		for _, field := range []string{"work", "elapsed", "aggregate-or-mismatch"} {
			for _, id := range []string{"cpu.quick", "memory.quick"} {
				t.Run(id+"/"+state+"/"+field, func(t *testing.T) {
					safety := completeCPUEvidence().Safety
					safety.MonitorStatus, safety.ThermalObservations = state, 0
					if state == "failed" {
						safety.MonitorFailure = "observation_timeout"
					}
					var evidence any
					if id == "cpu.quick" {
						result := quickSafetyFixture(id, quick.StatusError, safety).(quick.CPUResult)
						switch field {
						case "work":
							result.Iterations = 1
						case "elapsed":
							result.DurationMilliseconds = 1
						default:
							result.StateAggregate = 1
						}
						evidence = result
					} else {
						result := quickSafetyFixture(id, quick.StatusError, safety).(quick.MemoryResult)
						switch field {
						case "work":
							result.ExaminedBytes = 1
						case "elapsed":
							result.DurationMilliseconds = 1
						default:
							result.Coverage.WrittenBytes = 1
						}
						evidence = result
					}
					if _, err := newValidatedQuickTestResult(id, currentQuickFixtureVersion(id), app.TestError, evidence); !errors.Is(err, errInvalidQuickSafety) {
						t.Fatalf("unmonitored work accepted: %v", err)
					}
				})
			}
		}
	}
}

func TestQuickSafetyLargeCountsDoNotOverflowProductCheck(t *testing.T) {
	maximum := int(^uint(0) >> 1)
	for _, id := range []string{"cpu.quick", "memory.quick"} {
		safety := completeCPUEvidence().Safety
		safety.ThermalCoverage = "available"
		safety.ThermalSensors, safety.ThermalSamples, safety.ThermalObservations = maximum, maximum, 2
		if _, err := newValidatedQuickTestResult(id, currentQuickFixtureVersion(id), app.TestError, quickSafetyFixture(id, quick.StatusError, safety)); !errors.Is(err, errInvalidQuickSafety) {
			t.Fatalf("overflowing impossible aggregate accepted for %s: %v", id, err)
		}
		safety.ThermalCoverage, safety.ThermalEnumerationErrors = "partial", 1
		if _, err := newValidatedQuickTestResult(id, currentQuickFixtureVersion(id), app.TestError, quickSafetyFixture(id, quick.StatusError, safety)); err != nil {
			t.Fatalf("necessary count bound overflowed for %s: %v", id, err)
		}
	}
}

func TestFinalizeQuickEvidenceReadBoundaryAndMissingFallback(t *testing.T) {
	for _, id := range []string{"cpu.quick", "memory.quick"} {
		for _, outcome := range []string{"error", "aborted"} {
			for _, mode := range []string{"missing", "oversized", "directory", "symlink"} {
				t.Run(id+"/"+outcome+"/"+mode, func(t *testing.T) {
					args, snapshot, reportPath := prepareQuickFinalization(t, id, completeCPUEvidence(), outcome)
					var evidencePath string
					for index, argument := range args {
						if argument == "-evidence-dir" {
							for _, definition := range quickResultDefinitions {
								if definition.id == id {
									evidencePath = filepath.Join(args[index+1], definition.filename)
								}
							}
						}
					}
					retained := evidencePath + ".retained"
					if err := os.Rename(evidencePath, retained); err != nil {
						t.Fatal(err)
					}
					switch mode {
					case "oversized":
						if err := os.WriteFile(evidencePath, bytes.Repeat([]byte{' '}, (1<<20)+1), 0o600); err != nil {
							t.Fatal(err)
						}
					case "directory":
						if err := os.Mkdir(evidencePath, 0o700); err != nil {
							t.Fatal(err)
						}
					case "symlink":
						if err := os.Symlink(retained, evidencePath); err != nil {
							t.Skip("symlink creation unavailable")
						}
					}
					collections := 0
					var stdout, stderr bytes.Buffer
					code := runFinalizeTestWith(t, t.Context(), args, &stdout, &stderr,
						func(context.Context) (inventory.Snapshot, error) { collections++; return snapshot, nil },
						bytes.NewReader(bytes.Repeat([]byte{0x3a}, 256)), func() time.Time { return time.Unix(1, 0) })
					if mode == "missing" {
						if code != 0 || collections != 1 {
							t.Fatalf("missing-file diagnostic fallback rejected: %d %s", code, &stderr)
						}
						encoded, err := os.ReadFile(reportPath)
						if err != nil {
							t.Fatal(err)
						}
						artifact, err := bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
						if err != nil {
							t.Fatal(err)
						}
						if !bytes.Contains(artifact.InventoryJSON, []byte(`"source_status":"`+outcome+`"`)) {
							t.Fatal("missing-file execution note omitted")
						}
					} else {
						if code != 1 || collections != 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "validate evidence "+id) {
							t.Fatalf("existing unreadable evidence reached fallback: %d %s", code, &stderr)
						}
						if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
							t.Fatal("unreadable evidence produced report")
						}
					}
				})
			}
		}
	}
}
