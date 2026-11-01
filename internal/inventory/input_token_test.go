package inventory

import (
	"strings"
	"testing"
)

func TestInputTokenSingleWordCardinality(t *testing.T) {
	for _, item := range []struct {
		name, value string
		want        bool
	}{
		{"empty", "", false},
		{"ascii_space_only", " \t\r\n\v\f", false},
		{"one", "2", true},
		{"ascii_padding", " \t2\r\n", true},
		{"two", "0 2", false},
		{"several", "0 0 2", false},
		{"unicode_padding", "\u0085\u00a02\u2003\u3000", true},
		{"unicode_separator", "0\u20282", false},
		{"zero_width_is_not_space", "0\u200b2", true},
		{"invalid_utf8_is_a_word", "\xff", true},
		{"invalid_utf8_attached", "2\xff", true},
		{"invalid_utf8_separated", "2 \xff", false},
	} {
		t.Run(item.name, func(t *testing.T) {
			if got := inputHasSingleWord(item.value); got != item.want {
				t.Fatalf("single-word result = %v, want %v", got, item.want)
			}
		})
	}
}

func TestInputTokenHandlerBoundaries(t *testing.T) {
	for _, item := range []struct {
		name, handlers, wanted string
		exact, prefix          bool
	}{
		{"empty", "", "kbd", false, false},
		{"space_only", " \t\u2003", "kbd", false, false},
		{"exact", "kbd", "kbd", true, true},
		{"after_other_word", "event0 kbd event1", "kbd", true, true},
		{"prefix_only", "mouse0", "mouse", false, true},
		{"suffix_is_not_match", "xkbd", "kbd", false, false},
		{"longer_word", "kbdextra", "kbd", false, true},
		{"case_sensitive", "KBD", "kbd", false, false},
		{"unicode_boundaries", "event0\u00a0kbd\u2003event1", "kbd", true, true},
		{"zero_width_is_not_separator", "event0\u200bkbd", "kbd", false, false},
		{"invalid_utf8_prefix", "\xffkbd", "kbd", false, false},
		{"invalid_utf8_separated", "\xff kbd", "kbd", true, true},
		{"invalid_utf8_suffix", "kbd\xff", "kbd", false, true},
		{"empty_wanted_with_word", "event0", "", false, true},
		{"empty_wanted_without_word", "", "", false, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			if got := handlerHas(item.handlers, item.wanted); got != item.exact {
				t.Fatalf("exact handler result = %v, want %v", got, item.exact)
			}
			if got := handlerHasPrefix(item.handlers, item.wanted); got != item.prefix {
				t.Fatalf("prefix handler result = %v, want %v", got, item.prefix)
			}
		})
	}
}

func TestInputTokenBitmapWordSelection(t *testing.T) {
	for _, item := range []struct {
		name, value string
		code, width int
		want        bool
	}{
		{"empty", "", 0, 32, false},
		{"space_only", " \t\u2003", 0, 64, false},
		{"unknown_width", "2", 1, 0, false},
		{"unsupported_width", "2", 1, 16, false},
		{"negative_width", "2", 1, -32, false},
		{"negative_code", "2", -1, 32, false},
		{"low_32", "0 2", 1, 32, true},
		{"high_32", "1 0", 32, 32, true},
		{"right_index_32", "1 0", 0, 32, false},
		{"high_64", "1 0", 64, 64, true},
		{"right_index_64", "1 0", 0, 64, false},
		{"third_word_32", "4 0 2", 66, 32, true},
		{"third_word_64", "4 0 2", 130, 64, true},
		{"bit_31", "80000000", 31, 32, true},
		{"bit_63", "8000000000000000", 63, 64, true},
		{"missing_word", "80000000", 32, 32, false},
		{"bit_32_in_64_word", "100000000", 32, 64, true},
		{"selected_32_overflow", "100000002", 1, 32, false},
		{"same_value_fits_64", "100000002", 1, 64, true},
		{"uppercase_hex", "A", 1, 32, true},
		{"selected_zero", "0", 1, 32, false},
		{"selected_malformed", "2 zz", 1, 32, false},
		{"unselected_malformed", "zz 2", 1, 32, true},
		{"unselected_overflow", "10000000000000000 2", 1, 64, true},
		{"leading_zeroes", "00000000000000000000000000000002", 1, 32, true},
		{"selected_64_overflow", "10000000000000002", 1, 64, false},
		{"negative_word", "-2", 1, 32, false},
		{"explicit_prefix_is_not_base16_digit", "0x2", 1, 32, false},
		{"selected_invalid_utf8", "\xff", 1, 32, false},
		{"unselected_invalid_utf8", "\xff 2", 1, 32, true},
		{"unicode_word_order", "1\u00a00\u20032", 64, 32, true},
		{"unicode_low_word", "1\u00a00\u20032", 1, 32, true},
		{"out_of_range", "1 0", 256, 64, false},
		{"maximum_int_code", "2", int(^uint(0) >> 1), 32, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			if got := inputCapabilityBitWidth(item.value, item.code, item.width); got != item.want {
				t.Fatalf("selected bit = %v, want %v", got, item.want)
			}
		})
	}
}

func TestInputTokenClassificationCompatibility(t *testing.T) {
	for _, item := range []struct {
		name, identity, handlers, keys, absolute, properties string
		width                                                int
		want                                                 string
	}{
		{"unknown_width_single_property", "Fixture Panel", "event0", "", "3", "\u00a02\u2003", 0, "touchscreen"},
		{"unknown_width_multiple_properties", "Fixture Panel", "event0", "", "3", "0 2", 0, "input"},
		{"unknown_width_overflow", "Fixture Panel", "event0", "", "3", "100000002", 0, "input"},
		{"known_width_unselected_malformed", "Fixture Panel", "event0", "", "3", "zz 2", 32, "touchscreen"},
		{"known_width_selected_malformed", "Fixture Panel", "event0", "", "3", "2 zz", 64, "input"},
		{"unknown_key_width", "Fixture HID", "kbd event0", "40010000", "", "", 0, "input"},
		{"two_required_keys", "Fixture HID", "kbd event0", "40010000", "", "", 32, "keyboard"},
		{"only_one_required_key", "Fixture HID", "kbd event0", "10000", "", "", 32, "input"},
		{"two_word_32_keyboard", "Fixture HID", "kbd event0", "1000 10000", "", "", 32, "keyboard"},
		{"same_words_64_not_keyboard", "Fixture HID", "kbd event0", "1000 10000", "", "", 64, "input"},
		{"one_word_64_keyboard", "Fixture HID", "kbd event0", "100000010000", "", "", 64, "keyboard"},
		{"same_word_32_overflow", "Fixture HID", "kbd event0", "100000010000", "", "", 32, "input"},
		{"name_hint_precedence", "Fixture Touchpad", "event0", "", "3", "2", 32, "touchpad"},
		{"pointer_handler", "Fixture HID", "mouse0\u2003event0", "", "", "", 64, "pointer"},
		{"keyboard_name_hint", "Fixture Keyboard", "event0", "invalid", "", "", 0, "keyboard"},
	} {
		t.Run(item.name, func(t *testing.T) {
			if got := ClassifyInputWithBitmapWidth(item.identity, item.handlers, item.keys, "", item.absolute, item.properties, item.width); got != item.want {
				t.Fatalf("classification = %q, want %q", got, item.want)
			}
		})
	}
}

func TestInputTokenTouchscreenCandidateCompatibility(t *testing.T) {
	for _, item := range []struct {
		name, category, absolute, properties string
		width                                int
		want                                 bool
	}{
		{"named_candidate", "touchscreen", "", "", 0, true},
		{"direct_32", "input", "3", "0 2", 32, true},
		{"direct_64", "pointer", "3", "0\u20032", 64, true},
		{"unknown_width_not_authority", "input", "3", "2", 0, false},
		{"absolute_required", "input", "", "2", 32, false},
		{"unselected_malformed", "input", "3", "zz 2", 32, true},
		{"selected_malformed", "input", "3", "2 zz", 32, false},
		{"non_direct", "input", "3", "1", 64, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			if got := TouchscreenCandidate(item.category, item.absolute, item.properties, item.width); got != item.want {
				t.Fatalf("candidate selection = %v, want %v", got, item.want)
			}
		})
	}
}

func TestInputTokenLargeInputsPreserveSelection(t *testing.T) {
	// Construct the large inputs outside the assertions and measured closures.
	const words = 32768
	bitmap := strings.Repeat("0 ", words) + "2"
	handlers := strings.Repeat("event0 ", words)
	oneWord := strings.Repeat("0", words*2) + "2"
	for _, item := range []struct {
		name string
		run  func() bool
		want bool
	}{
		{"bitmap_last", func() bool { return inputCapabilityBitWidth(bitmap, 1, 32) }, true},
		{"bitmap_first", func() bool { return inputCapabilityBitWidth(bitmap, words*32, 32) }, false},
		{"bitmap_beyond", func() bool { return inputCapabilityBitWidth(bitmap, (words+1)*32, 32) }, false},
		{"handler_start", func() bool { return handlerHas("kbd "+handlers, "kbd") }, true},
		{"handler_end", func() bool { return handlerHas(handlers+"kbd", "kbd") }, true},
		{"handler_absent", func() bool { return handlerHas(handlers, "kbd") }, false},
		{"prefix_end", func() bool { return handlerHasPrefix(handlers+"mouse7", "mouse") }, true},
		{"one_large_word", func() bool { return inputHasSingleWord(oneWord) }, true},
		{"many_words", func() bool { return inputHasSingleWord(bitmap) }, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			if got := item.run(); got != item.want {
				t.Fatalf("large-input result = %v, want %v", got, item.want)
			}
		})
	}
}

var inputTokenAllocationSink bool

func TestInputTokenScansDoNotAllocateWordSlices(t *testing.T) {
	// Inputs and closures are created before AllocsPerRun. Valid selected words
	// avoid measuring ParseUint error construction. This is not a whole-heap or
	// full-classifier bound: name concatenation and lowercasing remain separate.
	const words = 32768
	bitmap := strings.Repeat("0 ", words) + "2"
	keys := strings.Repeat("0 ", words) + "40010000"
	handlers := strings.Repeat("event0 ", words)
	exactStart, exactEnd := "kbd "+handlers, handlers+"kbd"
	prefixStart, prefixEnd := "mouse7 "+handlers, handlers+"mouse7"
	oneWord := strings.Repeat("0", words*2) + "2"
	for _, item := range []struct {
		name string
		run  func() bool
		want bool
	}{
		{"bitmap_32", func() bool { return inputCapabilityBitWidth(bitmap, 1, 32) }, true},
		{"bitmap_64", func() bool { return inputCapabilityBitWidth(bitmap, 1, 64) }, true},
		{"bitmap_out_of_range", func() bool { return inputCapabilityBitWidth(bitmap, (words+1)*32, 32) }, false},
		{"keyboard_six_probes", func() bool { return keyboardKeyMapWidth(keys, 32) }, true},
		{"exact_start", func() bool { return handlerHas(exactStart, "kbd") }, true},
		{"exact_end", func() bool { return handlerHas(exactEnd, "kbd") }, true},
		{"exact_absent", func() bool { return handlerHas(handlers, "kbd") }, false},
		{"prefix_start", func() bool { return handlerHasPrefix(prefixStart, "mouse") }, true},
		{"prefix_end", func() bool { return handlerHasPrefix(prefixEnd, "mouse") }, true},
		{"prefix_absent", func() bool { return handlerHasPrefix(handlers, "mouse") }, false},
		{"single_large_word", func() bool { return inputHasSingleWord(oneWord) }, true},
		{"multiple_words", func() bool { return inputHasSingleWord(bitmap) }, false},
		{"no_word", func() bool { return inputHasSingleWord(" \t\u2003") }, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			if got := item.run(); got != item.want {
				t.Fatalf("allocation-test prerequisite = %v, want %v", got, item.want)
			}
			allocations := testing.AllocsPerRun(20, func() {
				inputTokenAllocationSink = item.run()
			})
			if allocations != 0 || inputTokenAllocationSink != item.want {
				t.Fatalf("allocations = %g, result = %v, want 0/%v", allocations, inputTokenAllocationSink, item.want)
			}
		})
	}
}
