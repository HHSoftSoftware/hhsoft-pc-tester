package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
)

var errInspectionConditionPrompt = errors.New("exterior condition prompt failed")

type inspectionConditionReader interface {
	ReadInspectionConditions(context.Context, func() error) ([]exteriorinspection.ConditionResponseV2, error)
}

// One batch belongs to one command. It retains at most four completed lines and
// one bounded partial line; partial text is never returned as an observation.
type inspectionConditionBatch struct {
	lines     [4]string
	completed int
	text      []byte
	pendingCR bool
}

func (batch *inspectionConditionBatch) consume(character byte) (bool, error) {
	if batch.completed < 0 || batch.completed >= 4 {
		return false, errSessionInput
	}
	if batch.pendingCR {
		batch.pendingCR = false
		if character != '\n' {
			return false, errSessionInput
		}
	}
	if character == '\r' {
		batch.pendingCR = true
		return false, nil
	}
	if character == '\n' {
		batch.lines[batch.completed] = string(batch.text)
		batch.completed++
		batch.text = batch.text[:0]
		return batch.completed == 4, nil
	}
	if len(batch.text) == maximumFindingLineBytes {
		return false, errSessionInput
	}
	batch.text = append(batch.text, character)
	return false, nil
}

func (batch *inspectionConditionBatch) responses(incomplete bool) []exteriorinspection.ConditionResponseV2 {
	out := make([]exteriorinspection.ConditionResponseV2, 0, 4)
	for index := 0; index < batch.completed; index++ {
		out = append(out, exteriorinspection.ConditionResponseV2{Text: batch.lines[index], Complete: true})
	}
	if incomplete && batch.completed < 4 {
		out = append(out, exteriorinspection.ConditionResponseV2{})
	}
	return out
}

func readInspectionConditionLines(ctx context.Context, input io.Reader, prompt func() error) ([]exteriorinspection.ConditionResponseV2, error) {
	if nilRetestInputValue(ctx) || nilRetestInputValue(input) || prompt == nil {
		return nil, errSessionInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if source, ok := input.(inspectionConditionReader); ok {
		return source.ReadInspectionConditions(ctx, prompt)
	}
	switch input.(type) {
	case *strings.Reader, *bytes.Reader, *bytes.Buffer:
		if err := prompt(); err != nil {
			return nil, errInspectionConditionPrompt
		}
		return readInspectionConditionBytes(ctx, input.Read)
	default:
		return nil, errFindingInputUnsupported
	}
}

// Only the explicit finite memory adapters above use this synchronous byte seam.
func readInspectionConditionBytes(ctx context.Context, read func([]byte) (int, error)) ([]exteriorinspection.ConditionResponseV2, error) {
	var batch inspectionConditionBatch
	if nilRetestInputValue(ctx) || read == nil {
		return nil, errSessionInput
	}
	var character [1]byte
	for {
		if err := inspectionConditionContextStop(ctx); err != nil {
			return batch.responses(true), err
		}
		count, err := read(character[:])
		if count < 0 || count > 1 {
			return batch.responses(true), errSessionInput
		}
		completedBefore := batch.completed
		if count == 1 {
			done, framingErr := batch.consume(character[0])
			if framingErr != nil {
				return batch.responses(true), framingErr
			}
			if done {
				if err == io.EOF {
					err = nil
				}
				return batch.responses(false), err
			}
			if batch.completed != completedBefore && err == nil {
				if stop := inspectionConditionContextStop(ctx); stop != nil {
					return batch.responses(false), stop
				}
			}
		}
		if err != nil {
			return batch.responses(batch.completed == completedBefore), err
		}
		if count == 0 {
			return batch.responses(true), errSessionInput
		}
	}
}

func inspectionConditionContextStop(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}
