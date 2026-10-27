// Package evidencejson validates exact bounded evidence JSON shapes.
package evidencejson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
)

var ErrShape = errors.New("evidence has an invalid JSON shape")

func ValidateShape(encoded []byte, kind reflect.Type) error {
	if kind == nil || len(encoded) == 0 || len(encoded) > 1<<20 {
		return ErrShape
	}
	return validateShape(encoded, kind, 0)
}
func validateShape(encoded []byte, kind reflect.Type, depth int) error {
	if depth > 32 {
		return ErrShape
	}
	if bytes.Equal(bytes.TrimSpace(encoded), []byte("null")) {
		if kind.Kind() == reflect.Slice {
			return nil
		}
		return ErrShape
	}
	switch kind.Kind() {
	case reflect.Pointer:
		return validateShape(encoded, kind.Elem(), depth+1)
	case reflect.Struct, reflect.Map:
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		first, err := decoder.Token()
		if err != nil || first != json.Delim('{') {
			return ErrShape
		}
		fields, required := map[string]reflect.Type{}, map[string]bool{}
		if kind.Kind() == reflect.Struct {
			for index := 0; index < kind.NumField(); index++ {
				field := kind.Field(index)
				if field.Tag.Get("json") == "-" {
					continue
				}
				parts := strings.Split(field.Tag.Get("json"), ",")
				fields[parts[0]] = field.Type
				required[parts[0]] = len(parts) == 1
			}
		}
		seen := map[string]bool{}
		for decoder.More() {
			token, err := decoder.Token()
			name, ok := token.(string)
			if err != nil || !ok || seen[name] {
				return ErrShape
			}
			seen[name] = true
			fieldType := fields[name]
			if kind.Kind() == reflect.Map {
				fieldType = kind.Elem()
			}
			if fieldType == nil {
				return ErrShape
			}
			var value json.RawMessage
			if decoder.Decode(&value) != nil || validateShape(value, fieldType, depth+1) != nil {
				return ErrShape
			}
		}
		if last, err := decoder.Token(); err != nil || last != json.Delim('}') {
			return ErrShape
		}
		if _, err := decoder.Token(); err != io.EOF {
			return ErrShape
		}
		for name, mandatory := range required {
			if mandatory && !seen[name] {
				return ErrShape
			}
		}
	case reflect.Slice:
		var values []json.RawMessage
		if json.Unmarshal(encoded, &values) != nil {
			return ErrShape
		}
		for _, value := range values {
			if err := validateShape(value, kind.Elem(), depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
