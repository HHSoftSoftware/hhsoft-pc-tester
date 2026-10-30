package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/comparison"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

func newDRMCommandFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, value := range map[string]string{
		"card0/dev": "226:0\n", "card0/device/vendor": "0x1234\n", "card0/device/device": "0xabcd\n",
		"card0/device/subsystem_vendor": "0x5678\n", "card0/device/subsystem_device": "0xef01\n",
		"renderD128/dev": "226:128\n", "renderD128/device/vendor": "0x1234\n", "renderD128/device/device": "0xabcd\n",
		"renderD128/device/subsystem_vendor": "0x5678\n", "renderD128/device/subsystem_device": "0xef01\n",
		"card0-HDMI-A-1/status": "connected\n", "card0-HDMI-A-1/enabled": "enabled\n",
	} {
		name = filepath.Join(root, "sys/class/drm", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Portable command fixture establishes the same nonregular wrong-role
	// endpoint; the inventory Linux fixture separately proves ordinary links.
	if err := os.MkdirAll(filepath.Join(root, "sys/class/drm/card0-HDMI-A-1/device/device"), 0700); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestInventoryDRMTopologyFeedsRealPlanCommand(t *testing.T) {
	root := newDRMCommandFixture(t)
	key := writeTestKey(t, root, "drm.key", bytes.Repeat([]byte{0x44}, 32))
	var stdout, stderr bytes.Buffer
	code := runPlanWithInput(t.Context(), []string{"-fingerprint-key", key, "-key-epoch", "drm", "-architecture", "x86_64", "-output", "-"}, &stdout, &stderr, inventory.NewLinuxCollector(root).Collect, writeDestination, captureFixtureInput(t))
	if code != 0 || stderr.Len() != 0 {
		t.Fatal("real inventory-to-plan command failed", code, stderr.String())
	}
	var frozen plan.Plan
	if err := json.Unmarshal(stdout.Bytes(), &frozen); err != nil {
		t.Fatal(err)
	}
	if err := plan.Validate(frozen); err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{"video:drm:card0": false, "video:drm:card0-HDMI-A-1": false, "video:drm:renderD128": false}
	for _, component := range frozen.HardwareGraph.Components {
		seen, ok := wanted[component.ID]
		if !ok || seen || component.Type != inventory.ComponentVideo {
			t.Fatal("plan changed DRM component identity")
		}
		wanted[component.ID] = true
	}
	for _, seen := range wanted {
		if !seen {
			t.Fatal("plan omitted a DRM component")
		}
	}
	scheduled := 0
	for _, row := range frozen.Tests {
		if row.ID == "graphics.quick" || row.ID == "display.operator.quick" {
			if row.Status != plan.TestScheduled {
				t.Fatal("DRM provider no longer supplies its plan capability", row.ID)
			}
			scheduled++
		}
	}
	if scheduled != 2 {
		t.Fatal("plan lost graphics or connector coverage")
	}
}

func TestInventoryDRMDeclaredFailureStopsPlanOutput(t *testing.T) {
	for _, target := range []string{"card0/device/device", "card0-HDMI-A-1/enabled", "renderD128/dev"} {
		for _, mode := range []string{"directory", "overflow"} {
			t.Run(target+"/"+mode, func(t *testing.T) {
				root := newDRMCommandFixture(t)
				key := writeTestKey(t, root, "drm.key", bytes.Repeat([]byte{0x44}, 32))
				collect := inventory.NewLinuxCollector(root).Collect
				var positive, positiveError bytes.Buffer
				if code := runPlanWithInput(t.Context(), []string{"-fingerprint-key", key, "-output", "-"}, &positive, &positiveError, collect, writeDestination, captureFixtureInput(t)); code != 0 || positive.Len() == 0 {
					t.Fatal("plan positive control failed", code, positiveError.String())
				}
				name := filepath.Join(root, "sys/class/drm", filepath.FromSlash(target))
				want := inventory.ErrScalarLimit
				if mode == "directory" {
					if err := os.Remove(name); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(name, 0700); err != nil {
						t.Fatal(err)
					}
					want = inventory.ErrScalarRead
				} else if err := os.WriteFile(name, bytes.Repeat([]byte{'x'}, 65537), 0600); err != nil {
					t.Fatal(err)
				}
				output := filepath.Join(root, "plan.json")
				var stdout, stderr bytes.Buffer
				code := runPlanWithInput(t.Context(), []string{"-fingerprint-key", key, "-output", output}, &stdout, &stderr, collect, writeDestination, captureFixtureInput(t))
				if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), want.Error()) {
					t.Fatal("declared scalar failure became plan output", code)
				}
				if _, err := os.Stat(output); !os.IsNotExist(err) {
					t.Fatal("failed collection created a plan", err)
				}
			})
		}
	}
}

func drmComparisonReport(t *testing.T, snapshot inventory.Snapshot) comparison.Report {
	t.Helper()
	payload, err := app.BuildInventoryPayload(snapshot, bytes.Repeat([]byte{0x44}, 32), "drm")
	if err != nil {
		t.Fatal(err)
	}
	if payload.SystemFingerprint.IdentifiedParts != 0 || payload.SystemFingerprint.UnidentifiedParts != 3 || payload.SystemFingerprint.Value != "" {
		t.Fatal("video observations became durable identity")
	}
	for _, component := range payload.Components {
		if component.Identity.Value != "" || component.Identity.Source != "" || component.Identity.Confidence != "unavailable" {
			t.Fatal("video identity policy changed")
		}
	}
	// Construct comparison inputs; this test does not claim signature validation.
	return comparison.Report{Inventory: payload, Reference: comparison.ReportReference{
		SessionID: "drm-fixture", GeneratedTime: "2026-10-28T00:00:00Z", SoftwareVersion: "0.1.0-dev.0",
		SignerKeyID: "fixture", ManifestDigest: "sha256:" + strings.Repeat("1", 64),
		SystemFingerprint: payload.SystemFingerprint, ComponentCount: len(payload.Components),
	}}
}

func drmComparisonAttribute(component comparison.Component, name string) string {
	for _, attribute := range component.Attributes {
		if attribute.Name == name {
			return attribute.Value
		}
	}
	return ""
}

func TestInventoryDRMPlanAndComparisonKeepIdentitySemantics(t *testing.T) {
	for _, mode := range []string{"connector-state", "pci-device"} {
		t.Run(mode, func(t *testing.T) {
			root := newDRMCommandFixture(t)
			collector := inventory.NewLinuxCollector(root)
			before, err := collector.Collect(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			beforePlan, err := plan.Build(before, plan.ProfileQuick, "x86_64", bytes.Repeat([]byte{0x44}, 32), "drm")
			if err != nil {
				t.Fatal(err)
			}
			changes := map[string]string{"card0-HDMI-A-1/status": "disconnected\n", "card0-HDMI-A-1/enabled": "disabled\n"}
			if mode == "pci-device" {
				changes = map[string]string{"card0/device/device": "0xbcde\n", "renderD128/device/device": "0xbcde\n"}
			}
			for name, value := range changes {
				if err := os.WriteFile(filepath.Join(root, "sys/class/drm", filepath.FromSlash(name)), []byte(value), 0600); err != nil {
					t.Fatal(err)
				}
			}
			after, err := collector.Collect(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			afterPlan, err := plan.Build(after, plan.ProfileQuick, "x86_64", bytes.Repeat([]byte{0x44}, 32), "drm")
			if err != nil {
				t.Fatal(err)
			}
			if (beforePlan.HardwareGraph.Value == afterPlan.HardwareGraph.Value) != (mode == "connector-state") {
				t.Fatal("stable graph lost its volatile-versus-PCI distinction")
			}
			if mode == "connector-state" && beforePlan.ID != afterPlan.ID {
				t.Fatal("volatile connector state changed the frozen plan")
			}
			result, err := comparison.Compare(drmComparisonReport(t, before), drmComparisonReport(t, after))
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != comparison.StatusInconclusive || !result.Comparable || result.Complete || result.Summary.Unresolved != 3 || result.Summary.Matched != 0 || result.Summary.Replaced != 0 || result.Summary.Added != 0 || result.Summary.Removed != 0 {
				t.Fatal("DRM metadata invented physical continuity or replacement")
			}
			seen := map[string]bool{}
			for _, row := range result.Unresolved {
				if len(row.Before) != 1 || len(row.After) != 1 || row.Before[0].ID != row.After[0].ID {
					t.Fatal("comparison changed DRM component IDs")
				}
				old, current := row.Before[0], row.After[0]
				if seen[old.ID] {
					t.Fatal("comparison duplicated a DRM observation")
				}
				seen[old.ID] = true
				if old.ID == "video:drm:card0-HDMI-A-1" {
					want := "connected"
					if mode == "connector-state" {
						want = "disconnected"
					}
					if drmComparisonAttribute(old, "status") != "connected" || drmComparisonAttribute(current, "status") != want || drmComparisonAttribute(current, "device_id") != "" {
						t.Fatal("comparison lost connector-specific evidence")
					}
				} else {
					want := "0xabcd"
					if mode == "pci-device" {
						want = "0xbcde"
					}
					if drmComparisonAttribute(old, "device_id") != "0xabcd" || drmComparisonAttribute(current, "device_id") != want || drmComparisonAttribute(current, "vendor_id") != "0x1234" {
						t.Fatal("comparison lost controller/render PCI observations")
					}
				}
			}
			if len(seen) != 3 {
				t.Fatal("comparison omitted a DRM observation")
			}
		})
	}
}
