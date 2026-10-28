package sessionevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// Literal hashes were independently derived from the old declaration order,
// tags and omission rules, before the version-specific encoder was inspected.
// The existing full report fixtures remain the input, not a new encoder output.
func TestStorageReaderPreservesLiteralV4V5WireAndBindings(t *testing.T) {
	cases := []struct {
		name          string
		journalSize   int
		journalSHA    string
		integritySize int
		integritySHA  string
	}{
		{"session-v4-unavailable", 3353, "814ff985c85a59b70f544852ee34c1230fc4e70132b76b692067bbf1b23aba66", 4002, "44844f31690829e8f98fdf926fe43dc773a62d92415770c9c6c951c9172a8387"},
		{"session-v4-global-heat", 3588, "ba28b0ceb4ae7fe6cbed3165681987ee5168603a27244ddd4d633eaa1f9e103d", 4237, "233d352d0cb9dc760d0e843dde9e2c354279ba35c0444a3e2d140af3cd71c7d4"},
		{"session-v4-local-heat", 3959, "a055d889062cec874db70ec10ae5c98c90dbea850f454ea22b6fd0143e0dc53b", 4596, "6fbee7e979bf6abe051a15c9810489363eb5b672a8acdbd6bac659bc9710dd59"},
		{"session-v5-keyboard-pass", 3680, "756b15e7be2efc93edd45efa8abcaff900e29a9359a1faf880e90bc1889c93f0", 4329, "d1fcd3a34cc29a075779fedc57059d4a19c1ed196053394f812928013753ae68"},
		{"session-v5-pointer-v2-pass", 3679, "426c5cfe370aefb6e102ef5dc940812109f3ebfc6cc35f637d3958748f422c83", 4329, "8f20a2c3bfa027c1db96d59f57f6465f8e524e4a1944b82b3b133b8e568e9424"},
		{"session-v5-cpu-v3-local-heat", 4034, "9acde525abb67d0df5262b324ee84e409ab4966e4aaf44396314913c332583e9", 4671, "e1340607a6263d30747b91e1e4c8aca884d9287655cd3f3ac436683b16b025ec"},
		{"session-v5-memory-v3-local-heat", 4043, "ecf627d493d56d5b704436027a35d208d1c46619cd1b0f8bcff4d7ee71532161", 4677, "300e5cb6545dff78978b8572daa4f5ed3f1a4ed4c261f464af509dc56f2a4f55"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			raw, err := os.ReadFile("../app/testdata/" + test.name + ".json")
			if err != nil {
				t.Fatal("legacy fixture unavailable")
			}
			var payload struct {
				TestResults []struct {
					ID            string          `json:"id"`
					Version       int             `json:"version"`
					Status        string          `json:"status"`
					PlannedStatus string          `json:"planned_status"`
					Evidence      json.RawMessage `json:"evidence"`
				} `json:"test_results"`
			}
			if json.Unmarshal(raw, &payload) != nil {
				t.Fatal("legacy fixture invalid")
			}
			bindings := []ResultBinding{}
			var integrityRaw json.RawMessage
			for _, row := range payload.TestResults {
				if row.ID == "run.integrity.quick" {
					integrityRaw = row.Evidence
					continue
				}
				bindings = append(bindings, ResultBinding{ID: row.ID, Version: row.Version, PlannedStatus: row.PlannedStatus, ReportStatus: row.Status, Evidence: row.Evidence})
			}
			value, err := DecodeIntegrity(integrityRaw)
			if err != nil || value.Session == nil {
				t.Fatal("legacy integrity rejected", err)
			}
			if err := ValidateBindings(*value.Session, bindings); err != nil {
				t.Fatal("legacy bindings changed", err)
			}
			for _, encoding := range []struct {
				value any
				size  int
				sha   string
			}{{value.Session, test.journalSize, test.journalSHA}, {value, test.integritySize, test.integritySHA}} {
				encoded, err := json.Marshal(encoding.value)
				if err != nil {
					t.Fatal(err)
				}
				sum := sha256.Sum256(encoded)
				if len(encoded) != encoding.size || hex.EncodeToString(sum[:]) != encoding.sha {
					t.Fatal("old wire bytes changed", test.name)
				}
			}
		})
	}
}
