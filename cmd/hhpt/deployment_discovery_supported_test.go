//go:build linux || windows

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDeploymentDiscoveryKeyOrdinaryFilesRequireExactLength(t *testing.T) {
	for _, size := range []int{31, 32, 33} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			h := newDeploymentInputFixture(t)
			h.path = filepath.Join(t.TempDir(), "discovery.key")
			h.args = []string{"-discovery-key", h.path, "-audit-output", h.auditPath}
			data := bytes.Repeat([]byte{0x55}, size)
			if size == 32 {
				data = append([]byte(nil), h.key...)
			}
			if err := os.WriteFile(h.path, data, 0600); err != nil {
				t.Fatal(err)
			}
			// Only key acquisition is real; every audit/media/activation effect is injected.
			h.acquire = readRetestInput
			code := h.run()
			if size == 32 {
				h.success(code)
			} else {
				h.rejected(code, "read media discovery key: input acquisition failed", 1)
			}
			got, err := os.ReadFile(h.path)
			if err != nil || !bytes.Equal(got, data) {
				t.Fatal("ordinary input fixture changed")
			}
		})
	}
}

func TestDeploymentDiscoveryKeyRealDispatchRejectsDirectory(t *testing.T) {
	for _, entry := range []string{"wrapper", "command"} {
		t.Run(entry, func(t *testing.T) {
			directory := t.TempDir()
			input := &deploymentInputConsole{reader: deploymentInputErrorReader{}}
			var stdout, stderr bytes.Buffer
			args := []string{"-discovery-key", directory}
			var code int
			if entry == "wrapper" {
				code = runDeploymentKeyLoadMedia(context.Background(), args, input, &stdout, &stderr)
			} else {
				code = runDeploymentKey(context.Background(), append([]string{"load-media"}, args...), input, &stdout, &stderr)
			}
			// A nonregular key is the barrier before availability, selection or media access.
			// No audit-output is supplied, so the real audit provider owns no file.
			if code != 1 || stderr.String() != "read media discovery key: input acquisition failed\n" ||
				stdout.Len() != 0 || input.reads != 0 {
				t.Fatal("real dispatch did not reject the nonregular key before media work", code, stderr.String())
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 0 {
				t.Fatal("nonregular fixture changed")
			}
		})
	}
}
