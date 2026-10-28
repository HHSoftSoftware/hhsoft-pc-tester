package sessionevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storageevidence"
)

func validateStorageNote(binding ResultBinding, sourceStatus string) error {
	var note executionNote
	if HasStorageClaim(binding.Evidence) || evidencejson.ValidateShape(binding.Evidence, reflect.TypeOf(note)) != nil ||
		json.Unmarshal(binding.Evidence, &note) != nil || note.Schema != "hhsoft.pc-tester.execution-note/v1" ||
		note.Status != binding.ReportStatus || note.SourceStatus != sourceStatus {
		return ErrInvalid
	}
	return nil
}

// ValidateUnexecutedStorage validates the inactive definition's frozen-plan
// disposition. It never accepts functional or attempted execution evidence.
func ValidateUnexecutedStorage(binding ResultBinding) error {
	if binding.ID != "storage.quick" || binding.Version != 2 || binding.ReportStatus != reportStatus(binding.PlannedStatus) {
		return ErrInvalid
	}
	switch binding.PlannedStatus {
	case "excluded", "not_present", "unsupported", "missing_driver":
	default:
		return ErrInvalid
	}
	return validateStorageNote(binding, binding.PlannedStatus)
}

func validateStorageBinding(j Journal, worker WorkerRecord, binding ResultBinding) error {
	if j.Schema != SchemaV6 || binding.ID != "storage.quick" || binding.Version != 2 {
		return ErrInvalid
	}
	var header struct {
		Schema string `json:"schema"`
	}
	if json.Unmarshal(binding.Evidence, &header) != nil {
		return ErrInvalid
	}
	if header.Schema == "hhsoft.pc-tester.execution-note/v1" {
		if j.StorageSource != nil {
			return ErrInvalid
		}
		if worker.State == "not_scheduled" {
			return ValidateUnexecutedStorage(binding)
		}
		if worker.State != "not_started" && worker.Outcome != "error" && worker.Outcome != "aborted" {
			return ErrInvalid
		}
		return validateStorageNote(binding, worker.Outcome)
	}
	value, err := storageevidence.Decode(binding.Evidence)
	if err != nil {
		return err
	}
	if j.StorageSource == nil || worker.Timing == nil || value.Scope != "planned" || value.PlanID != j.PlanID ||
		value.Schema != j.StorageSource.EvidenceSchema || value.Status != worker.Outcome ||
		value.DurationMilliseconds > worker.Timing.FinishedMilliseconds-worker.Timing.StartedMilliseconds {
		return ErrInvalid
	}
	canonical, err := storageevidence.CanonicalTypedBytes(value)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(canonical)
	if j.StorageSource.EvidenceTypedSHA256 != "sha256:"+hex.EncodeToString(digest[:]) {
		return ErrInvalid
	}
	// The original file digest is a signed producer assertion. Report JSON may
	// have been compacted; reconstructing that digest here would be misleading.
	return nil
}
