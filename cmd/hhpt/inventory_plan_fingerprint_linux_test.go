//go:build linux

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
	"syscall"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestInventoryPlanFingerprintLinuxSpecialFiles(t *testing.T) {
	for _, command := range inventoryPlanFingerprintCommands() {
		for _, kind := range []string{"link", "fifo"} {
			t.Run(command.name+"/"+kind, func(t *testing.T) {
				directory := t.TempDir()
				path := filepath.Join(directory, "input.fixture")
				if kind == "link" {
					writeTestKey(t, directory, "target.fixture", inventoryPlanFingerprintKey(0))
					if err := os.Symlink("target.fixture", path); err != nil {
						t.Fatal("owned link fixture failed")
					}
				} else if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal("owned FIFO fixture failed")
				}
				ops := realRetestInputOperations()
				open := ops.openMetadata
				acquired, closed, readable := 0, 0, 0
				ops.openMetadata = func(path string) (retestInputMetadata, error) {
					owner, err := open(path)
					if owner != nil {
						acquired++
						return &retestInputTrackedMetadata{owner, &closed}, err
					}
					return nil, err
				}
				ops.openReadable = func(string, retestInputMetadata) (retestInputReadable, error) {
					readable++
					return nil, errors.New("unexpected readable acquisition")
				}
				h := newInventoryPlanFingerprintHarness(t, command)
				h.keyPath = path
				h.args[1] = path
				h.readInput = func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
					return readRetestInputWithOperations(ctx, path, kind, ops)
				}
				// The enclosing test timeout bounds this fixture. Context
				// cancellation does not promise interruption of a blocked OS call.
				h.requireCoreFailure(h.run())
				if acquired != 1 || closed != 1 || readable != 0 {
					t.Fatal("special input acquired a readable owner or retained metadata")
				}
				defaultCollections, defaultOutputs := 0, 0
				defaultCollect := func(context.Context) (inventory.Snapshot, error) {
					defaultCollections++
					return inventory.Snapshot{}, errors.New("unexpected collection")
				}
				defaultOutput := func(string, io.Writer, func(io.Writer) error) error {
					defaultOutputs++
					return errors.New("unexpected destination")
				}
				var stdout, stderr bytes.Buffer
				code := command.defaultInput(t.Context(), h.args, &stdout, &stderr, defaultCollect, defaultOutput)
				if code != 1 || stderr.String() != "read fingerprint key: input acquisition failed\n" ||
					stdout.Len() != 0 || defaultCollections != 0 || defaultOutputs != 0 {
					t.Fatal("default adapter followed a link or accepted a special input")
				}

				if err := os.Remove(path); err != nil {
					t.Fatal("special input fixture cleanup failed")
				}
			})
		}
	}
}

func TestInventoryPlanFingerprintLinuxPinnedObject(t *testing.T) {
	for _, command := range inventoryPlanFingerprintCommands() {
		t.Run(command.name, func(t *testing.T) {
			directory := t.TempDir()
			path := writeTestKey(t, directory, "input.fixture", inventoryPlanFingerprintKey(0))
			replacement := writeTestKey(t, directory, "replacement.fixture", inventoryPlanFingerprintKey(32))
			retained := filepath.Join(directory, "retained.fixture")
			ops := realRetestInputOperations()
			openMetadata, openReadable := ops.openMetadata, ops.openReadable
			var metadataFile, readableFile *os.File
			metadataOpens, readableOpens := 0, 0
			var boundaryError error
			ops.openMetadata = func(path string) (retestInputMetadata, error) {
				metadataOpens++
				owner, err := openMetadata(path)
				if owner != nil {
					var ok bool
					metadataFile, ok = owner.(*os.File)
					if !ok {
						boundaryError = errors.New("unexpected metadata owner type")
					}
				}
				return owner, err
			}
			ops.openReadable = func(path string, owner retestInputMetadata) (retestInputReadable, error) {
				readableOpens++
				if err := os.Rename(path, retained); err != nil {
					return nil, err
				}
				if err := os.Rename(replacement, path); err != nil {
					return nil, err
				}
				reader, err := openReadable(path, owner)
				if reader != nil {
					var ok bool
					readableFile, ok = reader.(*os.File)
					if !ok {
						boundaryError = errors.New("unexpected readable owner type")
					}
				}
				return reader, err
			}
			h := newInventoryPlanFingerprintHarness(t, command)
			h.keyPath = path
			h.args[1] = path
			h.readInput = func(ctx context.Context, path string, kind retestInputKind) ([]byte, error) {
				return readRetestInputWithOperations(ctx, path, kind, ops)
			}
			h.afterRead = func() {
				for _, file := range []*os.File{metadataFile, readableFile} {
					if file == nil {
						t.Fatal("pinned-object fixture did not acquire both owners")
					}
					if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
						t.Fatal("real owner remained open before command collection")
					}
				}
			}
			h.requireSuccess(h.run())
			if boundaryError != nil || metadataOpens != 1 || readableOpens != 1 ||
				h.readError != nil || !bytes.Equal(h.readData, inventoryPlanFingerprintKey(0)) {
				t.Fatal("command did not consume the originally pinned object's bytes")
			}
			want := "eed5302c9ef92a0eafb9df1a5f0b378196ddbf95503697852af07fb03f3d635b"
			if command.name == "plan" {
				want = "c00cc9d4afbc0acb8b9b05ce5f6e20d84fb631951ae94359bcef8a1f5bc531a7"
			}
			if fmt.Sprintf("%x", sha256.Sum256(h.stdout.Bytes())) != want {
				t.Fatal("pinned object changed command output")
			}
			current, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(current, inventoryPlanFingerprintKey(32)) {
				t.Fatal("replacement fixture was not distinguishable")
			}
			original, err := os.ReadFile(retained)
			if err != nil || !bytes.Equal(original, inventoryPlanFingerprintKey(0)) {
				t.Fatal("original owned fixture changed")
			}
		})
	}
}
