package sessionevidence

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// HasStorageClaim selects strict validation; it never accepts an observation.
// Inspect tokens before duplicate members, case aliases or malformed containers
// could hide a sampling schema, source binding or new definition.
func HasStorageClaim(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	found, _, err := storageClaimToken(decoder, 0)
	if found || err != nil {
		return true
	}
	_, err = decoder.Token()
	return err != io.EOF
}

func storageClaimToken(decoder *json.Decoder, depth int) (bool, json.Token, error) {
	if depth > 64 {
		return false, nil, ErrInvalid
	}
	token, err := decoder.Token()
	if err != nil {
		return false, nil, err
	}
	switch token {
	case json.Delim('{'):
		definition, newVersion := false, false
		for decoder.More() {
			key, err := decoder.Token()
			name, ok := key.(string)
			if err != nil || !ok {
				return false, nil, ErrInvalid
			}
			if strings.EqualFold(name, "storage_source") {
				return true, nil, nil
			}
			found, scalar, err := storageClaimToken(decoder, depth+1)
			if found || err != nil {
				return found, nil, err
			}
			if value, ok := scalar.(string); ok && strings.EqualFold(name, "id") && strings.EqualFold(value, "storage.quick") {
				definition = true
			}
			if strings.EqualFold(name, "version") {
				value, ok := scalar.(json.Number)
				newVersion = newVersion || !ok || value.String() != "1"
			}
			if definition && newVersion {
				return true, nil, nil
			}
		}
		last, err := decoder.Token()
		if err != nil || last != json.Delim('}') {
			return false, nil, ErrInvalid
		}
		return false, nil, nil
	case json.Delim('['):
		for decoder.More() {
			found, _, err := storageClaimToken(decoder, depth+1)
			if found || err != nil {
				return found, nil, err
			}
		}
		last, err := decoder.Token()
		if err != nil || last != json.Delim(']') {
			return false, nil, ErrInvalid
		}
		return false, nil, nil
	default:
		value, ok := token.(string)
		if !ok {
			return false, token, nil
		}
		family, _, versioned := strings.Cut(value, "/")
		return versioned && strings.EqualFold(family, "hhsoft.pc-tester.storage-sample") ||
			strings.EqualFold(value, "fixed-buffered-three-window-repeat/v1"), token, nil
	}
}
