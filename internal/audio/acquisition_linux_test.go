//go:build linux

package audio

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

// These wrappers exercise the real production acquisition and real owned
// fixture files. Only the declared operation result/fault is substituted.
type audioRunHarness struct {
	t         *testing.T
	root      string
	events    []string
	acquired  map[string]int
	closes    map[string]int
	faults    map[string]error
	nilValues map[string]bool
	metadata  map[string]os.FileInfo
	cancelAt  string
	cancel    context.CancelFunc
}

func newAudioRunHarness(t *testing.T) *audioRunHarness {
	return &audioRunHarness{t: t, root: soundFixture(t), acquired: map[string]int{}, closes: map[string]int{}, faults: map[string]error{},
		nilValues: map[string]bool{}, metadata: map[string]os.FileInfo{}}
}

func (h *audioRunHarness) event(name string) {
	h.events = append(h.events, name)
	if name == h.cancelAt {
		h.cancel()
	}
}

func (h *audioRunHarness) close(name string, owned io.Closer) error {
	h.closes[name]++
	err := owned.Close()
	h.event(name)
	return errors.Join(err, h.faults[name])
}

func (h *audioRunHarness) operations() audioOperations {
	real := systemAudioOperations()
	return audioOperations{
		openRoot: func(path string) (audioRoot, error) {
			if path != h.root {
				h.t.Fatal("wrong root")
			}
			h.event("root-open")
			if h.nilValues["root-open"] {
				return (*audioHarnessRoot)(nil), h.faults["root-open"]
			}
			root, err := real.openRoot(path)
			if err != nil {
				h.t.Fatal(err)
			}
			if nilAudioValue(root) {
				h.t.Fatal("real root acquisition returned nil")
			}
			h.acquired["root-close"]++
			return &audioHarnessRoot{audioRoot: root, h: h}, h.faults["root-open"]
		},
		openReadable: func(handle audioPathFile) (audioReadableFile, error) {
			path := handle.(*audioHarnessPath).name
			event := "read-open:" + path
			h.event(event)
			if h.nilValues[event] {
				return (*audioHarnessReadable)(nil), h.faults[event]
			}
			file, err := real.openReadable(handle)
			if err != nil {
				h.t.Fatal(err)
			}
			if nilAudioValue(file) {
				h.t.Fatal("real readable acquisition returned nil")
			}
			h.acquired["read-close:"+path]++
			return &audioHarnessReadable{audioReadableFile: file, h: h, name: path}, h.faults[event]
		},
	}
}

type audioHarnessRoot struct {
	audioRoot
	h *audioRunHarness
}

func (root *audioHarnessRoot) Close() error { return root.h.close("root-close", root.audioRoot) }
func (root *audioHarnessRoot) OpenRegistry(path string, flags int) (audioDirectory, error) {
	if path != "sys/class/sound" || flags != os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NONBLOCK|syscall.O_CLOEXEC {
		root.h.t.Fatal("registry open policy changed")
	}
	root.h.event("registry-open")
	if root.h.nilValues["registry-open"] {
		return (*audioHarnessDirectory)(nil), root.h.faults["registry-open"]
	}
	directory, err := root.audioRoot.OpenRegistry(path, flags)
	if err != nil {
		return directory, err
	}
	if nilAudioValue(directory) {
		root.h.t.Fatal("real registry acquisition returned nil")
	}
	root.h.acquired["registry-close"]++
	return &audioHarnessDirectory{audioDirectory: directory, h: root.h}, root.h.faults["registry-open"]
}
func (root *audioHarnessRoot) OpenPath(path string, flags int) (audioPathFile, error) {
	if !strings.HasPrefix(path, "sys/class/sound/") || !strings.HasSuffix(path, "/dev") || flags != openPath|syscall.O_NOFOLLOW|syscall.O_CLOEXEC {
		root.h.t.Fatal("attribute open escaped metadata-only policy")
	}
	event := "path-open:" + path
	root.h.event(event)
	if root.h.nilValues[event] {
		return (*audioHarnessPath)(nil), root.h.faults[event]
	}
	file, err := root.audioRoot.OpenPath(path, flags)
	if err != nil {
		return file, err
	}
	if nilAudioValue(file) {
		root.h.t.Fatal("real O_PATH acquisition returned nil")
	}
	root.h.acquired["path-close:"+path]++
	return &audioHarnessPath{audioPathFile: file, h: root.h, name: path}, root.h.faults[event]
}
func (root *audioHarnessRoot) Stat(path string) (os.FileInfo, error) {
	info, err := root.audioRoot.Stat(path)
	event := "root-stat:" + path
	root.h.event(event)
	if value, ok := root.h.metadata[event]; ok {
		info = value
	}
	return info, errors.Join(err, root.h.faults[event])
}
func (root *audioHarnessRoot) Lstat(path string) (os.FileInfo, error) {
	event := "node-stat:" + path
	root.h.event(event)
	if value, ok := root.h.metadata[event]; ok {
		return value, root.h.faults[event]
	}
	if err := root.h.faults[event]; err != nil {
		return nil, err
	}
	minors := map[string]uint64{"dev/snd/controlC0": 0x7400, "dev/snd/pcmC0D0c": 0x7418, "dev/snd/pcmC0D0p": 0x7410, "dev/snd/pcmC0D1p": 0x7411}
	device, ok := minors[path]
	if !ok {
		root.h.t.Fatalf("unexpected node metadata query %q", path)
	}
	return deviceMetadata{mode: os.ModeDevice | os.ModeCharDevice, stat: &syscall.Stat_t{Rdev: device}}, nil
}

type audioHarnessDirectory struct {
	audioDirectory
	h *audioRunHarness
}

func (directory *audioHarnessDirectory) ReadDir(n int) ([]os.DirEntry, error) {
	if n < 1 || n > 32 {
		directory.h.t.Fatalf("unbounded registry request %d", n)
	}
	entries, err := directory.audioDirectory.ReadDir(n)
	directory.h.event("registry-read")
	if fault := directory.h.faults["registry-read"]; fault != nil {
		return entries, fault
	}
	return entries, err
}
func (directory *audioHarnessDirectory) Close() error {
	return directory.h.close("registry-close", directory.audioDirectory)
}

type audioHarnessPath struct {
	audioPathFile
	h    *audioRunHarness
	name string
}

func (file *audioHarnessPath) Stat() (os.FileInfo, error) {
	info, err := file.audioPathFile.Stat()
	event := "path-stat:" + file.name
	file.h.event(event)
	if value, ok := file.h.metadata[event]; ok {
		info = value
	}
	return info, errors.Join(err, file.h.faults[event])
}
func (file *audioHarnessPath) Close() error {
	return file.h.close("path-close:"+file.name, file.audioPathFile)
}

type audioHarnessReadable struct {
	audioReadableFile
	h    *audioRunHarness
	name string
}

func (file *audioHarnessReadable) Stat() (os.FileInfo, error) {
	info, err := file.audioReadableFile.Stat()
	event := "read-stat:" + file.name
	file.h.event(event)
	if value, ok := file.h.metadata[event]; ok {
		info = value
	}
	return info, errors.Join(err, file.h.faults[event])
}
func (file *audioHarnessReadable) Read(buffer []byte) (int, error) {
	if len(buffer) < 1 || len(buffer) > 32 {
		file.h.t.Fatalf("unbounded attribute request %d", len(buffer))
	}
	n, err := file.audioReadableFile.Read(buffer)
	event := "read:" + file.name
	file.h.event(event)
	if fault := file.h.faults[event]; fault != nil {
		return n, fault
	}
	return n, err
}
func (file *audioHarnessReadable) Close() error {
	return file.h.close("read-close:"+file.name, file.audioReadableFile)
}

func (h *audioRunHarness) run(ctx context.Context) Result {
	return runWithAudioOperations(ctx, Options{Root: h.root}, h.operations())
}
func (h *audioRunHarness) saw(event string) bool {
	for _, value := range h.events {
		if value == event {
			return true
		}
	}
	return false
}
func (h *audioRunHarness) assertClosed(t *testing.T) {
	t.Helper()
	if !reflect.DeepEqual(h.closes, h.acquired) {
		t.Fatalf("close map %#v does not match actual acquisitions %#v", h.closes, h.acquired)
	}
	positions := map[string]int{}
	for index, event := range h.events {
		if _, owned := h.acquired[event]; owned {
			if _, duplicate := positions[event]; duplicate {
				t.Fatalf("duplicate close event %s", event)
			}
			positions[event] = index
		}
	}
	for name, count := range h.acquired {
		if count != 1 {
			t.Fatalf("%s acquired %d times", name, count)
		}
		position, closed := positions[name]
		if !closed {
			t.Fatalf("owned handle %s has no close event", name)
		}
		if strings.HasPrefix(name, "read-close:") {
			pathClose := "path-close:" + strings.TrimPrefix(name, "read-close:")
			pathPosition, pathClosed := positions[pathClose]
			if h.acquired[pathClose] != 1 || !pathClosed || position >= pathPosition {
				t.Fatalf("readable descriptor did not close before O_PATH: %s", name)
			}
		}
	}
	if h.acquired["root-close"] == 1 && positions["root-close"] != len(h.events)-1 {
		t.Fatal("root did not outlive all metadata handles")
	}
}

func TestAudioRunOwnsAllMetadataHandles(t *testing.T) {
	h := newAudioRunHarness(t)
	result := h.run(t.Context())
	assertResult(t, result, StatusPass, ReadinessReady)
	if result.ReadyEndpoints != 2 || result.CaptureEndpoints != 1 || result.PlaybackEndpoints != 1 {
		t.Fatalf("normal result %#v", result)
	}
	want := map[string]int{"root-close": 1, "registry-close": 1}
	for _, name := range []string{"controlC0", "pcmC0D0c", "pcmC0D0p"} {
		want["path-close:sys/class/sound/"+name+"/dev"] = 1
		want["read-close:sys/class/sound/"+name+"/dev"] = 1
	}
	if !reflect.DeepEqual(h.closes, want) {
		t.Fatalf("ownership %#v want %#v", h.closes, want)
	}
	h.assertClosed(t)
	if h.events[len(h.events)-1] != "root-close" {
		t.Fatal("root did not outlive all file handles")
	}
	for _, missing := range []string{"root", "readable"} {
		t.Run("missing-operation-"+missing, func(t *testing.T) {
			opens := 0
			operations := h.operations()
			original := operations.openRoot
			operations.openRoot = func(path string) (audioRoot, error) { opens++; return original(path) }
			if missing == "root" {
				operations.openRoot = nil
			} else {
				operations.openReadable = nil
			}
			result := runWithAudioOperations(t.Context(), Options{Root: h.root}, operations)
			assertResult(t, result, StatusError, ReadinessUnknown)
			if opens != 0 || strings.Contains(result.Detail, h.root) {
				t.Fatal("invalid operations opened or disclosed root")
			}
		})
	}
}

func TestAudioRunCloseFailuresDominateCancellation(t *testing.T) {
	for _, phase := range []string{"root-close", "registry-close", "path-close:sys/class/sound/controlC0/dev", "read-close:sys/class/sound/controlC0/dev", "read-close:sys/class/sound/pcmC0D0p/dev"} {
		for _, withCancel := range []bool{false, true} {
			name := phase
			if withCancel {
				name += "-cancel"
			}
			t.Run(name, func(t *testing.T) {
				h := newAudioRunHarness(t)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				h.faults[phase] = errors.New("private injected close detail")
				if withCancel {
					h.cancelAt, h.cancel = phase, cancel
				}
				result := h.run(ctx)
				assertResult(t, result, StatusError, ReadinessUnknown)
				h.assertClosed(t)
				if result.Detail != "audio metadata closure could not be confirmed" {
					t.Fatalf("cleanup not reported safely: %#v", result)
				}
				if phase == "registry-close" && h.saw("root-stat:sys/class/sound/card0") {
					t.Fatal("used unclosed registry")
				}
				if strings.Contains(phase, "controlC0") && h.saw("path-open:sys/class/sound/pcmC0D0c/dev") {
					t.Fatal("opened later attribute after close failure")
				}
				if phase == "root-close" && (result.ReadyEndpoints != 2 || !result.Cards[0].Control.Ready) {
					t.Fatal("closed prefix diagnostics lost")
				}
			})
		}
	}
}

func TestAudioRunCombinedFaultsCloseEveryDescriptor(t *testing.T) {
	for _, test := range []struct{ name, operation, close string }{
		{"registry-fault-and-close", "registry-read", "registry-close"},
		{"path-stat-and-close", "path-stat:sys/class/sound/controlC0/dev", "path-close:sys/class/sound/controlC0/dev"},
		{"read-and-close", "read:sys/class/sound/controlC0/dev", "read-close:sys/class/sound/controlC0/dev"},
		{"both-descriptor-closes", "read-close:sys/class/sound/controlC0/dev", "path-close:sys/class/sound/controlC0/dev"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newAudioRunHarness(t)
			h.faults[test.operation] = errors.New("operation failure")
			h.faults[test.close] = errors.New("close failure")
			result := h.run(t.Context())
			assertResult(t, result, StatusError, ReadinessUnknown)
			h.assertClosed(t)
			if result.Detail != "audio metadata closure could not be confirmed" || h.saw("path-open:sys/class/sound/pcmC0D0c/dev") {
				t.Fatalf("cleanup precedence/later work: %#v", result)
			}
			if test.name == "both-descriptor-closes" && (h.closes[test.operation] != 1 || h.closes[test.close] != 1) {
				t.Fatal("first close failure skipped second close")
			}
		})
	}
}

func TestAudioRunFinalCancellationAndPriorFaults(t *testing.T) {
	for _, phase := range []string{"root-open", "registry-close", "path-open:sys/class/sound/controlC0/dev", "path-stat:sys/class/sound/controlC0/dev", "read-open:sys/class/sound/controlC0/dev", "read-stat:sys/class/sound/controlC0/dev", "read:sys/class/sound/controlC0/dev", "read-close:sys/class/sound/pcmC0D0p/dev", "path-close:sys/class/sound/pcmC0D0p/dev", "node-stat:dev/snd/pcmC0D0p", "root-close"} {
		t.Run(phase, func(t *testing.T) {
			h := newAudioRunHarness(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			h.cancelAt, h.cancel = phase, cancel
			assertResult(t, h.run(ctx), StatusAborted, ReadinessUnknown)
			h.assertClosed(t)
			if phase == "root-open" && h.saw("registry-open") {
				t.Fatal("registry opened after root cancellation")
			}
		})
	}
	for _, phase := range []string{"registry-read", "path-stat:sys/class/sound/controlC0/dev", "read:sys/class/sound/controlC0/dev"} {
		t.Run("fault-"+phase, func(t *testing.T) {
			h := newAudioRunHarness(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			h.cancelAt, h.cancel = phase, cancel
			h.faults[phase] = errors.New("prior fault")
			assertResult(t, h.run(ctx), StatusError, ReadinessUnknown)
			h.assertClosed(t)
		})
	}
	for _, phase := range []string{"registry-close", "root-close"} {
		t.Run("empty-"+phase, func(t *testing.T) {
			h := newAudioRunHarness(t)
			h.root = t.TempDir()
			mustDirectory(t, filepath.Join(h.root, "sys/class/sound"))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			h.cancelAt, h.cancel = phase, cancel
			result := h.run(ctx)
			assertResult(t, result, StatusAborted, ReadinessUnknown)
			h.assertClosed(t)
			if len(result.Cards) != 0 || len(result.Endpoints) != 0 {
				t.Fatal("empty result invented observations")
			}
		})
	}
	t.Run("prior-read-fault-survives-later-root-cancel", func(t *testing.T) {
		h := newAudioRunHarness(t)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		h.cancelAt, h.cancel = "root-close", cancel
		h.faults["read:sys/class/sound/controlC0/dev"] = errors.New("prior read failure")
		assertResult(t, h.run(ctx), StatusError, ReadinessUnknown)
		h.assertClosed(t)
	})
	for _, mode := range []string{"no-device", "prior-read-fault"} {
		t.Run("root-close-failure-"+mode, func(t *testing.T) {
			h := newAudioRunHarness(t)
			h.faults["root-close"] = errors.New("root close failure")
			if mode == "no-device" {
				h.root = t.TempDir()
			} else {
				h.faults["read:sys/class/sound/controlC0/dev"] = errors.New("read failure")
			}
			result := h.run(t.Context())
			assertResult(t, result, StatusError, ReadinessUnknown)
			h.assertClosed(t)
			if result.Detail != "audio metadata closure could not be confirmed" {
				t.Fatal("root cleanup not reported")
			}
		})
	}
}

func TestAudioRunClosesHandlesReturnedWithOpenErrors(t *testing.T) {
	for _, phase := range []string{"root-open", "registry-open", "path-open:sys/class/sound/controlC0/dev", "read-open:sys/class/sound/controlC0/dev"} {
		t.Run(phase, func(t *testing.T) {
			h := newAudioRunHarness(t)
			h.faults[phase] = errors.New("injected open error")
			result := h.run(t.Context())
			assertResult(t, result, StatusError, ReadinessUnknown)
			h.assertClosed(t)
			wantCloses := 1
			switch phase {
			case "registry-open":
				wantCloses = 2
			case "path-open:sys/class/sound/controlC0/dev":
				wantCloses = 3
			case "read-open:sys/class/sound/controlC0/dev":
				wantCloses = 4
			}
			if len(h.closes) != wantCloses {
				t.Fatalf("open error leaked handle: %#v", h.closes)
			}
			if h.saw("node-stat:dev/snd/controlC0") || h.saw("path-open:sys/class/sound/pcmC0D0c/dev") {
				t.Fatal("continued after open error")
			}
		})
	}
}

type acquisitionNilInfo struct{ os.FileInfo }

func TestAudioRunRejectsTypedNilAndIdentityChanges(t *testing.T) {
	for _, phase := range []string{"root-open", "registry-open", "path-open:sys/class/sound/controlC0/dev", "read-open:sys/class/sound/controlC0/dev",
		"root-stat:sys/class/sound/card0", "path-stat:sys/class/sound/controlC0/dev", "read-stat:sys/class/sound/controlC0/dev", "node-stat:dev/snd/controlC0"} {
		t.Run(phase, func(t *testing.T) {
			h := newAudioRunHarness(t)
			if strings.Contains(phase, "-open") {
				h.nilValues[phase] = true
			} else {
				var info *acquisitionNilInfo
				h.metadata[phase] = info
			}
			assertResult(t, h.run(t.Context()), StatusError, ReadinessUnknown)
			h.assertClosed(t)
			if h.saw("path-open:sys/class/sound/pcmC0D0c/dev") {
				t.Fatal("continued after nil observation")
			}
		})
	}
	h := newAudioRunHarness(t)
	other := filepath.Join(h.root, "different-attribute")
	mustWrite(t, other, "116:0\n")
	info, err := os.Stat(other)
	if err != nil {
		t.Fatal(err)
	}
	h.metadata["read-stat:sys/class/sound/controlC0/dev"] = info
	result := h.run(t.Context())
	assertResult(t, result, StatusError, ReadinessUnknown)
	h.assertClosed(t)
	if h.saw("read:sys/class/sound/controlC0/dev") || h.saw("node-stat:dev/snd/controlC0") {
		t.Fatal("read or credited substituted attribute identity")
	}
	if h.closes["read-close:sys/class/sound/controlC0/dev"] != 1 || h.closes["path-close:sys/class/sound/controlC0/dev"] != 1 {
		t.Fatal("identity failure leaked descriptor")
	}
	for _, phase := range []string{"path-stat:sys/class/sound/controlC0/dev", "read-stat:sys/class/sound/controlC0/dev"} {
		t.Run("nonregular-"+phase, func(t *testing.T) {
			h := newAudioRunHarness(t)
			h.metadata[phase] = deviceMetadata{mode: os.ModeNamedPipe}
			assertResult(t, h.run(t.Context()), StatusError, ReadinessUnknown)
			h.assertClosed(t)
			if h.saw("read:sys/class/sound/controlC0/dev") {
				t.Fatal("nonregular metadata reached read")
			}
			if strings.HasPrefix(phase, "path-stat") && h.saw("read-open:sys/class/sound/controlC0/dev") {
				t.Fatal("nonregular O_PATH proof reached readable open")
			}
		})
	}
	t.Run("full64-byte-primitive-is-not-device-grammar", func(t *testing.T) {
		h := newAudioRunHarness(t)
		mustAttribute(t, h.root, "controlC0", strings.Repeat("1", 64))
		result := h.run(t.Context())
		assertResult(t, result, StatusError, ReadinessUnknown)
		h.assertClosed(t)
		if result.Detail != "ALSA registry contains a malformed device number" || h.saw("node-stat:dev/snd/controlC0") {
			t.Fatal("complete byte read authorized malformed node identity")
		}
	})
	for _, test := range []struct {
		name     string
		metadata os.FileInfo
		err      error
		want     Status
	}{
		{"sole-statat", nil, &os.PathError{Op: "statat", Err: os.ErrNotExist}, StatusInconclusive},
		{"wrong-lstat", nil, &os.PathError{Op: "lstat", Err: os.ErrNotExist}, StatusError},
		{"joined-absence", nil, errors.Join(os.ErrNotExist, errors.New("other error")), StatusError},
		{"metadata-and-absence", deviceMetadata{mode: os.ModeDevice | os.ModeCharDevice, stat: &syscall.Stat_t{Rdev: 0x7400}}, &os.PathError{Op: "statat", Err: os.ErrNotExist}, StatusError},
	} {
		t.Run("node-"+test.name, func(t *testing.T) {
			h := newAudioRunHarness(t)
			h.metadata["node-stat:dev/snd/controlC0"] = test.metadata
			h.faults["node-stat:dev/snd/controlC0"] = test.err
			readiness := ReadinessUnknown
			if test.want == StatusInconclusive {
				readiness = ReadinessMissingNodes
			}
			assertResult(t, h.run(t.Context()), test.want, readiness)
			h.assertClosed(t)
			if test.want == StatusError && h.saw("path-open:sys/class/sound/pcmC0D0c/dev") {
				t.Fatal("later open after contradictory missing-node evidence")
			}
		})
	}
}

func TestAudioRunRetainsAcceptedPrefixWithoutLaterOpens(t *testing.T) {
	for _, mode := range []string{"read-error", "close-error", "cancel", "read-error-cancel", "close-error-cancel"} {
		t.Run(mode, func(t *testing.T) {
			h := newAudioRunHarness(t)
			mustAttribute(t, h.root, "pcmC0D1p", "116:17\n")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			phase := "read:sys/class/sound/pcmC0D0p/dev"
			want := StatusError
			if mode == "close-error" || mode == "close-error-cancel" {
				phase = "read-close:sys/class/sound/pcmC0D0p/dev"
			}
			if mode == "cancel" {
				want = StatusAborted
				h.cancelAt, h.cancel = phase, cancel
			} else {
				h.faults[phase] = errors.New("current acquisition fault")
			}
			if mode == "read-error-cancel" || mode == "close-error-cancel" {
				h.cancelAt, h.cancel = phase, cancel
			}
			result := h.run(ctx)
			assertResult(t, result, want, ReadinessUnknown)
			h.assertClosed(t)
			wantCards := []CardResult{{Interface: "card0", Card: 0, Control: NodeResult{Path: "/dev/snd/controlC0", Present: true, Ready: true}}}
			wantAccepted := PCMResult{Interface: "pcmC0D0c", Card: 0, Device: 0, Direction: "capture", Node: NodeResult{Path: "/dev/snd/pcmC0D0c", Present: true, Ready: true}}
			wantUnopened := PCMResult{Interface: "pcmC0D1p", Card: 0, Device: 1, Direction: "playback"}
			if !reflect.DeepEqual(result.Cards, wantCards) || len(result.Endpoints) != 3 || result.Endpoints[0] != wantAccepted || result.Endpoints[2] != wantUnopened ||
				result.CaptureEndpoints != 1 || result.PlaybackEndpoints != 0 || result.ReadyEndpoints != 1 || result.Endpoints[1].Node.Ready || result.MediaStreamsOpened {
				t.Fatalf("accepted prefix/current failure confused: %#v", result)
			}
			if mode == "close-error-cancel" && result.Detail != "audio metadata closure could not be confirmed" {
				t.Fatalf("close failure lost to cancellation: %#v", result)
			}
			for _, event := range []string{"path-open:sys/class/sound/pcmC0D1p/dev", "read-open:sys/class/sound/pcmC0D1p/dev", "node-stat:dev/snd/pcmC0D1p"} {
				if h.saw(event) {
					t.Fatalf("later endpoint acquired after terminal outcome: %s", event)
				}
			}
		})
	}
}

func TestAudioRealOperationsKeepContainedLinksAndNoFollow(t *testing.T) {
	for _, mode := range []string{"contained-parent", "final-link", "escaping-parent"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			mustDirectory(t, filepath.Join(root, "sys/class/sound/card0"))
			actual := filepath.Join(root, "sys/devices/audio/controlC0")
			mustWrite(t, filepath.Join(actual, "dev"), "116:0\n")
			link := filepath.Join(root, "sys/class/sound/controlC0")
			if mode == "escaping-parent" {
				outside := t.TempDir()
				mustWrite(t, filepath.Join(outside, "dev"), "116:0\n")
				mustSymlink(t, outside, link)
			} else {
				mustSymlink(t, "../../devices/audio/controlC0", link)
			}
			if mode == "final-link" {
				if err := os.Remove(filepath.Join(actual, "dev")); err != nil {
					t.Fatal(err)
				}
				mustWrite(t, filepath.Join(actual, "number"), "116:0\n")
				mustSymlink(t, "number", filepath.Join(actual, "dev"))
			}
			result := Run(t.Context(), Options{Root: root})
			if mode == "contained-parent" {
				assertResult(t, result, StatusInconclusive, ReadinessMissingNodes)
			} else {
				assertResult(t, result, StatusError, ReadinessUnknown)
			}
		})
	}
}

func TestAudioRootAbsenceUsesActualProducerOperations(t *testing.T) {
	path := t.TempDir()
	operations := systemAudioOperations()
	root, err := operations.openRoot(path)
	if err != nil || nilAudioValue(root) {
		t.Fatalf("open fixture root: %v", err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Errorf("close fixture root: %v", err)
		}
	}()
	directory, err := root.OpenRegistry("sys/class/sound", os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NONBLOCK|syscall.O_CLOEXEC)
	if !nilAudioValue(directory) {
		if closeErr := directory.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		t.Fatal("absent registry unexpectedly returned owned directory")
	}
	failure, ok := err.(*os.PathError)
	if !ok || failure.Op != "openat" || !soleAudioAbsence(err, "openat") || soleAudioAbsence(err, "open") {
		t.Fatalf("unexpected real root open producer: %#v", err)
	}
	metadata, err := root.Lstat("dev/snd/controlC0")
	failure, ok = err.(*os.PathError)
	if !nilAudioValue(metadata) || !ok || failure.Op != "statat" || !soleAudioAbsence(err, "statat") || soleAudioAbsence(err, "lstat") {
		t.Fatalf("unexpected real root stat producer: %#v", err)
	}
	assertResult(t, Run(t.Context(), Options{Root: path}), StatusInconclusive, ReadinessNoDevice)
	assertResult(t, Run(t.Context(), Options{Root: soundFixture(t)}), StatusInconclusive, ReadinessMissingNodes)
}
