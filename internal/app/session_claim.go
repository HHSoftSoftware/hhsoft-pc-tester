package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

// HasSessionThermalClaim selects strict validation; it does not accept evidence.
// Inspect every decoded token before a last-wins decoder can discard a claim.
// Malformed containers and case-fold aliases must not recover the opaque,
// historical inventory-only bundle profile.
func HasSessionThermalClaim(encoded []byte) bool {
	return hasReportClaim(encoded, thermalClaim)
}

// HasKeyboardClaim selects the strict report profile before duplicate fields or
// malformed containers can hide functional evidence or a new definition claim.
// Historical unsupported keyboard definition 1 remains outside this profile.
func HasKeyboardClaim(encoded []byte) bool {
	return hasReportClaim(encoded, keyboardClaim)
}

// HasCPUScalarClaim selects strict CPU-v3/definition-2 validation before decoding can discard aliases or duplicate claims.
func HasCPUScalarClaim(encoded []byte) bool { return hasReportClaim(encoded, cpuScalarClaim) }

// HasMemoryClaim selects strict v3 validation before aliases or duplicates can hide it.
func HasMemoryClaim(encoded []byte) bool { return hasReportClaim(encoded, memoryClaim) }

// HasPointerClaim selects strict validation for functional pointer evidence or
// a newer definition, including claims hidden by duplicate fields or aliases.
// ID-only session workers do not claim a definition or observed wheel coverage.
func HasPointerClaim(encoded []byte) bool {
	return hasReportClaim(encoded, pointerClaim)
}

// HasTouchpadClaim selects strict validation, including malformed definitions.
// Historical definition 1 and genuine ID-only worker references retain their profile.
func HasTouchpadClaim(encoded []byte) bool {
	if len(encoded) > maxTestEvidenceBytes || !utf8.Valid(encoded) {
		return true
	}
	return hasReportClaim(encoded, touchpadClaim)
}

// HasTouchscreenClaim selects strict validation for DIRECT functional claims.
// Historical definition 1 and genuine worker references retain their profile.
func HasTouchscreenClaim(encoded []byte) bool {
	if len(encoded) > maxTestEvidenceBytes || !utf8.Valid(encoded) {
		return true
	}
	return hasReportClaim(encoded, touchscreenClaim)
}

type reportClaim int

const (
	thermalClaim reportClaim = iota
	keyboardClaim
	anySessionClaim
	pointerClaim
	cpuScalarClaim
	memoryClaim
	touchpadClaim
	touchscreenClaim
)

func hasReportClaim(encoded []byte, kind reportClaim) bool {
	if len(encoded) == 0 {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	found, _, err := findReportClaim(decoder, 0, kind, false)
	if found || err != nil {
		return true
	}
	_, err = decoder.Token()
	return err != io.EOF
}

func findReportClaim(decoder *json.Decoder, depth int, kind reportClaim, definitionRow bool) (bool, json.Token, error) {
	functionalInput := kind == touchpadClaim || kind == touchscreenClaim
	if depth > 64 {
		return false, nil, errors.New("report claim nesting exceeds the supported limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return false, nil, err
	}
	switch token {
	case json.Delim('{'):
		definitionID, newVersion, versionSeen := false, false, false
		definitionShape := definitionRow
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return false, nil, err
			}
			name, ok := key.(string)
			if !ok {
				return false, nil, errors.New("invalid report claim member")
			}
			if functionalInput && !strings.EqualFold(name, "id") && !strings.EqualFold(name, "state") && !strings.EqualFold(name, "outcome") && !strings.EqualFold(name, "timing") {
				definitionShape = true
			}
			if kind == anySessionClaim && strings.EqualFold(name, "session") || kind == thermalClaim && (strings.EqualFold(name, "thermal") || strings.EqualFold(name, "local_thermal_sources")) {
				return true, nil, nil
			}
			if kind == pointerClaim && (strings.EqualFold(name, "wheel_axes") || strings.EqualFold(name, "wheels")) {
				return true, nil, nil
			}
			if kind == cpuScalarClaim && (strings.EqualFold(name, "scalar") || strings.EqualFold(name, "calculation_failures")) {
				return true, nil, nil
			}
			if kind == memoryClaim && strings.EqualFold(name, "memory_coverage") {
				return true, nil, nil
			}
			found, scalar, err := findReportClaim(decoder, depth+1, kind, functionalInput && strings.EqualFold(name, "test_results"))
			if found || err != nil {
				return found, nil, err
			}
			if kind == keyboardClaim || kind == pointerClaim || kind == cpuScalarClaim || kind == memoryClaim || functionalInput {
				wantedID := "input.keyboard.quick"
				if kind == pointerClaim {
					wantedID = "input.pointer.quick"
				}
				if kind == cpuScalarClaim {
					wantedID = "cpu.quick"
				}
				if kind == memoryClaim {
					wantedID = "memory.quick"
				}
				if kind == touchpadClaim {
					wantedID = touchpadReportID
				}
				if kind == touchscreenClaim {
					wantedID = touchscreenReportID
				}
				if value, ok := scalar.(string); ok && strings.EqualFold(name, "id") && strings.EqualFold(value, wantedID) {
					definitionID = true
					if functionalInput && (name != "id" || value != wantedID) {
						newVersion = true
					}
				}
				if strings.EqualFold(name, "version") {
					versionSeen = true
					if functionalInput && name != "version" {
						newVersion = true
					}
					if value, ok := scalar.(json.Number); ok {
						newVersion = newVersion || value.String() != "1"
					} else {
						newVersion = true
					}
				}
				if definitionID && newVersion {
					return true, nil, nil
				}
			}
		}
		if functionalInput && definitionShape && definitionID && !versionSeen {
			return true, nil, nil
		}
		_, err = decoder.Token()
		return false, nil, err
	case json.Delim('['):
		for decoder.More() {
			found, _, err := findReportClaim(decoder, depth+1, kind, definitionRow)
			if found || err != nil {
				return found, nil, err
			}
		}
		_, err = decoder.Token()
		return false, nil, err
	default:
		value, ok := token.(string)
		if kind == keyboardClaim {
			return ok && strings.EqualFold(value, inputtest.KeyboardSchema), token, nil
		}
		if kind == pointerClaim {
			name, _, versioned := strings.Cut(value, "/")
			return ok && versioned && strings.EqualFold(name, "hhsoft.pc-tester.pointer-functional"), token, nil
		}
		if kind == touchpadClaim {
			name, _, _ := strings.Cut(value, "/")
			return ok && strings.EqualFold(name, "hhsoft.pc-tester.touchpad-functional"), token, nil
		}
		if kind == touchscreenClaim {
			name, _, _ := strings.Cut(value, "/")
			return ok && strings.EqualFold(name, "hhsoft.pc-tester.touchscreen-functional"), token, nil
		}
		if kind == cpuScalarClaim {
			name, version, versioned := strings.Cut(value, "/")
			return ok && versioned && strings.EqualFold(name, "hhsoft.pc-tester.cpu") && version != "v2", token, nil
		}
		if kind == memoryClaim {
			name, version, versioned := strings.Cut(value, "/")
			return ok && (strings.EqualFold(value, quick.MemorySuite) || versioned && strings.EqualFold(name, "hhsoft.pc-tester.memory") && version != "v2"), token, nil
		}
		if kind == anySessionClaim {
			name, _, versioned := strings.Cut(value, "/")
			return ok && versioned && strings.EqualFold(name, "hhsoft.pc-tester.quick-session"), token, nil
		}
		return ok && (sessionevidence.IsStrictSchema(value) || strings.EqualFold(value, sessionevidence.SchemaV4) ||
			strings.EqualFold(value, sessionevidence.SchemaV5) || strings.EqualFold(value, sessionevidence.SchemaV6) || strings.EqualFold(value, sessionevidence.SchemaV7) || strings.EqualFold(value, sessionevidence.SchemaV8) || strings.EqualFold(value, sessionevidence.SchemaV9) || strings.EqualFold(value, sessionevidence.SchemaV10) || strings.EqualFold(value, sessionevidence.SchemaV11) || strings.EqualFold(value, sessionevidence.SchemaV12) || strings.EqualFold(value, sessionevidence.SchemaV13) || strings.EqualFold(value, sessionevidence.SchemaV14) || strings.EqualFold(value, sessionevidence.SchemaV15) || strings.EqualFold(value, sessionevidence.SchemaV16) || strings.EqualFold(value, "hhsoft.pc-tester.run-thermal/v1")), token, nil
	}
}
