package sessionevidence

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/thermal"
)

func storageJournal() Journal {
	j := shortJournal()
	j.Schema = SchemaV6
	j.Workers = append(j.Workers[:8:8], append([]WorkerRecord{{ID: "storage.quick", State: "not_scheduled", Outcome: "excluded"}}, j.Workers[8:]...)...)
	return j
}

func storageBindings(j Journal) []ResultBinding {
	rows := shortBindings(j)
	for i := range rows {
		if rows[i].ID == "storage.quick" {
			rows[i].Version = 2
		}
	}
	return rows
}

func TestStorageJournalV6KeepsStorageInactiveInV7(t *testing.T) {
	if len(WorkerIDsV7()) != 12 || WorkerIDsV7()[11] != "inspection.exterior.quick" || WorkerIDsV7()[8] != "usb.readiness.quick" {
		t.Fatal("storage reader changed the historical v7 contract")
	}
	j := storageJournal()
	if err := ValidateBindings(j, storageBindings(j)); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(j)
	if err != nil || !bytes.HasSuffix(raw, []byte(`,"storage_source":null}`)) {
		t.Fatal("v6 omitted its explicit null source", err)
	}
	got, err := Decode(raw)
	if err != nil || !reflect.DeepEqual(got, j) {
		t.Fatal("v6 all-off round trip", err)
	}
	if j.Workers[8].ID != "storage.quick" || j.Workers[7].ID != "storage.readiness.quick" || j.Workers[9].ID != "usb.readiness.quick" {
		t.Fatal("storage worker is in the wrong position")
	}
}

func TestStorageJournalWireRejectsMissingAmbiguousAndLegacySource(t *testing.T) {
	raw, err := json.Marshal(storageJournal())
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"missing":          bytes.Replace(raw, []byte(`,"storage_source":null`), nil, 1),
		"duplicate":        bytes.Replace(raw, []byte(`"storage_source":null`), []byte(`"storage_source":null,"storage_source":null`), 1),
		"case alias":       bytes.Replace(raw, []byte(`"storage_source"`), []byte(`"Storage_Source"`), 1),
		"array":            bytes.Replace(raw, []byte(`"storage_source":null`), []byte(`"storage_source":[]`), 1),
		"unstarted object": bytes.Replace(raw, []byte(`"storage_source":null`), []byte(`"storage_source":{}`), 1),
		"duplicate schema": bytes.Replace(raw, []byte(`"schema":"hhsoft.pc-tester.quick-session/v6"`), []byte(`"schema":"hhsoft.pc-tester.quick-session/v5","schema":"hhsoft.pc-tester.quick-session/v6"`), 1),
		"unknown version":  bytes.Replace(raw, []byte(`quick-session/v6`), []byte(`quick-session/v99`), 1),
		"trailing record":  append(append([]byte{}, raw...), []byte(` {}`)...),
		"invalid utf8":     append(append([]byte{}, raw...), 0xff),
	}
	for name, encoded := range cases {
		t.Run(name, func(t *testing.T) {
			if bytes.Equal(raw, encoded) {
				t.Fatal("negative did not change fixture")
			}
			if _, err := Decode(encoded); err == nil {
				t.Fatal("invalid journal accepted")
			}
		})
	}
	for _, schema := range []string{SchemaV4, SchemaV5} {
		j := shortJournal()
		j.Schema = schema
		if schema == SchemaV4 {
			j.Workers = append(j.Workers[:4:4], j.Workers[5:]...)
		}
		legacy, err := json.Marshal(j)
		if err != nil || bytes.Contains(legacy, []byte("storage_source")) {
			t.Fatal("legacy emission changed", err)
		}
		if _, err := Decode(legacy); err != nil {
			t.Fatal(err)
		}
		injected := append(append([]byte{}, legacy[:len(legacy)-1]...), []byte(`,"storage_source":null}`)...)
		if _, err := Decode(injected); err == nil {
			t.Fatal("legacy null source accepted")
		}
		j.StorageSource = &StorageSource{}
		if _, err := json.Marshal(j); err == nil {
			t.Fatal("legacy source was silently discarded")
		}
	}
}

func TestStorageJournalSourceOwnershipAndOriginalNestedBound(t *testing.T) {
	j := storageJournal()
	j.Workers[8] = WorkerRecord{ID: "storage.quick", State: "completed", Outcome: "error", Timing: &WorkerTiming{10, 60}}
	j.Safety = append(j.Safety[:1:1], append([]SafetyRecord{{Phase: "before-storage.quick", Result: j.Safety[0].Result}}, j.Safety[1:]...)...)
	j.StorageSource = &StorageSource{WorkerID: "storage.quick", EvidenceSchema: "hhsoft.pc-tester.storage-sample/v1", EvidenceFileSHA256: "sha256:" + strings.Repeat("b", 64), EvidenceTypedSHA256: "sha256:" + strings.Repeat("c", 64)}
	copy := CloneJournal(j)
	copy.StorageSource.EvidenceFileSHA256 = "changed"
	if j.StorageSource.EvidenceFileSHA256 == "changed" {
		t.Fatal("source clone aliases original")
	}
	encoded, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil || !reflect.DeepEqual(decoded, j) {
		t.Fatal("source shape round trip failed", err)
	}
	integrity := IntegrityEvidence{Schema: IntegritySchema, Status: "pass", PlanID: j.PlanID, PlannedHardwareGraph: "hmac-sha256:" + strings.Repeat("d", 64), FinalHardwareGraph: "hmac-sha256:" + strings.Repeat("d", 64), PlannedComponentCount: 1, FinalComponentCount: 1, Session: &j}
	outer, err := json.Marshal(integrity)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := DecodeIntegrity(outer); err != nil || value.Session == nil || value.Session.StorageSource == nil {
		t.Fatal("outer decoder lost v6 source", err)
	}
	padding := MaximumBytes - len(encoded)
	if padding <= 0 {
		t.Fatal("fixture unexpectedly exceeds bound")
	}
	boundary := append([]byte{'{'}, bytes.Repeat([]byte{' '}, padding)...)
	boundary = append(boundary, encoded[1:]...)
	if len(boundary) != MaximumBytes {
		t.Fatal("wrong literal boundary")
	}
	for _, extra := range []int{0, 1} {
		candidate := append([]byte{'{'}, bytes.Repeat([]byte{' '}, extra)...)
		candidate = append(candidate, boundary[1:]...)
		value := bytes.Replace(outer, encoded, candidate, 1)
		if bytes.Equal(value, outer) {
			t.Fatal("nested replacement did not run")
		}
		_, err := DecodeIntegrity(value)
		if (err == nil) != (extra == 0) {
			t.Fatal("nested original-byte bound was lost", extra, err)
		}
	}
	for _, mutate := range []func(*Journal){
		func(v *Journal) { v.StorageSource.WorkerID = "storage.readiness.quick" },
		func(v *Journal) { v.StorageSource.EvidenceSchema = "hhsoft.pc-tester.storage-readiness/v1" },
		func(v *Journal) { v.StorageSource.EvidenceFileSHA256 = strings.Repeat("a", 64) },
		func(v *Journal) { v.StorageSource.EvidenceTypedSHA256 = "sha256:" + strings.Repeat("A", 64) },
		func(v *Journal) { v.Workers[8].Outcome = "pass"; v.StorageSource = nil },
	} {
		value := CloneJournal(j)
		mutate(&value)
		if ValidateStructure(value) == nil {
			t.Fatal("contradictory source structure accepted")
		}
	}
}

func TestStorageClaimSelectionCannotHideNewEvidence(t *testing.T) {
	for _, raw := range []string{
		`{"schema":"hhsoft.pc-tester.storage-sample/v1"}`,
		`{"schema":"hhsoft.pc-tester.storage-sample/v99","schema":"legacy"}`,
		`{"Schema":"HHSOFT.PC-TESTER.STORAGE-SAMPLE/V1"}`,
		`{"storage\u005fsource":null}`,
		`{"id":"storage.quick","version":2,"version":1}`,
		`{"version":2,"id":"STORAGE.QUICK"}`,
		`{"id":"storage.quick","version":1.0}`,
		`{"id":"storage.quick","version":"1"}`,
		`{"nested":[{"policy":"fixed-buffered-three-window-repeat/v1"}]}`,
		`{"truncated":`,
	} {
		if !HasStorageClaim([]byte(raw)) {
			t.Fatal("new or ambiguous claim hidden", raw)
		}
	}
	for _, raw := range []string{`{"id":"storage.quick","version":1}`, `{"schema":"hhsoft.pc-tester.storage-readiness/v1"}`, `{"id":"storage.quick"}`} {
		if HasStorageClaim([]byte(raw)) {
			t.Fatal("historical evidence was activated", raw)
		}
	}
}

func TestStorageV6EveryWorkerCancellationAndHeatKeepCleanupGates(t *testing.T) {
	for index, id := range workerIDs(SchemaV6) {
		t.Run(id, func(t *testing.T) {
			j := storageJournal()
			j.StopReason = "operator_cancelled"
			j.Workers[index] = WorkerRecord{ID: id, State: "not_started", Outcome: "aborted"}
			rows := storageBindings(j)
			rows[index].PlannedStatus = "scheduled"
			if err := ValidateBindings(j, rows); err != nil {
				t.Fatal("honest unstarted cancellation rejected", err)
			}
			for _, mutate := range []func(*Journal){
				func(v *Journal) { v.Workers[index].Timing = &WorkerTiming{10, 60} },
				func(v *Journal) { v.Thermal.Joined = false },
				func(v *Journal) { v.Monitor.CleanupConfirmed = false },
				func(v *Journal) { v.Thermal.Final.Completed = false },
				func(v *Journal) { v.Safety = v.Safety[:len(v.Safety)-1] },
			} {
				value := CloneJournal(j)
				mutate(&value)
				if ValidateStructure(value) == nil {
					t.Fatal("cancelled session lost a final gate")
				}
			}
			j = storageJournal()
			j.StopReason = "thermal_limit"
			hot := quick.ThermalSnapshot{Coverage: quick.ThermalAvailable, Sensors: 1, PeakMilliCelsius: 95000}
			j.Thermal.Initial.Observation = &hot
			j.Thermal.Aggregate = thermal.Aggregate{Coverage: "partial", AcceptedObservations: 2, AvailableObservations: 1, UnavailableObservations: 1, MaximumSensors: 1, ValidReadings: 1, PeakMilliCelsius: 95000}
			j.Thermal.FirstFinding = &thermal.FindingRecord{Reason: "thermal_limit", Phase: "initial", ElapsedMilliseconds: 5, Observation: hot}
			if err := ValidateStructure(j); err != nil {
				t.Fatal("initial heat baseline rejected", err)
			}
			j.Workers[index] = WorkerRecord{ID: id, State: "completed", Outcome: "pass", Timing: &WorkerTiming{10, 60}}
			j.Safety = append(j.Safety[:1:1], append([]SafetyRecord{{Phase: "before-" + id, Result: j.Safety[0].Result}}, j.Safety[1:]...)...)
			if id == "storage.quick" {
				j.StorageSource = &StorageSource{WorkerID: id, EvidenceSchema: "hhsoft.pc-tester.storage-sample/v1", EvidenceFileSHA256: "sha256:" + strings.Repeat("a", 64), EvidenceTypedSHA256: "sha256:" + strings.Repeat("b", 64)}
			}
			cool := CloneJournal(j)
			cool.StopReason = "completed"
			cool.Thermal = shortJournal().Thermal
			if err := ValidateStructure(cool); err != nil {
				t.Fatal("worker baseline was invalid without heat", err)
			}
			if ValidateStructure(j) == nil {
				t.Fatal("latched heat allowed a worker")
			}
		})
	}
}

func TestStorageReaderOriginalNestedBoundAppliesToEverySupportedSchema(t *testing.T) {
	for _, schema := range []string{SchemaV4, SchemaV5, SchemaV6} {
		t.Run(schema, func(t *testing.T) {
			j := shortJournal()
			j.Schema = schema
			if schema == SchemaV4 {
				j.Workers = append(j.Workers[:4:4], j.Workers[5:]...)
			}
			if schema == SchemaV6 {
				j = storageJournal()
			}
			raw, err := json.Marshal(j)
			if err != nil {
				t.Fatal(err)
			}
			integrity := IntegrityEvidence{Schema: IntegritySchema, Status: "pass", PlanID: j.PlanID, PlannedHardwareGraph: "hmac-sha256:" + strings.Repeat("d", 64), FinalHardwareGraph: "hmac-sha256:" + strings.Repeat("d", 64), Session: &j}
			outer, err := json.Marshal(integrity)
			if err != nil {
				t.Fatal(err)
			}
			for _, size := range []int{MaximumBytes, MaximumBytes + 1} {
				padded := append([]byte{'{'}, bytes.Repeat([]byte{' '}, size-len(raw))...)
				padded = append(padded, raw[1:]...)
				if len(padded) != size {
					t.Fatal("original-size fixture incorrect")
				}
				_, directErr := Decode(padded)
				_, nestedErr := DecodeIntegrity(bytes.Replace(outer, raw, padded, 1))
				if (directErr == nil) != (size == MaximumBytes) || (nestedErr == nil) != (size == MaximumBytes) {
					t.Fatal("compaction changed original journal bound", size, directErr, nestedErr)
				}
			}
		})
	}
}
