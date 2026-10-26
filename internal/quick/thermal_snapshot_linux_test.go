package quick

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestThermalSnapshotRejectsOwnedFIFORegistryAndReading(t *testing.T) {
	for _, registry := range []bool{true, false} {
		name := "reading"
		if registry {
			name = "registry"
		}
		t.Run(name, func(t *testing.T) {
			root := thermalFixture(t, "95000\n")
			path := filepath.Join(root, "sys/class/hwmon")
			want := ThermalSnapshot{Coverage: ThermalPartial, Sensors: 1, PeakMilliCelsius: 95000, EnumerationErrors: 1}
			if !registry {
				writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/name", "coretemp\n")
				path = filepath.Join(root, "sys/class/hwmon/hwmon0/temp1_input")
				want.EnumerationErrors, want.RejectedReadings = 0, 1
			}
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(path, 0600); err != nil {
				t.Fatal(err)
			}
			if got := ObserveThermals(t.Context(), root); got != want {
				t.Fatalf("FIFO classification: %+v", got)
			}
		})
	}
}
