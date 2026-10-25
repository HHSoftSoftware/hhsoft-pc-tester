package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

const maximumFindingLineBytes = 4096

var errFindingInputUnsupported = errors.New("operator input does not support bounded reads")

// Session input brokers own their reader and must return only completed lines,
// never partial text. A completed line may accompany a cancellation error so
// an observed defect is retained without accepting late passing observations.
type findingLineReader interface {
	ReadFindingLine(context.Context, int) (string, error)
}

// A finding reader never starts background reads or reads beyond its newline.
// Arbitrary io.Reader implementations cannot promise cancellation and therefore
// must not be called by this bounded prompt.
func readFindingLine(ctx context.Context, input io.Reader, timeout time.Duration) (line string, timedOut bool, err error) {
	if err := commandStopReason(ctx); err != nil {
		return "", false, err
	}
	readContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var read func([]byte) (int, error)
	var restore func() error
	if source, ok := input.(findingLineReader); ok {
		line, err = source.ReadFindingLine(readContext, maximumFindingLineBytes)
		if len(line) > maximumFindingLineBytes {
			return "", false, errors.New("operator finding exceeds the input limit")
		}
	} else {
		switch source := input.(type) {
		case *strings.Reader, *bytes.Reader, *bytes.Buffer:
			read = input.Read
		case *os.File:
			read, restore, err = findingFileReader(readContext, source)
			if err != nil {
				return "", false, err
			}
		default:
			return "", false, errFindingInputUnsupported
		}
		if restore != nil {
			defer func() {
				if restoreErr := restore(); restoreErr != nil {
					line, timedOut, err = "", false, restoreErr
				}
			}()
		}
		line, err = readBoundedFinding(readContext, read)
	}
	if stop := commandStopReason(ctx); stop != nil {
		return line, false, stop
	}
	if errors.Is(commandStopReason(readContext), context.DeadlineExceeded) {
		return line, true, nil
	}
	return line, false, err
}

func readBoundedFinding(ctx context.Context, read func([]byte) (int, error)) (string, error) {
	line := make([]byte, 0, 64)
	var character [1]byte
	for len(line) <= maximumFindingLineBytes {
		if err := commandStopReason(ctx); err != nil {
			return "", err
		}
		count, err := read(character[:])
		if count == 1 && character[0] == '\n' {
			if stop := commandStopReason(ctx); stop != nil {
				return strings.TrimSpace(string(line)), stop
			}
			return strings.TrimSpace(string(line)), err
		}
		if stop := commandStopReason(ctx); stop != nil {
			return "", stop
		}
		if err != nil {
			return "", err
		}
		if count == 0 {
			return "", io.EOF
		}
		line = append(line, character[0])
	}
	return "", errors.New("operator finding exceeds the input limit")
}
