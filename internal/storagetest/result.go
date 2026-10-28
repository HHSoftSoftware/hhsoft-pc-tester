package storagetest

import "encoding/json"

const Schema = "hhsoft.pc-tester.storage-sample/v1"
const Policy = "fixed-buffered-three-window-repeat/v1"
const MaximumResultBytes = 512 << 10

// Result is a finite signed observation, not permission to open media. Its
// accepted flags describe producer assertions checked for internal coherence by
// report readers; they do not prove kernel behavior or physical identity.
type Result struct {
	Schema                string         `json:"schema"`
	Policy                string         `json:"policy"`
	Scope                 string         `json:"scope"`
	PlanID                string         `json:"plan_id"`
	Status                string         `json:"status"`
	StopReason            string         `json:"stop_reason"`
	Error                 string         `json:"error"`
	LimitMilliseconds     int64          `json:"limit_ms"`
	BudgetMilliseconds    int64          `json:"budget_ms"`
	DurationMilliseconds  int64          `json:"duration_ms"`
	CacheBypass           bool           `json:"cache_bypass"`
	MediaWrites           bool           `json:"media_writes"`
	CleanupConfirmed      bool           `json:"cleanup_confirmed"`
	FailureObserved       bool           `json:"failure_observed"`
	DiagnosticFailure     bool           `json:"diagnostic_failure"`
	DiscoveryAccepted     bool           `json:"discovery_accepted"`
	ComponentIDs          []string       `json:"component_ids"`
	FixtureIDs            []string       `json:"fixture_ids"`
	MissingComponents     []string       `json:"missing_components"`
	Snapshot              *Snapshot      `json:"snapshot"`
	SnapshotSHA256        string         `json:"snapshot_sha256"`
	Devices               []DeviceResult `json:"devices"`
	AcceptedReadBytes     uint64         `json:"accepted_read_bytes"`
	AcceptedComparedBytes uint64         `json:"accepted_compared_bytes"`
	AcceptedMatchedRanges uint32         `json:"accepted_matched_ranges"`
}

// Snapshot retains the typed facts used for admission. Fixture observations
// contain only selected public identities; all relations remain metadata.
type Snapshot struct {
	Discovery           Discovery            `json:"discovery"`
	FixtureObservations []FixtureObservation `json:"fixture_observations"`
	ResolvedFixtureMask uint8                `json:"resolved_fixture_mask"`
	NodeFixtureMasks    FixtureMasks         `json:"node_fixture_masks"`
}

// FixtureMasks uses a required integer array instead of encoding/json's special
// base64 representation for byte slices. Validation still bounds each to eight
// selected fixture bits and the array to the exact discovered node count.
type FixtureMasks []uint8

func (values FixtureMasks) MarshalJSON() ([]byte, error) {
	if values == nil {
		return []byte("null"), nil
	}
	integers := make([]uint16, len(values))
	for i, value := range values {
		integers[i] = uint16(value)
	}
	return json.Marshal(integers)
}

type DeviceResult struct {
	Interface             string             `json:"interface"`
	Requested             bool               `json:"requested"`
	Eligibility           string             `json:"eligibility"`
	Exclusion             string             `json:"exclusion"`
	Status                string             `json:"status"`
	Error                 string             `json:"error"`
	Accepted              bool               `json:"accepted"`
	StartedMilliseconds   int64              `json:"started_ms"`
	FinishedMilliseconds  int64              `json:"finished_ms"`
	CloseConfirmed        bool               `json:"close_confirmed"`
	FinalBindingConfirmed bool               `json:"final_binding_confirmed"`
	Observation           *SampleObservation `json:"observation"`
}
