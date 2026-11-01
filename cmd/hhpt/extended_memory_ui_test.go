package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtendedMemoryMenuRequiresExplicitOwnedLaunchSelection(t *testing.T) {
	for _, test := range []struct {
		name, input, mode            string
		interacted, legacy, existing bool
		code                         int
		profile                      string
	}{
		{"automatic_stays_quick", "e\n", "launch", false, false, false, 0, "quick"},
		{"explicit_extended", "e\n", "launch", true, false, false, 12, "extended"},
		{"legacy_extended", "E\n", "launch", true, true, false, 12, "extended"},
		{"old_selection_mode", "e\n", "selection", true, false, false, 1, ""},
		{"no_selection_owner", "e\n", "none", true, false, false, 1, ""},
		{"incomplete_choice", "e", "launch", true, false, false, 1, ""},
		{"quick_exclusions", "c\n1\n\ne\n", "launch", true, false, false, 1, ""},
		{"restored_defaults", "c\n1\n\ne\nc\na\n\ne\n", "launch", true, false, false, 12, "extended"},
		{"existing_output", "e\n", "launch", true, false, true, 1, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "launch.json")
			args := []string{"-auto-start", "1s"}
			if test.mode == "launch" {
				args = append(args, "-launch-selection-output", path)
			} else if test.mode == "selection" {
				args = append(args, "-selection-output", path)
			}
			architecture := nativeArchitecture()
			if test.legacy {
				args = append(args, "-legacy")
				architecture = "legacy-x86"
			} else {
				args = append(args, "-deployment-key-media")
			}
			if test.existing && os.WriteFile(path, []byte("preserved"), 0600) != nil {
				t.Fatal("owned output prerequisite")
			}
			var out, diagnostic bytes.Buffer
			waitCalls := 0
			code := runUIWith(t.Context(), args, strings.NewReader(test.input), &out, &diagnostic,
				func(context.Context, time.Duration, io.Reader) (bool, error) {
					waitCalls++
					return test.interacted, nil
				})
			if code != test.code || waitCalls != 1 {
				t.Fatalf("unexpected menu boundary: code=%d wait=%d diagnostic=%s", code, waitCalls, &diagnostic)
			}
			raw, err := os.ReadFile(path)
			if test.existing {
				if err != nil || string(raw) != "preserved" {
					t.Fatal("existing owner changed")
				}
			} else if test.profile == "" {
				if !os.IsNotExist(err) {
					t.Fatal("incomplete or inadmissible selection acquired an output")
				}
			} else {
				want := fmt.Sprintf("{\"schema_version\":1,\"profile\":%q,\"architecture\":%q,\"excluded_tests\":[]}\n", test.profile, architecture)
				if err != nil || string(raw) != want {
					t.Fatalf("wrong independent launch document: %s", raw)
				}
			}
			if test.profile == "extended" && (!strings.Contains(out.String(), "1800-second") ||
				!strings.Contains(out.String(), "45-minute") || strings.Contains(out.String(), "Stable deployment-key media selected")) {
				t.Fatal("explicit Extended scope or ephemeral identity was lost")
			}
			if test.name == "quick_exclusions" || test.name == "restored_defaults" {
				if !strings.Contains(diagnostic.String(), "Extended profile requires all defaults") {
					t.Fatal("exclusion rejection prerequisite was not exercised")
				}
			}
		})
	}
}
