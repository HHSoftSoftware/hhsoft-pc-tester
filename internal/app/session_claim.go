package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// HasSessionThermalClaim selects strict validation; it does not accept evidence.
// Inspect every decoded token before a last-wins decoder can discard a claim.
// Malformed containers and case-fold aliases must not recover the opaque,
// historical inventory-only bundle profile.
func HasSessionThermalClaim(encoded []byte) bool {
	if len(encoded) == 0 {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	found, err := findSessionThermalClaim(decoder, 0)
	if found || err != nil {
		return true
	}
	_, err = decoder.Token()
	return err != io.EOF
}

func findSessionThermalClaim(decoder *json.Decoder, depth int) (bool, error) {
	if depth > 64 {
		return false, errors.New("session claim nesting exceeds the supported limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return false, err
	}
	switch token {
	case json.Delim('{'):
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return false, err
			}
			name, ok := key.(string)
			if !ok {
				return false, errors.New("invalid session claim member")
			}
			if strings.EqualFold(name, "thermal") || strings.EqualFold(name, "local_thermal_sources") {
				return true, nil
			}
			found, err := findSessionThermalClaim(decoder, depth+1)
			if found || err != nil {
				return found, err
			}
		}
		_, err = decoder.Token()
		return false, err
	case json.Delim('['):
		for decoder.More() {
			found, err := findSessionThermalClaim(decoder, depth+1)
			if found || err != nil {
				return found, err
			}
		}
		_, err = decoder.Token()
		return false, err
	default:
		value, ok := token.(string)
		return ok && (strings.EqualFold(value, "hhsoft.pc-tester.quick-session/v4") ||
			strings.EqualFold(value, "hhsoft.pc-tester.run-thermal/v1")), nil
	}
}
