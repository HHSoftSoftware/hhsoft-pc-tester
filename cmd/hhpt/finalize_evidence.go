package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/audio"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/evidencejson"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/exteriorinspection"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/graphics"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/keyboardevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/networktest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/pointerevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quickevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storage"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/touchpadevidence"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/usbtest"
)

var errUnsupportedEvidence = errors.New("worker evidence is incomplete or contradicts its claimed result")
var errDisplayFailureDowngrade = errors.New("display evidence would downgrade an explicit failure")
var errMemoryFailureDowngrade = quickevidence.ErrFailureDowngrade
var errGraphicsFailureDowngrade = errors.New("graphics evidence would downgrade an explicit failure")
var errQuickCleanupUnconfirmed = quickevidence.ErrCleanupUnconfirmed
var errInvalidQuickSafety = quickevidence.ErrInvalid

// Worker schemas describe observations, not an authority to declare success.
// Acceptance checks the documented quick-test guarantees independently of the
// worker's status. It does not infer health, performance, or physical coverage
// from a node-readiness result. Partial non-pass observations remain reportable.
func testEvidenceStatus(encoded []byte, expectedSchema string) (string, error) {
	if expectedSchema == inputtest.TouchpadSchema {
		value, err := touchpadevidence.Decode(encoded)
		if err != nil {
			return "", err
		}
		return string(value.Status), nil
	}
	if expectedSchema == quick.CPUStressSchema {
		value, _, err := quickevidence.DecodeCPUStress(encoded)
		if err != nil {
			return "", err
		}
		return string(value.Status), nil
	}
	if expectedSchema == exteriorinspection.Schema {
		value, err := exteriorinspection.Decode(encoded)
		if err != nil {
			return "", err
		}
		return value.Status, nil
	}
	if expectedSchema == inputtest.PointerSchema || expectedSchema == inputtest.PointerSchemaV1 {
		value, err := pointerevidence.Decode(encoded)
		if err != nil || value.Schema != expectedSchema {
			return "", pointerevidence.ErrInvalid
		}
		return string(value.Status), nil
	}
	if expectedSchema == inputtest.KeyboardSchema {
		value, err := keyboardevidence.Decode(encoded)
		if err != nil {
			return "", err
		}
		return string(value.Status), nil
	}
	invalid := errUnsupportedEvidence
	quickEvidence := expectedSchema == quick.CPUSchema || expectedSchema == quick.MemorySchema
	if quickEvidence {
		// Current quick evidence must establish cleanup. A malformed or older
		// file cannot be replaced with an ordinary execution-error note.
		invalid = errInvalidQuickSafety
	}
	var header struct {
		Schema string `json:"schema"`
		Status string `json:"status"`
	}
	if len(encoded) == 0 || len(encoded) > 1<<20 || json.Unmarshal(encoded, &header) != nil || header.Schema != expectedSchema {
		return "", invalid
	}
	if header.Status != "pass" && header.Status != "inconclusive" && header.Status != "error" && header.Status != "aborted" &&
		!(header.Status == "fail" && (expectedSchema == quick.CPUSchema || expectedSchema == quick.MemorySchema || expectedSchema == graphics.Schema || expectedSchema == graphics.VisualSchema)) &&
		!(header.Status == "skip" && (expectedSchema == graphics.Schema || expectedSchema == graphics.VisualSchema)) {
		return "", invalid
	}
	var err error
	switch expectedSchema {
	case quick.CPUSchema, quick.MemorySchema:
		id := "cpu.quick"
		if expectedSchema == quick.MemorySchema {
			id = "memory.quick"
		}
		value, localErr := quickevidence.DecodeLocal(id, encoded)
		if localErr != nil {
			return "", localErr
		}
		return string(value.Status), nil
	case storage.Schema:
		err = acceptEvidence(encoded, validateStorageEvidence)
	case networktest.Schema:
		err = acceptEvidence(encoded, validateNetworkEvidence)
	case usbtest.Schema:
		err = acceptEvidence(encoded, validateUSBEvidence)
	case inputtest.Schema:
		err = acceptEvidence(encoded, validateInputEvidence)
	case audio.Schema:
		err = acceptEvidence(encoded, validateAudioEvidence)
	case graphics.Schema:
		failureDowngrade := false
		err = acceptEvidence(encoded, func(result graphics.Result) bool {
			failureDowngrade = result.Status != graphics.StatusFail && graphicsHasExplicitFailure(result)
			return validateGraphicsEvidence(result)
		})
		if failureDowngrade {
			return "", errGraphicsFailureDowngrade
		}
	case graphics.VisualSchema:
		failureDowngrade := false
		err = acceptEvidence(encoded, func(result graphics.VisualResult) bool {
			failureDowngrade = result.Status != graphics.StatusFail && displayHasExplicitFailure(result)
			return validateDisplayEvidence(result)
		})
		if failureDowngrade {
			return "", errDisplayFailureDowngrade
		}
	default:
		err = errUnsupportedEvidence
	}
	if err != nil {
		return "", invalid
	}
	return header.Status, nil
}

func newValidatedQuickTestResult(id string, version int, status app.TestStatus, evidence any) (app.TestResult, error) {
	if (id == "cpu.quick" || id == "memory.quick") && version != 2 || id == exteriorinspection.TestID && version != 1 {
		return app.TestResult{}, errUnsupportedEvidence
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return app.TestResult{}, err
	}
	for _, definition := range quickResultDefinitions {
		if definition.id != id {
			continue
		}
		source, err := testEvidenceStatus(encoded, definition.schema)
		if err != nil {
			return app.TestResult{}, err
		}
		if source != string(status) {
			return app.TestResult{}, errUnsupportedEvidence
		}
		return app.NewEncodedTestResult(id, version, status, encoded)
	}
	return app.TestResult{}, errUnsupportedEvidence
}

func acceptEvidence[T any](encoded []byte, validate func(T) bool) error {
	var evidence T
	if err := evidencejson.ValidateShape(encoded, reflect.TypeOf(evidence)); err != nil {
		return errUnsupportedEvidence
	}
	if err := json.Unmarshal(encoded, &evidence); err != nil || !validate(evidence) {
		return errUnsupportedEvidence
	}
	return nil
}

func validateStorageEvidence(result storage.Result) bool {
	if result.MediaContentRead || result.MediaContentWrites || result.EligibleDevices < 0 || result.ReadableDevices < 0 || result.WritableMounts < 0 {
		return false
	}
	if result.Status != storage.StatusPass {
		return true
	}
	if result.EligibleDevices == 0 || result.ReadableDevices != result.EligibleDevices || result.WritableMounts != 0 {
		return false
	}
	eligible, capacity, seen := 0, uint64(0), map[string]bool{}
	for _, device := range result.Devices {
		if !uniqueEvidenceName(seen, device.Interface) || device.WritableMount {
			return false
		}
		switch device.Kind {
		case "media":
			if !device.NodePresent || !device.Readable || device.SizeBytes == 0 || device.ReadinessIssue != "" || math.MaxUint64-capacity < device.SizeBytes {
				return false
			}
			switch strings.ToLower(strings.TrimSpace(device.State)) {
			case "offline", "dead", "blocked", "quiesce":
				return false
			}
			eligible++
			capacity += device.SizeBytes
		case "partition", "empty":
		default:
			return false
		}
	}
	return eligible == result.EligibleDevices && capacity == result.MediaBytes
}

func validateNetworkEvidence(result networktest.Result) bool {
	if result.NetworkTrafficGenerated || result.HardwareInterfaces < 0 || result.DriverBoundInterfaces < 0 || result.ActiveNonLoopInterfaces < 0 {
		return false
	}
	if result.Status != networktest.StatusPass {
		return true
	}
	if !result.RequireOffline || result.HardwareInterfaces == 0 || len(result.Interfaces) != result.HardwareInterfaces || result.DriverBoundInterfaces != result.HardwareInterfaces || result.ActiveNonLoopInterfaces != 0 {
		return false
	}
	seen := map[string]bool{}
	for _, network := range result.Interfaces {
		if !uniqueEvidenceName(seen, network.Interface) || network.Interface == "lo" || !network.DriverBound || network.Driver == "" || network.AdministrativeUp || network.ReadinessIssue != "" ||
			(network.Kind != "ethernet" && network.Kind != "wifi") {
			return false
		}
	}
	return true
}

func validateUSBEvidence(result usbtest.Result) bool {
	if result.USBTransfersGenerated || result.Controllers < 0 || result.AuthorizedDevices < 0 || result.ReadableDevices < 0 {
		return false
	}
	if result.Status != usbtest.StatusPass {
		return true
	}
	if result.Controllers == 0 || len(result.Devices) == 0 || result.AuthorizedDevices != len(result.Devices) || result.ReadableDevices != len(result.Devices) {
		return false
	}
	controllers, ports := 0, uint64(0)
	seen, addresses := map[string]bool{}, map[string]bool{}
	for _, device := range result.Devices {
		if !uniqueEvidenceName(seen, device.Path) || !uniqueEvidenceName(addresses, fmt.Sprintf("%d:%d", device.BusNumber, device.DeviceNumber)) ||
			device.BusNumber == 0 || device.BusNumber > 255 || device.DeviceNumber == 0 || device.DeviceNumber > 255 ||
			!device.AuthorizedKnown || !device.Authorized || !device.NodePresent || !device.Readable || device.ReadinessIssue != "" {
			return false
		}
		switch device.Kind {
		case "root_hub":
			if math.MaxUint64-ports < device.DownstreamPorts {
				return false
			}
			controllers++
			ports += device.DownstreamPorts
		case "hub", "device":
		default:
			return false
		}
	}
	return controllers == result.Controllers && ports == result.RootPorts
}

func uniqueEvidenceName(seen map[string]bool, value string) bool {
	if strings.TrimSpace(value) == "" || seen[value] {
		return false
	}
	seen[value] = true
	return true
}

func validateInputEvidence(result inputtest.Result) bool {
	if result.DurationMilliseconds < 0 {
		return false
	}
	if result.Status != inputtest.StatusPass {
		return true
	}
	if (result.Mode != "readiness" && result.Mode != "activity") || len(result.RequestedCategories) == 0 || len(result.Devices) == 0 || (result.Mode == "readiness" && result.DurationMilliseconds != 0) {
		return false
	}
	requested, observed, supported := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, category := range result.RequestedCategories {
		if !uniqueEvidenceName(requested, category) || (category != "keyboard" && category != "pointer" && category != "touchpad" && category != "touchscreen") {
			return false
		}
	}
	for _, category := range result.ObservedCategories {
		if !uniqueEvidenceName(observed, category) || !requested[category] {
			return false
		}
	}
	seen := map[string]bool{}
	for _, device := range result.Devices {
		if !uniqueEvidenceName(seen, device.Interface) || !requested[device.Category] || !device.Readable || device.EvidenceLoss {
			return false
		}
		count := uint64(0)
		for _, part := range []uint64{device.KeyPresses, device.KeyReleases, device.RelativeEvents, device.AbsoluteEvents, device.SwitchEvents} {
			if math.MaxUint64-count < part {
				return false
			}
			count += part
		}
		if count != device.Events || result.Mode == "readiness" && count != 0 {
			return false
		}
		active := result.Mode == "readiness"
		if result.Mode == "activity" {
			switch device.Category {
			case "keyboard":
				active = device.KeyPresses > 0 && device.KeyReleases > 0
			case "pointer":
				active = device.RelativeEvents > 0 || device.AbsoluteEvents > 0 || device.KeyPresses > 0
			case "touchpad", "touchscreen":
				active = device.AbsoluteEvents > 0
			}
		}
		if active {
			supported[device.Category] = true
		}
	}
	for category := range requested {
		if !observed[category] || !supported[category] {
			return false
		}
	}
	return true
}

func validateAudioEvidence(result audio.Result) bool {
	if result.MediaStreamsOpened || result.PlaybackEndpoints < 0 || result.CaptureEndpoints < 0 || result.ReadyEndpoints < 0 {
		return false
	}
	switch result.Status {
	case audio.StatusError, audio.StatusAborted:
		return result.Readiness == audio.ReadinessUnknown
	case audio.StatusInconclusive:
		return result.Readiness == audio.ReadinessNoDevice || result.Readiness == audio.ReadinessMissingNodes || result.Readiness == audio.ReadinessNoPCM
	}
	if result.Readiness != audio.ReadinessReady || len(result.Cards) == 0 || len(result.Endpoints) == 0 || result.ReadyEndpoints != len(result.Endpoints) {
		return false
	}
	cards, endpoints := map[uint32]bool{}, map[string]bool{}
	for _, card := range result.Cards {
		if cards[card.Card] || card.Card > 255 || card.Interface != fmt.Sprintf("card%d", card.Card) ||
			!card.Control.Present || !card.Control.Ready || card.Control.ReadinessIssue != "" || card.Control.Path != fmt.Sprintf("/dev/snd/controlC%d", card.Card) {
			return false
		}
		cards[card.Card] = true
	}
	playback, capture := 0, 0
	for _, endpoint := range result.Endpoints {
		suffix := "p"
		switch endpoint.Direction {
		case "playback":
			playback++
		case "capture":
			capture++
			suffix = "c"
		default:
			return false
		}
		name := fmt.Sprintf("pcmC%dD%d%s", endpoint.Card, endpoint.Device, suffix)
		if !cards[endpoint.Card] || endpoint.Device > 255 || !uniqueEvidenceName(endpoints, endpoint.Interface) || endpoint.Interface != name ||
			!endpoint.Node.Present || !endpoint.Node.Ready || endpoint.Node.ReadinessIssue != "" || endpoint.Node.Path != "/dev/snd/"+name {
			return false
		}
	}
	return playback == result.PlaybackEndpoints && capture == result.CaptureEndpoints
}

func graphicsHasExplicitFailure(result graphics.Result) bool {
	return result.Renderer.Status == graphics.StatusFail || result.Pattern.Status == graphics.StatusFail
}

func validateGraphicsEvidence(result graphics.Result) bool {
	if result.Status != graphics.StatusFail && graphicsHasExplicitFailure(result) {
		return false
	}
	if result.Status != graphics.StatusPass {
		return true
	}
	if len(result.Cards) == 0 || result.Renderer.Status != graphics.StatusPass || !result.Renderer.HardwareDetected || strings.TrimSpace(result.Renderer.Name) == "" {
		return false
	}
	for _, fallback := range []string{"llvmpipe", "softpipe", "swrast", "software rasterizer"} {
		if strings.Contains(strings.ToLower(result.Renderer.Name), fallback) {
			return false
		}
	}
	cards, connected := map[string]bool{}, map[string]bool{}
	for _, card := range result.Cards {
		if !uniqueEvidenceName(cards, card.Device) || card.Name == "" || card.Device != "/dev/dri/"+card.Name {
			return false
		}
		for _, connector := range card.Connectors {
			if connector.Status == "connected" {
				connected[card.Device] = true
			}
		}
	}
	switch result.Pattern.Status {
	case graphics.StatusPass:
		return result.Pattern.Frames > 0 && result.Pattern.Frames <= 600 && connected[result.Pattern.Device]
	case graphics.StatusSkip:
		return result.Pattern.Frames == 0 && result.Pattern.Device == "" &&
			(result.Pattern.Detail == "not requested" || result.Pattern.Detail == "no connected display connector" && len(connected) == 0)
	default:
		return false
	}
}

func validateDisplayEvidence(result graphics.VisualResult) bool {
	// A recorded wrong result remains a failure even when other observations
	// are missing, interrupted, or erroneous. Partial evidence without an
	// explicit failure can retain its non-pass status.
	if result.Status != graphics.StatusFail && displayHasExplicitFailure(result) {
		return false
	}
	if result.Status != graphics.StatusPass {
		return true
	}
	if len(result.Patterns) == 0 || len(result.Findings) == 0 {
		return false
	}
	connectors, findings, cards := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, pattern := range result.Patterns {
		if !uniqueEvidenceName(cards, pattern.Card) || pattern.Status != graphics.StatusPass || pattern.Seconds < 1 || pattern.Seconds > 30 || len(pattern.Connectors) == 0 {
			return false
		}
		for _, connector := range pattern.Connectors {
			if !uniqueEvidenceName(connectors, connector.Connector) || connector.PreferredMode == "" {
				return false
			}
		}
	}
	for _, finding := range result.Findings {
		if !uniqueEvidenceName(findings, finding.Connector) || !connectors[finding.Connector] || len(finding.Criteria) != len(graphics.VisualCriteria) {
			return false
		}
		for _, criterion := range graphics.VisualCriteria {
			state := finding.Criteria[criterion]
			if state != graphics.StatusPass {
				return false
			}
		}
	}
	return len(findings) == len(connectors)
}

func displayHasExplicitFailure(result graphics.VisualResult) bool {
	for _, pattern := range result.Patterns {
		if pattern.Status == graphics.StatusFail {
			return true
		}
	}
	for _, finding := range result.Findings {
		for _, status := range finding.Criteria {
			if status == graphics.StatusFail {
				return true
			}
		}
	}
	return false
}
