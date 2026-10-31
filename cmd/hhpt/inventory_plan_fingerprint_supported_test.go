//go:build linux || windows

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestInventoryPlanFingerprintSupportedDefaultFiles(t *testing.T) {
	for _, command := range inventoryPlanFingerprintCommands() {
		for _, test := range []struct {
			name     string
			size     int
			accepted bool
		}{
			{"empty", 0, false},
			{"short-31", 31, false},
			{"exact-32", 32, true},
			{"long-33", 33, false},
			{"signing-size-64", 64, false},
			{"oversized", 4096, false},
			{"missing", -1, false},
			{"directory", -1, false},
		} {
			t.Run(command.name+"/"+test.name, func(t *testing.T) {
				directory := t.TempDir()
				path := filepath.Join(directory, "fingerprint.fixture")
				if test.name == "directory" {
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal("owned directory fixture failed")
					}
				} else if test.size >= 0 {
					data := make([]byte, test.size)
					for i := range data {
						data[i] = byte(i % 251)
					}
					path = writeTestKey(t, directory, "fingerprint.fixture", data)
				}
				args := []string{"-fingerprint-key", path}
				if command.name == "plan" {
					args = append(args, "-architecture", "x86_64")
				}
				collects, outputs, callbacks := 0, 0, 0
				var stdout, stderr bytes.Buffer
				collect := func(ctx context.Context) (inventory.Snapshot, error) {
					collects++
					if !test.accepted {
						t.Fatal("invalid default-reader input reached collection")
					}
					if ctx != t.Context() {
						t.Fatal("default reader changed the collection context")
					}
					// Removal also detects retained Windows file ownership before
					// downstream work. The command must already own the key bytes.
					if err := os.Remove(path); err != nil {
						t.Fatal("input ownership remained at collection")
					}
					return inventoryPlanFingerprintSnapshot(), nil
				}
				output := func(path string, writer io.Writer, callback func(io.Writer) error) error {
					outputs++
					if !test.accepted || collects != 1 || path != "-" || writer != &stdout || callback == nil {
						t.Fatal("invalid default-reader destination boundary")
					}
					return writeDestinationWith(path, writer, func(destination io.Writer) error {
						callbacks++
						return callback(destination)
					}, func(string, int, os.FileMode) (io.WriteCloser, error) {
						t.Error("default-reader fixture attempted output-file creation")
						return nil, errors.New("unexpected output open")
					})
				}
				code := command.defaultInput(t.Context(), args, &stdout, &stderr, collect, output)
				if test.accepted {
					want, size := "eed5302c9ef92a0eafb9df1a5f0b378196ddbf95503697852af07fb03f3d635b", 2588
					if command.name == "plan" {
						want, size = "c098f579c456545d7dbf6eb197b07ecafda55a96edd81dc8335785c9a4fa593d", 7135
					}
					if code != 0 || stderr.Len() != 0 || collects != 1 || outputs != 1 || callbacks != 1 ||
						stdout.Len() != size || fmt.Sprintf("%x", sha256.Sum256(stdout.Bytes())) != want {
						t.Fatal("owned default-reader success changed command bytes or sequencing")
					}
				} else {
					if code != 1 || stderr.String() != "read fingerprint key: input acquisition failed\n" ||
						stdout.Len() != 0 || collects != 0 || outputs != 0 || callbacks != 0 {
						t.Fatal("default-reader failure leaked diagnostics or advanced to a consumer")
					}
					if test.name != "missing" {
						if err := os.Remove(path); err != nil {
							t.Fatal("failed input retained owned fixture")
						}
					}
				}
			})
		}
		if runtime.GOOS == "windows" {
			for _, test := range []struct{ name, suffix string }{
				{"windows-nul", "\x00"},
				{"windows-control", "\n"},
				{"windows-trailing-dot", "."},
				{"windows-trailing-space", " "},
				{"windows-stream", ":stream"},
			} {
				t.Run(command.name+"/"+test.name, func(t *testing.T) {
					// A valid owned base distinguishes spelling rejection from
					// a missing ordinary input. Adapter tests separately prove
					// lexical rejection before an OS file open.
					base := writeTestKey(t, t.TempDir(), "fingerprint.fixture", inventoryPlanFingerprintKey(0))
					path := base + test.suffix
					args := []string{"-fingerprint-key", path}
					if command.name == "plan" {
						args = append(args, "-architecture", "x86_64")
					}
					collects, outputs := 0, 0
					collect := func(context.Context) (inventory.Snapshot, error) {
						collects++
						return inventory.Snapshot{}, errors.New("unexpected collection")
					}
					output := func(string, io.Writer, func(io.Writer) error) error {
						outputs++
						return errors.New("unexpected destination")
					}
					var stdout, stderr bytes.Buffer
					code := command.defaultInput(t.Context(), args, &stdout, &stderr, collect, output)
					if code != 1 || stderr.String() != "read fingerprint key: input acquisition failed\n" ||
						stdout.Len() != 0 || collects != 0 || outputs != 0 {
						t.Fatal("ineligible Windows spelling reached a consumer or leaked diagnostics")
					}
				})
			}
		}

	}
}

func TestInventoryPlanFingerprintPublicDispatchDirectory(t *testing.T) {
	for _, command := range inventoryPlanFingerprintCommands() {
		t.Run(command.name, func(t *testing.T) {
			path := t.TempDir()
			args := []string{"-fingerprint-key", path}
			if command.name == "plan" {
				args = append(args, "-architecture", "x86_64")
			}
			var stdout, stderr bytes.Buffer
			// A directory is rejected before collection even by the former
			// unbounded file read, so this dispatch fixture cannot probe inventory.
			code := command.dispatch(t.Context(), args, &stdout, &stderr)
			if code != 1 || stderr.String() != "read fingerprint key: input acquisition failed\n" || stdout.Len() != 0 {
				t.Fatal("public dispatch did not use the opaque checked-reader rejection")
			}
			if err := os.Remove(path); err != nil {
				t.Fatal("public dispatch retained its directory input")
			}
		})
	}
}
