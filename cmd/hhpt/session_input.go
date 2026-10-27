package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
)

func prepareSessionInput(ctx context.Context, input io.Reader) (context.Context, io.Reader, func() error, error) {
	switch input.(type) {
	case *strings.Reader, *bytes.Reader, *bytes.Buffer:
		return ctx, input, func() error { return nil }, nil
	}
	owned, source, finish, err := prepareSessionConsole(ctx, input)
	// Inner finalization and the outer command boundary share one join attempt
	// and its retained receipt, including an uncertain result.
	return owned, source, sync.OnceValue(finish), err
}

var errSessionInput = errors.New("session console input could not be safely controlled")
var errSessionInputCleanup = errors.New("session console cleanup could not be confirmed")

// Only the existing console owner may consume queued input between workers.
// In-memory command fixtures have no concurrent terminal event stream.
func discardSessionInput(ctx context.Context, input io.Reader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if owner, ok := input.(interface {
		DiscardUnpromptedInput(context.Context) error
	}); ok {
		return owner.DiscardUnpromptedInput(ctx)
	}
	switch input.(type) {
	case *strings.Reader, *bytes.Reader, *bytes.Buffer:
		return nil
	default:
		return errSessionInput
	}
}
