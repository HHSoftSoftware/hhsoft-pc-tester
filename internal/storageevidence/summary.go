package storageevidence

import "github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"

type View struct {
	Scope, Status, StopReason                                                              string
	AcceptedReadBytes, AcceptedComparedBytes, DiagnosticReadBytes, DiagnosticComparedBytes uint64
	AcceptedMatchedRanges                                                                  uint32
	FailureObserved, DiagnosticFailure, CleanupConfirmed                                   bool
	Devices                                                                                []DeviceView
}
type DeviceView struct {
	Interface, Status, Exclusion string
	Accepted                     bool
	ReadBytes, ComparedBytes     uint64
	MatchedRanges                uint8
}

// Summary returns a validated, owned view for rendering. Diagnostic counters
// stay separate and do not increase accepted coverage.
func Summary(result storagetest.Result) (View, error) {
	if err := ValidateResult(result); err != nil {
		return View{}, err
	}
	view := View{Scope: result.Scope, Status: result.Status, StopReason: result.StopReason, AcceptedReadBytes: result.AcceptedReadBytes, AcceptedComparedBytes: result.AcceptedComparedBytes, AcceptedMatchedRanges: result.AcceptedMatchedRanges, FailureObserved: result.FailureObserved, DiagnosticFailure: result.DiagnosticFailure, CleanupConfirmed: result.CleanupConfirmed, Devices: []DeviceView{}}
	for _, device := range result.Devices {
		row := DeviceView{Interface: device.Interface, Status: device.Status, Exclusion: device.Exclusion, Accepted: device.Accepted}
		if device.Observation != nil {
			row.ReadBytes = device.Observation.ReadBytes
			row.ComparedBytes = device.Observation.ComparedBytes
			row.MatchedRanges = device.Observation.MatchedRanges
			if !device.Accepted {
				view.DiagnosticReadBytes += row.ReadBytes
				view.DiagnosticComparedBytes += row.ComparedBytes
			}
		}
		view.Devices = append(view.Devices, row)
	}
	return view, nil
}
