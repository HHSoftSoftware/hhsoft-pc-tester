package sessionevidence

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"unicode/utf8"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
)

// No methods are inherited. StorageSource has no legacy wire member.
type legacyJournalWire Journal

type journalV6Wire struct {
	legacyJournalWire
	StorageSource *StorageSource `json:"storage_source"`
}

type journalV8Wire struct {
	legacyJournalWire
	Profile         string        `json:"profile"`
	CPUStressSource *StressSource `json:"cpu_stress_source"`
}

// MarshalJSON preserves the exact legacy field order and omission behavior.
// A source cannot be silently discarded by changing the declared schema.
func (j Journal) MarshalJSON() ([]byte, error) {
	if !IsStandardSchema(j.Schema) && (j.Profile != "" || j.CPUStressSource != nil) {
		return nil, ErrInvalid
	}
	switch j.Schema {
	case SchemaV4, SchemaV5, SchemaV7, SchemaV9, SchemaV11:
		if j.StorageSource != nil {
			return nil, ErrInvalid
		}
		return json.Marshal(legacyJournalWire(j))
	case SchemaV6:
		return json.Marshal(journalV6Wire{legacyJournalWire: legacyJournalWire(j), StorageSource: j.StorageSource})
	case SchemaV8, SchemaV10, SchemaV12:
		if j.StorageSource != nil || j.Profile != "standard" {
			return nil, ErrInvalid
		}
		return json.Marshal(journalV8Wire{legacyJournalWire: legacyJournalWire(j), Profile: j.Profile, CPUStressSource: j.CPUStressSource})
	default:
		return nil, ErrInvalid
	}
}

// objectFields rejects duplicate members before any last-wins decoding. The
// caller validates the exact schema-specific members and their nested shapes.
func objectFields(raw []byte, maximum int) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > maximum || !utf8.Valid(raw) {
		return nil, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, ErrInvalid
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok {
			return nil, ErrInvalid
		}
		if _, duplicate := fields[name]; duplicate {
			return nil, ErrInvalid
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, ErrInvalid
		}
		fields[name] = value
	}
	if last, err := decoder.Token(); err != nil || last != json.Delim('}') {
		return nil, ErrInvalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, ErrInvalid
	}
	return fields, nil
}

func decodeJournalWire(raw []byte) (Journal, error) {
	fields, err := objectFields(raw, MaximumBytes)
	if err != nil {
		return Journal{}, err
	}
	var schema string
	if json.Unmarshal(fields["schema"], &schema) != nil || !IsStrictSchema(schema) {
		return Journal{}, ErrInvalid
	}
	legacyRaw := raw
	var source *StorageSource
	var stressSource *StressSource
	profile := ""
	if IsStandardSchema(schema) {
		encoded, present := fields["cpu_stress_source"]
		if !present || json.Unmarshal(fields["profile"], &profile) != nil || profile != "standard" {
			return Journal{}, ErrInvalid
		}
		if !bytes.Equal(bytes.TrimSpace(encoded), []byte("null")) {
			var value StressSource
			if evidencejson.ValidateShape(encoded, reflect.TypeOf(value)) != nil || json.Unmarshal(encoded, &value) != nil {
				return Journal{}, ErrInvalid
			}
			stressSource = &value
		}
		delete(fields, "profile")
		delete(fields, "cpu_stress_source")
		legacyRaw, err = json.Marshal(fields)
		if err != nil {
			return Journal{}, ErrInvalid
		}
	}
	if schema == SchemaV6 {
		encoded, present := fields["storage_source"]
		if !present {
			return Journal{}, ErrInvalid
		}
		if !bytes.Equal(bytes.TrimSpace(encoded), []byte("null")) {
			var value StorageSource
			if evidencejson.ValidateShape(encoded, reflect.TypeOf(value)) != nil || json.Unmarshal(encoded, &value) != nil {
				return Journal{}, ErrInvalid
			}
			source = &value
		}
		delete(fields, "storage_source")
		legacyRaw, err = json.Marshal(fields)
		if err != nil {
			return Journal{}, ErrInvalid
		}
	}
	var legacy legacyJournalWire
	if evidencejson.ValidateShape(legacyRaw, reflect.TypeOf(legacy)) != nil || json.Unmarshal(legacyRaw, &legacy) != nil {
		return Journal{}, ErrInvalid
	}
	value := Journal(legacy)
	value.StorageSource = source
	value.Profile = profile
	value.CPUStressSource = stressSource
	return value, nil
}

// integrityWireBytes validates the original nested journal before replacing it
// with its legacy-shaped domain encoding for the unchanged outer shape check.
// The replacement is never used as the journal's size or source-byte proof.
func integrityWireBytes(raw []byte) ([]byte, Journal, error) {
	fields, err := objectFields(raw, 1<<20)
	if err != nil {
		return nil, Journal{}, err
	}
	journal, err := Decode(fields["session"])
	if err != nil {
		return nil, Journal{}, err
	}
	fields["session"], err = json.Marshal(legacyJournalWire(journal))
	if err != nil {
		return nil, Journal{}, ErrInvalid
	}
	shapeBytes, err := json.Marshal(fields)
	if err != nil {
		return nil, Journal{}, ErrInvalid
	}
	return shapeBytes, journal, nil
}

func validStorageSourceStructure(j Journal) bool {
	if j.Schema != SchemaV6 {
		return j.StorageSource == nil
	}
	// ValidateStructure has already checked the exact twelve-worker sequence.
	worker := j.Workers[8]
	if worker.State == "not_scheduled" || worker.State == "not_started" {
		return j.StorageSource == nil
	}
	if worker.Outcome == "skip" {
		return false
	}
	if j.StorageSource == nil {
		return worker.Outcome == "error" || worker.Outcome == "aborted"
	}
	source := j.StorageSource
	return source.WorkerID == "storage.quick" && source.EvidenceSchema == "hhsoft.pc-tester.storage-sample/v1" &&
		validDigest(source.EvidenceFileSHA256) && validDigest(source.EvidenceTypedSHA256)
}
