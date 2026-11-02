package render

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

const capacityProjectionEqual = "OS-visible total equals the sum of reported firmware entries."
const capacityProjectionNotComparable = "Not comparable from these observations."

func capacityProjectionRows(os, first, second string) []app.ComponentRecord {
	rows := []app.ComponentRecord{{Type: inventory.ComponentMemory, ID: "memory:system", Attributes: []inventory.Attribute{{Name: "total_kib", Value: os}}}}
	for index, value := range []string{first, second} {
		rows = append(rows, app.ComponentRecord{Type: inventory.ComponentMemoryDevice, ID: "memory-device:" + strconv.Itoa(index), Attributes: []inventory.Attribute{
			{Name: "observation_source", Value: "smbios_type17"}, {Name: "capacity_bytes_state", Value: "reported"}, {Name: "capacity_bytes", Value: value}, {Name: "memory_type_state", Value: "reported"}, {Name: "memory_type", Value: "ddr4"},
		}})
	}
	return rows
}
func capacityProjectionSet(row *app.ComponentRecord, name, value string, remove bool) {
	out := make([]inventory.Attribute, 0, len(row.Attributes)+1)
	found := false
	for _, attribute := range row.Attributes {
		if attribute.Name == name {
			found = true
			if !remove {
				out = append(out, inventory.Attribute{Name: name, Value: value})
			}
		} else {
			out = append(out, attribute)
		}
	}
	if !found && !remove {
		out = append(out, inventory.Attribute{Name: name, Value: value})
	}
	row.Attributes = out
}
func capacityProjectionSummary(rows []app.ComponentRecord) memoryCapacityDocument {
	copied := append([]app.ComponentRecord(nil), rows...)
	sort.Slice(copied, func(i, j int) bool {
		if copied[i].Type != copied[j].Type {
			return copied[i].Type < copied[j].Type
		}
		return copied[i].ID < copied[j].ID
	})
	return memoryCapacityReportDocument(copied)
}
func capacityProjectionPositive(t *testing.T) []app.ComponentRecord {
	t.Helper()
	rows := capacityProjectionRows("16777216", "8589934592", "8589934592")
	d := capacityProjectionSummary(rows)
	if d.Relationship != capacityProjectionEqual || d.FirmwareCapacity != "17179869184 bytes (16 GiB)" || d.Entries != 2 || d.Populated != 2 || d.Invalid != 0 {
		t.Fatalf("independent valid prerequisite: %+v", d)
	}
	return rows
}

func TestMemoryCapacityProjectionLiteralRelationships(t *testing.T) {
	for _, tc := range []struct{ name, os, visible, relationship string }{
		{"equal", "16777216", "17179869184 bytes (16 GiB)", "OS-visible total equals the sum of reported firmware entries."},
		{"lower", "15728640", "16106127360 bytes (15 GiB)", "OS-visible memory is lower than the sum of reported firmware entries by 1073741824 bytes (1 GiB)."},
		{"higher", "17825792", "18253611008 bytes (17 GiB)", "OS-visible memory is higher than the sum of reported firmware entries by 1073741824 bytes (1 GiB)."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := capacityProjectionPositive(t)
			capacityProjectionSet(&rows[0], "total_kib", tc.os, false)
			d := capacityProjectionSummary(rows)
			if d.FirmwareLabel != "Sum of reported firmware entries" || d.OSKiB != tc.os+" KiB" || d.OSCapacity != tc.visible || d.Relationship != tc.relationship {
				t.Fatalf("literal units or relationship: %+v", d)
			}
		})
	}
}

func TestMemoryCapacityProjectionCheckedArithmetic(t *testing.T) {
	for _, tc := range []struct{ name, os, a, b, firmware, visible, relationship, firmwareStatus, osStatus string }{
		{"above_float_exact", "8796093022208", "9007199254740993", "0", "9007199254740993 bytes", "9007199254740992 bytes (8192 TiB)", "OS-visible memory is lower than the sum of reported firmware entries by 1 bytes.", "Complete reported capacities", "Reported"},
		{"maximum_sum", "18014398509481983", "18446744073709551614", "1", "18446744073709551615 bytes", "18446744073709550592 bytes (18014398509481983 KiB)", "OS-visible memory is lower than the sum of reported firmware entries by 1023 bytes.", "Complete reported capacities", "Reported"},
		{"sum_overflow", "18014398509481983", "18446744073709551614", "2", "Unavailable", "18446744073709550592 bytes (18014398509481983 KiB)", "Not comparable from these observations.", "Overflow", "Reported"},
		{"maximum_kib", "18014398509481983", "18446744073709550592", "0", "18446744073709550592 bytes (18014398509481983 KiB)", "18446744073709550592 bytes (18014398509481983 KiB)", "OS-visible total equals the sum of reported firmware entries.", "Complete reported capacities", "Reported"},
		{"kib_overflow", "18014398509481984", "8589934592", "8589934592", "17179869184 bytes (16 GiB)", "Unavailable", "Not comparable from these observations.", "Complete reported capacities", "Overflow"},
		{"decimal_overflow", "16777216", "18446744073709551616", "0", "Unavailable", "17179869184 bytes (16 GiB)", "Not comparable from these observations.", "Overflow", "Reported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capacityProjectionPositive(t)
			d := capacityProjectionSummary(capacityProjectionRows(tc.os, tc.a, tc.b))
			if d.FirmwareCapacity != tc.firmware || d.OSCapacity != tc.visible || d.Relationship != tc.relationship || d.FirmwareStatus != tc.firmwareStatus || d.OSStatus != tc.osStatus {
				t.Fatalf("checked unsigned arithmetic: %+v", d)
			}
		})
	}
}

func TestMemoryCapacityProjectionDecimalGrammar(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		valid       bool
		number      uint64
	}{
		{"zero", "0", true, 0}, {"one", "1", true, 1}, {"maximum", "18446744073709551615", true, 18446744073709551615},
		{"empty", "", false, 0}, {"space", " 1", false, 0}, {"newline", "1\n", false, 0}, {"plus", "+1", false, 0}, {"negative", "-1", false, 0}, {"leading_zero", "01", false, 0}, {"fraction", "1.0", false, 0}, {"exponent", "1e3", false, 0}, {"unit", "1 KiB", false, 0}, {"unicode_digit", "\u0661", false, 0}, {"too_long", "100000000000000000000", false, 0}, {"overflow", "18446744073709551616", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			number, valid, _ := memoryCapacityDecimal(tc.value)
			if valid != tc.valid || valid && number != tc.number {
				t.Fatalf("decimal %q: %d %t", tc.value, number, valid)
			}
		})
	}
}

func TestMemoryCapacityProjectionPartialStates(t *testing.T) {
	for _, state := range []string{"unknown", "unavailable", "unsupported", "malformed"} {
		t.Run(state, func(t *testing.T) {
			rows := capacityProjectionPositive(t)
			capacityProjectionSet(&rows[2], "capacity_bytes_state", state, false)
			capacityProjectionSet(&rows[2], "capacity_bytes", "", true)
			d := capacityProjectionSummary(rows)
			counts := map[string]int{"unknown": d.Unknown, "unavailable": d.Unavailable, "unsupported": d.Unsupported, "malformed": d.Malformed}
			if counts[state] != 1 || d.Unknown+d.Unavailable+d.Unsupported+d.Malformed != 1 || d.Invalid != 0 || d.FirmwareLabel != "Reported-entry subtotal (partial)" || d.FirmwareCapacity != "8589934592 bytes (8 GiB)" || d.Populated != 1 || d.Unpopulated != 0 || d.Relationship != capacityProjectionNotComparable {
				t.Fatalf("partial state collapsed into zero or error: %+v", d)
			}
		})
	}
}

func TestMemoryCapacityProjectionAmbiguousAttributes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func([]app.ComponentRecord) []app.ComponentRecord
	}{
		{"duplicate_source", func(r []app.ComponentRecord) []app.ComponentRecord {
			r[2].Attributes = append(r[2].Attributes, inventory.Attribute{Name: "observation_source", Value: "smbios_type17"})
			return r
		}},
		{"duplicate_state", func(r []app.ComponentRecord) []app.ComponentRecord {
			r[2].Attributes = append(r[2].Attributes, inventory.Attribute{Name: "capacity_bytes_state", Value: "reported"})
			return r
		}},
		{"duplicate_capacity", func(r []app.ComponentRecord) []app.ComponentRecord {
			r[2].Attributes = append(r[2].Attributes, inventory.Attribute{Name: "capacity_bytes", Value: "8589934592"})
			return r
		}},
		{"conflicting_capacity", func(r []app.ComponentRecord) []app.ComponentRecord {
			r[2].Attributes = append(r[2].Attributes, inventory.Attribute{Name: "capacity_bytes", Value: "1"})
			return r
		}},
		{"duplicate_type_state", func(r []app.ComponentRecord) []app.ComponentRecord {
			r[2].Attributes = append(r[2].Attributes, inventory.Attribute{Name: "memory_type_state", Value: "reported"})
			return r
		}},
		{"duplicate_type", func(r []app.ComponentRecord) []app.ComponentRecord {
			r[2].Attributes = append(r[2].Attributes, inventory.Attribute{Name: "memory_type", Value: "ddr4"})
			return r
		}},
		{"missing_source", func(r []app.ComponentRecord) []app.ComponentRecord {
			capacityProjectionSet(&r[2], "observation_source", "", true)
			return r
		}},
		{"wrong_source", func(r []app.ComponentRecord) []app.ComponentRecord {
			capacityProjectionSet(&r[2], "observation_source", "other", false)
			return r
		}},
		{"missing_state", func(r []app.ComponentRecord) []app.ComponentRecord {
			capacityProjectionSet(&r[2], "capacity_bytes_state", "", true)
			return r
		}},
		{"missing_capacity", func(r []app.ComponentRecord) []app.ComponentRecord {
			capacityProjectionSet(&r[2], "capacity_bytes", "", true)
			return r
		}},
		{"malformed_reported_capacity", func(r []app.ComponentRecord) []app.ComponentRecord {
			capacityProjectionSet(&r[2], "capacity_bytes", "not-a-number", false)
			return r
		}},
		{"missing_id", func(r []app.ComponentRecord) []app.ComponentRecord { r[2].ID = ""; return r }},
		{"contradictory_unknown", func(r []app.ComponentRecord) []app.ComponentRecord {
			capacityProjectionSet(&r[2], "capacity_bytes_state", "unknown", false)
			return r
		}},
		{"duplicate_identity", func(r []app.ComponentRecord) []app.ComponentRecord { r[2].ID = r[1].ID; return r }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := capacityProjectionSummary(tc.mutate(capacityProjectionPositive(t)))
			if d.Entries != 2 || d.Invalid == 0 || d.FirmwareCapacity != "Unavailable" || d.FirmwareStatus != "Ambiguous or invalid observations" || d.Relationship != capacityProjectionNotComparable {
				t.Fatalf("ambiguous rows admitted: %+v", d)
			}
		})
	}
}

func TestMemoryCapacityProjectionOSBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		mutate       func([]app.ComponentRecord) []app.ComponentRecord
	}{
		{"absent", "Unavailable", func(r []app.ComponentRecord) []app.ComponentRecord { return r[1:] }},
		{"zero", "Reported zero", func(r []app.ComponentRecord) []app.ComponentRecord {
			capacityProjectionSet(&r[0], "total_kib", "0", false)
			return r
		}},
		{"missing_attribute", "Malformed observation", func(r []app.ComponentRecord) []app.ComponentRecord {
			capacityProjectionSet(&r[0], "total_kib", "", true)
			return r
		}},
		{"malformed", "Malformed observation", func(r []app.ComponentRecord) []app.ComponentRecord {
			capacityProjectionSet(&r[0], "total_kib", "16 GiB", false)
			return r
		}},
		{"wrong_identity", "Unsupported observation", func(r []app.ComponentRecord) []app.ComponentRecord { r[0].ID = "memory:other"; return r }},
		{"duplicate_identity", "Ambiguous observations", func(r []app.ComponentRecord) []app.ComponentRecord { return append(r, r[0]) }},
		{"different_second_identity", "Ambiguous observations", func(r []app.ComponentRecord) []app.ComponentRecord {
			other := r[0]
			other.ID = "memory:other"
			return append(r, other)
		}},
		{"duplicate_attribute", "Ambiguous observations", func(r []app.ComponentRecord) []app.ComponentRecord {
			r[0].Attributes = append(r[0].Attributes, inventory.Attribute{Name: "total_kib", Value: "16777216"})
			return r
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := capacityProjectionSummary(tc.mutate(capacityProjectionPositive(t)))
			if d.OSStatus != tc.status || d.FirmwareCapacity != "17179869184 bytes (16 GiB)" || d.Relationship != capacityProjectionNotComparable {
				t.Fatalf("OS authority: %+v", d)
			}
			if tc.name == "zero" {
				if d.OSCapacity != "0 bytes" {
					t.Fatal("zero observation lost")
				}
			} else if d.OSCapacity != "Unavailable" {
				t.Fatal("unadmitted OS amount displayed")
			}
		})
	}
}

func TestMemoryCapacityProjectionEmptyAndMissing(t *testing.T) {
	for _, name := range []string{"absent_firmware", "all_empty", "empty_type_unknown", "all_unknown"} {
		t.Run(name, func(t *testing.T) {
			rows := capacityProjectionPositive(t)
			switch name {
			case "absent_firmware":
				rows = rows[:1]
			case "all_empty":
				capacityProjectionSet(&rows[1], "capacity_bytes", "0", false)
				capacityProjectionSet(&rows[2], "capacity_bytes", "0", false)
			case "empty_type_unknown":
				capacityProjectionSet(&rows[0], "total_kib", "8388608", false)
				capacityProjectionSet(&rows[2], "capacity_bytes", "0", false)
				capacityProjectionSet(&rows[2], "memory_type_state", "unknown", false)
				capacityProjectionSet(&rows[2], "memory_type", "", true)
			case "all_unknown":
				for i := 1; i < len(rows); i++ {
					capacityProjectionSet(&rows[i], "capacity_bytes_state", "unknown", false)
					capacityProjectionSet(&rows[i], "capacity_bytes", "", true)
				}
			}
			d := capacityProjectionSummary(rows)
			switch name {
			case "absent_firmware":
				if d.Entries != 0 || d.FirmwareStatus != "Unavailable" || d.FirmwareCapacity != "Unavailable" {
					t.Fatalf("absence: %+v", d)
				}
			case "all_empty":
				if d.Entries != 2 || d.Unpopulated != 2 || d.Populated != 0 || d.FirmwareCapacity != "0 bytes" {
					t.Fatalf("empty slots: %+v", d)
				}
			case "empty_type_unknown":
				if d.Unpopulated != 1 || d.Populated != 1 || d.Relationship != capacityProjectionEqual {
					t.Fatalf("empty slot type blocked valid populated row: %+v", d)
				}
			case "all_unknown":
				if d.Unknown != 2 || d.Unpopulated != 0 || d.Invalid != 0 || d.FirmwareCapacity != "Unavailable" || d.FirmwareStatus != "Partial reported capacities" {
					t.Fatalf("unknown became empty/invalid: %+v", d)
				}
			}
			if name != "empty_type_unknown" && d.Relationship != capacityProjectionNotComparable {
				t.Fatal("non-comparable observations acquired a relationship")
			}
		})
	}
}

func TestMemoryCapacityProjectionTypeBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		remove      bool
		state       string
		removeState bool
	}{
		{"mixed", "ddr5", false, "reported", false}, {"nonvolatile", "logical_nonvolatile_device", false, "reported", false}, {"unknown", "fixture-unknown", false, "reported", false}, {"missing", "", true, "reported", false}, {"unknown_state", "ddr4", false, "unknown", false}, {"missing_state", "ddr4", false, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := capacityProjectionPositive(t)
			capacityProjectionSet(&rows[2], "memory_type", tc.value, tc.remove)
			capacityProjectionSet(&rows[2], "memory_type_state", tc.state, tc.removeState)
			d := capacityProjectionSummary(rows)
			if d.FirmwareCapacity != "17179869184 bytes (16 GiB)" || d.Invalid != 0 || d.Relationship != capacityProjectionNotComparable {
				t.Fatalf("type policy: %+v", d)
			}
		})
	}
}

func capacityProjectionMany(count int) []app.ComponentRecord {
	rows := capacityProjectionRows(strconv.Itoa(count), "1024", "1024")[:1]
	for index := 0; index < count; index++ {
		row := capacityProjectionRows("0", "1024", "0")[1]
		row.ID = "memory-device:" + strconv.Itoa(index)
		rows = append(rows, row)
	}
	return rows
}
func TestMemoryCapacityProjectionAllInstances(t *testing.T) {
	for _, tc := range []struct {
		name     string
		count    int
		capacity string
	}{{"instances_325", 325, "332800 bytes (325 KiB)"}, {"instances_1025", 1025, "1049600 bytes (1025 KiB)"}} {
		t.Run(tc.name, func(t *testing.T) {
			d := capacityProjectionSummary(capacityProjectionMany(tc.count))
			if d.Entries != tc.count || d.Populated != tc.count || d.FirmwareCapacity != tc.capacity || d.Relationship != capacityProjectionEqual {
				t.Fatalf("instance loss: %+v", d)
			}
		})
	}
}
func TestMemoryCapacityProjectionLateFaults(t *testing.T) {
	for _, name := range []string{"unknown_after_325", "duplicate_after_325", "overflow_after_325"} {
		t.Run(name, func(t *testing.T) {
			rows := capacityProjectionMany(401)
			rows[401].ID = "memory-device:zz-final"
			if capacityProjectionSummary(rows).Relationship != capacityProjectionEqual {
				t.Fatal("large positive prerequisite")
			}
			switch name {
			case "unknown_after_325":
				capacityProjectionSet(&rows[401], "capacity_bytes_state", "unknown", false)
				capacityProjectionSet(&rows[401], "capacity_bytes", "", true)
			case "duplicate_after_325":
				rows[400].ID = rows[401].ID
			case "overflow_after_325":
				capacityProjectionSet(&rows[401], "capacity_bytes", "18446744073709551615", false)
			}
			late := make([]string, 0, 401)
			for _, row := range rows[1:] {
				late = append(late, row.ID)
			}
			sort.Strings(late)
			if len(late) != 401 || late[400] != "memory-device:zz-final" {
				t.Fatal("fault row is not last after sorting")
			}
			for _, id := range late[:325] {
				if id == "memory-device:zz-final" {
					t.Fatal("fault row occurs before the retained-instance boundary")
				}
			}
			d := capacityProjectionSummary(rows)
			if d.Entries != 401 || d.Relationship != capacityProjectionNotComparable {
				t.Fatalf("late fault discarded: %+v", d)
			}
			switch name {
			case "unknown_after_325":
				if d.Unknown != 1 || d.Populated != 400 || d.Invalid != 0 || d.FirmwareCapacity != "409600 bytes (400 KiB)" {
					t.Fatalf("late partial: %+v", d)
				}
			case "duplicate_after_325":
				if d.Invalid == 0 || d.FirmwareCapacity != "Unavailable" {
					t.Fatalf("late duplicate: %+v", d)
				}
			case "overflow_after_325":
				if d.FirmwareStatus != "Overflow" || d.FirmwareCapacity != "Unavailable" {
					t.Fatalf("late overflow: %+v", d)
				}
			}
		})
	}
}

func TestMemoryCapacityProjectionRenderNonmutation(t *testing.T) {
	rows := capacityProjectionPositive(t)
	capacityProjectionSet(&rows[1], "irrelevant_fixture_attribute", "fixture-provider-marker", false)
	rows[0], rows[2] = rows[2], rows[0]
	payload := app.InventoryPayload{SchemaVersion: 1, Components: rows, SystemFingerprint: app.SystemFingerprint{Algorithm: "hmac-sha256", KeyEpoch: "fixture", KeyID: "fixture-key", Value: "fixture-value"}, TestResults: []app.TestResult{{ID: "fixture.observation", Version: 1, Status: app.TestInconclusive, Evidence: json.RawMessage(`{"unchanged":true}`)}}}
	before, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	html, err := InventoryMemoryCapacityHTMLForVersion(payload, "0.0.1-fixture")
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := InventoryMemoryCapacityPDFForVersion(payload, "0.0.1-fixture")
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(payload)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("projection changed observations, identities, order, fingerprint or outcome")
	}
	if !bytes.Contains(html, []byte("Memory capacity observations")) || !bytes.Contains(pdf, []byte("17179869184 bytes")) {
		t.Fatal("new shared summary disconnected")
	}
	legacy, err := InventoryHTMLForVersion(payload, "0.0.1-fixture")
	if err != nil || bytes.Contains(legacy, []byte("Memory capacity observations")) {
		t.Fatal("legacy renderer silently acquired summary")
	}
}

// Decode only text-show lines emitted by the existing PDF writer, so wrapping
// cannot hide a missing caveat. This is a test reader, never product admission.
func capacityProjectionPDFText(t *testing.T, pdf []byte) string {
	t.Helper()
	var text strings.Builder
	for _, line := range strings.Split(string(pdf), "\n") {
		if !strings.HasPrefix(line, "(") || !strings.HasSuffix(line, ") Tj") {
			continue
		}
		literal := line[1 : len(line)-4]
		for i := 0; i < len(literal); i++ {
			if literal[i] != '\\' {
				text.WriteByte(literal[i])
				continue
			}
			if i+3 < len(literal) && literal[i+1] >= '0' && literal[i+1] <= '7' && literal[i+2] >= '0' && literal[i+2] <= '7' && literal[i+3] >= '0' && literal[i+3] <= '7' {
				n := (literal[i+1]-'0')*64 + (literal[i+2]-'0')*8 + literal[i+3] - '0'
				text.WriteByte(n)
				i += 3
			} else if i+1 < len(literal) {
				i++
				text.WriteByte(literal[i])
			} else {
				t.Fatal("truncated emitted PDF string")
			}
		}
		text.WriteByte(' ')
	}
	return strings.Join(strings.Fields(text.String()), " ")
}
func TestMemoryCapacityProjectionSharedPresentation(t *testing.T) {
	for _, name := range []string{"ordinary", "linked"} {
		t.Run(name, func(t *testing.T) {
			payload := app.InventoryPayload{SchemaVersion: 1, Components: capacityProjectionPositive(t)}
			var html, pdf []byte
			var err error
			if name == "ordinary" {
				html, err = InventoryMemoryCapacityHTMLForVersion(payload, "0.0.1-fixture")
				if err == nil {
					pdf, err = InventoryMemoryCapacityPDFForVersion(payload, "0.0.1-fixture")
				}
			} else {
				previous := report.PreviousReport{ManifestDigest: "sha256:" + strings.Repeat("a", 64), BundleSHA256: "sha256:" + strings.Repeat("b", 64)}
				html, err = InventoryMemoryCapacityRetestHTMLForVersion(payload, "0.0.1-fixture", previous)
				if err == nil {
					pdf, err = InventoryMemoryCapacityRetestPDFForVersion(payload, "0.0.1-fixture", previous)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			pdfText := capacityProjectionPDFText(t, pdf)
			for _, expected := range []string{"Sum of reported firmware entries", "17179869184 bytes (16 GiB)", "16777216 KiB", capacityProjectionEqual, "Firmware entries and an OS-visible observation do not prove installed or usable RAM, complete module coverage, or hardware health. Reserved memory, firmware coverage, differing scopes, and acquisition timing can affect the relationship. VM observations remain VM evidence."} {
				if !bytes.Contains(html, []byte(expected)) || !strings.Contains(pdfText, expected) {
					t.Fatalf("shared view omitted %q", expected)
				}
			}
		})
	}
}
