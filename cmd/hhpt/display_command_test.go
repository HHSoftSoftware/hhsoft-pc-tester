package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/graphics"
)

func TestDisplayCollectsCompactOperatorFindings(t *testing.T) {
	probe := func(context.Context, string, time.Duration, graphics.InteractiveRunner) graphics.VisualResult {
		return graphics.VisualResult{Schema: graphics.VisualSchema, Status: graphics.StatusInconclusive, Patterns: []graphics.DisplayPattern{{Card: "/dev/dri/card0", Status: graphics.StatusPass, Connectors: []graphics.DisplayMode{{Connector: "card0-DP-1", PreferredMode: "2560x1440"}, {Connector: "card0-HDMI-A-1", PreferredMode: "1920x1080"}}}}}
	}
	var stdout, stderr bytes.Buffer
	if result := runDisplayWith(context.Background(), nil, strings.NewReader("ppppppp,ppfpppp\n"), &stdout, &stderr, probe); result != 0 {
		t.Fatalf("display returned %d: %s", result, stderr.String())
	}
	var evidence graphics.VisualResult
	if err := json.Unmarshal(stdout.Bytes()[strings.Index(stdout.String(), "{\n"):], &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Status != graphics.StatusFail || evidence.Findings[1].Criteria["color"] != graphics.StatusFail {
		t.Fatalf("unexpected evidence: %#v", evidence)
	}
}

func TestFindingCodeParsingSupportsGlobalAndRejectsAmbiguity(t *testing.T) {
	if values := parseFindingCodes("P", 2); len(values) != 2 || values[1] != "P" {
		t.Fatalf("global pass = %#v", values)
	}
	if values := parseFindingCodes("ppppppp", 2); values != nil {
		t.Fatalf("ambiguous code accepted: %#v", values)
	}
}

func TestDisplayRequiresCompletedOperatorResponse(t *testing.T) {
	for _, test := range []struct {
		name   string
		input  io.Reader
		status graphics.Status
	}{
		{"complete-pass", strings.NewReader("P\n"), graphics.StatusPass},
		{"complete-fail", strings.NewReader("F\n"), graphics.StatusFail},
		{"empty-line", strings.NewReader("\n"), graphics.StatusInconclusive},
		{"empty-EOF", strings.NewReader(""), graphics.StatusInconclusive},
		{"partial-pass-EOF", strings.NewReader("P"), graphics.StatusInconclusive},
		{"partial-full-code-EOF", strings.NewReader("ppppppp"), graphics.StatusInconclusive},
		{"partial-pass-error", &findingReadError{data: "P", err: errors.New("synthetic input loss")}, graphics.StatusInconclusive},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe := func(context.Context, string, time.Duration, graphics.InteractiveRunner) graphics.VisualResult {
				return graphics.VisualResult{Schema: graphics.VisualSchema, Status: graphics.StatusInconclusive,
					Patterns: []graphics.DisplayPattern{{Card: "/dev/dri/card0", Status: graphics.StatusPass,
						Connectors: []graphics.DisplayMode{{Connector: "card0-DP-1", PreferredMode: "1920x1080"}}}}}
			}
			var stdout, stderr bytes.Buffer
			if code := runDisplayWith(t.Context(), nil, test.input, &stdout, &stderr, probe); code != 0 {
				t.Fatalf("display returned %d: %s", code, stderr.String())
			}
			start := strings.Index(stdout.String(), "{\n")
			if start < 0 {
				t.Fatal("display did not preserve its evidence")
			}
			var evidence graphics.VisualResult
			if err := json.Unmarshal(stdout.Bytes()[start:], &evidence); err != nil {
				t.Fatal(err)
			}
			if evidence.Status != test.status || len(evidence.Findings) != 1 {
				t.Fatalf("incomplete response changed acceptance: %#v", evidence)
			}
			for _, criterion := range graphics.VisualCriteria {
				if evidence.Findings[0].Criteria[criterion] != test.status {
					t.Fatalf("criterion %s = %s; want %s", criterion, evidence.Findings[0].Criteria[criterion], test.status)
				}
			}
		})
	}
}

type findingReadError struct {
	data string
	err  error
}

func (reader *findingReadError) Read(buffer []byte) (int, error) {
	count := copy(buffer, reader.data)
	reader.data = reader.data[count:]
	return count, reader.err
}
