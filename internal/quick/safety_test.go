package quick

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Existing sensor ABI fixtures compare counts and peaks. Production consumers
// use the typed snapshot, which also discloses incomplete coverage.
func thermalSample(root string) (int, int) {
	result := ObserveThermals(context.Background(), root)
	return result.Sensors, result.PeakMilliCelsius
}

func TestHwmonOnlyTemperatureGuardsCPUAndMemory(t *testing.T) {
	for _, temperature := range []string{"90000\n", "105000\n"} {
		t.Run(strings.TrimSpace(temperature), func(t *testing.T) {
			root := t.TempDir()
			writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/name", "coretemp\n")
			writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_input", temperature)
			cpu := RunCPU(context.Background(), CPUOptions{
				Duration: time.Second, GlobalTimeout: 2 * time.Second, Workers: 1,
				Root: root, ThermalLimit: 90000,
			})
			if cpu.Status != StatusAborted || cpu.Detail != "thermal safety limit reached" || cpu.Iterations != 0 || cpu.Safety.ThermalSensors != 1 || cpu.Safety.ThermalSamples != 1 {
				t.Fatalf("CPU started despite hwmon overheating: %+v", cpu)
			}
			memory := RunMemory(context.Background(), MemoryOptions{
				Bytes: minimumMemoryBytes, Passes: 1, GlobalTimeout: 2 * time.Second,
				Root: root, ThermalLimit: 90000,
			})
			if memory.Status != StatusAborted || memory.Detail != "thermal safety limit reached" || memory.ExaminedBytes != 0 || memory.Safety.ThermalSensors != 1 || memory.Safety.ThermalSamples != 1 {
				t.Fatalf("memory started despite hwmon overheating: %+v", memory)
			}
		})
	}
}

func TestHwmonOnlyColdWorkersAndCancellation(t *testing.T) {
	root := t.TempDir()
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon9/name", "k10temp\n")
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon9/temp3_input", "42000\n")
	cpuOptions := CPUOptions{
		Duration: 20 * time.Millisecond, GlobalTimeout: 2 * time.Second, Workers: 1,
		Root: root, ThermalLimit: 90000,
	}
	cpu := RunCPU(context.Background(), cpuOptions)
	if cpu.Status != StatusPass || cpu.Iterations == 0 || cpu.Safety.ThermalSensors != 1 || cpu.Safety.PeakMilliCelsius != 42000 {
		t.Fatalf("cold hwmon CPU run did not pass: %+v", cpu)
	}
	memoryOptions := MemoryOptions{
		Bytes: minimumMemoryBytes, Passes: 1, GlobalTimeout: 2 * time.Second,
		Root: root, ThermalLimit: 90000,
	}
	memory := RunMemory(context.Background(), memoryOptions)
	if memory.Status != StatusPass || memory.ExaminedBytes != minimumMemoryBytes*6 || memory.Coverage.CompletedPhases != 10 || memory.Safety.ThermalSensors != 1 || memory.Safety.PeakMilliCelsius != 42000 {
		t.Fatalf("cold hwmon memory run did not pass: %+v", memory)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if cpu = RunCPU(ctx, cpuOptions); cpu.Status != StatusAborted || cpu.Detail != "cancelled" {
		t.Fatalf("hwmon sampling masked CPU cancellation: %+v", cpu)
	}
	if memory = RunMemory(ctx, memoryOptions); memory.Status != StatusAborted || memory.Detail != "cancelled" {
		t.Fatalf("hwmon sampling masked memory cancellation: %+v", memory)
	}
}

func TestHwmonMonitorResamplesAndPreservesDeadline(t *testing.T) {
	root := t.TempDir()
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/name", "coretemp\n")
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_input", "41000\n")
	monitor := newSafetyMonitor(context.Background(), root, 3*time.Second, 90000)
	t.Cleanup(monitor.stop)
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_input", "97000\n")
	select {
	case <-monitor.context.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("periodic hwmon sampling did not stop an overheating run")
	}
	if !errors.Is(monitor.failure(), ErrThermalLimit) {
		t.Fatalf("unexpected stop cause: %v", monitor.failure())
	}
	if evidence := monitor.evidence(); evidence.ThermalSensors != 1 || evidence.ThermalSamples < 2 || evidence.PeakMilliCelsius != 97000 {
		t.Fatalf("periodic hwmon evidence was lost: %+v", evidence)
	}

	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_input", "41000\n")
	deadline := newSafetyMonitor(context.Background(), root, 20*time.Millisecond, 90000)
	t.Cleanup(deadline.stop)
	select {
	case <-deadline.context.Done():
	case <-time.After(time.Second):
		t.Fatal("cold hwmon readings suppressed the global deadline")
	}
	if !errors.Is(deadline.failure(), context.DeadlineExceeded) {
		t.Fatalf("unexpected deadline cause: %v", deadline.failure())
	}
}

func TestThermalSampleCombinesAllSupportedInputs(t *testing.T) {
	root := thermalFixture(t, "43000\n")
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/name", "coretemp\n")
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon1/name", "k10temp\n")
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon203/device/name", "nct6775\n")
	writeQuickFixture(t, root, "sys/class/thermal/thermal_zone101/temp", "47000\n")
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_input", "42000\n")
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp17_input", "51000\n")
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon203/device/temp2_input", "89000\n")
	for index := 1; index <= 65; index++ {
		writeQuickFixture(t, root, fmt.Sprintf("sys/class/hwmon/hwmon1/temp%d_input", index), "48000\n")
	}
	if sensors, peak := thermalSample(root); sensors != 70 || peak != 89000 {
		t.Fatalf("thermal sources were omitted: sensors=%d peak=%d", sensors, peak)
	}
}

func TestThermalSampleIgnoresNonInputNames(t *testing.T) {
	root := t.TempDir()
	for _, device := range []string{"hwmon0", "hwmon01", "hwmon_extra"} {
		writeQuickFixture(t, root, "sys/class/hwmon/"+device+"/name", "coretemp\n")
	}
	for _, name := range []string{
		"sys/class/hwmon/hwmon0/temp0_input",
		"sys/class/hwmon/hwmon0/temp01_input",
		"sys/class/hwmon/hwmon0/temp1_input_extra",
		"sys/class/hwmon/hwmon0/temp1_max",
		"sys/class/hwmon/hwmon0/temp1_crit",
		"sys/class/hwmon/hwmon0/temp1_highest",
		"sys/class/hwmon/hwmon0/temp1_label",
		"sys/class/hwmon/hwmon0/temp_input",
		"sys/class/hwmon/hwmon0/in0_input",
		"sys/class/hwmon/hwmon0/fan1_input",
		"sys/class/hwmon/hwmon01/temp1_input",
		"sys/class/hwmon/hwmon_extra/temp1_input",
		"sys/class/thermal/thermal_zone_extra/temp",
		"sys/class/thermal/thermal_zone01/temp",
	} {
		writeQuickFixture(t, root, name, "100000\n")
	}
	if sensors, peak := thermalSample(root); sensors != 0 || peak != 0 {
		t.Fatalf("non-temperature inputs were counted: sensors=%d peak=%d", sensors, peak)
	}
}

func TestThermalSampleRejectsInvalidReadingsWithoutInventingSensors(t *testing.T) {
	for _, value := range []string{
		"", "\n", "unavailable\n", "42.5\n", "42000 C\n", "42000\n43000\n",
		"1e5\n", "0x186a0\n", "-1\n", "250001\n", "99999999999999999999999\n",
		"42000" + strings.Repeat(" ", 60),
	} {
		t.Run(fmt.Sprintf("%q", value), func(t *testing.T) {
			root := thermalFixture(t, value)
			writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/name", "coretemp\n")
			writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_input", value)
			if sensors, peak := thermalSample(root); sensors != 0 || peak != 0 {
				t.Fatalf("invalid reading counted: sensors=%d peak=%d", sensors, peak)
			}
		})
	}
	root := t.TempDir()
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/name", "coretemp\n")
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_input", "0\n")
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp2_input", "250000\n")
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp3_input", "42000"+strings.Repeat(" ", 59))
	if sensors, peak := thermalSample(root); sensors != 3 || peak != 250000 {
		t.Fatalf("valid range or file-length boundary rejected: sensors=%d peak=%d", sensors, peak)
	}
	if sensors, peak := thermalSample(t.TempDir()); sensors != 0 || peak != 0 {
		t.Fatalf("missing telemetry counted: sensors=%d peak=%d", sensors, peak)
	}
}

func TestHwmonOptionalEnableAndFaultFlags(t *testing.T) {
	for _, test := range []struct {
		name   string
		enable string
		fault  string
		typeID string
		count  int
	}{
		{name: "flags absent", count: 1},
		{name: "enabled and healthy", enable: "1\n", fault: "0\n", count: 1},
		{name: "disabled", enable: "0\n"},
		{name: "faulted", fault: "1\n"},
		{name: "malformed enable", enable: "enabled\n"},
		{name: "malformed fault", fault: "healthy\n"},
		{name: "unknown enable", enable: "2\n"},
		{name: "unknown fault", fault: "-1\n"},
		{name: "empty enable", enable: "\n"},
		{name: "oversize fault", fault: "0" + strings.Repeat(" ", 64)},
		{name: "disabled by type", typeID: "0\n"},
		{name: "thermistor converted by driver", typeID: "4\n", count: 1},
		{name: "malformed type", typeID: "thermistor\n"},
		{name: "unknown type", typeID: "7\n"},
		{name: "empty type", typeID: "\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/name", "coretemp\n")
			writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_input", "105000\n")
			if test.enable != "" {
				writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_enable", test.enable)
			}
			if test.fault != "" {
				writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_fault", test.fault)
			}
			if test.typeID != "" {
				writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_type", test.typeID)
			}
			sensors, peak := thermalSample(root)
			if sensors != test.count || test.count == 0 && peak != 0 || test.count == 1 && peak != 105000 {
				t.Fatalf("untrustworthy telemetry counted: sensors=%d peak=%d", sensors, peak)
			}
		})
	}
}

func TestHwmonRejectsChannelsRequiringBoardSpecificConversion(t *testing.T) {
	for _, test := range []struct {
		chip    string
		channel string
	}{
		{chip: "vt1211", channel: "temp2"},
		{chip: "wm831x", channel: "temp1"},
	} {
		for _, layout := range []string{"", "/device"} {
			t.Run(test.chip+layout, func(t *testing.T) {
				root := t.TempDir()
				directory := "sys/class/hwmon/hwmon0" + layout
				writeQuickFixture(t, root, directory+"/name", test.chip+"\n")
				for channel := 1; channel <= 7; channel++ {
					writeQuickFixture(t, root, fmt.Sprintf("%s/temp%d_input", directory, channel), "140000\n")
				}
				writeQuickFixture(t, root, directory+"/"+test.channel+"_input", "42000\n")
				if sensors, peak := thermalSample(root); sensors != 1 || peak != 42000 {
					t.Fatalf("uncalibrated channel treated as temperature: sensors=%d peak=%d", sensors, peak)
				}
				writeQuickFixture(t, root, directory+"/"+test.channel+"_input", "99000\n")
				monitor := newSafetyMonitor(context.Background(), root, time.Second, 90000)
				defer monitor.stop()
				if !errors.Is(monitor.failure(), ErrThermalLimit) || monitor.evidence().ThermalSensors != 1 {
					t.Fatalf("converted channel no longer guards overheating: %+v", monitor.evidence())
				}
			})
		}
	}
}

func TestHwmonRequiresReadableValidChipName(t *testing.T) {
	for _, name := range []string{"", "\n", "vt1211 extra\n", "wm831x\x00\n", "bad-name\n", "bad*name\n", strings.Repeat("a", 65)} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			root := t.TempDir()
			writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_input", "105000\n")
			if name != "" {
				writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/name", name)
			}
			if sensors, peak := thermalSample(root); sensors != 0 || peak != 0 {
				t.Fatalf("unidentified chip counted: sensors=%d peak=%d", sensors, peak)
			}
		})
	}
}

func TestHwmonAcceptsUppercaseAndPunctuationInChipNames(t *testing.T) {
	for _, name := range []string{"BAT0", "ucsi_source_psy_USBC000:001", "peci_dimmtemp.cpu0"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/name", name+"\n")
			writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_input", "46000\n")
			if sensors, peak := thermalSample(root); sensors != 1 || peak != 46000 {
				t.Fatalf("valid chip name rejected: sensors=%d peak=%d", sensors, peak)
			}
		})
	}
}

func TestHwmonPECITargetsDoNotCauseFalseThermalAbort(t *testing.T) {
	root := t.TempDir()
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/name", "peci_cputemp.cpu12\n")
	for _, channel := range []int{1, 2, 6, 25} {
		writeQuickFixture(t, root, fmt.Sprintf("sys/class/hwmon/hwmon0/temp%d_input", channel), "42000\n")
	}
	for _, channel := range []int{3, 4, 5} {
		writeQuickFixture(t, root, fmt.Sprintf("sys/class/hwmon/hwmon0/temp%d_input", channel), "100000\n")
	}
	if sensors, peak := thermalSample(root); sensors != 4 || peak != 42000 {
		t.Fatalf("PECI thresholds were read as current temperatures: sensors=%d peak=%d", sensors, peak)
	}
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp25_input", "97000\n")
	monitor := newSafetyMonitor(context.Background(), root, time.Second, 90000)
	defer monitor.stop()
	if !errors.Is(monitor.failure(), ErrThermalLimit) || monitor.evidence().PeakMilliCelsius != 97000 {
		t.Fatalf("PECI core heat was not monitored: %+v", monitor.evidence())
	}
}

func TestHwmonBadChannelsDoNotHideHealthyHotChannel(t *testing.T) {
	root := t.TempDir()
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/name", "it87\n")
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_input", "110000\n")
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_type", "0\n")
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp2_input", "120000\n")
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp2_fault", "1\n")
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp3_input", "95000\n")
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp3_type", "4\n")
	if sensors, peak := thermalSample(root); sensors != 1 || peak != 95000 {
		t.Fatalf("channel filtering suppressed valid heat: sensors=%d peak=%d", sensors, peak)
	}
}

func TestHwmonMonitorObservesNewAndRecoveredSensors(t *testing.T) {
	for _, initiallyFaulted := range []bool{false, true} {
		t.Run(fmt.Sprintf("initial_fault_%t", initiallyFaulted), func(t *testing.T) {
			root := t.TempDir()
			writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/name", "coretemp\n")
			if initiallyFaulted {
				writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_input", "105000\n")
				writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_fault", "1\n")
			}
			monitor := newSafetyMonitor(context.Background(), root, 3*time.Second, 90000)
			defer monitor.stop()
			if monitor.evidence().ThermalSensors != 0 || monitor.failure() != nil {
				t.Fatalf("missing or faulted sensor counted: %+v", monitor.evidence())
			}
			writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_input", "105000\n")
			writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_fault", "0\n")
			select {
			case <-monitor.context.Done():
			case <-time.After(2 * time.Second):
				t.Fatal("new or recovered sensor was not monitored")
			}
			if !errors.Is(monitor.failure(), ErrThermalLimit) || monitor.evidence().ThermalSensors != 1 || monitor.evidence().PeakMilliCelsius != 105000 {
				t.Fatalf("new or recovered sensor did not guard overheating: %+v", monitor.evidence())
			}
		})
	}
}

func TestHwmonAliasesAreCountedOnceWithoutMergingSeparateSensors(t *testing.T) {
	root := t.TempDir()
	writeQuickFixture(t, root, "sys/devices/sensor/name", "coretemp\n")
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon2/name", "coretemp\n")
	writeQuickFixture(t, root, "sys/devices/sensor/temp1_input", "45000\n")
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon2/temp1_input", "45000\n")
	quickSymlink(t, root, "sys/class/hwmon/hwmon0", "sys/devices/sensor")
	quickSymlink(t, root, "sys/class/hwmon/hwmon1", "sys/devices/sensor")
	quickSymlink(t, root, "sys/devices/sensor/device", "sys/devices/sensor")
	quickSymlink(t, root, "sys/class/thermal/thermal_zone0/temp", "sys/devices/sensor/temp1_input")
	if sensors, peak := thermalSample(root); sensors != 2 || peak != 45000 {
		t.Fatalf("aliases or separate sensors were miscounted: sensors=%d peak=%d", sensors, peak)
	}
}

func TestThermalSampleRejectsUnreadableAttributesAndEscapingAliases(t *testing.T) {
	root := t.TempDir()
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/name", "coretemp\n")
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_input", "105000\n")
	if err := os.Mkdir(filepath.Join(root, "sys/class/hwmon/hwmon0/temp1_fault"), 0o700); err != nil {
		t.Fatal(err)
	}
	if sensors, peak := thermalSample(root); sensors != 0 || peak != 0 {
		t.Fatalf("unreadable fault flag was accepted: sensors=%d peak=%d", sensors, peak)
	}
	quickSymlink(t, root, "sys/class/hwmon/hwmon0/temp2_input", "missing")
	outside := t.TempDir()
	writeQuickFixture(t, outside, "name", "coretemp\n")
	writeQuickFixture(t, outside, "temp1_input", "110000\n")
	if err := os.Symlink(outside, filepath.Join(root, "sys/class/hwmon/hwmon1")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink creation unavailable: %v", err)
		}
		t.Fatal(err)
	}
	if sensors, peak := thermalSample(root); sensors != 0 || peak != 0 {
		t.Fatalf("unavailable or outside-root attribute was read: sensors=%d peak=%d", sensors, peak)
	}
}

func TestHwmonRejectsEscapingStatusAndNameAttributes(t *testing.T) {
	for _, attribute := range []string{"name", "temp1_enable", "temp1_fault", "temp1_type"} {
		t.Run(attribute, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_input", "105000\n")
			if attribute != "name" {
				writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/name", "coretemp\n")
			}
			value := "1\n"
			if attribute == "name" {
				value = "coretemp\n"
			} else if attribute == "temp1_fault" {
				value = "0\n"
			}
			writeQuickFixture(t, outside, "attribute", value)
			if err := os.Symlink(filepath.Join(outside, "attribute"), filepath.Join(root, "sys/class/hwmon/hwmon0", attribute)); err != nil {
				if runtime.GOOS == "windows" {
					t.Skipf("symlink creation unavailable: %v", err)
				}
				t.Fatal(err)
			}
			if sensors, peak := thermalSample(root); sensors != 0 || peak != 0 {
				t.Fatalf("outside metadata accepted: sensors=%d peak=%d", sensors, peak)
			}
		})
	}
}

func quickSymlink(t *testing.T, root, name, target string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, filepath.FromSlash(target)), path); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink creation unavailable: %v", err)
		}
		t.Fatal(err)
	}
}
