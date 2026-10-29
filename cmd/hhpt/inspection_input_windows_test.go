//go:build windows

package main

import (
	"bytes"
	"os"
	"testing"
)

func TestInspectionUnsupportedConsoleDoesNotReadOrWriteEvidence(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "ordinary-input")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString("N\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runInspection(t.Context(), nil, file, &stdout, &stderr)
	position, err := file.Seek(0, 1)
	if code != 1 || err != nil || position != 0 || stdout.Len() != 0 {
		t.Fatal("unsupported console acquired input or observation credit")
	}
}
