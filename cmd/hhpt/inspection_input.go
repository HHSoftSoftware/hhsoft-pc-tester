package main

import (
	"bytes"
	"context"
	"io"
	"strings"
)

// The broker returns completed lines only. A nonempty line proves completion
// even alongside an error; an empty line plus an error does not.
func readInspectionLine(ctx context.Context, input io.Reader) (string, bool, error) {
	if nilRetestInputValue(ctx) || nilRetestInputValue(input) {
		return "", false, errSessionInput
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if broker, ok := input.(findingLineReader); ok {
		line, err := broker.ReadFindingLine(ctx, maximumFindingLineBytes)
		if len(line) > maximumFindingLineBytes {
			return "", false, errSessionInput
		}
		return line, line != "" || err == nil, err
	}
	switch input.(type) {
	case *strings.Reader, *bytes.Reader, *bytes.Buffer:
		return readInspectionBytes(ctx, input.Read)
	default:
		return "", false, errFindingInputUnsupported
	}
}

// Only the finite memory adapters above reach this byte loop in production.
// The function seam exercises read protocol faults without admitting arbitrary
// blocking readers to the command.
func readInspectionBytes(ctx context.Context, read func([]byte) (int, error)) (string, bool, error) {
	if nilRetestInputValue(ctx) || read == nil {
		return "", false, errSessionInput
	}
	line := make([]byte, 0, 64)
	var character [1]byte
	for {
		if err := ctx.Err(); err != nil {
			return "", false, err
		}
		count, err := read(character[:])
		if count < 0 || count > len(character) {
			return "", false, errSessionInput
		}
		if count == 1 && (character[0] == '\r' || character[0] == '\n') {
			if err == io.EOF {
				err = nil
			}
			return string(line), true, err
		}
		if count == 1 && len(line) == maximumFindingLineBytes {
			return "", false, errSessionInput
		}
		if err != nil {
			return "", false, err
		}
		if count == 0 {
			return "", false, errSessionInput
		}
		if err := ctx.Err(); err != nil {
			return "", false, err
		}
		line = append(line, character[0])
	}
}
