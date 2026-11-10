//go:build linux || windows

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/comparison"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

const pcieCommandDirectory = "sys/bus/pci/devices/fixture-pci"
const pcieCommandCanary = "DISCARDED-PCIE-COMMAND-CANARY"

func pcieCommandPut(t *testing.T, root, name, value string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(pcieCommandDirectory+"/"+name))
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

type pcieCommandOpaqueError struct{ calls *int }

func (e pcieCommandOpaqueError) Error() string { (*e.calls)++; return pcieCommandCanary }
func (e pcieCommandOpaqueError) Is(error) bool { (*e.calls)++; return false }
func (e pcieCommandOpaqueError) Unwrap() error { (*e.calls)++; return nil }

type pcieCommandSource struct {
	inventory.PathSource
	failSpeed bool
	callbacks int
}

func (s *pcieCommandSource) ReadFile(name string) ([]byte, error) {
	if s.failSpeed && name == pcieCommandDirectory+"/max_link_speed" {
		return []byte(pcieCommandCanary), pcieCommandOpaqueError{&s.callbacks}
	}
	return s.PathSource.ReadFile(name)
}

func pcieCommandCollect(t *testing.T, root string, fail bool) inventory.Snapshot {
	t.Helper()
	source := &pcieCommandSource{PathSource: inventory.NewRootPathSource(root), failSpeed: fail}
	snapshot, diagnostics, err := inventory.NewLinuxCollectorWithSource(source).CollectWithDiagnostics(t.Context())
	want := 0
	if fail {
		want = 1
	}
	if err != nil || source.callbacks != 0 || int(diagnostics.ScalarFailures) != want {
		t.Fatal("actual collector/diagnostic prerequisite failed")
	}
	return snapshot
}

func pcieCommandFixture(t *testing.T) firmwareCommandFixture {
	t.Helper()
	f := newFirmwareCommandFixture(t)
	pcieCommandPut(t, f.root, "max_link_speed", "16.0 GT/s PCIe")
	pcieCommandPut(t, f.root, "max_link_width", "8")
	pcieCommandPut(t, f.root, "current_link_speed", "2.5 GT/s PCIe")
	pcieCommandPut(t, f.root, "current_link_width", "1")
	f.snapshot = pcieCommandCollect(t, f.root, false)
	excluded, err := plan.SelectableQuickTests("x86_64")
	if err != nil {
		t.Fatal(err)
	}
	f.frozen, err = plan.BuildWithSelection(f.snapshot, plan.ProfileQuick, "x86_64", bytes.Repeat([]byte{0x39}, 32), "power-fixture", excluded)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range f.frozen.Tests {
		if row.Status == plan.TestScheduled {
			t.Fatal("fixture scheduled an active worker")
		}
	}
	raw, err := json.Marshal(f.frozen)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.planPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return f
}

func pcieCommandRow(t *testing.T, payload app.InventoryPayload) app.ComponentRecord {
	t.Helper()
	var result app.ComponentRecord
	count := 0
	for _, row := range payload.Components {
		if row.Type == inventory.ComponentPCI {
			result = row
			count++
		}
	}
	if count != 1 || result.ID != "pci:fixture-pci" || result.Identity != (app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "power-fixture", Confidence: "unavailable"}) || len(payload.Components) != 4 || payload.SystemFingerprint.IdentifiedParts != 1 || payload.SystemFingerprint.UnidentifiedParts != 3 || payload.SystemFingerprint.Value != firmwareCommandSystem || payload.SystemFingerprint.KeyID != firmwareCommandKeyID {
		t.Fatal("PCI maximum acquired identity or changed durable control")
	}
	return result
}

func pcieCommandPayload(t *testing.T, artifact bundle.Artifact) app.InventoryPayload {
	t.Helper()
	var payload app.InventoryPayload
	if err := json.Unmarshal(artifact.InventoryJSON, &payload); err != nil {
		t.Fatal(err)
	}
	pcieCommandRow(t, payload)
	return payload
}

func pcieCommandCapture(t *testing.T, snapshot inventory.Snapshot, self bool, session string, second int64) ([]byte, bundle.Artifact, ed25519.PublicKey) {
	t.Helper()
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x73}, 32))
	public := private.Public().(ed25519.PublicKey)
	var raw []byte
	var err error
	if self {
		raw, err = captureSelfIssuedBundle(snapshot, nil, bytes.Repeat([]byte{0x39}, 32), "power-fixture", private, public, session, time.Unix(second, 0), app.CoverageDirectCapture)
	} else {
		raw, err = captureBundle(snapshot, nil, bytes.Repeat([]byte{0x39}, 32), "power-fixture", private, "pcie-consumer-key", session, time.Unix(second, 0), app.CoverageDirectCapture)
	}
	if err != nil {
		t.Fatal("production capture failed", err)
	}
	var artifact bundle.Artifact
	if self {
		artifact, err = bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
	} else {
		artifact, err = bundle.Verify(bytes.NewReader(raw), int64(len(raw)), report.Keyring{"pcie-consumer-key": public})
	}
	if err != nil {
		t.Fatal("actual full signature/member/view verification prerequisite", err)
	}
	memoryCapacityCommandProfile(t, artifact, true, false)
	return raw, artifact, public
}

func pcieCommandPrivacy(t *testing.T, values ...[]byte) {
	t.Helper()
	for _, raw := range values {
		assertFirmwareCommandPrivacy(t, raw)
		for _, forbidden := range []string{pcieCommandCanary} {
			if bytes.Contains(raw, []byte(forbidden)) {
				t.Fatal("discarded PCI input escaped a consumer")
			}
		}
	}
}

func TestPCIeCapabilityCurrentSignedConsumersKeepFiniteObservations(t *testing.T) {
	for _, tc := range []struct {
		name, speed, width, speedState, speedValue, widthState, widthValue string
		fail                                                               bool
	}{
		{name: "reported", speed: "16.0 GT/s PCIe", width: "8", speedState: "reported", speedValue: "16000", widthState: "reported", widthValue: "8"},
		{name: "unknown", speed: "Unknown", width: "255", speedState: "unknown", widthState: "unknown"},
		{name: "malformed", speed: pcieCommandCanary, width: "08", speedState: "malformed", widthState: "malformed"},
		{name: "read_error", fail: true, width: "16", speedState: "read_error", widthState: "reported", widthValue: "16"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, trust := range []string{"external", "self_issued"} {
				t.Run(trust, func(t *testing.T) {
					f := pcieCommandFixture(t)
					pcieCommandPut(t, f.root, "max_link_speed", tc.speed)
					pcieCommandPut(t, f.root, "max_link_width", tc.width)
					pcieCommandPut(t, f.root, "current_link_speed", pcieCommandCanary)
					snapshot := pcieCommandCollect(t, f.root, tc.fail)
					raw, artifact, public := pcieCommandCapture(t, snapshot, trust == "self_issued", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", 1)
					payload := pcieCommandPayload(t, artifact)
					row := pcieCommandRow(t, payload)
					want := []inventory.Attribute{
						{Name: "address", Value: "fixture-pci"},
						{Name: "current_link_speed_state", Value: "malformed"},
						{Name: "current_link_width_lanes", Value: "1"},
						{Name: "current_link_width_state", Value: "reported"},
					}
					if tc.speedValue != "" {
						want = append(want, inventory.Attribute{Name: "max_link_speed_mts", Value: tc.speedValue})
					}
					want = append(want, inventory.Attribute{Name: "max_link_speed_state", Value: tc.speedState})
					if tc.widthValue != "" {
						want = append(want, inventory.Attribute{Name: "max_link_width_lanes", Value: tc.widthValue})
					}
					want = append(want, inventory.Attribute{Name: "max_link_width_state", Value: tc.widthState})
					if !reflect.DeepEqual(row.Attributes, want) || len(payload.TestResults) != 0 {
						t.Fatal("signed maximum capability states or outcomes changed")
					}
					for _, attribute := range want {
						if !bytes.Contains(artifact.RenderedReport, []byte("<th scope=\"row\">"+attribute.Name+"</th><td>"+attribute.Value+"</td>")) || !bytes.Contains(artifact.RenderedPDF, []byte("    "+attribute.Name+": "+attribute.Value)) {
							t.Fatal("fully verified view lost generic PCI observation")
						}
					}
					path := filepath.Join(f.directory, "current.hhpcr")
					if err := os.WriteFile(path, raw, 0600); err != nil {
						t.Fatal(err)
					}
					keyPath := writeTestKey(t, f.directory, "current.pub", public)
					args := []string{"-public-key", keyPath, "-key-id", artifact.Envelope.KeyID, path}
					if trust == "self_issued" {
						args = []string{"-self-issued", path}
					}
					var out, diagnostic bytes.Buffer
					if code := runVerify(t.Context(), args, &out, &diagnostic); code != 0 {
						t.Fatal("real current CLI verification failed", code, diagnostic.String())
					}
					pcieCommandPrivacy(t, artifact.InventoryJSON, artifact.RenderedReport, artifact.RenderedPDF, out.Bytes(), diagnostic.Bytes())
					wrong := writeTestKey(t, f.directory, "wrong.pub", ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x74}, 32)).Public().(ed25519.PublicKey))
					out.Reset()
					diagnostic.Reset()
					if code := runVerify(t.Context(), []string{"-public-key", wrong, "-key-id", artifact.Envelope.KeyID, path}, &out, &diagnostic); code == 0 || out.Len() != 0 {
						t.Fatal("valid current report bypassed independent signer trust")
					}
					after, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(after, raw) {
						t.Fatal("verification changed original report")
					}
				})
			}
		})
	}
}

func TestPCIeCapabilityRealComparisonRemainsInconclusive(t *testing.T) {
	for _, trust := range []string{"external", "self_issued"} {
		t.Run(trust, func(t *testing.T) {
			for _, tc := range []struct{ name, field, value string }{
				{"speed", "max_link_speed", "32.0 GT/s PCIe"},
				{"width", "max_link_width", "16"},
				{"current_speed", "current_link_speed", "8.0 GT/s PCIe"},
				{"current_width", "current_link_width", "4"},
				{"current_malformed", "current_link_speed", pcieCommandCanary},
				{"current_reserved_width", "current_link_width", "255"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					f := pcieCommandFixture(t)
					left, l, public := pcieCommandCapture(t, f.snapshot, trust == "self_issued", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", 1)
					pcieCommandPut(t, f.root, tc.field, tc.value)
					current := pcieCommandCollect(t, f.root, false)
					right, r, _ := pcieCommandCapture(t, current, trust == "self_issued", "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff", 2)
					before, after := pcieCommandPayload(t, l), pcieCommandPayload(t, r)
					oldPCI, newPCI := pcieCommandRow(t, before), pcieCommandRow(t, after)
					if reflect.DeepEqual(oldPCI.Attributes, newPCI.Attributes) {
						t.Fatal("maximum mutation was ineffective")
					}
					leftPath, rightPath := filepath.Join(f.directory, "before.hhpcr"), filepath.Join(f.directory, "after.hhpcr")
					if err := os.WriteFile(leftPath, left, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(rightPath, right, 0600); err != nil {
						t.Fatal(err)
					}
					keyPath := writeTestKey(t, f.directory, "compare.pub", public)
					args := []string{"-public-key", keyPath, "-key-id", l.Envelope.KeyID, leftPath, rightPath}
					if trust == "self_issued" {
						args = []string{"-self-issued", leftPath, rightPath}
					}
					var out, diagnostic bytes.Buffer
					if code := runCompare(t.Context(), args, &out, &diagnostic); code != 0 {
						t.Fatal("real signed comparison failed", code, diagnostic.String())
					}
					var got comparison.Result
					if err := json.Unmarshal(out.Bytes(), &got); err != nil {
						t.Fatal(err)
					}
					ref := func(a bundle.Artifact, p app.InventoryPayload) comparison.ReportReference {
						return comparison.ReportReference{SessionID: a.Envelope.SessionID, GeneratedTime: a.Envelope.GeneratedTime, SoftwareVersion: product.Version, SignerKeyID: a.Envelope.KeyID, ManifestDigest: a.Envelope.ManifestDigest, SystemFingerprint: p.SystemFingerprint, ComponentCount: 4}
					}
					if got.Schema != "hhsoft.pc-tester.hardware-comparison/v1" || got.Policy != "hhsoft.parts-swap.v1" || got.Status != comparison.StatusInconclusive || !got.Comparable || got.Complete || !reflect.DeepEqual(got.Summary, comparison.Summary{Matched: 1, Unresolved: 3}) || len(got.Matched) != 1 || len(got.Unresolved) != 3 || !reflect.DeepEqual(got.Before, ref(l, before)) || !reflect.DeepEqual(got.After, ref(r, after)) {
						t.Fatal("comparison invented a complete identity verdict or lost authenticated references")
					}
					wantBefore := comparison.Component{Type: inventory.ComponentPCI, ID: "pci:fixture-pci", IdentityConfidence: "unavailable", Attributes: oldPCI.Attributes}
					wantAfter := comparison.Component{Type: inventory.ComponentPCI, ID: "pci:fixture-pci", IdentityConfidence: "unavailable", Attributes: newPCI.Attributes}
					found := 0
					for _, change := range got.Unresolved {
						if len(change.Before) == 1 && change.Before[0].Type == inventory.ComponentPCI {
							found++
							if !reflect.DeepEqual(change.Before, []comparison.Component{wantBefore}) || !reflect.DeepEqual(change.After, []comparison.Component{wantAfter}) || change.Reason != "stable identity is unavailable at the same observed location; continuity or replacement cannot be proven" {
								t.Fatal("PCI observations were promoted to continuity/replacement evidence")
							}
						}
					}
					if found != 1 {
						t.Fatal("comparison dropped PCI observations")
					}
					pcieCommandPrivacy(t, out.Bytes(), diagnostic.Bytes())
					for _, item := range []struct {
						path string
						raw  []byte
					}{{leftPath, left}, {rightPath, right}} {
						retained, err := os.ReadFile(item.path)
						if err != nil || !bytes.Equal(retained, item.raw) {
							t.Fatal("comparison changed original report")
						}
					}
				})
			}
		})
	}
}

func TestPCIeCapabilityRealFinalizerBindsMaximaAndRetainsCurrentObservations(t *testing.T) {
	for _, tc := range []struct {
		name, field, value string
		changed            bool
	}{
		{"current_only", "current_link_speed", pcieCommandCanary, false},
		{"maximum_speed", "max_link_speed", "32.0 GT/s PCIe", true},
		{"maximum_width", "max_link_width", "16", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := pcieCommandFixture(t)
			run := func(name string, changed bool) {
				t.Helper()
				path := filepath.Join(f.directory, name)
				calls := 0
				collect := func(ctx context.Context) (inventory.Snapshot, error) {
					calls++
					return inventory.NewLinuxCollector(f.root).Collect(ctx)
				}
				var out, diagnostic bytes.Buffer
				if code := runFinalizeWith(t.Context(), f.arguments(t, path), &out, &diagnostic, collect, bytes.NewReader(bytes.Repeat([]byte{0x45}, 64)), func() time.Time { return time.Unix(10, 0) }); code != 0 || calls != 1 {
					t.Fatal("real finalizer failed", code, calls, diagnostic.String())
				}
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				artifact, err := bundle.VerifySelfIssued(bytes.NewReader(raw), int64(len(raw)))
				if err != nil {
					t.Fatal(err)
				}
				memoryCapacityCommandProfile(t, artifact, true, false)
				payload := pcieCommandPayload(t, artifact)
				wantCurrent := map[string]string{
					"current_link_speed_state": "reported", "current_link_speed_mts": "2500",
					"current_link_width_state": "reported", "current_link_width_lanes": "1",
				}
				if name == "after.hhpcr" && !tc.changed {
					wantCurrent = map[string]string{
						"current_link_speed_state": "malformed",
						"current_link_width_state": "reported", "current_link_width_lanes": "32",
					}
				}
				if !reflect.DeepEqual(pcieNegotiatedCommandFields(pcieCommandRow(t, payload)), wantCurrent) {
					t.Fatal("finalizer omitted or changed the final negotiated observations")
				}
				if payload.CoverageScope != app.CoverageFrozenPlan {
					t.Fatal("finalizer lost frozen-plan scope")
				}
				current := pcieCommandCollect(t, f.root, false)
				graph, err := plan.ObserveHardwareGraph(current, bytes.Repeat([]byte{0x39}, 32), "power-fixture")
				if err != nil {
					t.Fatal(err)
				}
				wantStatus, wantEvidence := app.TestPass, "pass"
				var wantChanged []plan.HardwareComponentRef
				if changed {
					wantStatus, wantEvidence = app.TestFail, "fail"
					wantChanged = []plan.HardwareComponentRef{{Type: inventory.ComponentPCI, ID: "pci:fixture-pci"}}
				}
				count := 0
				for _, row := range payload.TestResults {
					if strings.Contains(row.ID, "pcie") {
						t.Fatal("passive capability invented a functional worker")
					}
					if row.ID == "run.integrity.quick" {
						count++
						var evidence sessionevidence.IntegrityEvidence
						if err := json.Unmarshal(row.Evidence, &evidence); err != nil {
							t.Fatal(err)
						}
						if row.Status != wantStatus || evidence.Status != wantEvidence || row.PlanID != f.frozen.ID || row.HardwareGraph != f.frozen.HardwareGraph.Value || evidence.PlannedHardwareGraph != f.frozen.HardwareGraph.Value || evidence.FinalHardwareGraph != graph.Value || evidence.PlannedComponentCount != 4 || evidence.FinalComponentCount != 4 || len(evidence.AddedComponents) != 0 || len(evidence.RemovedComponents) != 0 || !reflect.DeepEqual(evidence.ChangedComponents, wantChanged) || evidence.Session != nil {
							t.Fatal("signed integrity lost exact PCI configuration delta")
						}
					}
				}
				if count != 1 || !bytes.Contains(out.Bytes(), []byte("HHPT_RUN_INTEGRITY status="+wantEvidence)) || (graph.Value != f.frozen.HardwareGraph.Value) != changed {
					t.Fatal("finalizer status/configuration binding mismatch")
				}
				pcieCommandPrivacy(t, artifact.InventoryJSON, artifact.RenderedReport, artifact.RenderedPDF, out.Bytes(), diagnostic.Bytes())
			}
			run("positive.hhpcr", false)
			pcieCommandPut(t, f.root, tc.field, tc.value)
			if !tc.changed {
				pcieCommandPut(t, f.root, "current_link_width", "32")
			}
			run("after.hhpcr", tc.changed)
		})
	}
}
