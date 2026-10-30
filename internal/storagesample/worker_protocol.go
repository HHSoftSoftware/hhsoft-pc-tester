package storagesample

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"reflect"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
)

const sampleWorkerSchema = "hhsoft.pc-tester.storage-sample-worker/v1"
const maximumSampleWorkerReply = 8192

// This is a closed internal completion record, not permission to open a target.
// The parent still requires owned-process cleanup, its original real deadline,
// and its independently retained target/discovery owners before any acceptance.
type sampleWorkerReply struct {
	Schema                string                         `json:"schema"`
	Target                storagetest.Target             `json:"target"`
	SnapshotSHA256        string                         `json:"snapshot_sha256"`
	Observation           *storagetest.SampleObservation `json:"observation"`
	Error                 string                         `json:"error"`
	CloseConfirmed        bool                           `json:"close_confirmed"`
	FinalBindingConfirmed bool                           `json:"final_binding_confirmed"`
}

func sampleWorkerError(value string) bool {
	switch value {
	case "none", "open_error", "target_busy", "identity_changed", "safety_changed",
		"discovery_failed", "registry_invalid", "topology_invalid", "metadata_limit",
		"safety_view_invalid", "swap_active", "fixture_changed", "clock_invalid",
		"deadline", "cancelled", "close_error":
		return true
	default:
		return false
	}
}

func sampleSnapshotDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
}

func decodeSampleWorkerReply(target storagetest.Target, snapshot string, raw []byte) (sampleWorkerReply, storagetest.ObservationSummary, error) {
	invalid := func() (sampleWorkerReply, storagetest.ObservationSummary, error) {
		return sampleWorkerReply{}, storagetest.ObservationSummary{}, errSampleProtocol
	}
	if _, err := prepareSampleObservation(target); err != nil {
		return invalid()
	}
	if !sampleSnapshotDigest(snapshot) || len(raw) == 0 || len(raw) > maximumSampleWorkerReply || raw[len(raw)-1] != '\n' {
		return invalid()
	}
	var value sampleWorkerReply
	if json.Unmarshal(raw, &value) != nil {
		return invalid()
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(raw, append(canonical, '\n')) {
		// Exact emitter bytes reject missing, extra, duplicate or aliased fields,
		// explicit null scalars, unknown escapes, trailing data and extra frames.
		return invalid()
	}
	if value.Schema != sampleWorkerSchema || !reflect.DeepEqual(value.Target, target) ||
		value.SnapshotSHA256 != snapshot || !sampleWorkerError(value.Error) {
		return invalid()
	}
	if value.CloseConfirmed == (value.Error == "close_error") {
		return invalid()
	}
	if value.Error == "none" && (!value.FinalBindingConfirmed || value.Observation == nil) {
		return invalid()
	}
	if value.Observation == nil {
		if value.FinalBindingConfirmed || value.Error == "none" {
			return invalid()
		}
		return value, storagetest.ObservationSummary{}, nil
	}
	summary, err := storagetest.ValidateObservation(target, *value.Observation)
	if err != nil {
		return invalid()
	}
	if value.Error == "none" && value.Observation.Status != "pass" && value.Observation.Status != "fail" {
		return invalid()
	}
	return value, summary, nil
}

func encodeSampleWorkerReply(value sampleWorkerReply) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil || len(raw)+1 > maximumSampleWorkerReply {
		return nil, errSampleProtocol
	}
	raw = append(raw, '\n')
	if _, _, err := decodeSampleWorkerReply(value.Target, value.SnapshotSHA256, raw); err != nil {
		return nil, err
	}
	return raw, nil
}
