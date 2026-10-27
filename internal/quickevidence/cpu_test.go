package quickevidence

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
)

func scalarFixture() quick.CPUResult {
	return quick.CPUResult{Schema: quick.CPUSchema, Status: quick.StatusPass, Workers: 1, Iterations: 1,
		KnownAnswerSHA256: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", DurationMilliseconds: 10,
		Scalar: quick.CPUScalarEvidence{Suite: quick.CPUScalarSuite, VectorCount: 12, ChecksPassed: 12, WorkersCompleted: 1,
			Workers: []quick.CPUScalarWorker{{Index: 0, Iterations: 1, ChecksPassed: 12}}},
		Safety: quick.SafetyEvidence{GlobalTimeoutMilliseconds: 1000, ThermalLimitMilliCelsius: 90000,
			MonitorStatus: "completed", CleanupConfirmed: true, ThermalObservations: 1, ThermalCoverage: "unavailable"}}
}

func decodeScalarFixture(t *testing.T, value quick.CPUResult) (LocalEvidence, error) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return DecodeLocal("cpu.quick", raw)
}

func TestCPUScalarDecoderCadenceAndSummary(t *testing.T) {
	for _, test := range []struct{ iterations, checks uint64 }{{0, 0}, {0, 12}, {1, 12}, {255, 12}, {256, 12}, {256, 17}, {256, 24}, {257, 24}, {512, 36}} {
		v := scalarFixture()
		v.Status = quick.StatusInconclusive
		v.Iterations = test.iterations
		v.Scalar.ChecksPassed = test.checks
		v.Scalar.Workers[0].Iterations = test.iterations
		v.Scalar.Workers[0].ChecksPassed = test.checks
		if test.checks < 12 {
			v.Scalar.WorkersCompleted = 0
		}
		got, err := decodeScalarFixture(t, v)
		if err != nil || !got.Scalar.Present || got.Scalar.ChecksPassed != test.checks || got.Scalar.WorkersStarted != 1 {
			t.Fatalf("valid boundary %v: %v", test, err)
		}
		copy := got.Scalar
		v.Scalar.Workers[0].ChecksPassed++
		if got.Scalar != copy {
			t.Fatal("summary aliases mutable wire")
		}
	}
	v := scalarFixture()
	v.Status = quick.StatusFail
	v.Iterations = 256
	v.Scalar.ChecksPassed = 17
	v.Scalar.Workers[0].Iterations = 256
	v.Scalar.Workers[0].ChecksPassed = 17
	v.Scalar.Workers[0].Mismatch = &quick.CPUScalarMismatch{Vector: "sub3", ExpectedBits: "4260000000000000", ActualBits: "7ff8000000000001"}
	v.CalculationFailures.Scalar = true
	got, err := decodeScalarFixture(t, v)
	if err != nil || got.Scalar.WorkersCompleted != 1 || got.Scalar.MismatchedWorkers != 1 || !got.Scalar.CalculationFailures.Scalar {
		t.Fatal("lost later failure", err)
	}
}

func TestCPUScalarDecoderRejectsCounterAndFailureContradictions(t *testing.T) {
	cases := map[string]func(*quick.CPUResult){
		"missing rows":   func(v *quick.CPUResult) { v.Scalar.Workers = nil },
		"unknown suite":  func(v *quick.CPUResult) { v.Scalar.Suite += "x" },
		"missing vector": func(v *quick.CPUResult) { v.Scalar.VectorCount-- },
		"wrong index":    func(v *quick.CPUResult) { v.Scalar.Workers[0].Index = 1 },
		"duplicate row": func(v *quick.CPUResult) {
			v.Workers = 2
			v.Scalar.Workers = append(v.Scalar.Workers, v.Scalar.Workers[0])
		},
		"row beyond requested":         func(v *quick.CPUResult) { v.Workers = 0 },
		"wrong iterations":             func(v *quick.CPUResult) { v.Iterations++ },
		"wrong checks":                 func(v *quick.CPUResult) { v.Scalar.ChecksPassed++ },
		"wrong completion":             func(v *quick.CPUResult) { v.Scalar.WorkersCompleted = 0 },
		"fail without flag":            func(v *quick.CPUResult) { v.Status = quick.StatusFail },
		"flag without failure":         func(v *quick.CPUResult) { v.CalculationFailures.Integer = true },
		"scalar flag without mismatch": func(v *quick.CPUResult) { v.Status = quick.StatusFail; v.CalculationFailures.Scalar = true },
		"mismatch outside suite": func(v *quick.CPUResult) {
			v.Status = quick.StatusFail
			v.CalculationFailures.Scalar = true
			v.Scalar.Workers[0].Mismatch = &quick.CPUScalarMismatch{Vector: "add1", ExpectedBits: "400e000000000000", ActualBits: "400e000000000001"}
		},
		"overflow disposition": func(v *quick.CPUResult) { v.Status = quick.StatusError; v.Schema = "" },
		"sum overflow": func(v *quick.CPUResult) {
			v.Status = quick.StatusError
			v.Workers = 2
			v.Iterations = 0
			v.Scalar.Workers = []quick.CPUScalarWorker{{Index: 0, Iterations: math.MaxUint64, ChecksPassed: 12 * (math.MaxUint64/256 + 1)}, {Index: 1, Iterations: 1, ChecksPassed: 12}}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			v := scalarFixture()
			change(&v)
			if _, err := decodeScalarFixture(t, v); err == nil {
				t.Fatal("contradiction accepted")
			}
		})
	}
	for _, pair := range [][2]uint64{{0, 13}, {1, 0}, {255, 13}, {256, 11}, {256, 25}, {257, 23}, {512, 37}} {
		v := scalarFixture()
		v.Status = quick.StatusInconclusive
		v.Iterations = pair[0]
		v.Scalar.ChecksPassed = pair[1]
		v.Scalar.Workers[0].Iterations = pair[0]
		v.Scalar.Workers[0].ChecksPassed = pair[1]
		if _, err := decodeScalarFixture(t, v); err == nil {
			t.Fatalf("impossible cadence %v", pair)
		}
	}
}

func TestCPUScalarStrictShapeAndHistoricalDigest(t *testing.T) {
	v := scalarFixture()
	raw, _ := json.Marshal(v)
	for name, encoded := range map[string]string{
		"duplicate schema": strings.Replace(string(raw), `"schema":`, `"schema":"hhsoft.pc-tester.cpu/v2","schema":`, 1),
		"alias":            strings.Replace(string(raw), `"scalar":`, `"Scalar":`, 1),
		"float counter":    strings.Replace(string(raw), `"checks_passed":12`, `"checks_passed":12.0`, 1),
		"null mismatch":    strings.Replace(string(raw), `"index":0`, `"mismatch":null,"index":0`, 1),
		"unknown":          strings.Replace(string(raw), `"index":0`, `"other":true,"index":0`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeLocal("cpu.quick", []byte(encoded)); err == nil {
				t.Fatal("shape accepted")
			}
		})
	}
	old, err := DecodeLocal("cpu.quick", []byte(hotCPU))
	if err != nil || old.TypedSHA256 != "sha256:78df8ddd3a92a79a93536a162c05f901627fab69b48a664c9132b2e3d0c62093" || old.Scalar != (CPUScalarSummary{}) {
		t.Fatal("historical typed hash or summary changed", err)
	}
}

func TestCPUScalarCalculationFailuresNeedAStartedKATWitness(t *testing.T) {
	for _, sha := range []bool{false, true} {
		v := scalarFixture()
		v.Status = quick.StatusFail
		v.Iterations = 0
		v.Scalar.Workers = []quick.CPUScalarWorker{}
		v.Scalar.ChecksPassed = 0
		v.Scalar.WorkersCompleted = 0
		v.CalculationFailures.Integer, v.CalculationFailures.SHA256 = !sha, sha
		if _, err := decodeScalarFixture(t, v); err == nil {
			t.Fatal("failure without a worker accepted")
		}
		v.Scalar.Workers = []quick.CPUScalarWorker{{Index: 0}}
		if _, err := decodeScalarFixture(t, v); err != nil {
			t.Fatal("genuine initial KAT failure rejected", err)
		}
		v.Scalar.Workers[0].ChecksPassed = 12
		v.Scalar.ChecksPassed = 12
		v.Scalar.WorkersCompleted = 1
		if _, err := decodeScalarFixture(t, v); err == nil {
			t.Fatal("KAT failure after first suite with no next boundary")
		}
	}
	v := scalarFixture()
	v.Status = quick.StatusInconclusive
	v.Workers = 0
	v.Iterations = 0
	v.Scalar.Workers = []quick.CPUScalarWorker{}
	v.Scalar.ChecksPassed = 0
	v.Scalar.WorkersCompleted = 0
	if _, err := decodeScalarFixture(t, v); err == nil {
		t.Fatal("configured zero worker count accepted")
	}
	v.Status = quick.StatusError
	v.DurationMilliseconds = 0
	v.Safety = quick.SafetyEvidence{MonitorStatus: "not_started", CleanupConfirmed: true, ThermalCoverage: "unavailable"}
	if _, err := decodeScalarFixture(t, v); err != nil {
		t.Fatal("invalid-configuration evidence rejected", err)
	}
}

func TestCPUScalarDistinctFailureKindsNeedDistinctWorkers(t *testing.T) {
	v := scalarFixture()
	v.Status, v.Iterations = quick.StatusFail, 0
	v.Scalar.ChecksPassed, v.Scalar.WorkersCompleted = 0, 0
	v.Scalar.Workers = []quick.CPUScalarWorker{{Index: 0}}
	v.CalculationFailures.Integer, v.CalculationFailures.SHA256 = true, true
	if _, err := decodeScalarFixture(t, v); err == nil {
		t.Fatal("one worker cannot fail both sequential known-answer checks")
	}
	v.Workers = 2
	v.Scalar.Workers = append(v.Scalar.Workers, quick.CPUScalarWorker{Index: 1})
	if _, err := decodeScalarFixture(t, v); err != nil {
		t.Fatal("concurrent distinct failures rejected", err)
	}
	v.CalculationFailures.Scalar = true
	v.Scalar.Workers[1].Mismatch = &quick.CPUScalarMismatch{Vector: "add1", ExpectedBits: "400e000000000000", ActualBits: "7ff8000000000001"}
	if _, err := decodeScalarFixture(t, v); err == nil {
		t.Fatal("scalar failure row reused for earlier known-answer failure")
	}
	v.Workers = 3
	v.Scalar.Workers = append(v.Scalar.Workers, quick.CPUScalarWorker{Index: 2})
	if _, err := decodeScalarFixture(t, v); err != nil {
		t.Fatal("three distinct failure workers rejected", err)
	}
}
