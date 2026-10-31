package inventory

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Retained single-reader fixtures receive fresh budgets. Production requires
// an explicit per-collection budget and never calls these test adapters.
func readInventoryBytes(ctx context.Context, name string, limit int, open func(string) (inventoryScalar, error)) ([]byte, bool, error) {
	budget := newInventoryAcquisitionBudget()
	return readInventoryBytesWithBudget(ctx, name, limit, open, &budget)
}

func readInventoryScalar(ctx context.Context, name string, open func(string) (inventoryScalar, error)) ([]byte, bool, error) {
	budget := newInventoryAcquisitionBudget()
	return readInventoryScalarWithBudget(ctx, name, open, &budget)
}

func readInventoryProc(ctx context.Context, name string, role inventoryProcRole, open func(string) (inventoryScalar, error)) ([]byte, bool, error) {
	budget := newInventoryAcquisitionBudget()
	return readInventoryProcWithBudget(ctx, name, role, open, &budget)
}

func TestInventoryAcquisitionOwnedExactBoundary(t *testing.T) {
	for _, item := range []struct {
		name      string
		allowance int
		value     string
		short     int
		together  bool
		requests  []int
		want      error
	}{
		{"zero_empty", 0, "", 0, true, []int{1}, nil},
		{"zero_overflow", 0, "x", 0, true, []int{1}, ErrAcquisitionBudget},
		{"exact_together", 5, "abcde", 0, true, []int{6}, nil},
		{"exact_separate", 5, "abcde", 0, false, []int{6, 1}, nil},
		{"short_prefixes", 5, "abcde", 2, false, []int{6, 4, 2, 1}, nil},
		{"overflow_together", 5, "abcdef", 0, true, []int{6}, ErrAcquisitionBudget},
		{"overflow_probe", 5, "abcdef", 5, false, []int{6, 1}, ErrAcquisitionBudget},
	} {
		t.Run(item.name, func(t *testing.T) {
			budget := inventoryAcquisitionBudget{attempts: 1, bytes: item.allowance}
			file := scalarBytes([]byte(item.value), item.short, item.together)
			data, observed, err := readInventoryScalarWithBudget(t.Context(), "fixture", func(string) (inventoryScalar, error) { return file, nil }, &budget)
			if item.want != nil {
				requireScalarFailure(t, data, observed, err, item.want)
			} else if err != nil || !observed || string(data) != item.value || cap(data) > item.allowance {
				t.Fatal("exact completed boundary changed", observed, err, len(data), cap(data))
			}
			if budget.bytes != 0 || budget.attempts != 1 || file.closes != 1 || file.stats != 1 || !reflect.DeepEqual(file.requests, item.requests) {
				t.Fatal("unexpected accounting, ownership or overflow probe", budget, file.requests)
			}
		})
	}
}

func TestInventoryAcquisitionOwnedGrowthBound(t *testing.T) {
	value := bytes.Repeat([]byte("x"), 8193)
	file := scalarBytes(value, 0, true)
	budget := inventoryAcquisitionBudget{attempts: 1, bytes: 8193}
	data, observed, err := readInventoryScalarWithBudget(t.Context(), "fixture", func(string) (inventoryScalar, error) { return file, nil }, &budget)
	if err != nil || !observed || !bytes.Equal(data, value) || cap(data) != 8193 || budget.bytes != 0 || file.closes != 1 || !reflect.DeepEqual(file.requests, []int{4096, 4096, 2}) {
		t.Fatal("growth exceeded the remaining admission allowance or lost bytes", err, cap(data), file.requests)
	}
}

func TestInventoryAcquisitionDiscardedPrefixesStayCharged(t *testing.T) {
	for _, item := range []struct {
		name      string
		last      int
		readErr   error
		remaining int
	}{
		{"error_bytes", 2, fs.ErrPermission, 1},
		{"error_without_bytes", 0, fs.ErrPermission, 3},
		{"joined_eof_bytes", 2, errors.Join(io.EOF, fs.ErrPermission), 1},
	} {
		t.Run(item.name, func(t *testing.T) {
			budget := inventoryAcquisitionBudget{attempts: 2, bytes: 5}
			reads := 0
			file := &scalarHandle{info: &scalarInformation{}, read: func(p []byte) (int, error) {
				reads++
				if reads == 1 {
					copy(p, "ab")
					return 2, nil
				}
				copy(p, "cd")
				return item.last, item.readErr
			}}
			data, observed, err := readInventoryScalarWithBudget(t.Context(), "fixture", func(string) (inventoryScalar, error) { return file, nil }, &budget)
			if err != nil || observed || data != nil || budget.bytes != item.remaining || file.closes != 1 || !reflect.DeepEqual(file.requests, []int{6, 4}) {
				t.Fatal("discarded raw prefix was refunded or published", err, budget, file.requests)
			}
			next := scalarBytes(bytes.Repeat([]byte("x"), item.remaining+1), 0, true)
			data, observed, err = readInventoryProcWithBudget(t.Context(), "fixture", procMemory, func(string) (inventoryScalar, error) { return next, nil }, &budget)
			requireScalarFailure(t, data, observed, err, ErrAcquisitionBudget)
			if budget.bytes != 0 || next.closes != 1 || !reflect.DeepEqual(next.requests, []int{item.remaining + 1}) {
				t.Fatal("subsequent proc observation did not use the spent allowance")
			}
		})
	}
}

func TestInventoryAcquisitionOwnedFailurePrecedence(t *testing.T) {
	for _, item := range []struct {
		name                                                     string
		n                                                        int
		readErr                                                  error
		cancelRead, cancelClose, closeFail, nonregular, openFail bool
		want                                                     error
		remaining                                                int
	}{
		{name: "overflow_error", n: 2, readErr: fs.ErrPermission, want: ErrAcquisitionBudget},
		{name: "overflow_cancel", n: 2, readErr: io.EOF, cancelRead: true, want: ErrAcquisitionBudget},
		{name: "overflow_close", n: 2, readErr: io.EOF, closeFail: true, want: ErrScalarClose},
		{name: "overflow_close_cancel", n: 2, readErr: io.EOF, cancelClose: true, closeFail: true, want: ErrScalarClose},
		{name: "overflow_error_close_cancel", n: 2, readErr: fs.ErrPermission, cancelRead: true, closeFail: true, want: ErrScalarClose},
		{name: "within_read_cancel", n: 1, readErr: io.EOF, cancelRead: true, want: context.Canceled},
		{name: "within_close_cancel", n: 1, readErr: io.EOF, cancelClose: true, want: context.Canceled},
		{name: "negative_count", n: -1, readErr: io.EOF, want: ErrScalarRead, remaining: 1},
		{name: "oversized_count", n: 3, readErr: io.EOF, want: ErrScalarRead, remaining: 1},
		{name: "no_progress", n: 0, want: ErrScalarRead, remaining: 1},
		{name: "nonregular", nonregular: true, want: ErrScalarRead, remaining: 1},
		{name: "handle_with_open_error", openFail: true, want: ErrScalarRead, remaining: 1},
	} {
		t.Run(item.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			info := &scalarInformation{}
			if item.nonregular {
				info.mode = fs.ModeDir
			}
			file := &scalarHandle{info: info, read: func(p []byte) (int, error) {
				copy(p, "xy")
				if item.cancelRead {
					cancel()
				}
				return item.n, item.readErr
			}}
			if item.closeFail {
				file.closeErr = errors.New("provider detail")
			}
			if item.cancelClose {
				file.onClose = cancel
			}
			budget := inventoryAcquisitionBudget{attempts: 1, bytes: 1}
			data, observed, err := readInventoryScalarWithBudget(ctx, "fixture", func(string) (inventoryScalar, error) {
				if item.openFail {
					return file, fs.ErrPermission
				}
				return file, nil
			}, &budget)
			requireScalarFailure(t, data, observed, err, item.want)
			if budget.bytes != item.remaining || file.closes != 1 {
				t.Fatal("charge or close precedence changed", budget, file.closes)
			}
			if item.nonregular || item.openFail {
				if len(file.requests) != 0 {
					t.Fatal("unadmitted handle was read")
				}
			} else if !reflect.DeepEqual(file.requests, []int{2}) {
				t.Fatal("unexpected bounded read", file.requests)
			}
		})
	}
}

func TestInventoryAcquisitionPerFileOverflowPriority(t *testing.T) {
	for _, item := range []struct {
		name  string
		role  inventoryProcRole
		limit int
		want  error
	}{
		{"scalar", 0, 65536, ErrScalarLimit},
		{"memory", procMemory, 262144, ErrBulkLimit},
		{"cpu", procCPU, 16777216, ErrBulkLimit},
		{"input", procInput, 8388608, ErrBulkLimit},
	} {
		t.Run(item.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			left := item.limit + 1
			file := &scalarHandle{info: &scalarInformation{}, read: func(p []byte) (int, error) {
				n := min(len(p), left)
				left -= n
				if left == 0 {
					cancel()
					return n, fs.ErrPermission
				}
				return n, nil
			}}
			budget := inventoryAcquisitionBudget{attempts: 1, bytes: item.limit}
			open := func(string) (inventoryScalar, error) { return file, nil }
			var data []byte
			var observed bool
			var err error
			if item.role == 0 {
				data, observed, err = readInventoryScalarWithBudget(ctx, "fixture", open, &budget)
			} else {
				data, observed, err = readInventoryProcWithBudget(ctx, "fixture", item.role, open, &budget)
			}
			requireScalarFailure(t, data, observed, err, item.want)
			if left != 0 || budget.bytes != 0 || file.closes != 1 || file.requests[len(file.requests)-1] != 1 {
				t.Fatal("per-file boundary or final probe changed")
			}
			legacyCtx, stop := context.WithCancel(t.Context())
			defer stop()
			calls := 0
			collection := NewLinuxCollectorWithSource(collectionSource{file: func(string) ([]byte, error) { calls++; stop(); return make([]byte, item.limit+1), fs.ErrPermission }}).newCollection(legacyCtx)
			collection.acquisitionBudget = inventoryAcquisitionBudget{attempts: 1, bytes: item.limit}
			if item.role == 0 {
				data, observed = collection.readScalar("fixture")
			} else {
				data, observed = collection.readProc("fixture", item.role)
			}
			requireScalarFailure(t, data, observed, collection.err, item.want)
			if calls != 1 || collection.acquisitionBudget.bytes != 0 || collection.acquisitionBudget.attempts != 0 {
				t.Fatal("legacy raw overflow was not charged")
			}
		})
	}
}

func TestInventoryAcquisitionMixedRoutesShareBudget(t *testing.T) {
	for _, item := range []struct {
		name  string
		roles []inventoryProcRole
	}{
		{"scalar_first", []inventoryProcRole{0, procCPU, procMemory, procInput}},
		{"input_first", []inventoryProcRole{procInput, procMemory, procCPU, 0}},
	} {
		t.Run(item.name, func(t *testing.T) {
			calls := 0
			collection := NewLinuxCollectorWithSource(collectionSource{file: func(name string) ([]byte, error) {
				calls++
				if name != "same" {
					t.Fatal("fixture path changed")
				}
				return []byte("xy"), nil
			}}).newCollection(t.Context())
			collection.acquisitionBudget = inventoryAcquisitionBudget{attempts: 5, bytes: 8}
			for _, role := range item.roles {
				var data []byte
				var observed bool
				if role == 0 {
					data, observed = collection.readScalar("same")
				} else {
					data, observed = collection.readProc("same", role)
				}
				if !observed || string(data) != "xy" || collection.err != nil {
					t.Fatal("admitted mixed observation failed", collection.err)
				}
			}
			if calls != 4 || collection.acquisitionBudget.bytes != 0 || collection.acquisitionBudget.attempts != 1 {
				t.Fatal("roles did not share counters")
			}
			data, observed := collection.readScalar("same")
			requireScalarFailure(t, data, observed, collection.err, ErrAcquisitionBudget)
			collection.readProc("same", procMemory)
			if calls != 5 {
				t.Fatal("acquisition continued after aggregate failure")
			}
			snapshot, err := collection.snapshot([]Component{NewComponent(ComponentMemory, "prior", nil)}, NewSnapshot)
			requireNoCollection(t, snapshot, err, ErrAcquisitionBudget)
		})
	}
}

func TestInventoryAcquisitionUnavailableAttemptsCount(t *testing.T) {
	for _, item := range []struct {
		name           string
		observationErr error
		observed       bool
	}{
		{"missing", fs.ErrNotExist, false},
		{"empty", nil, true},
		{"permission", fs.ErrPermission, false},
		{"joined_error", errors.Join(fs.ErrNotExist, fs.ErrPermission), false},
	} {
		t.Run(item.name, func(t *testing.T) {
			calls := 0
			collection := NewLinuxCollectorWithSource(collectionSource{file: func(string) ([]byte, error) { calls++; return nil, item.observationErr }}).newCollection(t.Context())
			collection.acquisitionBudget = inventoryAcquisitionBudget{attempts: 3, bytes: 0}
			for index := 0; index < 3; index++ {
				var observed bool
				if index == 1 {
					_, observed = collection.readProc("repeated", procCPU)
				} else {
					_, observed = collection.readScalar("repeated")
				}
				if observed != item.observed || collection.err != nil {
					t.Fatal("optional observation changed", collection.err)
				}
			}
			data, observed := collection.readProc("repeated", procInput)
			requireScalarFailure(t, data, observed, collection.err, ErrAcquisitionBudget)
			if calls != 3 || collection.acquisitionBudget.attempts != 0 || collection.acquisitionBudget.bytes != 0 {
				t.Fatal("unavailable or repeated attempt escaped charge")
			}
		})
	}
}

func TestInventoryAcquisitionLegacyRawBytesCount(t *testing.T) {
	for _, item := range []struct {
		name           string
		raw            []byte
		observationErr error
	}{
		{"whitespace_crlf", []byte(" \t\r\n"), nil},
		{"multibyte", []byte{0xc3, 0xa9}, nil},
		{"invalid_bytes", []byte{0xff, 0, 0xfe}, nil},
		{"data_with_error", []byte("discard"), fs.ErrPermission},
	} {
		t.Run(item.name, func(t *testing.T) {
			original := append([]byte(nil), item.raw...)
			calls := 0
			collection := NewLinuxCollectorWithSource(collectionSource{file: func(string) ([]byte, error) {
				calls++
				if calls == 1 {
					return item.raw, item.observationErr
				}
				return []byte("x"), nil
			}}).newCollection(t.Context())
			collection.acquisitionBudget = inventoryAcquisitionBudget{attempts: 2, bytes: len(item.raw)}
			data, observed := collection.readScalar("fixture")
			if collection.err != nil || observed != (item.observationErr == nil) || !bytes.Equal(item.raw, original) || collection.acquisitionBudget.bytes != 0 {
				t.Fatal("raw length or declared error observation changed")
			}
			if item.observationErr != nil && data != nil {
				t.Fatal("error-returned bytes were retained")
			}
			if item.observationErr == nil && !bytes.Equal(data, original) {
				t.Fatal("raw bytes were normalized before admission")
			}
			data, observed = collection.readProc("next", procMemory)
			requireScalarFailure(t, data, observed, collection.err, ErrAcquisitionBudget)
			if calls != 2 {
				t.Fatal("wrong callback count")
			}
		})
	}
}

type acquisitionBudgetOverride struct {
	*RootPathSource
	calls int
}

func (source *acquisitionBudgetOverride) ReadFile(string) ([]byte, error) {
	source.calls++
	return []byte("xy"), nil
}

func TestInventoryAcquisitionRootAndDecoratorRouting(t *testing.T) {
	t.Run("owned_root", func(t *testing.T) {
		root := t.TempDir()
		for _, name := range []string{"scalar", "cpu", "memory", "input"} {
			if err := os.WriteFile(filepath.Join(root, name), []byte("ab"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		collector := NewLinuxCollectorWithSource(NewRootPathSource(root))
		collection := collector.newCollection(t.Context())
		collection.acquisitionBudget = inventoryAcquisitionBudget{attempts: 5, bytes: 8}
		for _, item := range []struct {
			name string
			role inventoryProcRole
		}{{"scalar", 0}, {"cpu", procCPU}, {"memory", procMemory}, {"input", procInput}} {
			var data []byte
			var observed bool
			if item.role == 0 {
				data, observed = collection.readScalar(item.name)
			} else {
				data, observed = collection.readProc(item.name, item.role)
			}
			if !observed || string(data) != "ab" || collection.err != nil {
				t.Fatal("owned route did not reach a valid file", collection.err)
			}
		}
		data, observed := collection.readScalar("scalar")
		requireScalarFailure(t, data, observed, collection.err, ErrAcquisitionBudget)
		if collection.acquisitionBudget.bytes != 0 || collection.acquisitionBudget.attempts != 0 {
			t.Fatal("owned routes did not share both counters")
		}
		limited := collector.newCollection(t.Context())
		limited.acquisitionBudget = inventoryAcquisitionBudget{attempts: 1, bytes: 4}
		limited.readScalar("scalar")
		data, observed = limited.readProc("cpu", procCPU)
		requireScalarFailure(t, data, observed, limited.err, ErrAcquisitionBudget)
		if limited.acquisitionBudget.bytes != 2 {
			t.Fatal("disallowed owned attempt consumed file bytes")
		}
	})
	t.Run("declared_override", func(t *testing.T) {
		source := &acquisitionBudgetOverride{RootPathSource: NewRootPathSource(t.TempDir())}
		collection := NewLinuxCollectorWithSource(source).newCollection(t.Context())
		collection.acquisitionBudget = inventoryAcquisitionBudget{attempts: 3, bytes: 4}
		first, ok := collection.readScalar("absent")
		second, ok2 := collection.readProc("absent", procCPU)
		if !ok || !ok2 || string(first) != "xy" || string(second) != "xy" || source.calls != 2 {
			t.Fatal("decorator override was bypassed")
		}
		data, observed := collection.readProc("absent", procInput)
		requireScalarFailure(t, data, observed, collection.err, ErrAcquisitionBudget)
		if source.calls != 3 || collection.acquisitionBudget.attempts != 0 || collection.acquisitionBudget.bytes != 0 {
			t.Fatal("override was not charged")
		}
	})
}

func TestInventoryAcquisitionInitialGuards(t *testing.T) {
	for _, item := range []struct {
		name                                      string
		nilContext, cancelled, nilSource, badRole bool
		want                                      error
	}{
		{name: "nil_context", nilContext: true, want: ErrInvalidContext},
		{name: "cancelled", cancelled: true, want: context.Canceled},
		{name: "nil_source", nilSource: true, want: ErrNoPathSource},
		{name: "invalid_role", badRole: true, want: ErrBulkRead},
	} {
		t.Run(item.name, func(t *testing.T) {
			calls := 0
			var source PathSource = collectionSource{file: func(string) ([]byte, error) { calls++; return nil, nil }}
			if item.nilSource {
				source = nil
			}
			ctx, stop := context.WithCancel(t.Context())
			defer stop()
			var current context.Context = ctx
			if item.nilContext {
				current = nil
			}
			if item.cancelled {
				stop()
			}
			collection := NewLinuxCollectorWithSource(source).newCollection(current)
			before := collection.acquisitionBudget
			var data []byte
			var observed bool
			if item.badRole {
				data, observed = collection.readProc("fixture", inventoryProcRole(255))
			} else {
				data, observed = collection.readScalar("fixture")
			}
			requireScalarFailure(t, data, observed, collection.err, item.want)
			if calls != 0 || collection.acquisitionBudget != before {
				t.Fatal("rejected initial state performed acquisition")
			}
		})
	}
}

func TestInventoryAcquisitionInvalidBudgetState(t *testing.T) {
	for _, item := range []struct {
		name   string
		budget *inventoryAcquisitionBudget
	}{
		{"nil", nil},
		{"negative_attempts", &inventoryAcquisitionBudget{attempts: -1, bytes: 1}},
		{"negative_bytes", &inventoryAcquisitionBudget{attempts: 1, bytes: -1}},
		{"excess_attempts", &inventoryAcquisitionBudget{attempts: 65537, bytes: 1}},
		{"excess_bytes", &inventoryAcquisitionBudget{attempts: 1, bytes: 67108865}},
	} {
		t.Run(item.name, func(t *testing.T) {
			data, observed, err := readInventoryScalarWithBudget(t.Context(), "fixture", func(string) (inventoryScalar, error) { t.Fatal("invalid budget opened a file"); return nil, nil }, item.budget)
			requireScalarFailure(t, data, observed, err, ErrAcquisitionBudget)
			if item.budget == nil {
				return
			}
			for _, role := range []inventoryProcRole{0, procCPU} {
				collection := NewLinuxCollectorWithSource(collectionSource{file: func(string) ([]byte, error) { t.Fatal("invalid budget called provider"); return nil, nil }}).newCollection(t.Context())
				collection.acquisitionBudget = *item.budget
				if role == 0 {
					data, observed = collection.readScalar("fixture")
				} else {
					data, observed = collection.readProc("fixture", role)
				}
				requireScalarFailure(t, data, observed, collection.err, ErrAcquisitionBudget)
			}
		})
	}
}

func TestInventoryAcquisitionCollectByteLimitAndReset(t *testing.T) {
	if maxInventoryFileAttempts != 65536 || maxInventoryAcquisitionBytes != 67108864 {
		t.Fatal("per-collection policy changed")
	}
	entries := make([]fs.DirEntry, 1025)
	for index := range entries {
		entries[index] = &enumerationEntry{name: fmt.Sprintf("tpm%d", index)}
	}
	raw := bytes.Repeat([]byte(" "), 65536)
	count, calls, failed := 1024, 0, false
	source := collectionSource{
		directory: func(name string) ([]fs.DirEntry, error) {
			if failed {
				t.Fatal("directory callback after byte exhaustion")
			}
			if name == "sys/class/tpm" {
				return entries[:count], nil
			}
			return nil, fs.ErrNotExist
		},
		file: func(name string) ([]byte, error) {
			if failed {
				t.Fatal("file callback after byte exhaustion")
			}
			if strings.HasPrefix(name, "sys/class/tpm/") {
				calls++
				if calls == 1025 {
					failed = true
				}
				return raw, nil
			}
			return nil, fs.ErrNotExist
		},
	}
	collector := NewLinuxCollectorWithSource(source)
	for _, requested := range []int{1024, 1025, 1024} {
		count, calls, failed = requested, 0, false
		snapshot, err := collector.Collect(t.Context())
		if requested == 1025 {
			requireNoCollection(t, snapshot, err, ErrAcquisitionBudget)
			if !failed || calls != 1025 {
				t.Fatal("overflow route was not reached")
			}
			continue
		}
		if err != nil || calls != 1024 || len(snapshot.Components) != 1024 {
			t.Fatal("exact allowance or fresh collection failed", err, calls, len(snapshot.Components))
		}
		for _, component := range snapshot.Components {
			state, found := component.Attribute("version_major_state")
			if component.Type != ComponentTPM || !found || state != "malformed" {
				t.Fatal("invalid raw observations were hidden or misclassified")
			}
		}
	}
}

func TestInventoryAcquisitionCollectAttemptLimitAndReset(t *testing.T) {
	entries := make([]fs.DirEntry, 6000)
	for index := range entries {
		entries[index] = &enumerationEntry{name: fmt.Sprintf("supply%d", index)}
	}
	count, calls := 5000, 0
	source := collectionSource{
		directory: func(name string) ([]fs.DirEntry, error) {
			if calls == 65536 {
				t.Fatal("directory callback after attempt exhaustion")
			}
			if name == "sys/class/power_supply" {
				return entries[:count], nil
			}
			return nil, fs.ErrNotExist
		},
		file: func(string) ([]byte, error) {
			calls++
			if calls > 65536 {
				t.Fatal("disallowed file callback")
			}
			return nil, fs.ErrNotExist
		},
	}
	collector := NewLinuxCollectorWithSource(source)
	for _, requested := range []int{5000, 6000, 5000} {
		count, calls = requested, 0
		snapshot, err := collector.Collect(t.Context())
		if requested == 6000 {
			requireNoCollection(t, snapshot, err, ErrAcquisitionBudget)
			if calls != 65536 {
				t.Fatal("aggregate attempt boundary was not reached", calls)
			}
			continue
		}
		if err != nil || calls < 55000 || calls >= 65536 || len(snapshot.Components) != 5000 {
			t.Fatal("valid large collection or reset failed", err, calls, len(snapshot.Components))
		}
		for _, component := range snapshot.Components {
			state, found := component.Attribute("supply_type_state")
			if component.Type != ComponentPowerSupply || !found || state != "unavailable" {
				t.Fatal("unavailable components were silently dropped")
			}
		}
	}
}

func TestInventoryAcquisitionConcurrentCollections(t *testing.T) {
	entries := make([]fs.DirEntry, 5000)
	for index := range entries {
		entries[index] = &enumerationEntry{name: fmt.Sprintf("supply%d", index)}
	}
	source := collectionSource{directory: func(name string) ([]fs.DirEntry, error) {
		if name == "sys/class/power_supply" {
			return entries, nil
		}
		return nil, fs.ErrNotExist
	}}
	collector := NewLinuxCollectorWithSource(source)
	first, second := collector.newCollection(t.Context()), collector.newCollection(t.Context())
	first.acquisitionBudget.attempts, first.acquisitionBudget.bytes = 0, 0
	if second.acquisitionBudget.attempts != 65536 || second.acquisitionBudget.bytes != 67108864 {
		t.Fatal("collections share mutable counters")
	}
	type outcome struct {
		snapshot Snapshot
		err      error
	}
	results := make(chan outcome, 2)
	for index := 0; index < 2; index++ {
		go func() { snapshot, err := collector.Collect(t.Context()); results <- outcome{snapshot, err} }()
	}
	for index := 0; index < 2; index++ {
		result := <-results
		if result.err != nil || len(result.snapshot.Components) != 5000 {
			t.Fatal("concurrent collections shared an attempt allowance", result.err, len(result.snapshot.Components))
		}
	}
}
