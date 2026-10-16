package inputtest

import (
	"context"
	"time"
)

const Schema = "hhsoft.pc-tester.input-readiness/v1"

type Status string

const (
	StatusPass         Status = "pass"
	StatusInconclusive Status = "inconclusive"
	StatusError        Status = "error"
	StatusAborted      Status = "aborted"
)

type Options struct {
	Root       string
	Duration   time.Duration
	Categories []string
}

type DeviceResult struct {
	Interface      string `json:"interface"`
	Category       string `json:"category"`
	Name           string `json:"name,omitempty"`
	Readable       bool   `json:"readable"`
	Events         uint64 `json:"events,omitempty"`
	KeyPresses     uint64 `json:"key_presses,omitempty"`
	KeyReleases    uint64 `json:"key_releases,omitempty"`
	RelativeEvents uint64 `json:"relative_events,omitempty"`
	AbsoluteEvents uint64 `json:"absolute_events,omitempty"`
	SwitchEvents   uint64 `json:"switch_events,omitempty"`
	EvidenceLoss   bool   `json:"evidence_loss,omitempty"`
}

type Result struct {
	Schema               string         `json:"schema"`
	Status               Status         `json:"status"`
	Mode                 string         `json:"mode"`
	RequestedCategories  []string       `json:"requested_categories,omitempty"`
	ObservedCategories   []string       `json:"observed_categories,omitempty"`
	Devices              []DeviceResult `json:"devices,omitempty"`
	DurationMilliseconds int64          `json:"duration_ms"`
	Detail               string         `json:"detail,omitempty"`
}

func Run(ctx context.Context, options Options) Result {
	return runPlatform(ctx, options)
}
