package graphics

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

const VisualSchema = "hhsoft.pc-tester.display-visual/v1"

var VisualCriteria = []string{"pixels", "geometry", "color", "flicker", "scaling", "overscan", "artifacts"}

type InteractiveRunner interface {
	Runner
	RunInteractive(context.Context, time.Duration, string, ...string) (string, error)
}

type VisualResult struct {
	Schema   string           `json:"schema"`
	Status   Status           `json:"status"`
	Patterns []DisplayPattern `json:"patterns,omitempty"`
	Findings []VisualFinding  `json:"operator_findings,omitempty"`
	Warnings []string         `json:"warnings,omitempty"`
}

type DisplayPattern struct {
	Card       string        `json:"card"`
	Connectors []DisplayMode `json:"connectors"`
	Status     Status        `json:"status"`
	Seconds    int           `json:"seconds,omitempty"`
	Detail     string        `json:"detail,omitempty"`
}

type DisplayMode struct {
	Connector     string `json:"connector"`
	PreferredMode string `json:"preferred_mode"`
}

type VisualFinding struct {
	Connector string            `json:"connector"`
	Criteria  map[string]Status `json:"criteria"`
}

func ProbePreferredDisplays(ctx context.Context, root string, displayTime time.Duration, runner InteractiveRunner) VisualResult {
	result := VisualResult{Schema: VisualSchema, Status: StatusSkip}
	cards, _ := discover(root)
	if runner == nil {
		result.Status = StatusInconclusive
		result.Warnings = []string{"preferred-mode visual worker unavailable"}
		return result
	}
	for _, card := range cards {
		pattern := DisplayPattern{Card: card.Device, Status: StatusSkip}
		for _, connector := range card.Connectors {
			if connector.Status == "connected" {
				mode := "unknown"
				if len(connector.AdvertisedModes) > 0 {
					mode = connector.AdvertisedModes[0]
				}
				pattern.Connectors = append(pattern.Connectors, DisplayMode{Connector: connector.Name, PreferredMode: mode})
			}
		}
		if len(pattern.Connectors) == 0 {
			continue
		}
		pattern.Seconds = int(displayTime.Round(time.Second) / time.Second)
		if pattern.Seconds < 1 {
			pattern.Seconds = 1
		}
		output, err := runner.RunInteractive(ctx, displayTime, "/usr/bin/modetest", "-D", card.Device, "-r")
		if err != nil {
			pattern.Status = StatusInconclusive
			pattern.Detail = normalizeDetail(err.Error())
		} else if detail := modesetFailure(output); detail != "" {
			pattern.Status = StatusInconclusive
			pattern.Detail = detail
		} else {
			pattern.Status = StatusPass
		}
		result.Patterns = append(result.Patterns, pattern)
	}
	if len(result.Patterns) == 0 {
		result.Warnings = []string{"no connected display connector with a preferred-mode target"}
		return result
	}
	result.Status = StatusInconclusive
	for _, pattern := range result.Patterns {
		if pattern.Status != StatusPass {
			return result
		}
	}
	return result
}

func ApplyVisualFindingCodes(result *VisualResult, codes []string) error {
	if result == nil {
		return fmt.Errorf("nil visual result")
	}
	connectors := make([]string, 0)
	for _, pattern := range result.Patterns {
		if pattern.Status != StatusPass {
			continue
		}
		for _, connector := range pattern.Connectors {
			connectors = append(connectors, connector.Connector)
		}
	}
	sort.Strings(connectors)
	if len(connectors) == 0 {
		return nil
	}
	if len(codes) != len(connectors) {
		return fmt.Errorf("need one visual finding code per successfully patterned connector")
	}
	findings := make([]VisualFinding, 0, len(connectors))
	overall := StatusPass
	for index, connector := range connectors {
		code := strings.ToLower(codes[index])
		if code == "p" || code == "i" || code == "f" {
			code = strings.Repeat(code, len(VisualCriteria))
		}
		if len(code) != len(VisualCriteria) {
			return fmt.Errorf("finding %d must be P/F/I or %d P/F/I characters", index+1, len(VisualCriteria))
		}
		finding := VisualFinding{Connector: connector, Criteria: make(map[string]Status, len(VisualCriteria))}
		for criterionIndex, criterion := range VisualCriteria {
			var status Status
			switch code[criterionIndex] {
			case 'p':
				status = StatusPass
			case 'f':
				status = StatusFail
				overall = StatusFail
			case 'i':
				status = StatusInconclusive
				if overall == StatusPass {
					overall = StatusInconclusive
				}
			default:
				return fmt.Errorf("finding %d contains a status other than P/F/I", index+1)
			}
			finding.Criteria[criterion] = status
		}
		findings = append(findings, finding)
	}
	result.Findings = findings
	result.Status = overall
	return nil
}

func modesetFailure(output string) string {
	normalized := strings.ToLower(output)
	for _, indicator := range []string{"failed to set mode", "failed fetching preferred mode", "can't find any preferred connector", "no suitable crtc", "could not find a suitable crtc", "failed to create dumb buffer", "permission denied", "atomic commit failed"} {
		if strings.Contains(normalized, indicator) {
			return normalizeDetail(output)
		}
	}
	return ""
}
