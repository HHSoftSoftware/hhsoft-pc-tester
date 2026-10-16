//go:build linux

package audio

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestRunNoDevice(t *testing.T) {
	for _, variant := range []string{"absent", "empty", "unrelated"} {
		t.Run(variant, func(t *testing.T) {
			root := t.TempDir()
			if variant != "absent" {
				mustDirectory(t, filepath.Join(root, "sys/class/sound"))
			}
			if variant == "unrelated" {
				mustDirectory(t, filepath.Join(root, "sys/class/sound/seq"))
				mustDirectory(t, filepath.Join(root, "sys/class/sound/timer"))
			}
			result := Run(t.Context(), Options{Root: root})
			assertResult(t, result, StatusInconclusive, ReadinessNoDevice)
			if len(result.Cards) != 0 || len(result.Endpoints) != 0 {
				t.Fatalf("unexpected devices: %#v", result)
			}
		})
	}
}

func TestRunMissingNodes(t *testing.T) {
	root := soundFixture(t)
	result := Run(t.Context(), Options{Root: root})
	assertResult(t, result, StatusInconclusive, ReadinessMissingNodes)
	if len(result.Cards) != 1 || len(result.Endpoints) != 2 || result.PlaybackEndpoints != 1 || result.CaptureEndpoints != 1 || result.ReadyEndpoints != 0 {
		t.Fatalf("unexpected discovery: %#v", result)
	}
	if result.Cards[0].Card != 0 || result.Cards[0].Control.Path != "/dev/snd/controlC0" || result.Cards[0].Control.Present {
		t.Fatalf("unexpected control: %#v", result.Cards[0])
	}
	for _, endpoint := range result.Endpoints {
		if endpoint.Card != 0 || endpoint.Device != 0 || endpoint.Node.Present || endpoint.Node.Ready || endpoint.Node.ReadinessIssue == "" {
			t.Fatalf("unexpected endpoint: %#v", endpoint)
		}
	}
}

func TestRunMissingControlRegistry(t *testing.T) {
	root := t.TempDir()
	mustDirectory(t, filepath.Join(root, "sys/class/sound/card0"))
	mustAttribute(t, root, "pcmC0D0p", "116:16\n")
	result := Run(t.Context(), Options{Root: root})
	assertResult(t, result, StatusInconclusive, ReadinessMissingNodes)
	if result.Cards[0].Control.ReadinessIssue != "ALSA control is absent from sysfs" {
		t.Fatalf("missing control not recorded: %#v", result)
	}
}

func TestRunReadyWithoutOpeningStreams(t *testing.T) {
	root := soundFixture(t)
	mustNode(t, root, "controlC0", 0x7400)
	mustNode(t, root, "pcmC0D0p", 0x7410)
	mustNode(t, root, "pcmC0D0c", 0x7418)
	result := Run(t.Context(), Options{Root: root})
	assertResult(t, result, StatusPass, ReadinessReady)
	if result.ReadyEndpoints != 2 || !result.Cards[0].Control.Ready || result.Endpoints[0].Interface != "pcmC0D0c" || result.Endpoints[1].Interface != "pcmC0D0p" {
		t.Fatalf("unexpected ready result: %#v", result)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"media_streams_opened":false`) || strings.Contains(string(data), root) {
		t.Fatalf("unexpected serialized evidence: %s", data)
	}
}

func TestRunNoPCM(t *testing.T) {
	root := t.TempDir()
	mustDirectory(t, filepath.Join(root, "sys/class/sound/card0"))
	mustAttribute(t, root, "controlC0", "116:0\n")
	mustNode(t, root, "controlC0", 0x7400)
	assertResult(t, Run(t.Context(), Options{Root: root}), StatusInconclusive, ReadinessNoPCM)
}

func TestRunRequiresEveryNode(t *testing.T) {
	root := soundFixture(t)
	mustNode(t, root, "controlC0", 0x7400)
	mustNode(t, root, "pcmC0D0p", 0x7410)
	result := Run(t.Context(), Options{Root: root})
	assertResult(t, result, StatusInconclusive, ReadinessMissingNodes)
	if result.ReadyEndpoints != 1 || result.Endpoints[0].Node.Present || !result.Endpoints[1].Node.Ready {
		t.Fatalf("partial readiness was lost: %#v", result)
	}
}

func TestRunSupportsContainedSysfsLinks(t *testing.T) {
	root := t.TempDir()
	mustDirectory(t, filepath.Join(root, "sys/class/sound"))
	cardPath := filepath.Join(root, "sys/devices/pci0000:00/sound/card2")
	mustDirectory(t, cardPath)
	for _, name := range []string{"card2", "controlC2", "pcmC2D3p"} {
		target := "../../devices/pci0000:00/sound/card2"
		if name != "card2" {
			target += "/" + name
			minor := "64"
			if name == "pcmC2D3p" {
				minor = "83"
			}
			mustWrite(t, filepath.Join(cardPath, name, "dev"), "116:"+minor+"\n")
		}
		mustSymlink(t, target, filepath.Join(root, "sys/class/sound", name))
	}
	result := Run(t.Context(), Options{Root: root})
	assertResult(t, result, StatusInconclusive, ReadinessMissingNodes)
	if result.Cards[0].Card != 2 || result.Endpoints[0].Card != 2 || result.Endpoints[0].Device != 3 || result.Endpoints[0].Direction != "playback" {
		t.Fatalf("sysfs links not resolved: %#v", result)
	}
}

func TestRunRejectsMalformedNames(t *testing.T) {
	for _, name := range []string{"card00", "card256", "card-1", "card0\n", "controlC00", "controlC256", "pcmC0D0x", "pcmC0D00p", "pcmC256D0p", "pcmC0D256p", "pcmC999999999999999999D0p"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			mustDirectory(t, filepath.Join(root, "sys/class/sound", name))
			assertResult(t, Run(t.Context(), Options{Root: root}), StatusError, ReadinessUnknown)
		})
	}
}

func TestRunRejectsOrphanEndpoints(t *testing.T) {
	for _, name := range []string{"controlC0", "pcmC0D0p"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			mustAttribute(t, root, name, "116:0\n")
			assertResult(t, Run(t.Context(), Options{Root: root}), StatusError, ReadinessUnknown)
		})
	}
}

func TestRunRejectsMalformedDeviceNumbers(t *testing.T) {
	for _, value := range []string{"", "116", "1:3\n", "116:-1\n", "116:+1\n", "116:01\n", "116:1048576\n", "116:0\n\n", "116:0\r\n", " 116:0\n", "116:0\x00", "116:0 trailing", strings.Repeat("1", maximumAttributeBytes+1)} {
		t.Run(strconv.Quote(value), func(t *testing.T) {
			root := soundFixture(t)
			mustAttribute(t, root, "controlC0", value)
			assertResult(t, Run(t.Context(), Options{Root: root}), StatusError, ReadinessUnknown)
		})
	}
}

func TestRunRejectsDuplicateDeviceNumbers(t *testing.T) {
	root := soundFixture(t)
	mustAttribute(t, root, "pcmC0D0p", "116:0\n")
	result := Run(t.Context(), Options{Root: root})
	assertResult(t, result, StatusError, ReadinessUnknown)
	if result.Detail != "ALSA registry contains duplicate device numbers" {
		t.Fatalf("duplicate node was not detected: %#v", result)
	}
}

func TestRunRejectsUnsafeAttributes(t *testing.T) {
	for _, kind := range []string{"missing", "directory", "fifo", "symlink", "character"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			mustDirectory(t, filepath.Join(root, "sys/class/sound/card0"))
			attribute := filepath.Join(root, "sys/class/sound/controlC0/dev")
			mustDirectory(t, filepath.Dir(attribute))
			switch kind {
			case "directory":
				mustDirectory(t, attribute)
			case "fifo":
				if err := syscall.Mkfifo(attribute, 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				mustWrite(t, filepath.Join(root, "attribute"), "116:0\n")
				mustSymlink(t, "../../../../attribute", attribute)
			case "character":
				makeCharacterNode(t, attribute, 0x7400)
			}
			assertResult(t, Run(t.Context(), Options{Root: root}), StatusError, ReadinessUnknown)
		})
	}
}

func TestRunRejectsUnsafeDevicePaths(t *testing.T) {
	for _, kind := range []string{"regular", "directory", "fifo", "symlink", "wrong-device"} {
		t.Run(kind, func(t *testing.T) {
			root := soundFixture(t)
			path := filepath.Join(root, "dev/snd/controlC0")
			mustDirectory(t, filepath.Dir(path))
			switch kind {
			case "regular":
				mustWrite(t, path, "do not open")
			case "directory":
				mustDirectory(t, path)
			case "fifo":
				if err := syscall.Mkfifo(path, 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				mustSymlink(t, "missing", path)
			case "wrong-device":
				makeCharacterNode(t, path, 0x0103)
			}
			result := Run(t.Context(), Options{Root: root})
			assertResult(t, result, StatusError, ReadinessUnknown)
			if !result.Cards[0].Control.Present || result.Cards[0].Control.Ready {
				t.Fatalf("unsafe node was not reported: %#v", result)
			}
		})
	}
}

func TestRunRejectsRootEscapes(t *testing.T) {
	for _, target := range []string{"card", "attribute-parent", "devfs"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			mustDirectory(t, filepath.Join(root, "sys/class/sound"))
			switch target {
			case "card":
				mustSymlink(t, outside, filepath.Join(root, "sys/class/sound/card0"))
			case "attribute-parent":
				mustDirectory(t, filepath.Join(root, "sys/class/sound/card0"))
				mustWrite(t, filepath.Join(outside, "dev"), "116:0\n")
				mustSymlink(t, outside, filepath.Join(root, "sys/class/sound/controlC0"))
			case "devfs":
				mustDirectory(t, filepath.Join(root, "sys/class/sound/card0"))
				mustAttribute(t, root, "controlC0", "116:0\n")
				mustDirectory(t, filepath.Join(root, "dev"))
				mustSymlink(t, outside, filepath.Join(root, "dev/snd"))
			}
			assertResult(t, Run(t.Context(), Options{Root: root}), StatusError, ReadinessUnknown)
		})
	}
}

func TestRunRejectsInvalidRootAndRegistry(t *testing.T) {
	for _, root := range []string{"relative", "/invalid\x00root", strings.Repeat("/", 4097), filepath.Join(t.TempDir(), "missing")} {
		assertResult(t, Run(t.Context(), Options{Root: root}), StatusError, ReadinessUnknown)
	}
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "sys/class/sound"), "invalid registry")
	assertResult(t, Run(t.Context(), Options{Root: root}), StatusError, ReadinessUnknown)
}

func TestRunRegistryLimit(t *testing.T) {
	root := t.TempDir()
	for index := 0; index <= maximumRegistryEntries; index++ {
		mustDirectory(t, filepath.Join(root, "sys/class/sound", "unrelated"+strconv.Itoa(index)))
	}
	result := Run(t.Context(), Options{Root: root})
	assertResult(t, result, StatusError, ReadinessUnknown)
	if result.Detail != "ALSA sound registry exceeds the supported limit" {
		t.Fatalf("registry limit not enforced: %#v", result)
	}
}

func TestRunEndpointLimit(t *testing.T) {
	root := t.TempDir()
	for index := 0; index <= maximumPCMEndpoints; index++ {
		name := "pcmC" + strconv.Itoa(index/256) + "D" + strconv.Itoa(index%256) + "p"
		mustDirectory(t, filepath.Join(root, "sys/class/sound", name))
	}
	result := Run(t.Context(), Options{Root: root})
	assertResult(t, result, StatusError, ReadinessUnknown)
	if result.Detail != "audio PCM registry exceeds the supported limits" {
		t.Fatalf("endpoint limit not enforced: %#v", result)
	}
}

func TestRunCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assertResult(t, Run(ctx, Options{Root: "invalid"}), StatusAborted, ReadinessUnknown)
}

type deviceMetadata struct {
	os.FileInfo
	mode os.FileMode
	stat *syscall.Stat_t
}

func (metadata deviceMetadata) Mode() os.FileMode { return metadata.mode }

func (metadata deviceMetadata) Sys() any { return metadata.stat }

func TestMatchesDevice(t *testing.T) {
	for _, test := range []struct {
		name     string
		metadata deviceMetadata
		expected deviceNumber
		matches  bool
	}{
		{"control", deviceMetadata{mode: os.ModeDevice | os.ModeCharDevice, stat: &syscall.Stat_t{Rdev: 0x7400}}, deviceNumber{116, 0}, true},
		{"large-minor", deviceMetadata{mode: os.ModeDevice | os.ModeCharDevice, stat: &syscall.Stat_t{Rdev: 0xfff074ff}}, deviceNumber{116, maximumMinorNumber}, true},
		{"wrong-minor", deviceMetadata{mode: os.ModeDevice | os.ModeCharDevice, stat: &syscall.Stat_t{Rdev: 0x7410}}, deviceNumber{116, 0}, false},
		{"wrong-major", deviceMetadata{mode: os.ModeDevice | os.ModeCharDevice, stat: &syscall.Stat_t{Rdev: 0x0103}}, deviceNumber{116, 0}, false},
		{"block", deviceMetadata{mode: os.ModeDevice, stat: &syscall.Stat_t{Rdev: 0x7400}}, deviceNumber{116, 0}, false},
		{"regular", deviceMetadata{stat: &syscall.Stat_t{Rdev: 0x7400}}, deviceNumber{116, 0}, false},
		{"symlink", deviceMetadata{mode: os.ModeSymlink | os.ModeDevice | os.ModeCharDevice, stat: &syscall.Stat_t{Rdev: 0x7400}}, deviceNumber{116, 0}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if actual := matchesDevice(test.metadata, test.expected); actual != test.matches {
				t.Fatalf("matchesDevice = %v, want %v", actual, test.matches)
			}
		})
	}
}

func soundFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustDirectory(t, filepath.Join(root, "sys/class/sound/card0"))
	mustAttribute(t, root, "controlC0", "116:0\n")
	mustAttribute(t, root, "pcmC0D0p", "116:16\n")
	mustAttribute(t, root, "pcmC0D0c", "116:24\n")
	return root
}

func assertResult(t *testing.T, result Result, status Status, readiness Readiness) {
	t.Helper()
	if result.Schema != Schema || result.Status != status || result.Readiness != readiness || result.MediaStreamsOpened {
		t.Fatalf("unexpected result: %#v; want %s/%s with no streams opened", result, status, readiness)
	}
}

func mustAttribute(t *testing.T, root, name, value string) {
	t.Helper()
	mustWrite(t, filepath.Join(root, "sys/class/sound", name, "dev"), value)
}

func mustDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, value string) {
	t.Helper()
	mustDirectory(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

func mustNode(t *testing.T, root, name string, device int) {
	t.Helper()
	path := filepath.Join(root, "dev/snd", name)
	mustDirectory(t, filepath.Dir(path))
	makeCharacterNode(t, path, device)
}

func makeCharacterNode(t *testing.T, path string, device int) {
	t.Helper()
	if err := syscall.Mknod(path, syscall.S_IFCHR, device); err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			t.Skip("character-node fixture requires CAP_MKNOD in the isolated test environment")
		}
		t.Fatal(err)
	}
}
