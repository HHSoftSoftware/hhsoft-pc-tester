package graphics

import (
	"context"
	"testing"
	"time"
)

type mixedDisplayRunner struct{ fakeInteractiveRunner }

func (runner *mixedDisplayRunner) RunInteractive(_ context.Context, _ time.Duration, _ string, arguments ...string) (string, error) {
	if arguments[1] == "/dev/dri/card1" {
		return "failed to set mode: denied", nil
	}
	return "", nil
}

func TestDisplayAcceptanceRequiresEveryConnectedOutputToShowItsPattern(t *testing.T) {
	root := t.TempDir()
	for _, card := range []string{"card0", "card1"} {
		writeFile(t, root, "sys/class/drm/"+card+"/device/uevent", "DRIVER=fixture\n")
		writeFile(t, root, "sys/class/drm/"+card+"-HDMI-A-1/status", "connected\n")
		writeFile(t, root, "sys/class/drm/"+card+"-HDMI-A-1/modes", "1920x1080\n")
	}
	result := ProbePreferredDisplays(context.Background(), root, 5*time.Second, &mixedDisplayRunner{})
	if len(result.Patterns) != 2 || result.Patterns[0].Status != StatusPass || result.Patterns[1].Status != StatusInconclusive {
		t.Fatalf("expected one visible pattern and one failed modeset: %#v", result)
	}
	if err := ApplyVisualFindingCodes(&result, []string{"p"}); err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusInconclusive {
		t.Fatalf("accepting one display must not accept an untested display: %#v", result)
	}
	if len(result.Findings) != 1 || result.Findings[0].Connector != "card0-HDMI-A-1" {
		t.Fatalf("retain observations only for the displayed pattern: %#v", result.Findings)
	}
	for _, criterion := range VisualCriteria {
		if result.Findings[0].Criteria[criterion] != StatusPass {
			t.Fatalf("partial observations changed: %#v", result.Findings)
		}
	}
}

func TestOtherDisplayCannotEraseFailedOrIncompletePattern(t *testing.T) {
	for _, state := range []Status{StatusFail, "error", StatusAborted, StatusInconclusive, StatusSkip} {
		for _, reverse := range []bool{false, true} {
			order := "first"
			if reverse {
				order = "last"
			}
			t.Run(string(state)+"/"+order, func(t *testing.T) {
				patterns := []DisplayPattern{
					{Card: "/dev/dri/card0", Status: state, Connectors: []DisplayMode{{Connector: "card0-DP-1"}}},
					{Card: "/dev/dri/card1", Status: StatusPass, Connectors: []DisplayMode{{Connector: "card1-DP-1"}}},
				}
				if reverse {
					patterns[0], patterns[1] = patterns[1], patterns[0]
				}
				result := VisualResult{Schema: VisualSchema, Status: StatusInconclusive, Patterns: patterns}
				if err := ApplyVisualFindingCodes(&result, []string{"p"}); err != nil {
					t.Fatal(err)
				}
				want := state
				if state == StatusSkip {
					want = StatusInconclusive
				}
				if result.Status != want || len(result.Findings) != 1 || result.Findings[0].Connector != "card1-DP-1" {
					t.Fatalf("accepted other display erased %s outcome: %#v", state, result)
				}
			})
		}
	}
}

func TestEveryDisplayedOutputAlsoRequiresCompleteOperatorAcceptance(t *testing.T) {
	for _, test := range []struct {
		name, secondCode string
		want             Status
	}{
		{"all accepted", "p", StatusPass},
		{"observation incomplete", "i", StatusInconclusive},
		{"visible defect and partial observations", "ppfpiip", StatusFail},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := VisualResult{Schema: VisualSchema, Status: StatusInconclusive, Patterns: []DisplayPattern{
				{Card: "/dev/dri/card0", Status: StatusPass, Connectors: []DisplayMode{{Connector: "card0-DP-1"}, {Connector: "card0-HDMI-A-1"}}},
			}}
			if err := ApplyVisualFindingCodes(&result, []string{"p", test.secondCode}); err != nil {
				t.Fatal(err)
			}
			if result.Status != test.want || len(result.Findings) != 2 {
				t.Fatalf("unexpected overall acceptance: %#v", result)
			}
			if test.secondCode == "ppfpiip" {
				criteria := result.Findings[1].Criteria
				if criteria["pixels"] != StatusPass || criteria["color"] != StatusFail || criteria["scaling"] != StatusInconclusive || criteria["overscan"] != StatusInconclusive {
					t.Fatalf("individual observations were lost: %#v", criteria)
				}
			}
		})
	}
}

func TestNoDisplayRemainsSkippedWithoutFabricatedAcceptance(t *testing.T) {
	result := VisualResult{Schema: VisualSchema, Status: StatusSkip}
	if err := ApplyVisualFindingCodes(&result, nil); err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusSkip || len(result.Findings) != 0 {
		t.Fatalf("no display must not become a pass: %#v", result)
	}
}
