package plan

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestValidateRejectsTamperedCatalogEvenAfterRehash(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *Plan)
	}{
		{"missing test", func(t *testing.T, p *Plan) { p.Tests = p.Tests[1:] }},
		{"no tests", func(t *testing.T, p *Plan) { p.Tests = nil }},
		{"extra test", func(t *testing.T, p *Plan) { p.Tests = append(p.Tests, Test{ID: "unknown"}) }},
		{"duplicate test", func(t *testing.T, p *Plan) { p.Tests[1] = p.Tests[0] }},
		{"reordered tests", func(t *testing.T, p *Plan) { p.Tests[0], p.Tests[1] = p.Tests[1], p.Tests[0] }},
		{"unknown test", func(t *testing.T, p *Plan) { findPlanTest(t, p, "cpu.quick").ID = "cpu.fake" }},
		{"changed version", func(t *testing.T, p *Plan) { findPlanTest(t, p, "cpu.quick").Version = 2 }},
		{"zero version", func(t *testing.T, p *Plan) { findPlanTest(t, p, "cpu.quick").Version = 0 }},
		{"changed estimate", func(t *testing.T, p *Plan) { findPlanTest(t, p, "cpu.quick").EstimatedSeconds = 4 }},
		{"zero timeout", func(t *testing.T, p *Plan) { findPlanTest(t, p, "cpu.quick").TimeoutSeconds = 0 }},
		{"extended timeout", func(t *testing.T, p *Plan) { findPlanTest(t, p, "cpu.quick").TimeoutSeconds = 21 }},
		{"destructive flag removed", func(t *testing.T, p *Plan) { findPlanTest(t, p, "storage.destructive").Destructive = false }},
		{"destructive flag added", func(t *testing.T, p *Plan) { findPlanTest(t, p, "cpu.quick").Destructive = true }},
		{"switched test capability", func(t *testing.T, p *Plan) {
			row := findPlanTest(t, p, "cpu.quick")
			row.Capability = "memory"
			row.ComponentIDs = []string{"memory:system"}
		}},
		{"unknown test capability", func(t *testing.T, p *Plan) { findPlanTest(t, p, "cpu.quick").Capability = "imaginary" }},
		{"unknown test status", func(t *testing.T, p *Plan) { findPlanTest(t, p, "cpu.quick").Status = "pass" }},
		{"unrecorded exclusion", func(t *testing.T, p *Plan) { findPlanTest(t, p, "cpu.quick").Status = TestExcluded }},
		{"false test absence", func(t *testing.T, p *Plan) { findPlanTest(t, p, "cpu.quick").Status = TestNotPresent }},
		{"false missing driver", func(t *testing.T, p *Plan) { findPlanTest(t, p, "cpu.quick").Status = TestMissingDriver }},
		{"unimplemented scheduled", func(t *testing.T, p *Plan) { findPlanTest(t, p, "audio.quick").Status = TestScheduled }},
		{"wrong profile scheduled", func(t *testing.T, p *Plan) { findPlanTest(t, p, "cpu.stress").Status = TestScheduled }},
		{"wrong profile disguised as unsupported", func(t *testing.T, p *Plan) { findPlanTest(t, p, "cpu.stress").Status = TestUnsupported }},
		{"destructive work in quick", func(t *testing.T, p *Plan) { findPlanTest(t, p, "storage.destructive").Status = TestScheduled }},
		{"architecture changed without status update", func(t *testing.T, p *Plan) { p.Architecture = "legacy-x86" }},
		{"profile changed without status update", func(t *testing.T, p *Plan) { p.Profile = ProfileExtended }},
		{"missing capability", func(t *testing.T, p *Plan) { p.Capabilities = p.Capabilities[1:] }},
		{"extra capability", func(t *testing.T, p *Plan) { p.Capabilities = append(p.Capabilities, Capability{ID: "unknown"}) }},
		{"duplicate capability", func(t *testing.T, p *Plan) { p.Capabilities[1] = p.Capabilities[0] }},
		{"reordered capabilities", func(t *testing.T, p *Plan) {
			p.Capabilities[0], p.Capabilities[1] = p.Capabilities[1], p.Capabilities[0]
		}},
		{"unknown capability", func(t *testing.T, p *Plan) { findPlanCapability(t, p, "cpu").ID = "imaginary" }},
		{"unknown capability status", func(t *testing.T, p *Plan) { findPlanCapability(t, p, "cpu").Status = "scheduled" }},
		{"absent capability with references", func(t *testing.T, p *Plan) { findPlanCapability(t, p, "cpu").Status = CapabilityAbsent }},
		{"present capability without references", func(t *testing.T, p *Plan) { replacePlanComponentRefs(t, p, "cpu", nil) }},
		{"missing-driver capability without references", func(t *testing.T, p *Plan) {
			replacePlanComponentRefs(t, p, "cpu", nil)
			findPlanCapability(t, p, "cpu").Status = CapabilityMissingDriver
		}},
		{"wrong component type", func(t *testing.T, p *Plan) { replacePlanComponentRefs(t, p, "cpu", []string{"memory:system"}) }},
		{"unknown component reference", func(t *testing.T, p *Plan) { replacePlanComponentRefs(t, p, "cpu", []string{"cpu:missing"}) }},
		{"duplicate component references", func(t *testing.T, p *Plan) {
			replacePlanComponentRefs(t, p, "cpu", []string{"cpu:0", "cpu:0", "cpu:1"})
		}},
		{"reordered component references", func(t *testing.T, p *Plan) { replacePlanComponentRefs(t, p, "cpu", []string{"cpu:1", "cpu:0"}) }},
		{"omitted unconditional provider", func(t *testing.T, p *Plan) { replacePlanComponentRefs(t, p, "cpu", []string{"cpu:0"}) }},
		{"test references differ from capability", func(t *testing.T, p *Plan) { findPlanTest(t, p, "cpu.quick").ComponentIDs = []string{"cpu:0"} }},
		{"test references reordered", func(t *testing.T, p *Plan) { findPlanTest(t, p, "cpu.quick").ComponentIDs = []string{"cpu:1", "cpu:0"} }},
		{"unconditional provider hidden as absent", func(t *testing.T, p *Plan) {
			replacePlanComponentRefs(t, p, "cpu", nil)
			findPlanCapability(t, p, "cpu").Status = CapabilityAbsent
			findPlanTest(t, p, "cpu.quick").Status = TestNotPresent
			findPlanTest(t, p, "cpu.stress").Status = TestNotPresent
		}},
		{"unconditional provider hidden as missing driver", func(t *testing.T, p *Plan) {
			findPlanCapability(t, p, "audio").Status = CapabilityMissingDriver
			replacePlanComponentRefs(t, p, "audio", []string{"pci:audio"})
			findPlanTest(t, p, "audio.quick").Status = TestMissingDriver
			findPlanTest(t, p, "audio.readiness.quick").Status = TestMissingDriver
		}},
		{"different catalog digest", func(t *testing.T, p *Plan) { p.DefinitionsDigest = "sha256:" + strings.Repeat("0", 64) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := buildValidationPlan(t, fullValidationSnapshot(), ProfileQuick, "x86_64")
			test.mutate(t, &p)
			encoded := rehashPlanForTest(t, &p)
			if err := Validate(p); err == nil {
				t.Fatal("accepted tampered typed plan with recomputed ID")
			}
			if _, err := Decode(encoded); err == nil {
				t.Fatal("accepted tampered encoded plan with recomputed ID")
			}
		})
	}
}

func TestValidateRejectsUnsupportedSchedulingAcrossTargets(t *testing.T) {
	for _, profile := range []Profile{ProfileQuick, ProfileStandard, ProfileExtended, ProfileDestructive} {
		for _, architecture := range []string{"x86_64", "x86", "legacy-x86", "aarch64"} {
			t.Run(string(profile)+"/"+architecture, func(t *testing.T) {
				p := buildValidationPlan(t, fullValidationSnapshot(), profile, architecture)
				encoded, err := json.MarshalIndent(p, "", "  ")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := Decode(encoded); err != nil {
					t.Fatalf("pretty-printed Build output rejected: %v", err)
				}
				if p.SchemaVersion != 2 {
					t.Fatal("plan wire schema changed")
				}
				findPlanTest(t, &p, "storage.destructive").Status = TestScheduled
				if _, err := Decode(rehashPlanForTest(t, &p)); err == nil {
					t.Fatal("unimplemented destructive test became executable")
				}
			})
		}
	}
	legacy := buildValidationPlan(t, fullValidationSnapshot(), ProfileQuick, "legacy-x86")
	for _, id := range []string{"graphics.quick", "display.operator.quick"} {
		findPlanTest(t, &legacy, id).Status = TestScheduled
	}
	if _, err := Decode(rehashPlanForTest(t, &legacy)); err == nil {
		t.Fatal("legacy image scheduled modern graphics work")
	}
}

func TestValidatePreservesAbsentAndMissingDriverPlans(t *testing.T) {
	for _, snapshot := range []inventory.Snapshot{
		inventory.NewSnapshot(nil),
		inventory.NewSnapshot([]inventory.Component{
			inventory.NewComponent(inventory.ComponentPCI, "z-pci", map[string]string{"class": "0x0400"}),
			inventory.NewComponent(inventory.ComponentUSB, "a-usb", map[string]string{"audio_interface": "true"}),
		}),
	} {
		for _, architecture := range []string{"x86_64", "legacy-x86"} {
			p := buildValidationPlan(t, snapshot, ProfileQuick, architecture)
			if _, err := Decode(rehashPlanForTest(t, &p)); err != nil {
				t.Fatalf("valid absent/missing-driver plan rejected: %v", err)
			}
			if len(snapshot.Components) != 0 {
				refs := findPlanCapability(t, &p, "audio").ComponentIDs
				if len(refs) != 2 || refs[0] != "z-pci" || refs[1] != "a-usb" {
					t.Fatalf("reference order must follow typed graph order: %v", refs)
				}
				findPlanTest(t, &p, "audio.readiness.quick").Status = TestScheduled
			} else {
				findPlanTest(t, &p, "cpu.quick").Status = TestScheduled
			}
			if _, err := Decode(rehashPlanForTest(t, &p)); err == nil {
				t.Fatal("unavailable test became executable")
			}
		}
	}
}

func TestValidateRejectsAmbiguousGraphReferences(t *testing.T) {
	p := buildValidationPlan(t, fullValidationSnapshot(), ProfileQuick, "x86_64")
	for index := range p.HardwareGraph.Components {
		if p.HardwareGraph.Components[index].Type == inventory.ComponentMemory {
			p.HardwareGraph.Components[index].ID = "cpu:0"
			break
		}
	}
	if _, err := Decode(rehashPlanForTest(t, &p)); err == nil {
		t.Fatal("type-ambiguous component ID accepted")
	}
}

func TestValidateDoesNotClaimToAuthenticateOpaqueHardwareHMACs(t *testing.T) {
	p := buildValidationPlan(t, fullValidationSnapshot(), ProfileQuick, "x86_64")
	p.HardwareGraph.Value = "hmac-sha256:" + strings.Repeat("1", 64)
	for index := range p.HardwareGraph.Components {
		p.HardwareGraph.Components[index].Value = "hmac-sha256:" + strings.Repeat("2", 64)
	}
	if _, err := Decode(rehashPlanForTest(t, &p)); err != nil {
		t.Fatalf("structural validation unexpectedly depends on unknown hardware key: %v", err)
	}
}

func TestDecodeRejectsContradictoryJSONMembers(t *testing.T) {
	p := buildValidationPlan(t, fullValidationSnapshot(), ProfileQuick, "x86_64")
	encoded := rehashPlanForTest(t, &p)
	for _, replacement := range []string{
		`"status":"scheduled","status":"unsupported"`,
		`"Status":"scheduled","status":"unsupported"`,
		`"status":"scheduled","STATUS":"unsupported"`,
		`"statu\u0073":"scheduled","status":"unsupported"`,
	} {
		altered := bytes.Replace(encoded, []byte(`"status":"unsupported"`), []byte(replacement), 1)
		// The standard decoder's last value still produces the genuine plan
		// and ID. Rejection must come from representation validation.
		var permissive Plan
		if err := json.Unmarshal(altered, &permissive); err != nil {
			t.Fatal(err)
		}
		if err := Validate(permissive); err != nil {
			t.Fatalf("fixture does not demonstrate a contradictory valid plan: %v", err)
		}
		if _, err := Decode(altered); err == nil {
			t.Fatalf("accepted contradictory status representation %s", replacement)
		}
	}
	for _, key := range []string{"schema_version", "hardware_graph", "components", "component_ids", "version", "timeout_seconds", "destructive", "id", "status"} {
		for _, replacement := range []string{strings.ToUpper(key), strings.ToUpper(key[:1]) + key[1:]} {
			original := []byte(`"` + key + `":`)
			if !bytes.Contains(encoded, original) {
				t.Fatalf("fixture lacks key %q", key)
			}
			altered := bytes.Replace(encoded, original, []byte(`"`+replacement+`":`), 1)
			if _, err := Decode(altered); err == nil {
				t.Fatalf("accepted noncanonical key %q", replacement)
			}
		}
	}
	unicodeAlias := bytes.Replace(encoded, []byte(`"status":`), []byte(`"ſtatus":`), 1)
	if _, err := Decode(unicodeAlias); err == nil {
		t.Fatal("accepted Unicode case alias for status")
	}
	duplicateGraph := bytes.Replace(encoded, []byte(`"algorithm":`), []byte(`"algorithm":"unexpected","algorithm":`), 1)
	if _, err := Decode(duplicateGraph); err == nil {
		t.Fatal("accepted duplicate nested graph metadata")
	}
}

func fullValidationSnapshot() inventory.Snapshot {
	return inventory.NewSnapshot([]inventory.Component{
		inventory.NewComponent(inventory.ComponentCPU, "cpu:0", nil),
		inventory.NewComponent(inventory.ComponentCPU, "cpu:1", nil),
		inventory.NewComponent(inventory.ComponentMemory, "memory:system", nil),
		inventory.NewComponent(inventory.ComponentSound, "sound:0", nil),
		inventory.NewComponent(inventory.ComponentPCI, "pci:audio", map[string]string{"class": "0x0400"}),
		inventory.NewComponent(inventory.ComponentVideo, "video:capture", map[string]string{"category": "video_capture"}),
		inventory.NewComponent(inventory.ComponentVideo, "video:connector", map[string]string{"category": "display_connector"}),
		inventory.NewComponent(inventory.ComponentVideo, "video:controller", map[string]string{"category": "display_controller"}),
		inventory.NewComponent(inventory.ComponentInput, "input:keyboard", map[string]string{"category": "keyboard"}),
		inventory.NewComponent(inventory.ComponentInput, "input:pointer", map[string]string{"category": "pointer"}),
		inventory.NewComponent(inventory.ComponentInput, "input:touchpad", map[string]string{"category": "touchpad"}),
		inventory.NewComponent(inventory.ComponentInput, "input:touchscreen", map[string]string{"category": "touchscreen"}),
		inventory.NewComponent(inventory.ComponentNetwork, "network:eth0", map[string]string{"name": "eth0"}),
		inventory.NewComponent(inventory.ComponentBlock, "block:sda", map[string]string{"kind": "device"}),
		inventory.NewComponent(inventory.ComponentUSB, "usb:1-1", nil),
	})
}

func buildValidationPlan(t *testing.T, snapshot inventory.Snapshot, profile Profile, architecture string) Plan {
	t.Helper()
	p, err := Build(snapshot, profile, architecture, bytes.Repeat([]byte{11}, 32), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func findPlanTest(t *testing.T, p *Plan, id string) *Test {
	t.Helper()
	for index := range p.Tests {
		if p.Tests[index].ID == id {
			return &p.Tests[index]
		}
	}
	t.Fatalf("fixture has no test %q", id)
	return nil
}

func findPlanCapability(t *testing.T, p *Plan, id string) *Capability {
	t.Helper()
	for index := range p.Capabilities {
		if p.Capabilities[index].ID == id {
			return &p.Capabilities[index]
		}
	}
	t.Fatalf("fixture has no capability %q", id)
	return nil
}

func replacePlanComponentRefs(t *testing.T, p *Plan, capability string, refs []string) {
	t.Helper()
	findPlanCapability(t, p, capability).ComponentIDs = append([]string(nil), refs...)
	for index := range p.Tests {
		if p.Tests[index].Capability == capability {
			p.Tests[index].ComponentIDs = append([]string(nil), refs...)
		}
	}
}

func rehashPlanForTest(t *testing.T, p *Plan) []byte {
	t.Helper()
	p.ID = ""
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	p.ID = fmt.Sprintf("sha256:%x", digest)
	encoded, err = json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
