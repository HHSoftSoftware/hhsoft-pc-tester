package inventory

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"testing"
)

func procBudgetFixture(t *testing.T, files map[string][]byte, failAfter string) *LinuxCollector {
	t.Helper()
	delivered := false
	source := collectionSource{
		file: func(name string) ([]byte, error) {
			if delivered {
				t.Fatal("file acquisition after the failing parser observation")
			}
			if raw, ok := files[name]; ok {
				delivered = failAfter != "" && name == failAfter
				return raw, nil
			}
			return nil, fs.ErrNotExist
		},
		directory: func(string) ([]fs.DirEntry, error) {
			if delivered {
				t.Fatal("directory fallback or later category after parser failure")
			}
			return nil, fs.ErrNotExist
		},
	}
	collector := NewLinuxCollectorWithSource(source)
	collector.inputBitmapWordBits = func() int {
		if delivered {
			t.Fatal("derived input observation after parser failure")
		}
		return 64
	}
	return collector
}

func TestInventoryProcBudgetLiteralLimits(t *testing.T) {
	if maxInventoryProcRecords != 65536 || maxInventoryProcFields != 262144 || maxInventoryProcBytes != 16777216 {
		t.Fatal("parser resource contract changed")
	}
	for _, kind := range []string{"records", "fields", "bytes"} {
		t.Run(kind, func(t *testing.T) {
			c := NewLinuxCollectorWithSource(collectionSource{}).newCollection(t.Context())
			if !c.chargeProc(65536, 262144, 16777216) || !c.chargeProc(0, 0, 0) {
				t.Fatal("exact remaining allowance rejected")
			}
			r, f, b := 0, 0, 0
			switch kind {
			case "records":
				r = 1
			case "fields":
				f = 1
			case "bytes":
				b = 1
			}
			if c.chargeProc(r, f, b) || c.err != ErrProcBudget {
				t.Fatal("one-over allowance admitted")
			}
		})
	}
}

func TestInventoryProcRawBlockBoundaries(t *testing.T) {
	for _, item := range []struct {
		name, text string
		blocks     []string
	}{
		{"empty", "", []string{""}},
		{"pair", "\n\n", []string{"", ""}},
		{"odd", "\n\n\n", []string{"", "\n"}},
		{"gaps", "a\n\n\n\nb\n\n", []string{"a", "", "b", ""}},
		{"crlf", "a\r\n\r\nb\r\nc", []string{"a", "b\r\nc"}},
		{"mixed-first", "a\n\r\nb", []string{"a", "b"}},
		{"mixed-second", "a\r\n\nb", []string{"a", "b"}},
	} {
		t.Run(item.name, func(t *testing.T) {
			c := NewLinuxCollectorWithSource(collectionSource{}).newCollection(t.Context())
			var got []string
			for index, block := range c.procBlocks(item.text) {
				if index != len(got) {
					t.Fatal("raw block index changed")
				}
				got = append(got, block)
			}
			if c.err != nil || !reflect.DeepEqual(got, item.blocks) || c.procBudget.records != 65536-len(item.blocks) || c.procBudget.fields != 262144 {
				t.Fatal("incremental raw block or accounting semantics changed")
			}
		})
	}
}

func TestInventoryProcFieldsAndRawSpelling(t *testing.T) {
	c := NewLinuxCollectorWithSource(collectionSource{}).newCollection(t.Context())
	var records []map[string]string
	for index, record := range c.parseRecords("bad\r\n\r\nPROCESSOR: 7\r\nmodel name: First\r\nModel Name: Final\tCPU\r\n\r\n\r\n\r\nmodel name: Fallback") {
		if index != len(records) {
			t.Fatal("CPU fallback index is not compacted")
		}
		records = append(records, record)
	}
	if c.err != nil || !reflect.DeepEqual(records, []map[string]string{{"processor": "7", "model name": "Final CPU"}, {"model name": "Fallback"}}) {
		t.Fatal("CPU duplicate/CRLF/truncated record semantics changed")
	}
	input := c.parseInputDevices("\r\n\r\nI: Bus=0003 Vendor=first junk Vendor=last\r\nN: Name=\"touch  screen\"\r\nB: truncated\r\n\r\n\r\n\r\nN: Name=\"Last")
	if c.err != nil || len(input) != 2 || input[0].ID != "input:1" || input[1].ID != "input:3" {
		t.Fatal("input raw-block or truncated-name semantics changed")
	}
	for name, want := range map[string]string{"bus": "0003", "vendor": "last", "name": "touch screen", "category": "input"} {
		if got, _ := input[0].Attribute(name); got != want {
			t.Fatal("input was normalized before its original-spelling classification", name)
		}
	}
	if got, _ := input[1].Attribute("name"); got != "Last" {
		t.Fatal("incomplete final input block changed")
	}
	words := []string{}
	for word := range inventoryProcWords(" \tfirst\u00a0second\u2003third\xff ") {
		words = append(words, word)
	}
	if !reflect.DeepEqual(words, []string{"first", "second", "third\xff"}) {
		t.Fatal("Unicode whitespace or original invalid byte changed")
	}
	for _, emptyFirst := range []bool{false, true} {
		content := "mEmToTaL: 4096\t kB\r\nMemTotal: 9999 kB\nMemAvailable: 1000 kB\nMemAvailable: 9999 kB"
		if emptyFirst {
			content = "MemTotal:\n" + content
		}
		snapshot, err := procBudgetFixture(t, map[string][]byte{"proc/meminfo": []byte(content)}, "").Collect(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if emptyFirst {
			if len(snapshot.Components) != 0 {
				t.Fatal("later memory value replaced an empty first match")
			}
		} else {
			assertAttribute(t, snapshot, ComponentMemory, "memory:system", "total_kib", "4096")
			assertAttribute(t, snapshot, ComponentMemory, "memory:system", "available_kib", "1000")
		}
	}
}

func TestInventoryProcPreallocationFailure(t *testing.T) {
	for _, mode := range []string{"record", "field", "text", "index", "lowercase"} {
		t.Run(mode, func(t *testing.T) {
			c := NewLinuxCollectorWithSource(collectionSource{}).newCollection(t.Context())
			attributes := map[string]string{"old": "retained"}
			switch mode {
			case "record":
				c.procBudget.records = 0
				for range c.parseRecords("name: value") {
					t.Fatal("record yielded without record admission")
				}
				if c.procBudget.fields != 262144 {
					t.Fatal("record failure performed field work")
				}
			case "field":
				c.procBudget.fields = 0
				if c.storeProcText(attributes, "x", "one two", false, "", true) {
					t.Fatal("normalization without word admission")
				}
			case "text":
				c.procBudget.bytes = 2
				if c.storeProcText(attributes, "x", "ab", false, "", false) {
					t.Fatal("retention without text admission")
				}
			case "index":
				c.procBudget.bytes = 2
				if _, valid := c.procIndex(325); valid {
					t.Fatal("generated ID allocated beyond retained-text allowance")
				}
			case "lowercase":
				c.procBudget.bytes = 1
				if c.storeProcText(attributes, "\xff", "", true, "", false) {
					t.Fatal("conservative UTF-8 key charge omitted")
				}
			}
			if c.err != ErrProcBudget || !reflect.DeepEqual(attributes, map[string]string{"old": "retained"}) {
				t.Fatal("failed admission modified retained fields")
			}
		})
	}
}

func TestInventoryProcRecordFloodBelowRawLimits(t *testing.T) {
	for _, item := range []struct{ name, path, content string }{
		{"cpu-fallback", "proc/cpuinfo", strings.Repeat("model name: x\n\n", 65536) + "model name: x"},
		{"cpu-empty", "proc/cpuinfo", strings.Repeat("\n\n", 65536)},
		{"cpu-malformed", "proc/cpuinfo", strings.Repeat("bad\n\n", 65536) + "bad"},
		{"input-empty", "proc/bus/input/devices", strings.Repeat("\n\n", 65536)},
		{"input-malformed", "proc/bus/input/devices", strings.Repeat("bad\n\n", 65536) + "bad"},
	} {
		t.Run(item.name, func(t *testing.T) {
			if len(item.content) >= 8388608 {
				t.Fatal("record fixture would hit acquisition instead of parser budget")
			}
			snapshot, err := procBudgetFixture(t, map[string][]byte{item.path: []byte(item.content)}, item.path).Collect(t.Context())
			requireNoCollection(t, snapshot, err, ErrProcBudget)
		})
	}
	c := NewLinuxCollectorWithSource(collectionSource{}).newCollection(t.Context())
	count := 0
	for range c.parseRecords(strings.Repeat("x:v\n\n", 65535) + "x:v") {
		count++
	}
	if count != 65536 || c.err != nil || c.procBudget.records != 0 {
		t.Fatal("exact raw-record allowance truncated a successful traversal")
	}
}

func TestInventoryProcFieldWorkFloodBelowRawLimits(t *testing.T) {
	var distinct strings.Builder
	for index := 0; index < 131073; index++ {
		fmt.Fprintf(&distinct, "key%d:v\n", index)
	}
	for _, item := range []struct{ name, path, content string }{
		{"malformed-lines", "proc/cpuinfo", strings.Repeat("bad\n", 262144)},
		{"duplicate-fields", "proc/cpuinfo", strings.Repeat("x:v\n", 131073)},
		{"unique-fields", "proc/cpuinfo", distinct.String()},
		{"value-words", "proc/cpuinfo", "flags: " + strings.Repeat("x ", 262144)},
		{"identity-tokens", "proc/bus/input/devices", "I: " + strings.Repeat("bad ", 262144)},
		{"bitmap-words", "proc/bus/input/devices", "B: KEY=" + strings.Repeat("x ", 262144)},
	} {
		t.Run(item.name, func(t *testing.T) {
			if len(item.content) >= 8388608 {
				t.Fatal("field fixture reaches a raw limit")
			}
			snapshot, err := procBudgetFixture(t, map[string][]byte{item.path: []byte(item.content)}, item.path).Collect(t.Context())
			requireNoCollection(t, snapshot, err, ErrProcBudget)
		})
	}
	c := NewLinuxCollectorWithSource(collectionSource{}).newCollection(t.Context())
	count := 0
	for range c.procLines(strings.Repeat("x\n", 262143) + "x") {
		count++
	}
	if count != 262144 || c.err != nil || c.procBudget.fields != 0 {
		t.Fatal("exact visited-line allowance rejected")
	}
}

func TestInventoryProcLargeRetainedCopies(t *testing.T) {
	content := []byte("model name: " + strings.Repeat("x", 9<<20))
	if len(content) >= 16777216 {
		t.Fatal("large field fixture reaches raw CPU cap")
	}
	snapshot, err := procBudgetFixture(t, map[string][]byte{"proc/cpuinfo": content}, "proc/cpuinfo").Collect(t.Context())
	requireNoCollection(t, snapshot, err, ErrProcBudget)
}

func TestInventoryProcSharedRecordBudget(t *testing.T) {
	files := map[string][]byte{
		"proc/cpuinfo":           []byte(strings.Repeat("\n\n", 32766) + "model name: x"),
		"proc/meminfo":           []byte("MemTotal: 4096 kB"),
		"proc/bus/input/devices": []byte(strings.Repeat("\n\n", 32768) + "N: Name=\"panel\""),
	}
	for _, path := range []string{"proc/cpuinfo", "proc/meminfo", "proc/bus/input/devices"} {
		snapshot, err := procBudgetFixture(t, map[string][]byte{path: files[path]}, "").Collect(t.Context())
		if err != nil || len(snapshot.Components) != 1 {
			t.Fatal("independent within-budget prerequisite failed", path, err)
		}
	}
	// 32767 CPU blocks + one memory record + 32769 input blocks = 65537.
	snapshot, err := procBudgetFixture(t, files, "proc/bus/input/devices").Collect(t.Context())
	requireNoCollection(t, snapshot, err, ErrProcBudget)
}

func TestInventoryProcSharedFieldWorkBudget(t *testing.T) {
	files := map[string][]byte{
		"proc/cpuinfo":           []byte(strings.Repeat("bad\n", 110000) + "model name: x"),
		"proc/meminfo":           []byte(strings.Repeat("bad\n", 60000) + "MemTotal: 4096 kB"),
		"proc/bus/input/devices": []byte(strings.Repeat("bad\n", 110000) + "N: Name=\"panel\""),
	}
	if len(files["proc/meminfo"]) >= 262144 {
		t.Fatal("memory fixture reaches acquisition cap")
	}
	for _, path := range []string{"proc/cpuinfo", "proc/meminfo", "proc/bus/input/devices"} {
		snapshot, err := procBudgetFixture(t, map[string][]byte{path: files[path]}, "").Collect(t.Context())
		if err != nil || len(snapshot.Components) != 1 {
			t.Fatal("individual field-work prerequisite failed", path, err)
		}
	}
	// At least 280000 visited malformed lines are shared across all three
	// observations. Each independent input is below its own raw/field limit.
	snapshot, err := procBudgetFixture(t, files, "proc/bus/input/devices").Collect(t.Context())
	requireNoCollection(t, snapshot, err, ErrProcBudget)
}

func TestInventoryProcSharedRetainedBytes(t *testing.T) {
	files := map[string][]byte{
		"proc/cpuinfo":           []byte("model name: " + strings.Repeat("c", 4<<20)),
		"proc/meminfo":           []byte("MemTotal: 4096 kB"),
		"proc/bus/input/devices": []byte("N: Name=\"" + strings.Repeat("i", 3<<20) + "\""),
	}
	for _, path := range []string{"proc/cpuinfo", "proc/meminfo", "proc/bus/input/devices"} {
		snapshot, err := procBudgetFixture(t, map[string][]byte{path: files[path]}, "").Collect(t.Context())
		if err != nil || len(snapshot.Components) != 1 {
			t.Fatal("individual retained-text prerequisite failed", path, err)
		}
	}
	// CPU record/alias/component retains 12 MiB; input record/component adds
	// 6 MiB. Each raw file and each independent collection is below its cap.
	snapshot, err := procBudgetFixture(t, files, "proc/bus/input/devices").Collect(t.Context())
	requireNoCollection(t, snapshot, err, ErrProcBudget)
}

func TestInventoryProcDuplicateRetentionNeverRefunds(t *testing.T) {
	c := NewLinuxCollectorWithSource(collectionSource{}).newCollection(t.Context())
	c.procBudget.bytes = 7
	attributes := map[string]string{}
	for repeat := 0; repeat < 2; repeat++ {
		if !c.storeProcText(attributes, "x", "ab", false, "", true) {
			t.Fatal("valid duplicate prerequisite failed")
		}
	}
	if c.procBudget.bytes != 1 || c.storeProcText(attributes, "x", "ab", false, "", true) || c.err != ErrProcBudget || attributes["x"] != "ab" {
		t.Fatal("duplicate field refunded retained-text work")
	}
}

func TestInventoryProcBudgetFreshCollectAndSourceOwnership(t *testing.T) {
	bad := true
	good := []byte("model name: Stable CPU")
	saved := append([]byte(nil), good...)
	source := collectionSource{file: func(name string) ([]byte, error) {
		if name != "proc/cpuinfo" {
			return nil, fs.ErrNotExist
		}
		if bad {
			return []byte(strings.Repeat("\n\n", 65536)), nil
		}
		return good, nil
	}}
	collector := NewLinuxCollectorWithSource(source)
	snapshot, err := collector.Collect(t.Context())
	requireNoCollection(t, snapshot, err, ErrProcBudget)
	bad = false
	for attempt := 0; attempt < 2; attempt++ {
		snapshot, err = collector.Collect(t.Context())
		if err != nil || len(snapshot.Components) != 1 || !bytes.Equal(good, saved) {
			t.Fatal("parser state persisted or provider bytes changed", err)
		}
		assertAttribute(t, snapshot, ComponentCPU, "cpu:0", "model", "Stable CPU")
	}
}

func TestInventoryProcIteratorCancellation(t *testing.T) {
	for _, kind := range []string{"block", "line", "word"} {
		for _, mode := range []string{"break", "continue"} {
			t.Run(kind+"-"+mode, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				c := NewLinuxCollectorWithSource(collectionSource{}).newCollection(ctx)
				visits := 0
				visit := func() bool {
					visits++
					cancel()
					return mode == "continue"
				}
				switch kind {
				case "block":
					for range c.procBlocks("first\n\nsecond") {
						if !visit() {
							break
						}
					}
				case "line":
					for range c.procLines("first\nsecond") {
						if !visit() {
							break
						}
					}
				case "word":
					for range c.procWords("first second") {
						if !visit() {
							break
						}
					}
				}
				if visits != 1 || c.err != context.Canceled {
					t.Fatal("iterator missed cancellation after yield or break")
				}
			})
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	c := NewLinuxCollectorWithSource(collectionSource{}).newCollection(ctx)
	attributes := map[string]string{}
	if c.storeProcText(attributes, "x", "value", false, "", true) || c.err != context.Canceled || len(attributes) != 0 || c.procBudget != newInventoryProcBudget() {
		t.Fatal("preexisting cancellation lost precedence over parsing")
	}
}

func TestInventoryProcMixed325InstancesAndNoDrive(t *testing.T) {
	for _, mode := range []string{"no-drive", "mixed-325"} {
		t.Run(mode, func(t *testing.T) {
			files := map[string][]byte{}
			if mode == "mixed-325" {
				var cpu, input strings.Builder
				for index := 0; index < 325; index++ {
					fmt.Fprintf(&cpu, "processor: %d\nmodel name: CPU-%03d\n\n", index, index)
					fmt.Fprintf(&input, "N: Name=\"Input-%03d\"\n\n", index)
				}
				files["proc/cpuinfo"], files["proc/bus/input/devices"] = []byte(cpu.String()), []byte(input.String())
				files["proc/meminfo"] = []byte("MemTotal: 8192 kB")
			}
			snapshot, err := procBudgetFixture(t, files, "").Collect(t.Context())
			want := 0
			if mode == "mixed-325" {
				want = 651
			}
			if err != nil || snapshot.SchemaVersion != 1 || len(snapshot.Components) != want {
				t.Fatal("complete inventory was truncated or diskless inventory rejected", err)
			}
			if mode == "mixed-325" {
				for index := 0; index < 325; index++ {
					assertAttribute(t, snapshot, ComponentCPU, fmt.Sprintf("cpu:%d", index), "model", fmt.Sprintf("CPU-%03d", index))
					assertAttribute(t, snapshot, ComponentInput, fmt.Sprintf("input:%d", index), "name", fmt.Sprintf("Input-%03d", index))
				}
				assertAttribute(t, snapshot, ComponentMemory, "memory:system", "total_kib", "8192")
			}
		})
	}
}
