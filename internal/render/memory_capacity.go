package render

import (
	"strconv"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

const memoryCapacityCaveat = "Firmware entries and an OS-visible observation do not prove installed or usable RAM, complete module coverage, or hardware health. Reserved memory, firmware coverage, differing scopes, and acquisition timing can affect the relationship. VM observations remain VM evidence."

type memoryCapacityDocument struct {
	FirmwareStatus   string
	FirmwareLabel    string
	FirmwareCapacity string
	Entries          int
	Populated        int
	Unpopulated      int
	Unknown          int
	Unavailable      int
	Unsupported      int
	Malformed        int
	Invalid          int
	TypeStatus       string
	OSStatus         string
	OSKiB            string
	OSCapacity       string
	Relationship     string
	Caveat           string
}

type memoryCapacityFields struct {
	source, state, capacity, typeState, memoryType string
	seen                                           uint8
	duplicate                                      bool
}

func memoryCapacityAttributes(attributes []inventory.Attribute) memoryCapacityFields {
	var result memoryCapacityFields
	for _, attribute := range attributes {
		var bit uint8
		var target *string
		switch attribute.Name {
		case "observation_source":
			bit, target = 1, &result.source
		case "capacity_bytes_state":
			bit, target = 2, &result.state
		case "capacity_bytes":
			bit, target = 4, &result.capacity
		case "memory_type_state":
			bit, target = 8, &result.typeState
		case "memory_type":
			bit, target = 16, &result.memoryType
		default:
			continue
		}
		if result.seen&bit != 0 {
			result.duplicate = true
			continue
		}
		result.seen |= bit
		*target = attribute.Value
	}
	return result
}

// Capacities never pass through int, signed arithmetic, float, or normalization.
func memoryCapacityDecimal(value string) (uint64, bool, bool) {
	if len(value) == 0 || len(value) > 20 || len(value) > 1 && value[0] == '0' {
		return 0, false, false
	}
	var number uint64
	for index := 0; index < len(value); index++ {
		digit := value[index]
		if digit < '0' || digit > '9' {
			return 0, false, false
		}
		n := uint64(digit - '0')
		if number > (^uint64(0)-n)/10 {
			return 0, false, true
		}
		number = number*10 + n
	}
	return number, true, false
}

func memoryCapacityType(value string) bool {
	switch value {
	case "dram", "edram", "ram", "cdram", "3dram", "sdram", "rdram", "ddr", "ddr2", "ddr2_fb_dimm", "ddr3", "fbd2", "ddr4", "lpddr", "lpddr2", "lpddr3", "lpddr4", "hbm", "hbm2", "ddr5", "lpddr5", "hbm3", "mrdimm":
		return true
	default:
		return false
	}
}

func memoryCapacityBytes(value uint64) string {
	text := strconv.FormatUint(value, 10) + " bytes"
	if value != 0 {
		for _, unit := range []struct {
			bytes uint64
			name  string
		}{{1 << 40, "TiB"}, {1 << 30, "GiB"}, {1 << 20, "MiB"}, {1 << 10, "KiB"}} {
			if value%unit.bytes == 0 {
				return text + " (" + strconv.FormatUint(value/unit.bytes, 10) + " " + unit.name + ")"
			}
		}
	}
	return text
}

// The callers already own a complete Type/ID-sorted copy for their component
// views. Reuse that order for adjacent identity checks, without another map or
// instance limit. Every memory-device row is counted, including rejected rows.
func memoryCapacityReportDocument(components []app.ComponentRecord) memoryCapacityDocument {
	d := memoryCapacityDocument{FirmwareStatus: "Unavailable", FirmwareLabel: "Reported firmware entries", FirmwareCapacity: "Unavailable", TypeStatus: "No populated entries", OSStatus: "Unavailable", OSKiB: "Unavailable", OSCapacity: "Unavailable", Relationship: "Not comparable from these observations.", Caveat: memoryCapacityCaveat}
	var firmware, osBytes uint64
	reported, osCount := 0, 0
	aggregateInvalid, overflow, osValid, mixedTypes, typeIssue := false, false, false, false, false
	previousID, firstType := "", ""
	for _, component := range components {
		if component.Type == inventory.ComponentMemory {
			osCount++
			if osCount != 1 {
				d.OSStatus, d.OSKiB, d.OSCapacity = "Ambiguous observations", "Unavailable", "Unavailable"
				osValid = false
				continue
			}
			if component.ID != "memory:system" {
				d.OSStatus = "Unsupported observation"
				continue
			}
			seen := false
			value := ""
			for _, attribute := range component.Attributes {
				if attribute.Name != "total_kib" {
					continue
				}
				if seen {
					d.OSStatus = "Ambiguous observations"
					osValid = false
					break
				}
				seen = true
				value = attribute.Value
			}
			if d.OSStatus == "Ambiguous observations" {
				continue
			}
			kib, valid, tooLarge := memoryCapacityDecimal(value)
			if !seen || !valid {
				d.OSStatus = "Malformed observation"
				if tooLarge {
					d.OSStatus = "Overflow"
				}
				continue
			}
			if kib > (^uint64(0))/1024 {
				d.OSStatus = "Overflow"
				continue
			}
			osBytes = kib * 1024
			osValid = true
			d.OSStatus = "Reported"
			if kib == 0 {
				d.OSStatus = "Reported zero"
			}
			d.OSKiB = strconv.FormatUint(kib, 10) + " KiB"
			d.OSCapacity = memoryCapacityBytes(osBytes)
		}
		if component.Type != inventory.ComponentMemoryDevice {
			continue
		}
		d.Entries++
		duplicateID := d.Entries > 1 && component.ID == previousID
		previousID = component.ID
		fields := memoryCapacityAttributes(component.Attributes)
		if component.ID == "" || duplicateID || fields.duplicate || fields.seen&1 == 0 || fields.source != "smbios_type17" {
			d.Invalid++
			aggregateInvalid = true
			continue
		}
		if fields.state != "reported" {
			if fields.seen&4 != 0 {
				d.Invalid++
				aggregateInvalid = true
				continue
			}
			switch fields.state {
			case "unknown":
				d.Unknown++
			case "unavailable":
				d.Unavailable++
			case "unsupported":
				d.Unsupported++
			case "malformed":
				d.Malformed++
			default:
				d.Invalid++
				aggregateInvalid = true
			}
			continue
		}
		value, valid, tooLarge := memoryCapacityDecimal(fields.capacity)
		if fields.seen&4 == 0 || !valid {
			d.Invalid++
			aggregateInvalid = true
			if tooLarge {
				overflow = true
			}
			continue
		}
		reported++
		if value == 0 {
			d.Unpopulated++
			continue
		}
		d.Populated++
		if !overflow {
			if value > (^uint64(0))-firmware {
				overflow = true
			} else {
				firmware += value
			}
		}
		if fields.typeState != "reported" || !memoryCapacityType(fields.memoryType) {
			typeIssue = true
			continue
		}
		if firstType == "" {
			firstType = fields.memoryType
		} else if firstType != fields.memoryType {
			mixedTypes = true
		}
	}
	partial := d.Unknown+d.Unavailable+d.Unsupported+d.Malformed != 0
	switch {
	case d.Entries == 0:
	case overflow:
		d.FirmwareStatus = "Overflow"
	case aggregateInvalid:
		d.FirmwareStatus = "Ambiguous or invalid observations"
	case partial:
		d.FirmwareStatus = "Partial reported capacities"
	default:
		d.FirmwareStatus = "Complete reported capacities"
	}
	if reported > 0 && !aggregateInvalid && !overflow {
		d.FirmwareLabel = "Sum of reported firmware entries"
		if partial {
			d.FirmwareLabel = "Reported-entry subtotal (partial)"
		}
		d.FirmwareCapacity = memoryCapacityBytes(firmware)
	}
	switch {
	case d.Populated == 0:
	case typeIssue && mixedTypes:
		d.TypeStatus = "Mixed and unsupported or unknown populated types"
	case typeIssue:
		d.TypeStatus = "Unsupported or unknown populated types"
	case mixedTypes:
		d.TypeStatus = "Mixed populated memory types"
	default:
		d.TypeStatus = "One supported type across reported populated entries"
	}
	if d.Entries != 0 && d.Populated != 0 && !partial && !aggregateInvalid && !overflow && !typeIssue && !mixedTypes && osValid && osCount == 1 && osBytes > 0 {
		switch {
		case osBytes == firmware:
			d.Relationship = "OS-visible total equals the sum of reported firmware entries."
		case osBytes < firmware:
			d.Relationship = "OS-visible memory is lower than the sum of reported firmware entries by " + memoryCapacityBytes(firmware-osBytes) + "."
		default:
			d.Relationship = "OS-visible memory is higher than the sum of reported firmware entries by " + memoryCapacityBytes(osBytes-firmware) + "."
		}
	}
	return d
}
