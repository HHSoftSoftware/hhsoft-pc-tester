package storageevidence

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"unicode/utf8"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/workerexec"
)

var ErrInvalid = errors.New("invalid storage sampling evidence")

// Decode admits only the finite public result. Cleanup uncertainty retains the
// diagnostic value and an error; signing/rendering callers must reject it.
func Decode(raw []byte) (storagetest.Result, error) {
	var result storagetest.Result
	if len(raw) == 0 || len(raw) > storagetest.MaximumResultBytes || !utf8.Valid(raw) || resultShape(raw, reflect.TypeOf(result), 0) != nil || json.Unmarshal(raw, &result) != nil {
		return storagetest.Result{}, ErrInvalid
	}
	err := ValidateResult(result)
	if err != nil && !errors.Is(err, workerexec.ErrCleanupUncertain) {
		return storagetest.Result{}, err
	}
	return result, err
}

// Required nullable objects are local to this new schema. The shared legacy
// evidence shape validator and all v4/v5 wire rules remain unchanged.
func resultShape(raw []byte, kind reflect.Type, depth int) error {
	if depth > 24 {
		return ErrInvalid
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if kind == reflect.TypeOf((*storagetest.Snapshot)(nil)) || kind == reflect.TypeOf((*storagetest.SampleObservation)(nil)) || kind == reflect.TypeOf((*storagetest.LoopBacking)(nil)) {
			return nil
		}
		return ErrInvalid
	}
	switch kind.Kind() {
	case reflect.Pointer:
		return resultShape(raw, kind.Elem(), depth+1)
	case reflect.Struct:
		decoder := json.NewDecoder(bytes.NewReader(raw))
		first, err := decoder.Token()
		if err != nil || first != json.Delim('{') {
			return ErrInvalid
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < kind.NumField(); i++ {
			field := kind.Field(i)
			name := field.Tag.Get("json")
			if name == "" {
				return ErrInvalid
			}
			fields[name] = field.Type
		}
		seen := map[string]bool{}
		for decoder.More() {
			token, err := decoder.Token()
			name, ok := token.(string)
			if err != nil || !ok || seen[name] || fields[name] == nil {
				return ErrInvalid
			}
			seen[name] = true
			var value json.RawMessage
			if decoder.Decode(&value) != nil || resultShape(value, fields[name], depth+1) != nil {
				return ErrInvalid
			}
		}
		last, err := decoder.Token()
		if err != nil || last != json.Delim('}') || len(seen) != len(fields) {
			return ErrInvalid
		}
		if _, err = decoder.Token(); err != io.EOF {
			return ErrInvalid
		}
	case reflect.Slice:
		var values []json.RawMessage
		if json.Unmarshal(raw, &values) != nil || values == nil {
			return ErrInvalid
		}
		for _, value := range values {
			if resultShape(value, kind.Elem(), depth+1) != nil {
				return ErrInvalid
			}
		}
	default:
		value := reflect.New(kind).Interface()
		if json.Unmarshal(raw, value) != nil {
			return ErrInvalid
		}
	}
	return nil
}

// CanonicalTypedBytes defines the journal typed digest: no LF or domain prefix.
func CanonicalTypedBytes(result storagetest.Result) ([]byte, error) {
	if err := ValidateResult(result); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw)+1 > storagetest.MaximumResultBytes {
		return nil, ErrInvalid
	}
	return raw, nil
}
