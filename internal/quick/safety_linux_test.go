package quick

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestThermalAttributesRejectFIFOWithoutWaitingForWriter(t *testing.T) {
	root := t.TempDir()
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/name", "coretemp\n")
	directory := filepath.Join(root, "sys/class/hwmon/hwmon0")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(directory, "temp1_input"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeQuickFixture(t, root, "sys/class/hwmon/hwmon0/temp2_input", "100000\n")
	if err := syscall.Mkfifo(filepath.Join(directory, "temp2_enable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if sensors, peak := thermalSample(root); sensors != 0 || peak != 0 {
		t.Fatalf("FIFO metadata counted: sensors=%d peak=%d", sensors, peak)
	}
}

func TestThermalAttributeOpenDoesNotWaitForFIFOWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "temperature")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	type openedAttribute struct {
		file *os.File
		err  error
	}
	opened := make(chan openedAttribute, 1)
	go func() {
		file, err := openThermalAttribute(path)
		opened <- openedAttribute{file: file, err: err}
	}()
	var result openedAttribute
	select {
	case result = <-opened:
	case <-time.After(time.Second):
		// Release a mistakenly blocking open before reporting the regression.
		writer, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer syscall.Close(writer)
		select {
		case result = <-opened:
			if result.file != nil {
				result.file.Close()
			}
		case <-time.After(time.Second):
			t.Fatal("blocked thermal open could not be released")
		}
		t.Fatal("thermal attribute open waited for a FIFO writer")
	}
	if result.err != nil {
		t.Fatal(result.err)
	}
	defer result.file.Close()
	info, err := result.file.Stat()
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatal("test did not open the FIFO fixture")
	}
}
