//go:build !linux

package main

import (
	"context"
	"io"
)

func prepareSessionConsole(ctx context.Context, input io.Reader) (context.Context, io.Reader, func() error, error) {
	return ctx, input, func() error { return nil }, errSessionInput
}
