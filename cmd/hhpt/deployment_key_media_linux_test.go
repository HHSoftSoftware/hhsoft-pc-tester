//go:build linux

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/deploymentkey"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

func TestDeploymentKeyMountRequiresExactReadOnlyPolicy(t *testing.T) {
	candidate := reporttarget.Candidate{Device: "/dev/sdb1", DeviceMajorMinor: "8:17"}
	valid := "31 24 8:17 / /run/hhpt/deployment-key-media ro,nosuid,nodev,noexec - ext4 /dev/sdb1 ro,noload\n"
	if !deploymentKeyMountMatches(strings.NewReader(valid), "ext4", candidate) {
		t.Fatal("valid read-only deployment-key mount was rejected")
	}
	for _, invalid := range []string{
		strings.Replace(valid, "ro,nosuid", "rw,nosuid", 1),
		strings.Replace(valid, "nodev,", "", 1),
		strings.Replace(valid, "8:17", "8:18", 1),
		strings.Replace(valid, "/dev/sdb1", "/dev/sdc1", 1),
		strings.Replace(valid, " - ext4 ", " - vfat ", 1),
	} {
		if deploymentKeyMountMatches(strings.NewReader(invalid), "ext4", candidate) {
			t.Fatalf("invalid mount policy accepted: %q", invalid)
		}
	}
}

func TestDeploymentKeyFIFOIsRejectedWithoutWaitingForAWriter(t *testing.T) {
	const childDirectory = "HHPT_TEST_DEPLOYMENT_FIFO_DIRECTORY"
	if root := os.Getenv(childDirectory); root != "" {
		directory, err := os.Open(root)
		if err != nil {
			t.Fatal(err)
		}
		defer directory.Close()
		if _, err := readDeploymentKeyDirectory(directory); err == nil {
			t.Fatal("FIFO accepted as deployment key")
		}
		return
	}
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, deploymentkey.Filename), 0o600); err != nil {
		t.Fatal(err)
	}
	// Bound the real open/read path in a subprocess so a regression cannot
	// hang the test process. No writer ever opens this FIFO.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDeploymentKeyFIFOIsRejectedWithoutWaitingForAWriter$")
	command.Env = append(os.Environ(), childDirectory+"="+root)
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatal("deployment-key read waited for FIFO I/O instead of rejecting its type")
	}
	if err != nil {
		t.Fatalf("FIFO rejection failed: %v: %s", err, output)
	}
}

func TestDeploymentKeyDirectoryReadsOnlyBoundedDirectRegularFiles(t *testing.T) {
	record, err := deploymentkey.New("test-token", bytes.NewReader(bytes.Repeat([]byte{0x5d}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := deploymentkey.Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"regular", "empty", "oversized", "directory", "symlink", "corrupt"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, deploymentkey.Filename)
			switch kind {
			case "directory":
				err = os.Mkdir(path, 0o700)
			case "symlink":
				target := filepath.Join(root, "regular-token")
				if err := os.WriteFile(target, encoded, 0o600); err != nil {
					t.Fatal(err)
				}
				err = os.Symlink(target, path)
			default:
				data := encoded
				switch kind {
				case "empty":
					data = nil
				case "oversized":
					data = bytes.Repeat([]byte{'x'}, deploymentkey.MaximumEncodedSize+1)
				case "corrupt":
					data = append([]byte(nil), encoded...)
					data[len(data)-1] ^= 1
				}
				err = os.WriteFile(path, data, 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
			directory, err := os.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			defer directory.Close()
			actual, err := readDeploymentKeyDirectory(directory)
			if kind == "regular" {
				if err != nil || actual != record {
					t.Fatalf("valid token did not round-trip: %v", err)
				}
			} else if err == nil {
				t.Fatalf("%s accepted as deployment key", kind)
			}
		})
	}
}

func TestDeploymentKeyReadRemainsInOpenedDirectory(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "media")
	if err := os.Mkdir(original, 0o700); err != nil {
		t.Fatal(err)
	}
	record, err := deploymentkey.New("original-token", bytes.NewReader(bytes.Repeat([]byte{0x4c}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := deploymentkey.Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(original, deploymentkey.Filename), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	directory, err := os.Open(original)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if err := os.Rename(original, filepath.Join(root, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(original, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(original, deploymentkey.Filename), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	actual, err := readDeploymentKeyDirectory(directory)
	if err != nil || actual != record {
		t.Fatalf("read followed a replaced directory name: %v", err)
	}
}
