package audio

import "context"

const Schema = "hhsoft.pc-tester.audio-readiness/v1"

type Status string

const (
	StatusPass         Status = "pass"
	StatusInconclusive Status = "inconclusive"
	StatusError        Status = "error"
	StatusAborted      Status = "aborted"
)

type Readiness string

const (
	ReadinessUnknown      Readiness = "unknown"
	ReadinessNoDevice     Readiness = "no_device"
	ReadinessMissingNodes Readiness = "missing_nodes"
	ReadinessNoPCM        Readiness = "no_pcm"
	ReadinessReady        Readiness = "ready"
)

type Options struct {
	Root string
}

type NodeResult struct {
	Path           string `json:"path"`
	Present        bool   `json:"present"`
	Ready          bool   `json:"ready"`
	ReadinessIssue string `json:"readiness_issue,omitempty"`
}

type CardResult struct {
	Interface string     `json:"interface"`
	Card      uint32     `json:"card"`
	Control   NodeResult `json:"control"`
}

type PCMResult struct {
	Interface string     `json:"interface"`
	Card      uint32     `json:"card"`
	Device    uint32     `json:"device"`
	Direction string     `json:"direction"`
	Node      NodeResult `json:"node"`
}

type Result struct {
	Schema             string       `json:"schema"`
	Status             Status       `json:"status"`
	Readiness          Readiness    `json:"readiness"`
	Cards              []CardResult `json:"cards,omitempty"`
	Endpoints          []PCMResult  `json:"endpoints,omitempty"`
	PlaybackEndpoints  int          `json:"playback_endpoints"`
	CaptureEndpoints   int          `json:"capture_endpoints"`
	ReadyEndpoints     int          `json:"ready_endpoints"`
	MediaStreamsOpened bool         `json:"media_streams_opened"`
	Detail             string       `json:"detail,omitempty"`
}

func Run(ctx context.Context, options Options) Result {
	return runPlatform(ctx, options)
}
