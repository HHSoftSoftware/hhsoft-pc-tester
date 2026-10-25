package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
)

func prepareSessionInput(ctx context.Context, input io.Reader) (context.Context, io.Reader, func() error, error) {
	switch input.(type) {
	case *strings.Reader, *bytes.Reader, *bytes.Buffer:
		return ctx, input, func() error { return nil }, nil
	}
	return prepareSessionConsole(ctx, input)
}

var errSessionInput = errors.New("session console input could not be safely controlled")
