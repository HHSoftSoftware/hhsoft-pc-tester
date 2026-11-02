package inventory

import "testing"

func TestPCIeMaximumSpeedKnownLiterals(t *testing.T) {
	for _, tc := range []struct {
		name, raw, state, value string
		parse                   func([]byte) (string, string)
	}{
		{"two_point_five", "2.5 GT/s PCIe", "reported", "2500", parsePCIeMaximumSpeed},
		{"two_point_five_lf", "2.5 GT/s PCIe\n", "reported", "2500", parsePCIeMaximumSpeed},
		{"five", "5.0 GT/s PCIe", "reported", "5000", parsePCIeMaximumSpeed},
		{"five_lf", "5.0 GT/s PCIe\n", "reported", "5000", parsePCIeMaximumSpeed},
		{"eight", "8.0 GT/s PCIe", "reported", "8000", parsePCIeMaximumSpeed},
		{"eight_lf", "8.0 GT/s PCIe\n", "reported", "8000", parsePCIeMaximumSpeed},
		{"sixteen", "16.0 GT/s PCIe", "reported", "16000", parsePCIeMaximumSpeed},
		{"sixteen_lf", "16.0 GT/s PCIe\n", "reported", "16000", parsePCIeMaximumSpeed},
		{"thirty_two", "32.0 GT/s PCIe", "reported", "32000", parsePCIeMaximumSpeed},
		{"thirty_two_lf", "32.0 GT/s PCIe\n", "reported", "32000", parsePCIeMaximumSpeed},
		{"sixty_four", "64.0 GT/s PCIe", "reported", "64000", parsePCIeMaximumSpeed},
		{"sixty_four_lf", "64.0 GT/s PCIe\n", "reported", "64000", parsePCIeMaximumSpeed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, value := tc.parse([]byte(tc.raw))
			if state != tc.state || value != tc.value {
				t.Fatalf("classification = %q/%q; want %q/%q", state, value, tc.state, tc.value)
			}
		})
	}
}

func TestPCIeMaximumWidthKnownLiterals(t *testing.T) {
	for _, tc := range []struct {
		name, raw, state, value string
		parse                   func([]byte) (string, string)
	}{
		{"one", "1", "reported", "1", parsePCIeMaximumWidth},
		{"one_lf", "1\n", "reported", "1", parsePCIeMaximumWidth},
		{"two", "2", "reported", "2", parsePCIeMaximumWidth},
		{"two_lf", "2\n", "reported", "2", parsePCIeMaximumWidth},
		{"four", "4", "reported", "4", parsePCIeMaximumWidth},
		{"four_lf", "4\n", "reported", "4", parsePCIeMaximumWidth},
		{"eight", "8", "reported", "8", parsePCIeMaximumWidth},
		{"eight_lf", "8\n", "reported", "8", parsePCIeMaximumWidth},
		{"twelve", "12", "reported", "12", parsePCIeMaximumWidth},
		{"twelve_lf", "12\n", "reported", "12", parsePCIeMaximumWidth},
		{"sixteen", "16", "reported", "16", parsePCIeMaximumWidth},
		{"sixteen_lf", "16\n", "reported", "16", parsePCIeMaximumWidth},
		{"thirty_two", "32", "reported", "32", parsePCIeMaximumWidth},
		{"thirty_two_lf", "32\n", "reported", "32", parsePCIeMaximumWidth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, value := tc.parse([]byte(tc.raw))
			if state != tc.state || value != tc.value {
				t.Fatalf("classification = %q/%q; want %q/%q", state, value, tc.state, tc.value)
			}
		})
	}
}

func TestPCIeMaximumCapabilityUnknownAndUnsupported(t *testing.T) {
	for _, tc := range []struct {
		name, raw, state, value string
		parse                   func([]byte) (string, string)
	}{
		{"speed_unknown", "Unknown", "unknown", "", parsePCIeMaximumSpeed},
		{"speed_unknown_lf", "Unknown\n", "unknown", "", parsePCIeMaximumSpeed},
		{"width_unknown", "255", "unknown", "", parsePCIeMaximumWidth},
		{"width_unknown_lf", "255\n", "unknown", "", parsePCIeMaximumWidth},
		{"speed_zero", "0.0 GT/s PCIe", "unsupported", "", parsePCIeMaximumSpeed},
		{"speed_unlisted_fraction", "0.1 GT/s PCIe", "unsupported", "", parsePCIeMaximumSpeed},
		{"speed_future", "128.0 GT/s PCIe", "unsupported", "", parsePCIeMaximumSpeed},
		{"speed_future_lf", "128.0 GT/s PCIe\n", "unsupported", "", parsePCIeMaximumSpeed},
		{"speed_largest_representable_tenth", "18446744073709551.6 GT/s PCIe", "unsupported", "", parsePCIeMaximumSpeed},
		{"width_reserved_zero", "0", "unsupported", "", parsePCIeMaximumWidth},
		{"width_unlisted_three", "3", "unsupported", "", parsePCIeMaximumWidth},
		{"width_future_sixty_four", "64", "unsupported", "", parsePCIeMaximumWidth},
		{"width_beyond_unknown_sentinel", "256", "unsupported", "", parsePCIeMaximumWidth},
		{"width_uint64_max", "18446744073709551615", "unsupported", "", parsePCIeMaximumWidth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, value := tc.parse([]byte(tc.raw))
			if state != tc.state || value != tc.value {
				t.Fatalf("classification = %q/%q; want %q/%q", state, value, tc.state, tc.value)
			}
		})
	}
}

func TestPCIeMaximumSpeedMalformedBytes(t *testing.T) {
	for _, tc := range []struct {
		name, raw, state, value string
		parse                   func([]byte) (string, string)
	}{
		{"empty", "", "malformed", "", parsePCIeMaximumSpeed},
		{"lf_only", "\n", "malformed", "", parsePCIeMaximumSpeed},
		{"crlf_only", "\r\n", "malformed", "", parsePCIeMaximumSpeed},
		{"leading_space", " 8.0 GT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"trailing_space", "8.0 GT/s PCIe ", "malformed", "", parsePCIeMaximumSpeed},
		{"double_separator", "8.0  GT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"tab_separator", "8.0\tGT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"unicode_space", "8.0\u00a0GT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"unicode_digit", "\u0668.0 GT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"fullwidth_fraction", "8.\uff10 GT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"nul_suffix", "8.0 GT/s PCIe\u0000", "malformed", "", parsePCIeMaximumSpeed},
		{"bare_cr", "8.0 GT/s PCIe\r", "malformed", "", parsePCIeMaximumSpeed},
		{"crlf", "8.0 GT/s PCIe\r\n", "malformed", "", parsePCIeMaximumSpeed},
		{"repeated_lf", "8.0 GT/s PCIe\n\n", "malformed", "", parsePCIeMaximumSpeed},
		{"embedded_lf", "8.0\nGT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"unknown_lowercase", "unknown", "malformed", "", parsePCIeMaximumSpeed},
		{"unknown_suffix", "Unknown value", "malformed", "", parsePCIeMaximumSpeed},
		{"unit_case", "8.0 gt/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"pcie_case", "8.0 GT/s pcie", "malformed", "", parsePCIeMaximumSpeed},
		{"missing_pcie", "8.0 GT/s", "malformed", "", parsePCIeMaximumSpeed},
		{"signed_plus", "+8.0 GT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"signed_minus", "-8.0 GT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"exponent", "8e0 GT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"leading_zero", "08.0 GT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"missing_integer", ".0 GT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"missing_fraction", "8. GT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"fraction_below_ascii_digit", "8./ GT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"fraction_above_ascii_digit", "8.: GT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"extra_fraction", "8.00 GT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"integer_only", "8 GT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"trailing_token", "8.0 GT/s PCIe extra", "malformed", "", parsePCIeMaximumSpeed},
		{"addition_overflow", "18446744073709551.7 GT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"multiplication_overflow", "18446744073709552.0 GT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"parse_uint_overflow", "18446744073709551616.0 GT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"huge_decimal", "999999999999999999999999999999999999999.0 GT/s PCIe", "malformed", "", parsePCIeMaximumSpeed},
		{"invalid_utf8", "8.0 GT/s PCIe\xff", "malformed", "", parsePCIeMaximumSpeed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, value := tc.parse([]byte(tc.raw))
			if state != tc.state || value != tc.value {
				t.Fatalf("classification = %q/%q; want %q/%q", state, value, tc.state, tc.value)
			}
		})
	}
}

func TestPCIeMaximumWidthMalformedBytes(t *testing.T) {
	for _, tc := range []struct {
		name, raw, state, value string
		parse                   func([]byte) (string, string)
	}{
		{"empty", "", "malformed", "", parsePCIeMaximumWidth},
		{"lf_only", "\n", "malformed", "", parsePCIeMaximumWidth},
		{"crlf_only", "\r\n", "malformed", "", parsePCIeMaximumWidth},
		{"leading_space", " 8", "malformed", "", parsePCIeMaximumWidth},
		{"trailing_space", "8 ", "malformed", "", parsePCIeMaximumWidth},
		{"tab", "8\t", "malformed", "", parsePCIeMaximumWidth},
		{"unicode_space", "8\u00a0", "malformed", "", parsePCIeMaximumWidth},
		{"unicode_digit", "\u0668", "malformed", "", parsePCIeMaximumWidth},
		{"fullwidth_digit", "\uff18", "malformed", "", parsePCIeMaximumWidth},
		{"nul", "8\u0000", "malformed", "", parsePCIeMaximumWidth},
		{"bare_cr", "8\r", "malformed", "", parsePCIeMaximumWidth},
		{"crlf", "8\r\n", "malformed", "", parsePCIeMaximumWidth},
		{"repeated_lf", "8\n\n", "malformed", "", parsePCIeMaximumWidth},
		{"embedded_lf", "1\n6", "malformed", "", parsePCIeMaximumWidth},
		{"text_unknown", "Unknown", "malformed", "", parsePCIeMaximumWidth},
		{"signed_plus", "+8", "malformed", "", parsePCIeMaximumWidth},
		{"signed_minus", "-8", "malformed", "", parsePCIeMaximumWidth},
		{"leading_zero", "08", "malformed", "", parsePCIeMaximumWidth},
		{"double_zero", "00", "malformed", "", parsePCIeMaximumWidth},
		{"fraction", "8.0", "malformed", "", parsePCIeMaximumWidth},
		{"exponent", "8e0", "malformed", "", parsePCIeMaximumWidth},
		{"trailing_token", "8 lanes", "malformed", "", parsePCIeMaximumWidth},
		{"overflow", "18446744073709551616", "malformed", "", parsePCIeMaximumWidth},
		{"huge_decimal", "999999999999999999999999999999999999999", "malformed", "", parsePCIeMaximumWidth},
		{"invalid_utf8", "8\xff", "malformed", "", parsePCIeMaximumWidth},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, value := tc.parse([]byte(tc.raw))
			if state != tc.state || value != tc.value {
				t.Fatalf("classification = %q/%q; want %q/%q", state, value, tc.state, tc.value)
			}
		})
	}
}
