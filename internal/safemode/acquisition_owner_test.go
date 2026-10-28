package safemode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type safetyOwnerHarness struct {
	t                *testing.T
	root             string
	ctx              context.Context
	cancel           context.CancelFunc
	target, mode     string
	additional       map[string]string
	fired            bool
	category         int
	acquired, closes map[string]int
	order, events    []string
	cancelAt         int
	other            os.FileInfo
}

func newSafetyOwnerHarness(t *testing.T) *safetyOwnerHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	h := &safetyOwnerHarness{t: t, root: fixture(t), ctx: ctx, cancel: cancel, acquired: map[string]int{}, closes: map[string]int{}, cancelAt: -1}
	writeFixture(t, h.root, "other", "different identity")
	var err error
	h.other, err = os.Stat(filepath.Join(h.root, "other"))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *safetyOwnerHarness) event(name string) string {
	h.events = append(h.events, name)
	if mode, present := h.additional[name]; present {
		delete(h.additional, name)
		return mode
	}
	if h.fired || name != h.target {
		return ""
	}
	h.fired = true
	if strings.Contains(h.mode, "cancel") {
		h.cancel()
		h.cancelAt = len(h.events) - 1
	}
	return h.mode
}
func (h *safetyOwnerHarness) own(name string) string { h.acquired[name]++; return name }
func (h *safetyOwnerHarness) closed(name string, close func() error) error {
	h.closes[name]++
	h.order = append(h.order, name)
	err := close()
	mode := h.event("close:" + name)
	if strings.HasPrefix(mode, "error") {
		return errors.New("injected close")
	}
	return err
}
func (h *safetyOwnerHarness) operations() safetyOperations {
	return safetyOperations{openRoot: func(path string) (safetyRoot, error) {
		category := "storage"
		if h.category == 1 {
			category = "network"
		}
		if h.category > 1 {
			h.t.Fatal("unexpected extra category")
		}
		h.category++
		mode := h.event("open:" + category + "/root")
		if mode == "nil" {
			return nil, nil
		}
		if mode == "typed-nil" {
			var p *safetyFaultRoot
			return p, nil
		}
		if mode == "error" {
			return nil, errors.New("open root")
		}
		root, err := systemSafetyOperations().openRoot(path)
		if nilSafetyValue(root) {
			return nil, err
		}
		wrapped := &safetyFaultRoot{safetyRoot: root, h: h, category: category, id: h.own(category + "/root")}
		if mode == "handle-error" {
			return wrapped, errors.New("root with error")
		}
		return wrapped, err
	}}
}
func (h *safetyOwnerHarness) assertClosed() {
	h.t.Helper()
	if !reflect.DeepEqual(h.acquired, h.closes) {
		h.t.Fatalf("owned/closed mismatch: %v / %v", h.acquired, h.closes)
	}
	positions := map[string]int{}
	for index, id := range h.order {
		positions[id] = index
	}
	for id, count := range h.acquired {
		if count != 1 || h.closes[id] != 1 {
			h.t.Fatal("handle was not acquired/closed exactly once")
		}
		parts := strings.SplitN(id, "/", 2)
		root := parts[0] + "/root"
		if id != root && positions[id] >= positions[root] {
			h.t.Fatal("root closed before its owned leaf")
		}
		if strings.Contains(id, "/readable:") {
			path := strings.Replace(id, "/readable:", "/path:", 1)
			if positions[id] >= positions[path] {
				h.t.Fatal("path closed before readable descriptor")
			}
		}
	}
	if h.cancelAt >= 0 {
		for _, event := range h.events[h.cancelAt+1:] {
			if strings.HasPrefix(event, "open:") {
				h.t.Fatalf("acquisition after cancellation: %s", event)
			}
		}
	}
}

type safetyFaultRoot struct {
	safetyRoot
	h            *safetyOwnerHarness
	category, id string
	lstats       map[string]int
}

func (root *safetyFaultRoot) Lstat(path string) (os.FileInfo, error) {
	if root.lstats == nil {
		root.lstats = map[string]int{}
	}
	root.lstats[path]++
	info, err := root.safetyRoot.Lstat(path)
	mode := root.h.event(fmt.Sprintf("lstat%d:%s/%s", root.lstats[path], root.category, path))
	if mode == "error" || mode == "error-cancel" {
		return info, errors.New("stat")
	}
	if mode == "nil" {
		return nil, nil
	}
	if mode == "typed-nil" {
		var p *safetyNilInfo
		return p, nil
	}
	if mode == "identity" {
		return root.h.other, nil
	}
	return info, err
}
func (root *safetyFaultRoot) OpenPath(path string) (safetyPath, error) {
	id := root.category + "/path:" + path
	mode := root.h.event("open:" + id)
	if mode == "nil" {
		return nil, nil
	}
	if mode == "typed-nil" {
		var p *safetyFaultPath
		return p, nil
	}
	if mode == "error" {
		return nil, errors.New("path")
	}
	file, err := root.safetyRoot.OpenPath(path)
	if nilSafetyValue(file) {
		return nil, err
	}
	wrapped := &safetyFaultPath{safetyPath: file, h: root.h, id: root.h.own(id)}
	if mode == "handle-error" {
		return wrapped, errors.New("path with error")
	}
	return wrapped, err
}
func (root *safetyFaultRoot) OpenReadable(path string, handle safetyPath) (safetyFile, error) {
	id := root.category + "/readable:" + path
	mode := root.h.event("open:" + id)
	if mode == "nil" {
		return nil, nil
	}
	if mode == "typed-nil" {
		var p *safetyFaultFile
		return p, nil
	}
	if mode == "error" {
		return nil, errors.New("readable")
	}
	file, err := root.safetyRoot.OpenReadable(path, handle)
	if nilSafetyValue(file) {
		return nil, err
	}
	wrapped := &safetyFaultFile{safetyFile: file, h: root.h, id: root.h.own(id)}
	if mode == "handle-error" {
		return wrapped, errors.New("file with error")
	}
	return wrapped, err
}
func (root *safetyFaultRoot) Close() error { return root.h.closed(root.id, root.safetyRoot.Close) }

type safetyNilInfo struct{ os.FileInfo }
type safetyFaultPath struct {
	safetyPath
	h  *safetyOwnerHarness
	id string
}

func (file *safetyFaultPath) Stat() (os.FileInfo, error) {
	info, err := file.safetyPath.Stat()
	mode := file.h.event("stat:" + file.id)
	if mode == "error" || mode == "error-cancel" {
		return info, errors.New("path stat")
	}
	if mode == "identity" {
		return file.h.other, nil
	}
	if mode == "typed-nil" {
		var p *safetyNilInfo
		return p, nil
	}
	return info, err
}
func (file *safetyFaultPath) Close() error { return file.h.closed(file.id, file.safetyPath.Close) }

type safetyFaultFile struct {
	safetyFile
	h     *safetyOwnerHarness
	id    string
	stats int
}

func (file *safetyFaultFile) Stat() (os.FileInfo, error) {
	file.stats++
	info, err := file.safetyFile.Stat()
	mode := file.h.event(fmt.Sprintf("stat%d:%s", file.stats, file.id))
	if mode == "error" || mode == "error-cancel" {
		return info, errors.New("file stat")
	}
	if mode == "identity" {
		return file.h.other, nil
	}
	if mode == "typed-nil" {
		var p *safetyNilInfo
		return p, nil
	}
	return info, err
}
func (file *safetyFaultFile) Read(p []byte) (int, error) {
	mode := file.h.event("read:" + file.id)
	if mode == "error" || mode == "error-cancel" {
		return 0, errors.New("read")
	}
	if mode == "zero" {
		return 0, nil
	}
	if mode == "negative" {
		return -1, nil
	}
	if mode == "oversized" {
		return len(p) + 1, nil
	}
	return file.safetyFile.Read(p)
}
func (file *safetyFaultFile) ReadDir(n int) ([]os.DirEntry, error) {
	mode := file.h.event("readdir:" + file.id)
	if mode == "error" || mode == "error-cancel" {
		return nil, errors.New("directory")
	}
	if mode == "zero" {
		return nil, nil
	}
	if mode == "joined-eof" {
		return nil, errors.Join(io.EOF, errors.New("directory"))
	}
	return file.safetyFile.ReadDir(n)
}
func (file *safetyFaultFile) Close() error { return file.h.closed(file.id, file.safetyFile.Close) }

func TestSafetyAcquisitionActualOwnersCloseExactlyOnce(t *testing.T) {
	h := newSafetyOwnerHarness(t)
	result := checkWith(h.ctx, h.root, h.operations())
	h.assertClosed()
	want := map[string]int{"storage/root": 1, "storage/path:sys/class/block": 1, "storage/readable:sys/class/block": 1, "storage/path:proc/self/mountinfo": 1, "storage/readable:proc/self/mountinfo": 1, "network/root": 1, "network/path:sys/class/net": 1, "network/readable:sys/class/net": 1, "network/path:sys/class/net/lo/flags": 1, "network/readable:sys/class/net/lo/flags": 1}
	if !result.Safe() || !reflect.DeepEqual(h.acquired, want) {
		t.Fatalf("actual owned proof incomplete: %#v %v", result, h.acquired)
	}
}

func TestSafetyAcquisitionObservationRejectsNilContextAndOperations(t *testing.T) {
	var typed *safetyNilContext
	for _, ctx := range []context.Context{nil, typed} {
		result := checkWith(ctx, "unused", safetyOperations{openRoot: func(string) (safetyRoot, error) { t.Fatal("nil context opened a root"); return nil, nil }})
		if result.Status != "error" || result.Safe() || result.StorageObserved || result.NetworkObserved {
			t.Fatal("nil context accepted")
		}
	}
	if result := checkWith(t.Context(), "unused", safetyOperations{}); result.Status != "error" || result.Safe() {
		t.Fatal("nil opener accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result := checkWith(ctx, "unused", safetyOperations{openRoot: func(string) (safetyRoot, error) {
		t.Fatal("already cancelled observation opened a root")
		return nil, nil
	}})
	if result.Status != "aborted" || result.Safe() {
		t.Fatal("already cancelled result changed")
	}
}

func TestSafetyAcquisitionOwnerFaultsCannotCertifySafe(t *testing.T) {
	for _, row := range []struct {
		stage string
		modes []string
	}{
		{"open:storage/root", []string{"nil", "typed-nil", "error", "handle-error"}},
		{"lstat1:storage/proc/self/mountinfo", []string{"nil", "typed-nil", "error"}},
		{"open:storage/path:proc/self/mountinfo", []string{"nil", "typed-nil", "error", "handle-error"}},
		{"stat:storage/path:proc/self/mountinfo", []string{"typed-nil", "error", "identity"}},
		{"open:storage/readable:proc/self/mountinfo", []string{"nil", "typed-nil", "error", "handle-error"}},
		{"stat1:storage/readable:proc/self/mountinfo", []string{"typed-nil", "error", "identity"}},
		{"read:storage/readable:proc/self/mountinfo", []string{"error", "zero", "negative", "oversized"}},
		{"stat2:storage/readable:proc/self/mountinfo", []string{"typed-nil", "error", "identity"}},
		{"lstat2:storage/proc/self/mountinfo", []string{"nil", "typed-nil", "error", "identity"}},
		{"readdir:storage/readable:sys/class/block", []string{"zero", "error", "joined-eof"}},
		{"close:storage/readable:proc/self/mountinfo", []string{"error"}},
		{"close:storage/path:proc/self/mountinfo", []string{"error"}},
		{"close:storage/readable:sys/class/block", []string{"error"}},
		{"close:storage/path:sys/class/block", []string{"error"}},
		{"close:storage/root", []string{"error"}},
	} {
		for _, mode := range row.modes {
			t.Run(row.stage+"/"+mode, func(t *testing.T) {
				h := newSafetyOwnerHarness(t)
				h.target, h.mode = row.stage, mode
				result := checkWith(h.ctx, h.root, h.operations())
				h.assertClosed()
				if !h.fired || result.Safe() || result.Status != "error" || result.StorageObserved || !result.NetworkObserved || result.StorageDetail != "block mount safety could not be established" {
					t.Fatalf("fault lost or other category skipped: %#v", result)
				}
			})
		}
	}
}

func TestSafetyAcquisitionCancellationClosesEveryOwnedHandle(t *testing.T) {
	for _, stage := range []string{"open:storage/root", "lstat1:storage/proc/self/mountinfo", "open:storage/path:proc/self/mountinfo", "stat:storage/path:proc/self/mountinfo", "open:storage/readable:proc/self/mountinfo", "stat1:storage/readable:proc/self/mountinfo", "read:storage/readable:proc/self/mountinfo", "stat2:storage/readable:proc/self/mountinfo", "lstat2:storage/proc/self/mountinfo", "close:storage/readable:proc/self/mountinfo", "close:storage/path:proc/self/mountinfo", "close:storage/root", "readdir:storage/readable:sys/class/block", "close:network/root"} {
		t.Run(stage, func(t *testing.T) {
			h := newSafetyOwnerHarness(t)
			h.target, h.mode = stage, "cancel"
			result := checkWith(h.ctx, h.root, h.operations())
			h.assertClosed()
			if !h.fired || result.Status != "aborted" || result.Safe() {
				t.Fatalf("cancelled observation passed or lost: %#v", result)
			}
		})
	}
	for _, stage := range []string{"read:storage/readable:proc/self/mountinfo", "close:storage/readable:proc/self/mountinfo", "close:storage/path:proc/self/mountinfo", "close:storage/root"} {
		t.Run(stage+"/error-cancel", func(t *testing.T) {
			h := newSafetyOwnerHarness(t)
			h.target, h.mode = stage, "error-cancel"
			result := checkWith(h.ctx, h.root, h.operations())
			h.assertClosed()
			if !h.fired || result.Status != "error" || result.Safe() {
				t.Fatal("hard acquisition or cleanup error downgraded")
			}
		})
	}
}

func TestSafetyAcquisitionIndependentUnsafeCountsSurviveFaults(t *testing.T) {
	for _, category := range []string{"storage", "network"} {
		t.Run(category, func(t *testing.T) {
			h := newSafetyOwnerHarness(t)
			writeFixture(t, h.root, "sys/class/block/sda/dev", "8:0\n")
			writeFixture(t, h.root, "proc/self/mountinfo", "1 0 8:0 / /disk rw - ext4 source rw\n")
			writeFixture(t, h.root, "sys/class/net/eth0/flags", "0x1\n")
			h.mode = "error"
			if category == "storage" {
				h.target = "read:storage/readable:proc/self/mountinfo"
			} else {
				h.target = "read:network/readable:sys/class/net/eth0/flags"
			}
			result := checkWith(h.ctx, h.root, h.operations())
			h.assertClosed()
			if result.Status != "fail" || result.Safe() || !h.fired {
				t.Fatal("independent unsafe category erased")
			}
			if category == "storage" {
				if result.WritableBlockMounts != 0 || result.ActiveNonLoopInterfaces != 1 || result.StorageObserved || !result.NetworkObserved {
					t.Fatal("wrong independent network evidence")
				}
			} else if result.WritableBlockMounts != 1 || result.ActiveNonLoopInterfaces != 0 || !result.StorageObserved || result.NetworkObserved {
				t.Fatal("wrong independent storage evidence")
			}
		})
	}
}

func TestSafetyAcquisitionCompleteUnsafeDataSurvivesLateCleanup(t *testing.T) {
	for _, stage := range []string{"close:storage/readable:proc/self/mountinfo", "close:storage/path:proc/self/mountinfo", "close:storage/root", "close:network/readable:sys/class/net/eth0/flags", "close:network/root"} {
		for _, mode := range []string{"error", "cancel", "error-cancel"} {
			t.Run(stage+"/"+mode, func(t *testing.T) {
				h := newSafetyOwnerHarness(t)
				writeFixture(t, h.root, "sys/class/block/sda/dev", "8:0\n")
				writeFixture(t, h.root, "proc/self/mountinfo", "1 0 8:0 / /disk rw - ext4 source rw\n")
				writeFixture(t, h.root, "sys/class/net/eth0/flags", "0x1\n")
				h.target, h.mode = stage, mode
				result := checkWith(h.ctx, h.root, h.operations())
				h.assertClosed()
				if !h.fired || result.Status != "fail" || result.WritableBlockMounts != 1 || result.Safe() {
					t.Fatal("unsafe mount lost to final closure/cancellation")
				}
				if strings.Contains(stage, "network") && result.ActiveNonLoopInterfaces != 1 {
					t.Fatal("complete unsafe network flag erased")
				}
			})
		}
	}
}

func TestSafetyAcquisitionRetainsOnlyCompleteNetworkPrefix(t *testing.T) {
	for _, mode := range []string{"error", "error-cancel"} {
		for _, phase := range []string{"read", "close"} {
			t.Run(phase+"/"+mode, func(t *testing.T) {
				h := newSafetyOwnerHarness(t)
				for _, name := range []string{"eth0", "eth1", "eth2"} {
					writeFixture(t, h.root, "sys/class/net/"+name+"/flags", "0x1\n")
				}
				h.target, h.mode = phase+":network/readable:sys/class/net/eth1/flags", mode
				result := checkWith(h.ctx, h.root, h.operations())
				h.assertClosed()
				want := 1
				if phase == "close" {
					want = 2
				}
				if !h.fired || result.Status != "fail" || result.ActiveNonLoopInterfaces != want || result.NetworkObserved || result.NetworkDetail != "offline network state could not be established" {
					t.Fatalf("retained prefix wrong: %#v", result)
				}
				for _, event := range h.events {
					if strings.Contains(event, "eth2") {
						t.Fatal("later interface acquired after failure")
					}
				}
			})
		}
	}
}

func TestSafetyAcquisitionIdentityFailureCannotRetainUnprovedUnsafeBytes(t *testing.T) {
	for _, category := range []string{"storage", "network"} {
		for _, phase := range []string{"stat2", "lstat2"} {
			t.Run(category+"/"+phase, func(t *testing.T) {
				h := newSafetyOwnerHarness(t)
				path := "proc/self/mountinfo"
				if category == "storage" {
					writeFixture(t, h.root, "sys/class/block/sda/dev", "8:0\n")
					writeFixture(t, h.root, path, "1 0 8:0 / /disk rw - ext4 alias rw\n")
				} else {
					path = "sys/class/net/eth0/flags"
					writeFixture(t, h.root, path, "0x1\n")
				}
				h.target = phase + ":" + category + "/" + path
				if phase == "stat2" {
					h.target = phase + ":" + category + "/readable:" + path
				}
				h.mode = "identity"
				h.additional = map[string]string{"close:" + category + "/readable:" + path: "error"}
				result := checkWith(h.ctx, h.root, h.operations())
				h.assertClosed()
				if !h.fired || len(h.additional) != 0 || result.Status != "error" || result.WritableBlockMounts != 0 || result.ActiveNonLoopInterfaces != 0 || result.Safe() {
					t.Fatalf("failed identity leaked unsafe bytes or safe credit: %#v", result)
				}
			})
		}
	}
}

func TestSafetyAcquisitionUncertainWireAndMonitorRemainUnsignable(t *testing.T) {
	h := newSafetyOwnerHarness(t)
	h.target, h.mode = "close:storage/readable:proc/self/mountinfo", "error"
	result := checkWith(h.ctx, h.root, h.operations())
	h.assertClosed()
	encoded, err := json.Marshal(result)
	want := `{"schema":"hhsoft.pc-tester.runtime-safety/v1","status":"error","storage_observed":false,"network_observed":true,"writable_block_mounts":0,"active_non_loop_interfaces":0,"storage_detail":"block mount safety could not be established"}`
	if err != nil || string(encoded) != want {
		t.Fatalf("fixed incomplete producer wire changed: %s", encoded)
	}
	assessment := assessMonitorObservation(result)
	if assessment.FailureReason != "invalid_observation" || assessment.Value != result {
		t.Fatal("uncertain producer became safe monitor evidence")
	}
	// A fresh real owner runs inside the actual periodic observer. Its leaf
	// fault is not replaced by a precomputed successful inspector.
	monitored := newSafetyOwnerHarness(t)
	monitored.target, monitored.mode = "close:storage/readable:proc/self/mountinfo", "error"
	monitor, err := StartMonitor(func(ctx context.Context) Result { return checkWith(ctx, monitored.root, monitored.operations()) }, MonitorOptions{Started: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopTestMonitor(t, monitor) })
	awaitMonitorFailure(t, monitor)
	summary := stopTestMonitor(t, monitor)
	monitored.assertClosed()
	if summary.Status != "error" || summary.FirstFailure == nil || summary.FirstFailure.Reason != "invalid_observation" || summary.FirstFailure.Observation == nil || *summary.FirstFailure.Observation != result || !summary.CleanupConfirmed {
		t.Fatal("joined monitor accepted uncertain leaf cleanup")
	}
}
