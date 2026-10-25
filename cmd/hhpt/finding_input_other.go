//go:build !linux

package main

import (
	"context"
	"os"
)

func findingFileReader(context.Context, *os.File) (func([]byte) (int, error), func() error, error) {
	return nil, nil, errFindingInputUnsupported
}
