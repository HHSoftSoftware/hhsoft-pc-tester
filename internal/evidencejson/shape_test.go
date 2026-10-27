package evidencejson

import (
	"reflect"
	"testing"
)

func TestExactShapeRejectsFalseZeroAndSpellingBypasses(t *testing.T) {
	type observation struct {
		Cleanup bool `json:"cleanup_confirmed"`
		Count   int  `json:"count"`
	}
	kind := reflect.TypeOf(observation{})
	if err := ValidateShape([]byte(`{"cleanup_confirmed":false,"count":0}`), kind); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{}`, `null`, `{"cleanup_confirmed":null,"count":0}`,
		`{"Cleanup_confirmed":true,"count":0}`, `{"cleanup_confirmed":true,"count":0,"extra":1}`,
		`{"cleanup_confirmed":false,"cleanup_confirmed":true,"count":0}`,
		`{"cleanup_confirmed":false,"\u0063leanup_confirmed":true,"count":0}`,
		`{"cleanup_confirmed":true,"count":0} {}`,
	} {
		if ValidateShape([]byte(raw), kind) == nil {
			t.Errorf("accepted invalid shape %s", raw)
		}
	}
}
