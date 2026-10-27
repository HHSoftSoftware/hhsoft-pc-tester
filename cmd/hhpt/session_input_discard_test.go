package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type unexpectedSessionReader struct{ reads int }

func (reader *unexpectedSessionReader) Read([]byte) (int, error) {
	reader.reads++
	return 0, io.EOF
}

func TestDiscardSessionInputRejectsCompetingReader(t *testing.T) {
	reader := &unexpectedSessionReader{}
	if err := discardSessionInput(t.Context(), reader); !errors.Is(err, errSessionInput) || reader.reads != 0 {
		t.Fatal("unowned input was accepted or consumed")
	}
}

func TestDiscardSessionInputInertFixturesPreserveScriptedFindings(t *testing.T) {
	for _, reader := range []io.Reader{strings.NewReader("p\n"), bytes.NewReader([]byte("p\n")), bytes.NewBufferString("p\n")} {
		if err := discardSessionInput(t.Context(), reader); err != nil {
			t.Fatal(err)
		}
		retained, err := io.ReadAll(reader)
		if err != nil || string(retained) != "p\n" {
			t.Fatal("inert scripted finding was consumed")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := discardSessionInput(ctx, strings.NewReader("p\n")); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled handoff was accepted")
	}
}
