package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func uiLaunchArchitecture(legacy bool) string {
	if legacy {
		return "legacy-x86"
	}
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "386":
		return "x86"
	case "arm64":
		return "aarch64"
	default:
		return runtime.GOARCH
	}
}

func uiLaunchExpected(profile string, legacy bool, exclusions string) string {
	return fmt.Sprintf("{\"schema_version\":1,\"profile\":%q,\"architecture\":%q,\"excluded_tests\":%s}\n", profile, uiLaunchArchitecture(legacy), exclusions)
}

func TestUILaunchSelectionStartPaths(t *testing.T) {
	for _, c := range []struct {
		name, input, profile                        string
		legacy, deployment, automatic, monitorError bool
		code                                        int
	}{
		{name: "automatic", input: "s\n", profile: "quick", automatic: true},
		{name: "enter", input: "\n", profile: "quick"},
		{name: "one", input: "1\n", profile: "quick"},
		{name: "deployment", input: "k\n", profile: "quick", deployment: true, code: 10},
		{name: "standard", input: "s\n", profile: "standard", code: 11},
		{name: "standard-uppercase", input: " S \n", profile: "standard", code: 11},
		{name: "wait-standard", input: "w\ns\n", profile: "standard", code: 11},
		{name: "legacy-standard", input: "k\ns\n", profile: "standard", legacy: true, deployment: true, code: 11},
		{name: "legacy-automatic", profile: "quick", legacy: true, automatic: true},
		{name: "monitor-failure-standard", input: "s\n", profile: "standard", monitorError: true, code: 11},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "launch.json")
			args := []string{"-launch-selection-output", path}
			if c.legacy {
				args = append(args, "-legacy")
			}
			if c.deployment {
				args = append(args, "-deployment-key-media")
			}
			wait := func(context.Context, time.Duration, io.Reader) (bool, error) {
				if c.monitorError {
					return false, errors.New("owned monitor failure")
				}
				return !c.automatic, nil
			}
			var out bytes.Buffer
			if code := runUIWith(t.Context(), args, strings.NewReader(c.input), &out, &out, wait); code != c.code {
				t.Fatalf("code=%d want=%d: %s", code, c.code, &out)
			}
			assertUISelectionFile(t, path, uiLaunchExpected(c.profile, c.legacy, "[]"))
			if !c.automatic && (!strings.Contains(out.String(), "600-second sustained CPU load") || !strings.Contains(out.String(), "15-minute cooperative session budget")) {
				t.Fatal("standard load disclosure missing")
			}
			if c.legacy && (!strings.Contains(out.String(), "LEGACY BIOS/CSM") || strings.Contains(out.String(), "[K]")) {
				t.Fatal("legacy scope changed")
			}
			if c.profile == "standard" && !strings.Contains(out.String(), "standard profile with ephemeral identity.") {
				t.Fatal("standard identity not explicit")
			}
		})
	}
}

func TestUILaunchSelectionRequiresExplicitDefaults(t *testing.T) {
	for _, c := range []struct {
		name, input, profile, exclusions string
		code                             int
		created                          bool
	}{
		{"blocked", "c\n2\n\ns\n", "", "", 1, false},
		{"quick-preserved", "c\n2\n\ns\n1\n", "quick", `["cpu.quick"]`, 0, true},
		{"deployment-preserved", "c\n2\n\ns\nk\n", "quick", `["cpu.quick"]`, 10, true},
		{"explicit-restore", "c\n2\n\ns\nc\na\n\ns\n", "standard", "[]", 11, true},
		{"false-toggle-entry", "c\n2\n2\n\ns\n", "standard", "[]", 11, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "launch.json")
			var out bytes.Buffer
			code := runUIWith(t.Context(), []string{"-launch-selection-output", path, "-deployment-key-media"}, strings.NewReader(c.input), &out, &out, uiOperatorWait)
			if code != c.code {
				t.Fatalf("code=%d want=%d: %s", code, c.code, &out)
			}
			if c.created {
				assertUISelectionFile(t, path, uiLaunchExpected(c.profile, false, c.exclusions))
			} else if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("blocked standard wrote a handoff")
			}
			if c.name != "false-toggle-entry" && !strings.Contains(out.String(), "Choices unchanged; no test started.") {
				t.Fatal("exclusions silently changed")
			}
		})
	}
}

func TestUILaunchSelectionFlagPresenceRejectsBeforeWaitOrCreate(t *testing.T) {
	for _, args := range [][]string{
		{"-launch-selection-output="}, {"-launch-selection-output", ""}, {"-launch-selection-output", " "}, {"-launch-selection-output", "-"},
		{"-selection-output=", "-launch-selection-output="}, {"-selection-output=", "-launch-selection-output", "owned.json"},
		{"-selection-output", "owned.json", "-launch-selection-output="}, {"-selection-output", "old.json", "-launch-selection-output", "new.json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			waits, creates := 0, 0
			wait := func(context.Context, time.Duration, io.Reader) (bool, error) { waits++; return false, nil }
			create := func(string, int, os.FileMode) (uiSelectionFile, error) { creates++; return nil, errors.New("tripwire") }
			var out bytes.Buffer
			if code := runUIWithSelectionOutput(t.Context(), args, strings.NewReader("s\n"), &out, &out, wait, create); code != 2 || waits != 0 || creates != 0 {
				t.Fatalf("invalid flag reached effects: code=%d waits=%d creates=%d", code, waits, creates)
			}
			assertUINoLaunch(t, out.String())
		})
	}
}

func TestUILaunchSelectionUnavailableWithoutNewMode(t *testing.T) {
	for _, mode := range []string{"no-output", "old-empty", "old-file"} {
		t.Run(mode, func(t *testing.T) {
			var args []string
			path := filepath.Join(t.TempDir(), "old.json")
			if mode == "old-empty" {
				args = []string{"-selection-output="}
			} else if mode == "old-file" {
				args = []string{"-selection-output", path}
			}
			var out bytes.Buffer
			if code := runUIWith(t.Context(), args, strings.NewReader("s\n1\n"), &out, &out, uiOperatorWait); code != 0 {
				t.Fatalf("old mode code=%d: %s", code, &out)
			}
			if strings.Contains(out.String(), "[S]") || !strings.Contains(out.String(), "Standard profile needs -launch-selection-output") {
				t.Fatal("unrecorded standard offered")
			}
			if mode == "old-file" {
				assertUISelectionFile(t, path, `{"excluded_tests":[]}`+"\n")
			} else if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("no-output behavior changed")
			}
		})
	}
}

func TestUILaunchSelectionIncompleteInputNeverCreates(t *testing.T) {
	for _, input := range []string{"", "s", "w\n", "c\n2\n", "unknown\n", strings.Repeat(" ", maximumMenuLineBytes+1) + "s\n"} {
		t.Run(fmt.Sprintf("input-%d", len(input)), func(t *testing.T) {
			creates := 0
			create := func(string, int, os.FileMode) (uiSelectionFile, error) { creates++; return nil, errors.New("tripwire") }
			var out bytes.Buffer
			code := runUIWithSelectionOutput(t.Context(), []string{"-launch-selection-output", filepath.Join(t.TempDir(), "launch.json")}, strings.NewReader(input), &out, &out, uiOperatorWait, create)
			if code != 1 || creates != 0 {
				t.Fatalf("incomplete input launched: code=%d creates=%d", code, creates)
			}
			assertUINoLaunch(t, out.String())
		})
	}
}

type uiPublicationOwner struct {
	file           *os.File
	fault          string
	cancel         context.CancelFunc
	writes, closes int
	stored         []byte
}

func (owner *uiPublicationOwner) Write(data []byte) (int, error) {
	owner.writes++
	part := data
	switch owner.fault {
	case "write-error":
		part = data[:3]
	case "short-nil":
		part = data[:len(data)-1]
	case "negative-count":
		part = data[:1]
	}
	n, err := owner.file.Write(part)
	owner.stored = append(owner.stored, part[:n]...)
	if err != nil {
		return n, err
	}
	switch owner.fault {
	case "write-error", "full-error":
		return n, errors.New("owned write failure")
	case "negative-count":
		return -1, nil
	case "long-count":
		return len(data) + 1, nil
	case "cancel-write":
		owner.cancel()
	}
	return n, nil
}
func (owner *uiPublicationOwner) Close() error {
	owner.closes++
	err := owner.file.Close()
	if owner.fault == "cancel-close" {
		owner.cancel()
	}
	if err == nil && owner.fault == "close-error" {
		return errors.New("owned close failure")
	}
	return err
}
func assertUINoLaunch(t *testing.T, out string) {
	t.Helper()
	for _, marker := range []string{"Starting automatic", "Starting operator-selected", "Stable deployment-key media selected."} {
		if strings.Contains(out, marker) {
			t.Fatalf("failed operation reported launch: %s", out)
		}
	}
}

func TestUISelectionPublicationChecksAllOwnershipBoundaries(t *testing.T) {
	for _, mode := range []struct {
		name, input               string
		launch, automatic, legacy bool
		code                      int
	}{
		{"old-auto", "", false, true, false, 0}, {"old-manual", "1\n", false, false, false, 0}, {"old-key", "k\n", false, false, false, 10},
		{"launch-auto", "", true, true, false, 0}, {"launch-manual", "1\n", true, false, false, 0}, {"launch-key", "k\n", true, false, false, 10},
		{"standard", "s\n", true, false, false, 11}, {"legacy-standard", "s\n", true, false, true, 11},
	} {
		for _, fault := range []string{"success", "cancel-before-create", "create-error", "nil-owner", "typed-nil-owner", "owner-and-error", "cancel-create", "write-error", "short-nil", "negative-count", "long-count", "full-error", "close-error", "cancel-write", "cancel-close"} {
			t.Run(mode.name+"/"+fault, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "selection.json")
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				flag := "-selection-output"
				if mode.launch {
					flag = "-launch-selection-output"
				}
				args := []string{flag, path, "-deployment-key-media"}
				if mode.legacy {
					args = append(args, "-legacy")
				}
				creates := 0
				var owner *uiPublicationOwner
				create := func(got string, flags int, permissions os.FileMode) (uiSelectionFile, error) {
					creates++
					if got != path || flags != os.O_WRONLY|os.O_CREATE|os.O_EXCL || permissions != 0o600 {
						t.Fatal("exclusive output arguments changed")
					}
					switch fault {
					case "create-error":
						return nil, errors.New("owned create failure")
					case "nil-owner":
						return nil, nil
					case "typed-nil-owner":
						return (*uiPublicationOwner)(nil), nil
					}
					f, err := os.OpenFile(got, flags, permissions)
					if err != nil {
						t.Fatal(err)
					}
					owner = &uiPublicationOwner{file: f, fault: fault, cancel: cancel}
					if fault == "cancel-create" {
						cancel()
					}
					if fault == "owner-and-error" {
						return owner, errors.New("owned create receipt failure")
					}
					return owner, nil
				}
				wait := func(context.Context, time.Duration, io.Reader) (bool, error) {
					if fault == "cancel-before-create" {
						cancel()
					}
					return !mode.automatic, nil
				}
				var out bytes.Buffer
				code := runUIWithSelectionOutput(ctx, args, strings.NewReader(mode.input), &out, &out, wait, create)
				want := 1
				if fault == "success" {
					want = mode.code
				}
				if code != want {
					t.Fatalf("code=%d want=%d: %s", code, want, &out)
				}
				wantCreates := 1
				if fault == "cancel-before-create" {
					wantCreates = 0
				}
				if creates != wantCreates {
					t.Fatal("creation repeated or bypassed")
				}
				if owner != nil {
					wantWrites := 1
					if fault == "cancel-create" || fault == "owner-and-error" {
						wantWrites = 0
					}
					if owner.closes != 1 || owner.writes != wantWrites {
						t.Fatalf("owner calls: write=%d close=%d", owner.writes, owner.closes)
					}
					actual, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(actual, owner.stored) {
						t.Fatal("created evidence removed or replaced")
					}
					if _, err := owner.file.Stat(); err == nil {
						t.Fatal("owned descriptor remains open")
					}
					if fault == "success" {
						expected := `{"excluded_tests":[]}` + "\n"
						if mode.launch {
							profile := "quick"
							if mode.code == 11 {
								profile = "standard"
							}
							expected = uiLaunchExpected(profile, mode.legacy, "[]")
						}
						if string(actual) != expected {
							t.Fatalf("payload bytes changed: %q", actual)
						}
					}
				} else if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("unacquired output appeared")
				}
				if fault != "success" {
					assertUINoLaunch(t, out.String())
				}
			})
		}
	}
}

func TestUILaunchSelectionRealOutputFailuresPreservePaths(t *testing.T) {
	for _, kind := range []string{"existing", "directory", "missing-parent"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "launch.json")
			switch kind {
			case "existing":
				if err := os.WriteFile(path, []byte("retained\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "missing-parent":
				path = filepath.Join(path, "launch.json")
			}
			var out bytes.Buffer
			if code := runUIWith(t.Context(), []string{"-launch-selection-output", path}, strings.NewReader("s\n"), &out, &out, uiOperatorWait); code != 1 {
				t.Fatalf("unsafe output code=%d", code)
			}
			assertUINoLaunch(t, out.String())
			if kind == "existing" {
				assertUISelectionFile(t, path, "retained\n")
			}
			if kind == "directory" {
				info, err := os.Stat(path)
				if err != nil || !info.IsDir() {
					t.Fatal("directory replaced")
				}
			}
		})
	}
}

func TestUILaunchSelectionExistingLinkIsPreserved(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	link := filepath.Join(dir, "launch.json")
	if err := os.WriteFile(target, []byte("retained target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("owned symlink creation unavailable on Windows")
		}
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := runUIWith(t.Context(), []string{"-launch-selection-output", link}, strings.NewReader("s\n"), &out, &out, uiOperatorWait); code != 1 {
		t.Fatalf("existing link code=%d", code)
	}
	assertUINoLaunch(t, out.String())
	assertUISelectionFile(t, target, "retained target\n")
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("owned link replaced")
	}
}

func TestUISelectionNoOutputNeverAcquiresOwner(t *testing.T) {
	for _, emptyFlag := range []bool{false, true} {
		for index, input := range []string{"", "1\n", "k\n"} {
			t.Run(fmt.Sprintf("empty-flag-%t/input-%d", emptyFlag, index), func(t *testing.T) {
				args := []string{"-deployment-key-media"}
				if emptyFlag {
					args = append(args, "-selection-output=")
				}
				creates := 0
				create := func(string, int, os.FileMode) (uiSelectionFile, error) {
					creates++
					return nil, errors.New("output tripwire")
				}
				wait := func(context.Context, time.Duration, io.Reader) (bool, error) { return input != "", nil }
				want := 0
				if input == "k\n" {
					want = 10
				}
				var out bytes.Buffer
				if code := runUIWithSelectionOutput(t.Context(), args, strings.NewReader(input), &out, &out, wait, create); code != want || creates != 0 {
					t.Fatalf("unrecorded quick behavior changed: code=%d creates=%d", code, creates)
				}
				if strings.Contains(out.String(), "[S]") {
					t.Fatal("unrecorded standard offered")
				}
			})
		}
	}
}

func TestUILaunchSelectionReadFailureNeverAcquiresOwner(t *testing.T) {
	for _, prefix := range []string{"", "c\n2\n"} {
		t.Run(fmt.Sprintf("prefix-%d", len(prefix)), func(t *testing.T) {
			creates := 0
			create := func(string, int, os.FileMode) (uiSelectionFile, error) {
				creates++
				return nil, errors.New("output tripwire")
			}
			input := io.MultiReader(strings.NewReader(prefix), uiFailedReader{})
			var out bytes.Buffer
			code := runUIWithSelectionOutput(t.Context(), []string{"-launch-selection-output", filepath.Join(t.TempDir(), "launch.json")}, input, &out, &out, uiOperatorWait, create)
			if code != 1 || creates != 0 {
				t.Fatalf("failed reader reached output: code=%d creates=%d", code, creates)
			}
			assertUINoLaunch(t, out.String())
		})
	}
}
