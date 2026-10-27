package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
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

type reportClaim int

const (
	thermalClaim reportClaim = iota
	keyboardClaim
	anySessionClaim
)

func hasReportClaim(encoded []byte, kind reportClaim) bool {
	if len(encoded) == 0 {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	found, _, err := findReportClaim(decoder, 0, kind)
	if found || err != nil {
		return true
	}
	_, err = decoder.Token()
	return err != io.EOF
}

func findReportClaim(decoder *json.Decoder, depth int, kind reportClaim) (bool, json.Token, error) {
	if depth > 64 {
		return false, nil, errors.New("report claim nesting exceeds the supported limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return false, nil, err
	}
	switch token {
	case json.Delim('{'):
		keyboardID, newVersion := false, false
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return false, nil, err
			}
			name, ok := key.(string)
			if !ok {
				return false, nil, errors.New("invalid report claim member")
			}
			if kind == anySessionClaim && strings.EqualFold(name, "session") || kind == thermalClaim && (strings.EqualFold(name, "thermal") || strings.EqualFold(name, "local_thermal_sources")) {
				return true, nil, nil
			}
			found, scalar, err := findReportClaim(decoder, depth+1, kind)
			if found || err != nil {
				return found, nil, err
			}
			if kind == keyboardClaim {
				if value, ok := scalar.(string); ok && strings.EqualFold(name, "id") && strings.EqualFold(value, "input.keyboard.quick") {
					keyboardID = true
				}
				if strings.EqualFold(name, "version") {
					if value, ok := scalar.(json.Number); ok {
						newVersion = newVersion || value.String() != "1"
					} else {
						newVersion = true
					}
				}
				if keyboardID && newVersion {
					return true, nil, nil
				}
			}
		}
		_, err = decoder.Token()
		return false, nil, err
	case json.Delim('['):
		for decoder.More() {
			found, _, err := findReportClaim(decoder, depth+1, kind)
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
		if kind == anySessionClaim {
			name, _, versioned := strings.Cut(value, "/")
			return ok && versioned && strings.EqualFold(name, "hhsoft.pc-tester.quick-session"), token, nil
		}
		return ok && (sessionevidence.IsStrictSchema(value) || strings.EqualFold(value, sessionevidence.SchemaV4) ||
			strings.EqualFold(value, sessionevidence.Schema) || strings.EqualFold(value, "hhsoft.pc-tester.run-thermal/v1")), token, nil
	}
}
