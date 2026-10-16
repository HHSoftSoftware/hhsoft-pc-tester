//go:build linux

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestSaveAuditRejectsNonConsoleBeforeWritableOpen(t *testing.T) {
	directory := t.TempDir()
	regular := filepath.Join(directory, "regular")
	if err := os.WriteFile(regular, []byte("preserved fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(directory, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{regular, directory, link, fifo, filepath.Join(directory, "absent")} {
		opened := false
		file, err := openPinnedSaveConsole(path, func(string, int, os.FileMode) (*os.File, error) {
			opened = true
			t.Fatal("non-console node reached writable open")
			return nil, nil
		})
		if err == nil || file != nil || opened {
			t.Fatal("non-console output was not rejected before I/O")
		}
	}
	contents, err := os.ReadFile(regular)
	if err != nil || string(contents) != "preserved fixture" {
		t.Fatal("audit rejection changed fixture data")
	}
}

func TestSaveAuditRequiresConsoleDeviceIdentity(t *testing.T) {
	for _, fixture := range []consoleAuditInfo{
		{mode: os.ModeDevice | os.ModeCharDevice, device: 5<<8 | 1, accepted: true},
		{mode: os.ModeDevice | os.ModeCharDevice, device: 1<<8 | 3}, // null is not console
		{mode: os.ModeDevice, device: 5<<8 | 1},                     // block is never accepted
		{mode: os.ModeSymlink, device: 5<<8 | 1},
		{mode: 0, device: 5<<8 | 1},
		{mode: os.ModeNamedPipe, device: 5<<8 | 1},
	} {
		if isSaveConsole(fixture) != fixture.accepted {
			t.Fatal("console audit accepted the wrong node type or device identity")
		}
	}
}

type consoleAuditInfo struct {
	mode     os.FileMode
	device   uint64
	accepted bool
}

func (info consoleAuditInfo) Name() string       { return "fixture" }
func (info consoleAuditInfo) Size() int64        { return 0 }
func (info consoleAuditInfo) Mode() os.FileMode  { return info.mode }
func (info consoleAuditInfo) ModTime() time.Time { return time.Time{} }
func (info consoleAuditInfo) IsDir() bool        { return false }
func (info consoleAuditInfo) Sys() any           { return &syscall.Stat_t{Rdev: info.device} }
