//go:build linux

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestSessionJournalRejectsSpecialFilesBeforeReading(t *testing.T) {
	fixture := newJournalFixture(t)
	regular := filepath.Join(fixture.directory, "regular.json")
	if err := writeSessionJournal(regular, fixture.journal); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(fixture.directory, "link.json")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(fixture.directory, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, fifo} {
		if err := writeSessionJournal(path, fixture.journal); err == nil {
			t.Fatal("existing special file overwritten")
		}
		done := make(chan error, 1)
		go func() {
			_, err := readSessionJournal(path)
			done <- err
		}()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("special file accepted as a session journal")
			}
		case <-time.After(time.Second):
			t.Fatal("session journal reader blocked on a special file")
		}
	}
	if _, err := readSessionJournal(regular); err != nil {
		t.Fatal("special-file rejection changed the original journal")
	}
}
