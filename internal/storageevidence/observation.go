// Package storageevidence validates bounded storage evidence without media data.
package storageevidence

import (
	"encoding/json"
	"reflect"
	"unicode/utf8"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
)

// DecodeObservation validates a single sample payload. It is not the complete
// child protocol or an accepted result; parent lifecycle checks remain required.
func DecodeObservation(target storagetest.Target, raw []byte) (storagetest.SampleObservation, storagetest.ObservationSummary, error) {
	var value storagetest.SampleObservation
	if len(raw) == 0 || len(raw) > 8<<10 || !utf8.Valid(raw) || evidencejson.ValidateShape(raw, reflect.TypeOf(value)) != nil || json.Unmarshal(raw, &value) != nil {
		return storagetest.SampleObservation{}, storagetest.ObservationSummary{}, storagetest.ErrObservation
	}
	summary, err := storagetest.ValidateObservation(target, value)
	if err != nil {
		return storagetest.SampleObservation{}, storagetest.ObservationSummary{}, err
	}
	return value, summary, nil
}
