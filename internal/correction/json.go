package correction

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

// The small request/addendum grammar has separate token/depth bounds. Source
// inventory retains the existing bundle bounds rather than this small grammar.
func decodeBounded(raw []byte, maximum int, value any) error {
	if len(raw) == 0 || len(raw) > maximum || !utf8.Valid(raw) || !validJSONStringEscapes(raw) {
		return ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	remaining := 256
	if uniqueValue(decoder, 0, &remaining) != nil {
		return ErrInvalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ErrInvalid
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		return ErrInvalid
	}
	return nil
}

func uniqueValue(decoder *json.Decoder, depth int, remaining *int) error {
	if depth > 8 || *remaining <= 0 {
		return ErrInvalid
	}
	*remaining--
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalid
	}
	switch token {
	case json.Delim('{'):
		seen := make(map[string]bool)
		for decoder.More() {
			if *remaining <= 0 {
				return ErrInvalid
			}
			*remaining--
			key, err := decoder.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return ErrInvalid
			}
			seen[name] = true
			if uniqueValue(decoder, depth+1, remaining) != nil {
				return ErrInvalid
			}
		}
		if *remaining <= 0 {
			return ErrInvalid
		}
		*remaining--
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return ErrInvalid
		}
	case json.Delim('['):
		for decoder.More() {
			if uniqueValue(decoder, depth+1, remaining) != nil {
				return ErrInvalid
			}
		}
		if *remaining <= 0 {
			return ErrInvalid
		}
		*remaining--
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return ErrInvalid
		}
	default:
		if _, delimiter := token.(json.Delim); delimiter {
			return ErrInvalid
		}
	}
	return nil
}

func equivalentObjects(left, right []byte) bool {
	encode := func(raw []byte) ([]byte, error) {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
			return nil, ErrSource
		}
		return json.Marshal(value)
	}
	a, err := encode(left)
	if err != nil {
		return false
	}
	b, err := encode(right)
	return err == nil && bytes.Equal(a, b)
}

// encoding/json replaces unpaired UTF-16 escapes during decoding. Inspect the
// bounded original bytes first so valid U+FFFD text stays distinguishable from
// that lossy conversion. This scanner allocates no per-string storage; the JSON
// decoder still enforces the complete JSON grammar and UTF-8 is checked above.
func validJSONStringEscapes(raw []byte) bool {
	inside := false
	for index := 0; index < len(raw); index++ {
		if !inside {
			if raw[index] == '"' {
				inside = true
			}
			continue
		}
		if raw[index] == '"' {
			inside = false
			continue
		}
		if raw[index] != '\\' {
			continue
		}
		index++
		if index >= len(raw) {
			return false
		}
		if raw[index] != 'u' {
			continue
		}
		first, ok := jsonHexUnit(raw, index+1)
		if !ok {
			return false
		}
		index += 4
		if first >= 0xd800 && first <= 0xdbff {
			if len(raw)-index <= 6 || raw[index+1] != '\\' || raw[index+2] != 'u' {
				return false
			}
			second, ok := jsonHexUnit(raw, index+3)
			if !ok || second < 0xdc00 || second > 0xdfff {
				return false
			}
			index += 6
		} else if first >= 0xdc00 && first <= 0xdfff {
			return false
		}
	}
	return !inside
}

func jsonHexUnit(raw []byte, start int) (uint16, bool) {
	if start < 0 || len(raw)-start < 4 {
		return 0, false
	}
	var value uint16
	for _, character := range raw[start : start+4] {
		value <<= 4
		switch {
		case character >= '0' && character <= '9':
			value |= uint16(character - '0')
		case character >= 'a' && character <= 'f':
			value |= uint16(character - 'a' + 10)
		case character >= 'A' && character <= 'F':
			value |= uint16(character - 'A' + 10)
		default:
			return 0, false
		}
	}
	return value, true
}
