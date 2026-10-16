package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/audio"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/graphics"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/networktest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storage"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/usbtest"
)

func completeCPUEvidence() quick.CPUResult {
	return quick.CPUResult{Schema: quick.CPUSchema, Status: quick.StatusPass, Workers: 1, Iterations: 42,
		StateAggregate: 17, KnownAnswerSHA256: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		DurationMilliseconds: 20, Safety: quick.SafetyEvidence{GlobalTimeoutMilliseconds: 20000, ThermalLimitMilliCelsius: 90000}}
}

func completeMemoryEvidence() quick.MemoryResult {
	return quick.MemoryResult{Schema: quick.MemorySchema, Status: quick.StatusPass, AllocatedBytes: 4 << 20, ExaminedBytes: 12 << 20,
		Passes: 1, PatternsPerPass: 3, DurationMilliseconds: 20,
		Safety: quick.SafetyEvidence{GlobalTimeoutMilliseconds: 30000, ThermalLimitMilliCelsius: 90000}}
}

func completeStorageEvidence() storage.Result {
	return storage.Result{Schema: storage.Schema, Status: storage.StatusPass, EligibleDevices: 1, ReadableDevices: 1, MediaBytes: 4096,
		Devices: []storage.DeviceResult{{Interface: "sda", Kind: "media", SizeBytes: 4096, NodePresent: true, Readable: true, State: "running"}}}
}

func completeNetworkEvidence() networktest.Result {
	return networktest.Result{Schema: networktest.Schema, Status: networktest.StatusPass, RequireOffline: true,
		HardwareInterfaces: 1, DriverBoundInterfaces: 1,
		Interfaces: []networktest.InterfaceResult{{Interface: "eth0", Kind: "ethernet", Driver: "e1000", DriverBound: true}}}
}

func completeUSBEvidence() usbtest.Result {
	return usbtest.Result{Schema: usbtest.Schema, Status: usbtest.StatusPass, Controllers: 1, RootPorts: 4, AuthorizedDevices: 1, ReadableDevices: 1,
		Devices: []usbtest.DeviceResult{{Path: "usb1", Kind: "root_hub", BusNumber: 1, DeviceNumber: 1, DownstreamPorts: 4,
			AuthorizedKnown: true, Authorized: true, NodePresent: true, Readable: true}}}
}

func completeInputEvidence() inputtest.Result {
	return inputtest.Result{Schema: inputtest.Schema, Status: inputtest.StatusPass, Mode: "readiness",
		RequestedCategories: []string{"keyboard"}, ObservedCategories: []string{"keyboard"},
		Devices: []inputtest.DeviceResult{{Interface: "event0", Category: "keyboard", Readable: true}}}
}

func completeGraphicsEvidence() graphics.Result {
	return graphics.Result{Schema: graphics.Schema, Status: graphics.StatusPass,
		Cards:    []graphics.Card{{Name: "card0", Device: "/dev/dri/card0", Connectors: []graphics.Connector{{Name: "card0-DP-1", Status: "connected"}}}},
		Renderer: graphics.RendererResult{Status: graphics.StatusPass, Name: "Mesa Intel", HardwareDetected: true},
		Pattern:  graphics.PatternResult{Status: graphics.StatusPass, Frames: 12, Device: "/dev/dri/card0"}}
}

func completeAudioEvidence() audio.Result {
	return audio.Result{Schema: audio.Schema, Status: audio.StatusPass, Readiness: audio.ReadinessReady, PlaybackEndpoints: 1, ReadyEndpoints: 1,
		Cards: []audio.CardResult{{Interface: "card0", Card: 0, Control: audio.NodeResult{Path: "/dev/snd/controlC0", Present: true, Ready: true}}},
		Endpoints: []audio.PCMResult{{Interface: "pcmC0D0p", Card: 0, Device: 0, Direction: "playback",
			Node: audio.NodeResult{Path: "/dev/snd/pcmC0D0p", Present: true, Ready: true}}}}
}

func completeDisplayEvidence() graphics.VisualResult {
	return graphics.VisualResult{Schema: graphics.VisualSchema, Status: graphics.StatusPass,
		Patterns: []graphics.DisplayPattern{{Card: "/dev/dri/card0", Status: graphics.StatusPass, Seconds: 5,
			Connectors: []graphics.DisplayMode{{Connector: "card0-DP-1", PreferredMode: "1920x1080"}}}},
		Findings: []graphics.VisualFinding{{Connector: "card0-DP-1", Criteria: map[string]graphics.Status{
			"pixels": graphics.StatusPass, "geometry": graphics.StatusPass, "color": graphics.StatusPass,
			"flicker": graphics.StatusPass, "scaling": graphics.StatusPass, "overscan": graphics.StatusPass, "artifacts": graphics.StatusPass}}}}
}

func evidenceFixtures() map[string]any {
	return map[string]any{quick.CPUSchema: completeCPUEvidence(), quick.MemorySchema: completeMemoryEvidence(), storage.Schema: completeStorageEvidence(),
		networktest.Schema: completeNetworkEvidence(), usbtest.Schema: completeUSBEvidence(), inputtest.Schema: completeInputEvidence(),
		audio.Schema: completeAudioEvidence(), graphics.Schema: completeGraphicsEvidence(), graphics.VisualSchema: completeDisplayEvidence()}
}

func encodeEvidenceFixture(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestWorkerEvidenceRequiresObservationsForEveryPass(t *testing.T) {
	for schema, evidence := range evidenceFixtures() {
		t.Run(schema, func(t *testing.T) {
			if status, err := testEvidenceStatus(encodeEvidenceFixture(t, evidence), schema); err != nil || status != "pass" {
				t.Fatalf("complete observations refused: status=%q error=%v", status, err)
			}
			headerOnly := encodeEvidenceFixture(t, map[string]string{"schema": schema, "status": "pass"})
			if _, err := testEvidenceStatus(headerOnly, schema); err == nil {
				t.Fatal("pass without observations accepted")
			}
		})
	}
}

func TestWorkerEvidenceRejectsMissingSafetyAndContradictions(t *testing.T) {
	first := func(value map[string]any, list string) map[string]any { return value[list].([]any)[0].(map[string]any) }
	cases := []struct {
		name, schema string
		change       func(map[string]any)
	}{
		{"cpu no work", quick.CPUSchema, func(v map[string]any) { v["iterations"] = 0 }},
		{"cpu no worker", quick.CPUSchema, func(v map[string]any) { v["workers"] = 0 }},
		{"cpu wrong known answer", quick.CPUSchema, func(v map[string]any) { v["known_answer_sha256"] = strings.Repeat("0", 64) }},
		{"cpu missing safety", quick.CPUSchema, func(v map[string]any) { delete(v, "safety") }},
		{"cpu missing zero sensor observation", quick.CPUSchema, func(v map[string]any) { delete(v["safety"].(map[string]any), "thermal_sensors") }},
		{"cpu over temperature", quick.CPUSchema, func(v map[string]any) { v["safety"].(map[string]any)["peak_millicelsius"] = 95000 }},
		{"cpu overdue", quick.CPUSchema, func(v map[string]any) { v["duration_ms"] = 20001 }},
		{"cpu null counter", quick.CPUSchema, func(v map[string]any) { v["iterations"] = nil }},
		{"memory no work", quick.MemorySchema, func(v map[string]any) { v["examined_bytes"] = 0 }},
		{"memory partial pass", quick.MemorySchema, func(v map[string]any) { v["examined_bytes"] = 4 << 20 }},
		{"memory wrong pattern coverage", quick.MemorySchema, func(v map[string]any) { v["patterns_per_pass"] = 1; v["examined_bytes"] = 4 << 20 }},
		{"memory mismatch", quick.MemorySchema, func(v map[string]any) { v["actual_byte"] = 1 }},
		{"storage writable mount", storage.Schema, func(v map[string]any) { v["writable_mounts"] = 1 }},
		{"storage hidden writable device", storage.Schema, func(v map[string]any) { first(v, "devices")["writable_mount"] = true }},
		{"storage no devices", storage.Schema, func(v map[string]any) { delete(v, "devices") }},
		{"storage unreadable", storage.Schema, func(v map[string]any) { first(v, "devices")["readable"] = false }},
		{"storage failed state", storage.Schema, func(v map[string]any) { first(v, "devices")["state"] = "offline" }},
		{"storage content read", storage.Schema, func(v map[string]any) { v["media_content_read"] = true }},
		{"storage content write", storage.Schema, func(v map[string]any) { v["media_content_writes"] = true }},
		{"storage unobserved writes", storage.Schema, func(v map[string]any) { delete(v, "media_content_writes") }},
		{"storage null writes", storage.Schema, func(v map[string]any) { v["media_content_writes"] = nil }},
		{"storage contradictory capacity", storage.Schema, func(v map[string]any) { v["media_bytes"] = 8192 }},
		{"network offline bypass", networktest.Schema, func(v map[string]any) { v["require_offline"] = false }},
		{"network active", networktest.Schema, func(v map[string]any) { v["active_non_loop_interfaces"] = 1 }},
		{"network hidden active device", networktest.Schema, func(v map[string]any) { first(v, "interfaces")["administrative_up"] = true }},
		{"network traffic", networktest.Schema, func(v map[string]any) { v["network_traffic_generated"] = true }},
		{"network unobserved traffic", networktest.Schema, func(v map[string]any) { delete(v, "network_traffic_generated") }},
		{"network missing driver", networktest.Schema, func(v map[string]any) { first(v, "interfaces")["driver_bound"] = false }},
		{"usb missing root", usbtest.Schema, func(v map[string]any) { v["controllers"] = 0 }},
		{"usb no work", usbtest.Schema, func(v map[string]any) { delete(v, "devices"); v["authorized_devices"] = 0; v["readable_devices"] = 0 }},
		{"usb unauthorized", usbtest.Schema, func(v map[string]any) { first(v, "devices")["authorized"] = false }},
		{"usb unknown authorization", usbtest.Schema, func(v map[string]any) { first(v, "devices")["authorized_known"] = false }},
		{"usb transfer", usbtest.Schema, func(v map[string]any) { v["usb_transfers_generated"] = true }},
		{"usb unobserved transfers", usbtest.Schema, func(v map[string]any) { delete(v, "usb_transfers_generated") }},
		{"input empty selection", inputtest.Schema, func(v map[string]any) { delete(v, "requested_categories") }},
		{"input absent category", inputtest.Schema, func(v map[string]any) { v["requested_categories"] = []string{"keyboard", "pointer"} }},
		{"input unreadable", inputtest.Schema, func(v map[string]any) { first(v, "devices")["readable"] = false }},
		{"input lost evidence", inputtest.Schema, func(v map[string]any) { first(v, "devices")["evidence_loss"] = true }},
		{"input no activity", inputtest.Schema, func(v map[string]any) { v["mode"] = "activity" }},
		{"audio missing control", audio.Schema, func(v map[string]any) { first(v, "cards")["control"].(map[string]any)["ready"] = false }},
		{"audio invented ready count", audio.Schema, func(v map[string]any) { v["ready_endpoints"] = 2 }},
		{"audio absent PCM", audio.Schema, func(v map[string]any) { first(v, "endpoints")["node"].(map[string]any)["present"] = false }},
		{"audio media opened", audio.Schema, func(v map[string]any) { v["media_streams_opened"] = true }},
		{"graphics software fallback", graphics.Schema, func(v map[string]any) { v["renderer"].(map[string]any)["name"] = "llvmpipe" }},
		{"graphics no acceleration", graphics.Schema, func(v map[string]any) { v["renderer"].(map[string]any)["hardware_detected"] = false }},
		{"graphics zero frames", graphics.Schema, func(v map[string]any) { v["pattern"].(map[string]any)["frames"] = 0 }},
		{"graphics failed pattern", graphics.Schema, func(v map[string]any) { v["pattern"].(map[string]any)["status"] = "inconclusive" }},
		{"display no findings", graphics.VisualSchema, func(v map[string]any) { delete(v, "operator_findings") }},
		{"display missing criterion", graphics.VisualSchema, func(v map[string]any) { delete(first(v, "operator_findings")["criteria"].(map[string]any), "pixels") }},
		{"display failed criterion", graphics.VisualSchema, func(v map[string]any) { first(v, "operator_findings")["criteria"].(map[string]any)["color"] = "fail" }},
		{"display failed pattern", graphics.VisualSchema, func(v map[string]any) { first(v, "patterns")["status"] = "inconclusive" }},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(encodeEvidenceFixture(t, evidenceFixtures()[item.schema]), &value); err != nil {
				t.Fatal(err)
			}
			item.change(value)
			if _, err := testEvidenceStatus(encodeEvidenceFixture(t, value), item.schema); err == nil {
				t.Fatal("incomplete or contradictory pass accepted")
			}
		})
	}
}

func TestWorkerEvidenceRejectsAmbiguousJSON(t *testing.T) {
	valid := encodeEvidenceFixture(t, completeStorageEvidence())
	for _, invalid := range [][]byte{
		bytes.Replace(valid, []byte(`"writable_mounts":0`), []byte(`"writable_mounts":1,"writable_mounts":0`), 1),
		bytes.Replace(valid, []byte(`"writable_mounts":0`), []byte(`"Writable_Mounts":0`), 1),
		bytes.Replace(valid, []byte(`"readable":true`), []byte(`"readable":false,"readable":true`), 1),
		bytes.Replace(valid, []byte(`"media_content_writes":false`), []byte(`"media_content_writes":false,"unexpected":true`), 1),
	} {
		if _, err := testEvidenceStatus(invalid, storage.Schema); err == nil {
			t.Fatalf("ambiguous evidence accepted: %s", invalid)
		}
	}
}

func TestWorkerEvidencePreservesNonPassObservationsAndLimitedReadiness(t *testing.T) {
	cpu := completeCPUEvidence()
	cpu.Status, cpu.Iterations, cpu.Detail = quick.StatusAborted, 0, "global deadline exceeded"
	disk := completeStorageEvidence()
	disk.Status, disk.WritableMounts, disk.Detail = storage.StatusError, 1, "a block device has a writable mount"
	input := completeInputEvidence()
	input.Mode, input.Devices[0].Events, input.Devices[0].KeyPresses, input.Devices[0].KeyReleases = "activity", 2, 1, 1
	headless := completeGraphicsEvidence()
	headless.Cards[0].Connectors = nil
	headless.Pattern = graphics.PatternResult{Status: graphics.StatusSkip, Detail: "no connected display connector"}
	display := completeDisplayEvidence()
	display.Status, display.Findings[0].Criteria["pixels"] = graphics.StatusFail, graphics.StatusFail
	for schema, evidence := range map[string]any{quick.CPUSchema: cpu, storage.Schema: disk, inputtest.Schema: input, graphics.Schema: headless, graphics.VisualSchema: display} {
		if _, err := testEvidenceStatus(encodeEvidenceFixture(t, evidence), schema); err != nil {
			t.Fatalf("legitimate partial result/readiness refused for %s: %v", schema, err)
		}
	}
	for schema, evidence := range map[string]any{
		inputtest.Schema:   inputtest.Result{Schema: inputtest.Schema, Status: inputtest.StatusError, Detail: "Linux boot environment required"},
		networktest.Schema: networktest.Result{Schema: networktest.Schema, Status: networktest.StatusError, Detail: "Linux boot environment required"},
	} {
		if _, err := testEvidenceStatus(encodeEvidenceFixture(t, evidence), schema); err != nil {
			t.Fatalf("unsupported-platform error refused for %s: %v", schema, err)
		}
	}
}

func TestDisplayEvidenceNeverDowngradesAnExplicitFailure(t *testing.T) {
	for _, source := range []string{"pattern", "operator finding"} {
		for _, status := range []graphics.Status{graphics.StatusPass, graphics.StatusInconclusive, graphics.StatusSkip, "error", graphics.StatusAborted} {
			t.Run(source+"/"+string(status), func(t *testing.T) {
				result := completeDisplayEvidence()
				result.Status = status
				if source == "pattern" {
					result.Patterns[0].Status = graphics.StatusFail
				} else {
					result.Findings[0].Criteria["pixels"] = graphics.StatusFail
				}
				if _, err := testEvidenceStatus(encodeEvidenceFixture(t, result), graphics.VisualSchema); !errors.Is(err, errDisplayFailureDowngrade) {
					t.Fatalf("explicit failure lost its distinct rejection: %v", err)
				}
			})
		}
	}
}

func TestDisplayEvidenceRetainsPartialObservationsWithoutInventingSuccess(t *testing.T) {
	for _, status := range []graphics.Status{graphics.StatusInconclusive, "error", graphics.StatusAborted} {
		t.Run(string(status), func(t *testing.T) {
			result := graphics.VisualResult{Schema: graphics.VisualSchema, Status: status,
				Patterns: []graphics.DisplayPattern{{Card: "/dev/dri/card0", Status: status}}}
			if got, err := testEvidenceStatus(encodeEvidenceFixture(t, result), graphics.VisualSchema); err != nil || got != string(status) {
				t.Fatalf("legitimate partial result was rejected: status=%q error=%v", got, err)
			}
			// A defect on another output must retain fail even though the first
			// output was interrupted and has no completed operator findings.
			result.Status = graphics.StatusFail
			result.Findings = []graphics.VisualFinding{{Connector: "card1-DP-1", Criteria: map[string]graphics.Status{"pixels": graphics.StatusFail}}}
			if got, err := testEvidenceStatus(encodeEvidenceFixture(t, result), graphics.VisualSchema); err != nil || got != "fail" {
				t.Fatalf("known defect with partial observations was rejected: status=%q error=%v", got, err)
			}
		})
	}
}
