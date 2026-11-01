package inventory

import (
	"encoding/json"
	"strings"
	"testing"
	"unsafe"
)

func TestInventoryNormalizeValueLiterals(t *testing.T) {
	for _, item := range []struct {
		name, input, want string
	}{
		{"empty", "", ""},
		{"ascii_whitespace", " \t\r\n\v\f", ""},
		{"unicode_whitespace", "\u0085\u00a0\u1680\u2000\u2003\u2028\u2029\u202f\u205f\u3000", ""},
		{"one_word", "alpha", "alpha"},
		{"one_word_padding", " \talpha\r\n", "alpha"},
		{"already_canonical", "alpha beta", "alpha beta"},
		{"ascii_collapse", " alpha\t beta\r\ngamma ", "alpha beta gamma"},
		{"unicode_separators", "a\u0085b\u00a0c\u1680d\u2000e\u2003f\u2028g\u2029h\u202fi\u205fj\u3000k", "a b c d e f g h i j k"},
		{"zero_width_not_whitespace", "a\u200bb", "a\u200bb"},
		{"bom_not_whitespace", "\ufeffalpha", "\ufeffalpha"},
		{"nul_is_a_word_byte", "\x00 \talpha", "\x00 alpha"},
		{"unicode_word_bytes", " \u00e9\u00a0\u4e2d ", "\u00e9 \u4e2d"},
		{"invalid_utf8_single", " \xff ", "\xff"},
		{"invalid_utf8_attached", " alpha\xff ", "alpha\xff"},
		{"invalid_utf8_multiple", "\xff\t\xfe \xfd", "\xff \xfe \xfd"},
		{"truncated_utf8", "\xc2\tbeta", "\xc2 beta"},
		{"invalid_sequence_not_repaired", "\xe2\x28\xa1 \xff", "\xe2\x28\xa1 \xff"},
		{"nonbreaking_space_padding", "\u00a0alpha\u00a0", "alpha"},
	} {
		t.Run(item.name, func(t *testing.T) {
			got := normalizeValue(item.input)
			if got != item.want || len(got) > len(item.input) {
				t.Fatalf("normalization = %q, want %q", got, item.want)
			}
			if normalizeValue(got) != item.want {
				t.Fatal("normalization changed already-normalized bytes")
			}
		})
	}
}

func TestInventoryNormalizeValueSharingAndOwnership(t *testing.T) {
	const padding = 1 << 18
	oneBacking := strings.Repeat("x", padding) + "alpha" + strings.Repeat("y", padding)
	oneSlice := oneBacking[padding : padding+len("alpha")]
	multiBacking := strings.Repeat("x", padding) + "alpha beta" + strings.Repeat("y", padding)
	multiSlice := multiBacking[padding : padding+len("alpha beta")]
	paddedOne := " \talpha\r\n"
	paddedMulti := strings.Repeat(" ", padding) + "alpha beta" + strings.Repeat("\t", padding)
	invalidOne := " \xff "
	for _, item := range []struct {
		name, input, alias, want string
		share                    bool
	}{
		{"single_literal", "alpha", "alpha", "alpha", true},
		{"single_padded", paddedOne, paddedOne[2:7], "alpha", true},
		{"single_large_backing", oneSlice, oneSlice, "alpha", true},
		{"single_invalid_utf8", invalidOne, invalidOne[1:2], "\xff", true},
		{"multiple_canonical", "alpha beta", "alpha beta", "alpha beta", false},
		{"multiple_large_backing", multiSlice, multiSlice, "alpha beta", false},
		{"multiple_padded_large_backing", paddedMulti, paddedMulti[padding : padding+len("alpha beta")], "alpha beta", false},
		{"multiple_invalid_utf8", "\xff \xfe", "\xff \xfe", "\xff \xfe", false},
	} {
		t.Run(item.name, func(t *testing.T) {
			got := normalizeValue(item.input)
			if got != item.want {
				t.Fatal("ownership-test bytes differ from the literal expectation")
			}
			// Compare addresses only; never dereference or mutate string storage.
			shared := unsafe.StringData(got) == unsafe.StringData(item.alias)
			if shared != item.share {
				t.Fatalf("shares source storage = %v, want %v", shared, item.share)
			}
		})
	}
}

func TestInventoryNormalizeValueLargeInputs(t *testing.T) {
	padding := strings.Repeat(" \t\u00a0", 1<<15)
	one := strings.Repeat("x", 1<<18)
	many := strings.Repeat("alpha\t", 32768) + "omega"
	manyWant := strings.Repeat("alpha ", 32768) + "omega"
	invalid := strings.Repeat("\xff \xfe\t", 8192) + "\xfd"
	invalidWant := strings.Repeat("\xff \xfe ", 8192) + "\xfd"
	for _, item := range []struct {
		name, input, want string
	}{
		{"whitespace_only", padding, ""},
		{"one_large_word", one, one},
		{"whitespace_heavy_two_words", padding + "alpha" + padding + "beta" + padding, "alpha beta"},
		{"many_words", many, manyWant},
		{"invalid_utf8_many_words", invalid, invalidWant},
	} {
		t.Run(item.name, func(t *testing.T) {
			got := normalizeValue(item.input)
			if got != item.want || len(got) > len(item.input) {
				t.Fatalf("large normalization bytes differ: length %d, want %d", len(got), len(item.want))
			}
		})
	}
}

var inventoryNormalizationAllocationSink string

func TestInventoryNormalizeValueAllocationCounts(t *testing.T) {
	// Construct every input before measurement and retain every result in a sink.
	// Counts describe this normalizer on the validated SDK, not whole-heap use.
	padding := strings.Repeat(" \t\u00a0", 1<<15)
	largeOne := strings.Repeat("x", 1<<18)
	many := strings.Repeat("alpha\t", 32768) + "omega"
	manyWant := strings.Repeat("alpha ", 32768) + "omega"
	for _, item := range []struct {
		name, input, want string
		allocations       float64
	}{
		{"empty", "", "", 0},
		{"whitespace_only", padding, "", 0},
		{"single", "alpha", "alpha", 0},
		{"single_padded", padding + "alpha" + padding, "alpha", 0},
		{"single_large", largeOne, largeOne, 0},
		{"single_invalid_utf8", " \xff ", "\xff", 0},
		{"multiple_canonical", "alpha beta", "alpha beta", 1},
		{"multiple_separators", "alpha\t\nbeta", "alpha beta", 1},
		{"multiple_unicode", "\u00e9\u00a0\u4e2d", "\u00e9 \u4e2d", 1},
		{"multiple_invalid_utf8", "\xff\t\xfe", "\xff \xfe", 1},
		{"whitespace_heavy", padding + "alpha" + padding + "beta" + padding, "alpha beta", 1},
		{"many_words", many, manyWant, 1},
	} {
		t.Run(item.name, func(t *testing.T) {
			if normalizeValue(item.input) != item.want {
				t.Fatal("allocation-test prerequisite differs")
			}
			allocations := testing.AllocsPerRun(20, func() {
				inventoryNormalizationAllocationSink = normalizeValue(item.input)
			})
			if allocations != item.allocations || inventoryNormalizationAllocationSink != item.want {
				t.Fatalf("allocations = %g, want %g; result correct = %v", allocations, item.allocations, inventoryNormalizationAllocationSink == item.want)
			}
		})
	}
}

func TestInventoryNormalizeValueWhitespaceAllocationBytes(t *testing.T) {
	// Allocation count alone cannot detect Grow(len(raw)). The tiny output must
	// not reserve the much larger whitespace-heavy input's storage. The slack
	// permits the allocator's size class; this is not an exact whole-heap bound.
	padding := strings.Repeat(" \t\u00a0", 1<<17)
	input := padding + "alpha" + padding + "beta" + padding
	if normalizeValue(input) != "alpha beta" {
		t.Fatal("byte-allocation prerequisite differs")
	}
	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			inventoryNormalizationAllocationSink = normalizeValue(input)
		}
	})
	if inventoryNormalizationAllocationSink != "alpha beta" || result.AllocsPerOp() != 1 || result.AllocedBytesPerOp() > 64 {
		t.Fatalf("tiny normalized output allocated %d bytes in %d allocations per call", result.AllocedBytesPerOp(), result.AllocsPerOp())
	}
}

func TestInventoryNormalizationComponentSnapshotBytes(t *testing.T) {
	attributes := map[string]string{
		" model ": " \tFixture\u00a0Device ",
		"vendor":  "ABCD",
		"blank":   "\t \n",
		"":        "discard",
	}
	component := NewComponent(ComponentUSB, " usb:2 ", attributes)
	encoded, err := json.Marshal(component)
	if err != nil {
		t.Fatal(err)
	}
	const componentWant = `{"type":"usb","id":"usb:2","attributes":[{"name":"model","value":"Fixture Device"},{"name":"vendor","value":"ABCD"}]}`
	if string(encoded) != componentWant || attributes[" model "] != " \tFixture\u00a0Device " {
		t.Fatal("component canonical bytes or caller-owned attributes changed")
	}
	input := []Component{
		component,
		{Type: ComponentCPU, ID: " cpu:0 ", Attributes: []Attribute{
			{Name: "label", Value: "first value"},
			{Name: "label", Value: " last\tvalue "},
		}},
	}
	snapshot := NewSnapshot(input)
	encoded, err = json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	// Equal raw attribute names retain their existing last-value-wins behavior.
	const snapshotWant = `{"schema_version":1,"components":[{"type":"cpu","id":"cpu:0","attributes":[{"name":"label","value":"last value"}]},{"type":"usb","id":"usb:2","attributes":[{"name":"model","value":"Fixture Device"},{"name":"vendor","value":"ABCD"}]}]}`
	if string(encoded) != snapshotWant || input[1].Attributes[1].Value != " last\tvalue " {
		t.Fatal("snapshot schema, ordering, duplicate resolution, bytes or input ownership changed")
	}
}
