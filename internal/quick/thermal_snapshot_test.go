package quick

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestThermalSnapshotDistinguishesAbsenceInvalidAndUnavailable(t *testing.T) {
	for _, test := range []struct {
		name  string
		files map[string]string
		want  ThermalSnapshot
	}{
		{name: "empty system", want: ThermalSnapshot{Coverage: ThermalUnavailable}},
		{name: "valid zero temperature", files: map[string]string{"sys/class/thermal/thermal_zone0/temp": "0\n"},
			want: ThermalSnapshot{Coverage: ThermalAvailable, Sensors: 1}},
		{name: "malformed reading", files: map[string]string{"sys/class/thermal/thermal_zone0/temp": "unknown\n"},
			want: ThermalSnapshot{Coverage: ThermalPartial, RejectedReadings: 1}},
		{name: "missing required reading", files: map[string]string{"sys/class/thermal/thermal_zone0/type": "fixture\n"},
			want: ThermalSnapshot{Coverage: ThermalPartial, RejectedReadings: 1}},
		{name: "missing hwmon identity", files: map[string]string{"sys/class/hwmon/hwmon0/temp1_input": "42000\n"},
			want: ThermalSnapshot{Coverage: ThermalPartial, EnumerationErrors: 1}},
		{name: "disabled", files: map[string]string{"sys/class/hwmon/hwmon0/name": "coretemp\n", "sys/class/hwmon/hwmon0/temp1_input": "99000\n", "sys/class/hwmon/hwmon0/temp1_enable": "0\n"},
			want: ThermalSnapshot{Coverage: ThermalUnavailable, UnavailableReadings: 1}},
		{name: "faulted", files: map[string]string{"sys/class/hwmon/hwmon0/name": "coretemp\n", "sys/class/hwmon/hwmon0/temp1_input": "99000\n", "sys/class/hwmon/hwmon0/temp1_fault": "1\n"},
			want: ThermalSnapshot{Coverage: ThermalUnavailable, UnavailableReadings: 1}},
		{name: "unused type", files: map[string]string{"sys/class/hwmon/hwmon0/name": "it87\n", "sys/class/hwmon/hwmon0/temp1_input": "99000\n", "sys/class/hwmon/hwmon0/temp1_type": "0\n"},
			want: ThermalSnapshot{Coverage: ThermalUnavailable, UnavailableReadings: 1}},
		{name: "malformed optional flag", files: map[string]string{"sys/class/hwmon/hwmon0/name": "coretemp\n", "sys/class/hwmon/hwmon0/temp1_input": "99000\n", "sys/class/hwmon/hwmon0/temp1_fault": "2\n"},
			want: ThermalSnapshot{Coverage: ThermalPartial, RejectedReadings: 1}},
		{name: "hot valid plus known unavailable", files: map[string]string{"sys/class/hwmon/hwmon0/name": "coretemp\n", "sys/class/hwmon/hwmon0/temp1_input": "99000\n", "sys/class/hwmon/hwmon0/temp2_input": "41000\n", "sys/class/hwmon/hwmon0/temp2_fault": "1\n"},
			want: ThermalSnapshot{Coverage: ThermalPartial, Sensors: 1, PeakMilliCelsius: 99000, UnavailableReadings: 1}},
		{name: "hot valid plus invalid", files: map[string]string{"sys/class/thermal/thermal_zone0/temp": "99000\n", "sys/class/thermal/thermal_zone1/temp": "unknown\n"},
			want: ThermalSnapshot{Coverage: ThermalPartial, Sensors: 1, PeakMilliCelsius: 99000, RejectedReadings: 1}},
		{name: "calibration-dependent input excluded", files: map[string]string{"sys/class/hwmon/hwmon0/name": "vt1211\n", "sys/class/hwmon/hwmon0/temp1_input": "99000\n", "sys/class/hwmon/hwmon0/temp2_input": "41000\n"},
			want: ThermalSnapshot{Coverage: ThermalAvailable, Sensors: 1, PeakMilliCelsius: 41000}},
		{name: "control targets excluded", files: map[string]string{"sys/class/hwmon/hwmon0/name": "peci_cputemp.cpu0\n", "sys/class/hwmon/hwmon0/temp1_input": "41000\n", "sys/class/hwmon/hwmon0/temp3_input": "99000\n"},
			want: ThermalSnapshot{Coverage: ThermalAvailable, Sensors: 1, PeakMilliCelsius: 41000}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			for path, value := range test.files {
				writeQuickFixture(t, root, path, value)
			}
			if got := ObserveThermals(t.Context(), root); got != test.want {
				t.Fatalf("snapshot=%+v, want=%+v", got, test.want)
			}
		})
	}
}

func TestThermalSnapshotReportsRegistryFailuresWithoutDiscardingHeat(t *testing.T) {
	root := thermalFixture(t, "95000\n")
	writeQuickFixture(t, root, "sys/class/hwmon", "not a directory\n")
	want := ThermalSnapshot{Coverage: ThermalPartial, Sensors: 1, PeakMilliCelsius: 95000, EnumerationErrors: 1}
	if got := ObserveThermals(t.Context(), root); got != want {
		t.Fatalf("broken registry hid heat: %+v", got)
	}
	if got := ObserveThermals(t.Context(), filepath.Join(root, "missing-root")); got.Coverage != ThermalPartial || got.EnumerationErrors != 1 || got.Sensors != 0 {
		t.Fatalf("missing observation root became clean absence: %+v", got)
	}
	if got := ObserveThermals(t.Context(), filepath.Join(root, "sys/class/hwmon")); got.Coverage != ThermalPartial || got.EnumerationErrors != 1 {
		t.Fatalf("regular-file root became clean absence: %+v", got)
	}
}

func TestThermalSnapshotEnumeratesEveryBatchAndContainsNoPaths(t *testing.T) {
	root := t.TempDir()
	const count = 257
	for index := 0; index < count; index++ {
		writeQuickFixture(t, root, fmt.Sprintf("sys/class/thermal/thermal_zone%d/temp", index), fmt.Sprintf("%d\n", 41000+index))
	}
	result := ObserveThermals(t.Context(), root)
	if result.Coverage != ThermalAvailable || result.Sensors != count || result.PeakMilliCelsius != 41256 || result.EnumerationErrors != 0 {
		t.Fatalf("enumeration was truncated: %+v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > 512 || strings.Contains(string(encoded), "thermal_zone") || strings.Contains(string(encoded), "sys/") {
		t.Fatal("thermal snapshot contains unbounded device details")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil || len(fields) != 6 {
		t.Fatal("thermal snapshot wire shape changed")
	}
}

type cancelThermalTraversal struct {
	context.Context
	cancel context.CancelFunc
	checks atomic.Int32
}

func (ctx *cancelThermalTraversal) Err() error {
	if ctx.checks.Add(1) == 100 {
		ctx.cancel()
	}
	return ctx.Context.Err()
}

func TestThermalSnapshotCancellationIsNotMissingTelemetry(t *testing.T) {
	root := t.TempDir()
	for index := 0; index < 257; index++ {
		writeQuickFixture(t, root, fmt.Sprintf("sys/class/thermal/thermal_zone%d/temp", index), "41000\n")
	}
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &cancelThermalTraversal{Context: base, cancel: cancel}
	result := ObserveThermals(ctx, root)
	if base.Err() == nil || ctx.checks.Load() != 100 || result.Coverage != ThermalPartial || result.EnumerationErrors != 1 || result.Sensors == 0 || result.Sensors >= 257 {
		t.Fatalf("traversal ignored cancellation: checks=%d snapshot=%+v", ctx.checks.Load(), result)
	}
	if before := ObserveThermals(base, root); before != (ThermalSnapshot{Coverage: ThermalPartial, EnumerationErrors: 1}) {
		t.Fatalf("pre-cancelled observation read sensors: %+v", before)
	}
	if invalid := ObserveThermals(nil, root); invalid.Coverage != ThermalPartial || invalid.EnumerationErrors != 1 {
		t.Fatal("nil context became clean absence")
	}
}

func TestThermalSnapshotUnreadableInputHasNoFalseSensorCount(t *testing.T) {
	root := t.TempDir()
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/name", "coretemp\n")
	if err := os.MkdirAll(filepath.Join(root, "sys/class/hwmon/hwmon0/temp1_input"), 0700); err != nil {
		t.Fatal(err)
	}
	result := ObserveThermals(t.Context(), root)
	if result != (ThermalSnapshot{Coverage: ThermalPartial, RejectedReadings: 1}) {
		t.Fatalf("unreadable input counted as sensor: %+v", result)
	}
}

func TestThermalSnapshotAliasesDoNotInflateDegradedCounts(t *testing.T) {
	for _, faulted := range []bool{false, true} {
		name := "rejected aliases"
		if faulted {
			name = "unavailable aliases"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeQuickFixture(t, root, "sys/devices/sensor/name", "coretemp\n")
			value := "malformed\n"
			if faulted {
				value = "95000\n"
				writeQuickFixture(t, root, "sys/devices/sensor/temp1_fault", "1\n")
			}
			writeQuickFixture(t, root, "sys/devices/sensor/temp1_input", value)
			quickSymlink(t, root, "sys/class/hwmon/hwmon0", "sys/devices/sensor")
			quickSymlink(t, root, "sys/class/hwmon/hwmon1", "sys/devices/sensor")
			quickSymlink(t, root, "sys/devices/sensor/device", "sys/devices/sensor")
			want := ThermalSnapshot{Coverage: ThermalPartial, RejectedReadings: 1}
			if faulted {
				want = ThermalSnapshot{Coverage: ThermalUnavailable, UnavailableReadings: 1}
			}
			if got := ObserveThermals(t.Context(), root); got != want {
				t.Fatalf("aliases inflated degraded readings: %+v", got)
			}
		})
	}
}

func TestThermalSnapshotMissingEscapingAncestorIsNotCleanAbsence(t *testing.T) {
	base := t.TempDir()
	root, outside := filepath.Join(base, "root"), filepath.Join(base, "outside")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	writeQuickFixture(t, outside, "hwmon/hwmon0/name", "coretemp\n")
	writeQuickFixture(t, outside, "hwmon/hwmon0/temp1_input", "105000\n")
	quickSymlink(t, root, "sys/class", "../outside")
	result := ObserveThermals(t.Context(), root)
	if result != (ThermalSnapshot{Coverage: ThermalPartial, EnumerationErrors: 2}) {
		t.Fatalf("escaping ancestor was read or treated as clean absence: %+v", result)
	}
}

func TestThermalSnapshotDanglingProviderAndStatusAreErrors(t *testing.T) {
	t.Run("provider", func(t *testing.T) {
		root := t.TempDir()
		quickSymlink(t, root, "sys/class/thermal", "missing-provider")
		if got := ObserveThermals(t.Context(), root); got != (ThermalSnapshot{Coverage: ThermalPartial, EnumerationErrors: 1}) {
			t.Fatalf("dangling provider became clean absence: %+v", got)
		}
	})
	t.Run("status", func(t *testing.T) {
		root := t.TempDir()
		writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/name", "coretemp\n")
		writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp1_input", "105000\n")
		quickSymlink(t, root, "sys/class/hwmon/hwmon0/temp1_fault", "missing-status")
		if got := ObserveThermals(t.Context(), root); got != (ThermalSnapshot{Coverage: ThermalPartial, RejectedReadings: 1}) {
			t.Fatalf("dangling status became an available reading: %+v", got)
		}
	})
}
