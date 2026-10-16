package networktest

import "context"

const Schema = "hhsoft.pc-tester.network-readiness/v1"

type Status string

const (
	StatusPass         Status = "pass"
	StatusInconclusive Status = "inconclusive"
	StatusError        Status = "error"
	StatusAborted      Status = "aborted"
)

type Options struct {
	Root        string
	AllowActive bool
}

type InterfaceResult struct {
	Interface        string `json:"interface"`
	Kind             string `json:"kind"`
	Driver           string `json:"driver,omitempty"`
	DriverBound      bool   `json:"driver_bound"`
	AdministrativeUp bool   `json:"administrative_up"`
	OperationalState string `json:"operational_state,omitempty"`
	CarrierDetected  bool   `json:"carrier_detected,omitempty"`
	Carrier          bool   `json:"carrier,omitempty"`
	SpeedMbps        uint64 `json:"speed_mbps,omitempty"`
	Duplex           string `json:"duplex,omitempty"`
	ReadinessIssue   string `json:"readiness_issue,omitempty"`
}

type Result struct {
	Schema                  string            `json:"schema"`
	Status                  Status            `json:"status"`
	RequireOffline          bool              `json:"require_offline"`
	Interfaces              []InterfaceResult `json:"interfaces,omitempty"`
	HardwareInterfaces      int               `json:"hardware_interfaces"`
	DriverBoundInterfaces   int               `json:"driver_bound_interfaces"`
	ActiveNonLoopInterfaces int               `json:"active_non_loop_interfaces"`
	NetworkTrafficGenerated bool              `json:"network_traffic_generated"`
	Detail                  string            `json:"detail,omitempty"`
}

func Run(ctx context.Context, options Options) Result {
	return runPlatform(ctx, options)
}
