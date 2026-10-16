package storage

import "context"

const Schema = "hhsoft.pc-tester.storage-readiness/v1"

type Status string

const (
	StatusPass         Status = "pass"
	StatusInconclusive Status = "inconclusive"
	StatusError        Status = "error"
	StatusAborted      Status = "aborted"
)

type Options struct {
	Root string
}

type DeviceResult struct {
	Interface      string `json:"interface"`
	Kind           string `json:"kind"`
	SizeBytes      uint64 `json:"size_bytes,omitempty"`
	ReadOnly       bool   `json:"read_only,omitempty"`
	Removable      bool   `json:"removable,omitempty"`
	State          string `json:"state,omitempty"`
	NodePresent    bool   `json:"node_present,omitempty"`
	Readable       bool   `json:"readable,omitempty"`
	Mounted        bool   `json:"mounted,omitempty"`
	WritableMount  bool   `json:"writable_mount,omitempty"`
	ReadinessIssue string `json:"readiness_issue,omitempty"`
}

type Result struct {
	Schema             string         `json:"schema"`
	Status             Status         `json:"status"`
	Devices            []DeviceResult `json:"devices,omitempty"`
	EligibleDevices    int            `json:"eligible_devices"`
	ReadableDevices    int            `json:"readable_devices"`
	WritableMounts     int            `json:"writable_mounts"`
	MediaBytes         uint64         `json:"media_bytes"`
	MediaContentRead   bool           `json:"media_content_read"`
	MediaContentWrites bool           `json:"media_content_writes"`
	Detail             string         `json:"detail,omitempty"`
}

func Run(ctx context.Context, options Options) Result {
	return runPlatform(ctx, options)
}
