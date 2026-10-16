package graphics

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type fakeInteractiveRunner struct {
	calls [][]string
}

func (runner *fakeInteractiveRunner) Run(_ context.Context, name string, arguments ...string) (string, error) {
	runner.calls = append(runner.calls, append([]string{name}, arguments...))
	return "", nil
}

func (runner *fakeInteractiveRunner) RunInteractive(_ context.Context, _ time.Duration, name string, arguments ...string) (string, error) {
	runner.calls = append(runner.calls, append([]string{name}, arguments...))
	return "", nil
}

type fakeRunner struct {
	output string
	err    error
	calls  [][]string
}

func (runner *fakeRunner) Run(_ context.Context, name string, arguments ...string) (string, error) {
	runner.calls = append(runner.calls, append([]string{name}, arguments...))
	return runner.output, runner.err
}

func TestProbeDetectsDriverRendererAndDisplayPattern(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "sys/class/drm/card0/device/uevent", "DRIVER=amdgpu\n")
	writeFile(t, root, "sys/class/drm/card0/device/vendor", "0x1002\n")
	writeFile(t, root, "sys/class/drm/card0/device/device", "0x744c\n")
	writeFile(t, root, "sys/class/drm/card0-HDMI-A-1/status", "connected\n")
	writeFile(t, root, "sys/class/drm/card0-HDMI-A-1/enabled", "enabled\n")
	writeFile(t, root, "sys/class/drm/card0-HDMI-A-1/modes", "3840x2160\n1920x1080\n")
	writeFile(t, root, "sys/class/drm/renderD128/dev", "226:128\n")
	runner := &fakeRunner{output: "OpenGL core profile renderer: AMD Radeon RX 7900 XTX\n"}

	result := Probe(context.Background(), root, 120, runner)

	if result.Status != StatusPass || result.Renderer.Name != "AMD Radeon RX 7900 XTX" || !result.Renderer.HardwareDetected {
		t.Fatalf("renderer result = %#v", result)
	}
	if len(result.Cards) != 1 || result.Cards[0].Driver != "amdgpu" || !reflect.DeepEqual(result.RenderNodes, []string{"/dev/dri/renderD128"}) {
		t.Fatalf("cards = %#v, nodes = %#v", result.Cards, result.RenderNodes)
	}
	if !reflect.DeepEqual(result.Cards[0].Connectors[0].AdvertisedModes, []string{"3840x2160", "1920x1080"}) {
		t.Fatalf("modes = %#v", result.Cards[0].Connectors[0].AdvertisedModes)
	}
	if len(runner.calls) != 2 || !reflect.DeepEqual(runner.calls[1], []string{"/usr/bin/kmscube", "-D", "/dev/dri/card0", "-c", "120", "-M", "rgba"}) {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestProbeRejectsSoftwareRenderingFallback(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "sys/class/drm/card0/device/uevent", "DRIVER=virtio_gpu\n")
	runner := &fakeRunner{output: "OpenGL ES profile renderer: llvmpipe (LLVM 22.1.3)\n"}

	result := Probe(context.Background(), root, 120, runner)

	if result.Status != StatusInconclusive || result.Renderer.HardwareDetected || result.Pattern.Status != StatusSkip {
		t.Fatalf("result = %#v", result)
	}
}

func TestProbeRecordsToolFailureWithoutFailingClosedInventory(t *testing.T) {
	runner := &fakeRunner{err: errors.New("permission denied")}
	result := Probe(context.Background(), t.TempDir(), 0, runner)
	if result.Status != StatusInconclusive || result.Renderer.Detail != "permission denied" {
		t.Fatalf("result = %#v", result)
	}
}

func TestPreferredDisplayProbePatternsEveryConnectedConnector(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "sys/class/drm/card0/device/uevent", "DRIVER=virtio_gpu\n")
	writeFile(t, root, "sys/class/drm/card0-HDMI-A-1/status", "connected\n")
	writeFile(t, root, "sys/class/drm/card0-HDMI-A-1/modes", "3840x2160\n1920x1080\n")
	writeFile(t, root, "sys/class/drm/card0-DP-1/status", "connected\n")
	writeFile(t, root, "sys/class/drm/card0-DP-1/modes", "2560x1440\n")
	writeFile(t, root, "sys/class/drm/card1-Virtual-1/status", "connected\n")
	writeFile(t, root, "sys/class/drm/card1-Virtual-1/modes", "1920x1080\n")
	writeFile(t, root, "sys/class/drm/card1/device/uevent", "DRIVER=virtio_gpu\n")
	runner := &fakeInteractiveRunner{}

	result := ProbePreferredDisplays(context.Background(), root, 5*time.Second, runner)

	if result.Status != StatusInconclusive || len(result.Patterns) != 2 || len(result.Patterns[0].Connectors) != 2 || result.Patterns[0].Connectors[0].PreferredMode != "2560x1440" {
		t.Fatalf("unexpected visual result: %#v", result)
	}
	expected := [][]string{{"/usr/bin/modetest", "-D", "/dev/dri/card0", "-r"}, {"/usr/bin/modetest", "-D", "/dev/dri/card1", "-r"}}
	if !reflect.DeepEqual(runner.calls, expected) {
		t.Fatalf("visual calls = %#v", runner.calls)
	}
	if err := ApplyVisualFindingCodes(&result, []string{"ppppppp", "ppfpiip", "p"}); err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusFail || len(result.Findings) != 3 || result.Findings[1].Criteria["color"] != StatusFail {
		t.Fatalf("operator findings = %#v", result)
	}
}

func TestPreferredDisplayProbeMakesMismatchedModesInconclusive(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "sys/class/drm/card0/device/uevent", "DRIVER=virtio_gpu\n")
	writeFile(t, root, "sys/class/drm/card0-HDMI-A-1/status", "connected\n")
	writeFile(t, root, "sys/class/drm/card0-HDMI-A-1/modes", "1920x1080\n")
	runner := &failingInteractiveRunner{}
	result := ProbePreferredDisplays(context.Background(), root, 5*time.Second, runner)
	if result.Status != StatusInconclusive || result.Patterns[0].Status != StatusInconclusive {
		t.Fatalf("modeset failure passed: %#v", result)
	}
}

type failingInteractiveRunner struct{ fakeInteractiveRunner }

func (runner *failingInteractiveRunner) RunInteractive(context.Context, time.Duration, string, ...string) (string, error) {
	return "failed to set mode: denied", nil
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	fullPath := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
