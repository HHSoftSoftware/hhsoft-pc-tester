package quick

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCPUQuickPassAndThermalAbort(t *testing.T) {
	root := thermalFixture(t, "42000\n")
	result := RunCPU(context.Background(), CPUOptions{
		Duration: 20 * time.Millisecond, GlobalTimeout: time.Second, Workers: 2,
		Root: root, ThermalLimit: 90000,
	})
	if result.Status != StatusPass || result.Iterations == 0 || result.Safety.ThermalSensors != 1 {
		t.Fatalf("unexpected CPU result: %#v", result)
	}

	hotRoot := thermalFixture(t, "42000\n")
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(hotRoot, "sys/class/thermal/thermal_zone0/temp"), []byte("100000\n"), 0o600)
	}()
	result = RunCPU(context.Background(), CPUOptions{
		Duration: time.Second, GlobalTimeout: 2 * time.Second, Workers: 1,
		Root: hotRoot, ThermalLimit: 90000,
	})
	if result.Status != StatusAborted || result.Safety.PeakMilliCelsius != 100000 {
		t.Fatalf("thermal abort not recorded: %#v", result)
	}

	result = RunCPU(context.Background(), CPUOptions{
		Duration: time.Second, GlobalTimeout: 50 * time.Millisecond, Workers: 1,
		Root: root, ThermalLimit: 90000,
	})
	if result.Status != StatusAborted || result.Detail != "global deadline exceeded" {
		t.Fatalf("global deadline not enforced: %#v", result)
	}
}

func TestMemoryQuickPassAndCancellation(t *testing.T) {
	result := RunMemory(context.Background(), MemoryOptions{
		Bytes: minimumMemoryBytes, Passes: 1, GlobalTimeout: 2 * time.Second,
		Root: t.TempDir(), ThermalLimit: 90000,
	})
	if result.Status != StatusPass || result.ExaminedBytes != minimumMemoryBytes*3 {
		t.Fatalf("unexpected memory result: %#v", result)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result = RunMemory(ctx, MemoryOptions{
		Bytes: minimumMemoryBytes, Passes: 1, GlobalTimeout: 2 * time.Second,
		Root: t.TempDir(), ThermalLimit: 90000,
	})
	if result.Status != StatusAborted {
		t.Fatalf("cancelled test was not aborted: %#v", result)
	}
}

func TestRecommendedMemoryBytesIsBounded(t *testing.T) {
	root := t.TempDir()
	writeQuickFixture(t, root, "proc/meminfo", "MemTotal: 262144 kB\nMemAvailable: 131072 kB\n")
	if value := RecommendedMemoryBytes(root); value != 32<<20 {
		t.Fatalf("recommended bytes = %d", value)
	}
}

func thermalFixture(t *testing.T, temperature string) string {
	t.Helper()
	root := t.TempDir()
	writeQuickFixture(t, root, "sys/class/thermal/thermal_zone0/temp", temperature)
	return root
}

func writeQuickFixture(t *testing.T, root, name, contents string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
