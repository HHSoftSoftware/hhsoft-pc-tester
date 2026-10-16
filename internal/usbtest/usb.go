package usbtest

import "context"

const Schema = "hhsoft.pc-tester.usb-readiness/v1"

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
	Path            string `json:"path"`
	Kind            string `json:"kind"`
	Class           string `json:"class,omitempty"`
	BusNumber       uint64 `json:"bus_number"`
	DeviceNumber    uint64 `json:"device_number"`
	LinkSpeedMbps   string `json:"link_speed_mbps,omitempty"`
	DownstreamPorts uint64 `json:"downstream_ports,omitempty"`
	AuthorizedKnown bool   `json:"authorized_known"`
	Authorized      bool   `json:"authorized,omitempty"`
	NodePresent     bool   `json:"node_present,omitempty"`
	Readable        bool   `json:"readable,omitempty"`
	ReadinessIssue  string `json:"readiness_issue,omitempty"`
}

type Result struct {
	Schema                string         `json:"schema"`
	Status                Status         `json:"status"`
	Devices               []DeviceResult `json:"devices,omitempty"`
	Controllers           int            `json:"controllers"`
	RootPorts             uint64         `json:"root_ports"`
	AuthorizedDevices     int            `json:"authorized_devices"`
	ReadableDevices       int            `json:"readable_devices"`
	USBTransfersGenerated bool           `json:"usb_transfers_generated"`
	Detail                string         `json:"detail,omitempty"`
}

func Run(ctx context.Context, options Options) Result {
	return runPlatform(ctx, options)
}
