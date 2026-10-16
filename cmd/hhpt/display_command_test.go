package main

import (
	"bytes"
	"context"
	"encoding/json"
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
