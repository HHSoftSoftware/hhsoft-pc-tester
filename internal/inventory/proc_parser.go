package inventory

import (
	"errors"
	"iter"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Product parser limits, not kernel ABI or hardware instance limits.
const (
	maxInventoryProcRecords = 65536
	maxInventoryProcFields  = 262144
	maxInventoryProcBytes   = 16 << 20
)

// ErrProcBudget carries no source path, field content or provider error text.
var ErrProcBudget = errors.New("inventory: primary observation parser budget exceeded")

// One Collect owns this state. Counts include overwritten/discarded fields;
// they are not peak-heap accounting or a CPU/kernel-time guarantee.
type inventoryProcBudget struct {
	records int
	fields  int
	bytes   int
}

func newInventoryProcBudget() inventoryProcBudget {
	return inventoryProcBudget{maxInventoryProcRecords, maxInventoryProcFields, maxInventoryProcBytes}
}

func (collector *linuxCollection) chargeProc(records, fields, bytes int) bool {
	if !collector.check() {
		return false
	}
	budget := &collector.procBudget
	if records < 0 || fields < 0 || bytes < 0 ||
		budget.records < 0 || budget.records > maxInventoryProcRecords ||
		budget.fields < 0 || budget.fields > maxInventoryProcFields ||
		budget.bytes < 0 || budget.bytes > maxInventoryProcBytes ||
		records > budget.records || fields > budget.fields || bytes > budget.bytes {
		collector.err = ErrProcBudget
		return false
	}
	budget.records -= records
	budget.fields -= fields
	budget.bytes -= bytes
	return collector.check()
}

// Find the next block without allocating a whole-input CRLF copy/split slice.
// Adjacent normalized newlines are consumed in nonoverlapping pairs, matching
// ReplaceAll(CRLF, LF) then Split(LF+LF), including empty and trailing blocks.
func inventoryProcBlock(content string, start int) (end, next int) {
	for position := start; position < len(content); {
		offset := strings.IndexByte(content[position:], '\n')
		if offset < 0 {
			break
		}
		newline := position + offset
		separator := newline
		if separator > start && content[separator-1] == '\r' {
			separator--
		}
		after := newline + 1
		if after < len(content) && content[after] == '\n' {
			return separator, after + 1
		}
		if after+1 < len(content) && content[after] == '\r' && content[after+1] == '\n' {
			return separator, after + 2
		}
		position = after
	}
	return len(content), -1
}

func (collector *linuxCollection) procBlocks(content string) iter.Seq2[int, string] {
	return func(yield func(int, string) bool) {
		for start, index := 0, 0; ; index++ {
			if !collector.check() {
				return
			}
			end, next := inventoryProcBlock(content, start)
			// Charge before the caller can allocate its record map.
			if !collector.chargeProc(1, 0, 0) {
				return
			}
			keepGoing := yield(index, content[start:end])
			if !collector.check() || !keepGoing || next < 0 {
				return
			}
			start = next
		}
	}
}

func (collector *linuxCollection) procLines(content string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for start := 0; ; {
			if !collector.check() {
				return
			}
			offset := strings.IndexByte(content[start:], '\n')
			end := len(content)
			if offset >= 0 {
				end = start + offset
			}
			if !collector.chargeProc(0, 1, 0) {
				return
			}
			keepGoing := yield(content[start:end])
			if !collector.check() || !keepGoing || offset < 0 {
				return
			}
			start = end + 1
		}
	}
}

// Match strings.Fields boundaries without allocating a token slice. Invalid
// UTF-8 remains non-whitespace and the original field bytes are retained.
func inventoryProcWords(content string) iter.Seq[string] {
	return func(yield func(string) bool) {
		start := -1
		for offset, value := range content {
			if unicode.IsSpace(value) {
				if start >= 0 {
					if !yield(content[start:offset]) {
						return
					}
					start = -1
				}
			} else if start < 0 {
				start = offset
			}
		}
		if start >= 0 {
			yield(content[start:])
		}
	}
}

func (collector *linuxCollection) procWords(content string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for word := range inventoryProcWords(content) {
			if !collector.chargeProc(0, 1, 0) {
				return
			}
			keepGoing := yield(word)
			if !collector.check() || !keepGoing {
				return
			}
		}
	}
}

// Reserve before ToLower, normalizeValue, key concatenation or map insertion.
// Every value word is a work unit, even when retaining its original spelling;
// this bounds later field slices in unchanged component/classifier helpers.
func (collector *linuxCollection) reserveProcText(name, value string, lower bool, suffix string, normalize bool) bool {
	if !collector.check() {
		return false
	}
	nameBytes := len(name)
	if lower {
		nameBytes = 0
		for _, character := range name {
			nameBytes += utf8.RuneLen(unicode.ToLower(character))
			if nameBytes > maxInventoryProcBytes {
				collector.chargeProc(0, 0, nameBytes)
				return false
			}
		}
		// Invalid UTF-8 is conservatively charged as replacement-rune bytes.
		nameBytes = max(nameBytes, len(name))
	}
	if !collector.chargeProc(0, 0, nameBytes) || !collector.chargeProc(0, 0, len(suffix)) {
		return false
	}
	valueBytes, words := 0, 0
	for word := range collector.procWords(value) {
		if words > 0 {
			valueBytes++
		}
		valueBytes += len(word)
		words++
	}
	if !collector.check() {
		return false
	}
	if !normalize {
		valueBytes = len(value)
	}
	return collector.chargeProc(0, 0, valueBytes)
}

func (collector *linuxCollection) storeProcText(attributes map[string]string, name, value string, lower bool, suffix string, normalize bool) bool {
	if !collector.reserveProcText(name, value, lower, suffix, normalize) {
		return false
	}
	if lower {
		name = strings.ToLower(name)
	}
	if normalize {
		value = normalizeValue(value)
	}
	if !collector.check() {
		return false
	}
	attributes[name+suffix] = value
	return collector.check()
}

func (collector *linuxCollection) procIndex(index int) (string, bool) {
	if !collector.check() {
		return "", false
	}
	if index < 0 || index >= maxInventoryProcRecords {
		collector.err = ErrProcBudget
		return "", false
	}
	digits := 1
	for n := index; n >= 10; n /= 10 {
		digits++
	}
	if !collector.chargeProc(0, 0, digits) {
		return "", false
	}
	return strconv.Itoa(index), collector.check()
}

func (collector *linuxCollection) reserveProcComponent(prefix, id string, attributes map[string]string) bool {
	if !collector.chargeProc(0, 0, len(prefix)) || !collector.chargeProc(0, 0, len(id)) {
		return false
	}
	for name, value := range attributes {
		if !collector.reserveProcText(strings.TrimSpace(name), value, false, "", true) {
			return false
		}
	}
	return collector.check()
}
