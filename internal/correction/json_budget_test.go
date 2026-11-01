package correction

import (
	"encoding/json"
	"strings"
	"testing"
)

// Valid JSON is decoded into any, so typed-schema rejection cannot mask a
// removed resource guard. The scalar sits at eight array levels; token counts
// include both array delimiters and each scalar number.
func TestCorrectionDecoderResourceBudgets(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     string
		accept  bool
		depth   int
		scalars int
	}{
		{"depth_at_limit", strings.Repeat("[", 8) + "0" + strings.Repeat("]", 8), true, 8, 0},
		{"depth_over_limit", strings.Repeat("[", 9) + "0" + strings.Repeat("]", 9), false, 9, 0},
		{"tokens_at_limit", "[" + strings.Repeat("0,", 253) + "0]", true, 0, 254},
		{"tokens_over_limit", "[" + strings.Repeat("0,", 254) + "0]", false, 0, 255},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(tc.raw)
			if !json.Valid(raw) || len(raw) >= MaximumRequestBytes {
				t.Fatal("budget fixture lost valid, under-byte-cap JSON prerequisite")
			}
			if tc.scalars != 0 && (strings.Count(tc.raw, ",") != tc.scalars-1 || strings.Count(tc.raw, "0") != tc.scalars || len(raw) != 2*tc.scalars+1) {
				t.Fatal("literal token-count prerequisite differs")
			}
			if tc.depth != 0 && (strings.Count(tc.raw, "[") != tc.depth || strings.Count(tc.raw, "]") != tc.depth || len(raw) != 2*tc.depth+1) {
				t.Fatal("literal depth prerequisite differs")
			}
			var decoded any
			err := decodeBounded(raw, MaximumRequestBytes, &decoded)
			if !tc.accept {
				if err == nil || decoded != nil {
					t.Fatal("over-budget JSON produced a decoded value")
				}
				return
			}
			if err != nil {
				t.Fatal("exact-budget JSON rejected")
			}
			if tc.depth != 0 {
				value := decoded
				for level := 0; level < tc.depth; level++ {
					array, ok := value.([]any)
					if !ok || len(array) != 1 {
						t.Fatal("accepted depth shape differs")
					}
					value = array[0]
				}
				number, ok := value.(float64)
				if !ok || number != 0 {
					t.Fatal("accepted depth terminal differs")
				}
			} else {
				array, ok := decoded.([]any)
				if !ok || len(array) != 254 {
					t.Fatal("accepted token shape differs")
				}
				for _, value := range array {
					number, ok := value.(float64)
					if !ok || number != 0 {
						t.Fatal("accepted token scalar differs")
					}
				}
			}
		})
	}
}
