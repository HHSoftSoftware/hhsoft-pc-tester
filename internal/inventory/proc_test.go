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
	"testing"
)

func procAcquire(ctx context.Context, role inventoryProcRole, file *scalarHandle) ([]byte, bool, error) {
	return readInventoryProc(ctx, "owned-fixture", role, func(string) (inventoryScalar, error) { return file, nil })
}

func TestInventoryProcLiteralBudgetsAndRawEOF(t *testing.T) {
	for _, item := range []struct {
		role  inventoryProcRole
		limit int
	}{{procMemory, 262144}, {procCPU, 16777216}, {procInput, 8388608}} {
		for _, size := range []int{0, 1, item.limit - 1, item.limit, item.limit + 1} {
			for _, together := range []bool{false, true} {
				t.Run(fmt.Sprintf("%d/%d/%v", item.role, size, together), func(t *testing.T) {
					value := bytes.Repeat([]byte{' '}, size)
					if size > 2 {
						value[size-2], value[size-1] = '\r', '\n'
					}
					file := scalarBytes(value, 0, together)
					data, observed, err := procAcquire(t.Context(), item.role, file)
					if size > item.limit {
						requireScalarFailure(t, data, observed, err, ErrBulkLimit)
					} else if err != nil || !observed || !bytes.Equal(data, value) || cap(data) > item.limit {
						t.Fatal("complete raw proc bytes changed or exceeded capacity", err)
					}
					if size <= 1 && cap(data) > 4096 {
						t.Fatal("small observation eagerly reserved its bulk budget")
					}
					if file.closes != 1 || file.stats != 1 {
						t.Fatal("owned protocol did not close once")
					}
					consumed := 0
					for index, request := range file.requests {
						if request <= 0 || request > 4096 || request > item.limit-consumed+1 {
							t.Fatal("read request escaped remaining-byte bound")
						}
						consumed += min(request, max(0, size-consumed))
						if index == len(file.requests)-1 && size == item.limit && !together && request != 1 {
							t.Fatal("exact budget lacked EOF probe")
						}
					}
				})
			}
		}
	}
}

func TestInventoryProcShortReadsAndSizeAreNotCompletion(t *testing.T) {
	value := bytes.Repeat([]byte("cpu\r\n"), 1700)
	for _, stated := range []int64{-1, 0, 1, 1 << 40} {
		for _, role := range []inventoryProcRole{procMemory, procCPU, procInput} {
			file := scalarBytes(value, 37, false)
			sizeCalls := 0
			file.info = &scalarInformation{size: stated, sizeCalls: &sizeCalls}
			data, observed, err := procAcquire(t.Context(), role, file)
			if err != nil || !observed || !bytes.Equal(data, value) || len(file.requests) < 2 || sizeCalls != 0 || file.closes != 1 {
				t.Fatal("short read or Stat.Size became completion", err)
			}
		}
	}
}

func TestInventoryProcOverflowErrorCancelAndCleanupPrecedence(t *testing.T) {
	for _, terminal := range []error{nil, io.EOF, fs.ErrPermission, fmt.Errorf("wrapped: %w", io.EOF)} {
		for _, canceled := range []bool{false, true} {
			for _, closeFailed := range []bool{false, true} {
				ctx, cancel := context.WithCancel(t.Context())
				remaining := 262145
				file := &scalarHandle{info: &scalarInformation{}, read: func(p []byte) (int, error) {
					if remaining == 0 {
						t.Fatal("read after observed overflow")
					}
					n := min(len(p), remaining)
					remaining -= n
					if remaining == 0 {
						if len(p) != 1 {
							t.Fatal("unbounded overflow probe")
						}
						if canceled {
							cancel()
						}
						return n, terminal
					}
					return n, nil
				}}
				want := ErrBulkLimit
				if closeFailed {
					file.closeErr, want = fs.ErrPermission, ErrBulkClose
				}
				data, observed, err := procAcquire(ctx, procMemory, file)
				requireScalarFailure(t, data, observed, err, want)
				if remaining != 0 || file.closes != 1 {
					t.Fatal("overflow/cleanup oracle was not reached")
				}
				cancel()
			}
		}
	}
}

func TestInventoryProcOptionalReadFailureDiscardsEveryPrefix(t *testing.T) {
	for _, terminal := range []error{fs.ErrPermission, io.ErrUnexpectedEOF, fmt.Errorf("wrapped: %w", io.EOF), errors.Join(io.EOF, fs.ErrPermission), errors.Join(io.EOF)} {
		for _, canceled := range []bool{false, true} {
			for _, role := range []inventoryProcRole{procMemory, procCPU, procInput} {
				ctx, cancel := context.WithCancel(t.Context())
				calls := 0
				file := &scalarHandle{info: &scalarInformation{}, read: func(p []byte) (int, error) {
					calls++
					if calls == 1 {
						return copy(p, "processor: 9\nmodel name: uncredited\n"), nil
					}
					if canceled {
						cancel()
					}
					return copy(p, "valid-looking-tail"), terminal
				}}
				data, observed, err := procAcquire(ctx, role, file)
				var want error
				if canceled {
					want = context.Canceled
				}
				requireScalarFailure(t, data, observed, err, want)
				if calls != 2 || file.closes != 1 {
					t.Fatal("partial observation was not exercised and closed")
				}
				cancel()
			}
		}
	}
}

func TestInventoryProcProtocolAndTypeProofStayHard(t *testing.T) {
	var nilInfo *scalarInformation
	for _, mode := range []string{"stat-error", "nil-info", "typed-nil-info", "directory", "device", "negative", "over-request", "zero-progress"} {
		for _, canceled := range []bool{false, true} {
			for _, closeFailed := range []bool{false, true} {
				ctx, cancel := context.WithCancel(t.Context())
				file := scalarBytes([]byte("ordinary"), 0, true)
				switch mode {
				case "stat-error":
					file.statErr = fs.ErrPermission
				case "nil-info":
					file.info = nil
				case "typed-nil-info":
					file.info = nilInfo
				case "directory":
					file.info = &scalarInformation{mode: fs.ModeDir}
				case "device":
					file.info = &scalarInformation{mode: fs.ModeDevice}
				default:
					file.read = func(p []byte) (int, error) {
						if canceled {
							cancel()
						}
						if mode == "negative" {
							return -1, io.EOF
						}
						if mode == "over-request" {
							return len(p) + 1, io.EOF
						}
						return 0, nil
					}
				}
				if canceled && mode != "negative" && mode != "over-request" && mode != "zero-progress" {
					file.onStat = cancel
				}
				want := ErrBulkRead
				if closeFailed {
					file.closeErr, want = fs.ErrPermission, ErrBulkClose
				}
				data, observed, err := procAcquire(ctx, procCPU, file)
				requireScalarFailure(t, data, observed, err, want)
				if file.closes != 1 {
					t.Fatal("hard failure lost handle ownership")
				}
				if mode != "negative" && mode != "over-request" && mode != "zero-progress" && len(file.requests) != 0 {
					t.Fatal("read after failed type proof")
				}
				cancel()
			}
		}
	}
}

func TestInventoryProcOpenDependenciesAndClosedRoles(t *testing.T) {
	for _, role := range []inventoryProcRole{0, 255} {
		data, found, err := readInventoryProc(t.Context(), "unused", role, func(string) (inventoryScalar, error) { t.Fatal("invalid role opened"); return nil, nil })
		requireScalarFailure(t, data, found, err, ErrBulkRead)
	}
	for _, limit := range []int{-1, 0, 1, 262145, 16777217, int(^uint(0) >> 1)} {
		data, found, err := readInventoryBytes(t.Context(), "unused", limit, func(string) (inventoryScalar, error) { t.Fatal("invalid budget opened"); return nil, nil })
		requireScalarFailure(t, data, found, err, errInventoryFileRead)
	}
	var nilFile *scalarHandle
	var nilContext *nilScalarContext
	for _, ctx := range []context.Context{nil, nilContext} {
		data, found, err := readInventoryProc(ctx, "unused", procMemory, func(string) (inventoryScalar, error) { t.Fatal("nil context opened"); return nil, nil })
		requireScalarFailure(t, data, found, err, ErrInvalidContext)
	}
	data, found, err := readInventoryProc(t.Context(), "unused", procMemory, nil)
	requireScalarFailure(t, data, found, err, ErrBulkRead)
	for _, absent := range []inventoryScalar{nil, nilFile} {
		for _, openErr := range []error{nil, fs.ErrPermission} {
			data, found, err = readInventoryProc(t.Context(), "unused", procMemory, func(string) (inventoryScalar, error) { return absent, openErr })
			var want error
			if openErr == nil {
				want = ErrBulkRead
			}
			requireScalarFailure(t, data, found, err, want)
		}
	}
	for _, closeFailed := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		file := scalarBytes(nil, 0, false)
		want := ErrBulkRead
		if closeFailed {
			file.closeErr, want = fs.ErrPermission, ErrBulkClose
		}
		data, found, err = readInventoryProc(ctx, "fixture", procMemory, func(string) (inventoryScalar, error) { cancel(); return file, fs.ErrPermission })
		requireScalarFailure(t, data, found, err, want)
		if file.closes != 1 || file.stats != 0 || len(file.requests) != 0 {
			t.Fatal("handle-plus-open-error was not closed without observation")
		}
	}
}

func TestInventoryProcSemanticCancellationAndCloseDominance(t *testing.T) {
	for _, stage := range []string{"pre-open", "open", "stat", "read", "eof", "close", "absent-open"} {
		for _, closeFailed := range []bool{false, true} {
			ctx, cancel := context.WithCancel(t.Context())
			opens := 0
			file := scalarBytes([]byte("complete"), 0, false)
			if stage == "pre-open" {
				cancel()
			}
			if stage == "stat" {
				file.onStat = cancel
			}
			if stage == "close" {
				file.onClose = cancel
			}
			if stage == "read" || stage == "eof" {
				file.read = func(p []byte) (int, error) {
					cancel()
					if stage == "eof" {
						return copy(p, "complete"), io.EOF
					}
					return copy(p, "partial"), nil
				}
			}
			if closeFailed {
				file.closeErr = fs.ErrPermission
			}
			data, found, err := readInventoryProc(ctx, "fixture", procInput, func(string) (inventoryScalar, error) {
				opens++
				if stage == "open" || stage == "absent-open" {
					cancel()
				}
				if stage == "absent-open" {
					return nil, fs.ErrPermission
				}
				return file, nil
			})
			want := context.Canceled
			if closeFailed && stage != "pre-open" && stage != "absent-open" {
				want = ErrBulkClose
			}
			requireScalarFailure(t, data, found, err, want)
			if stage == "pre-open" {
				if opens != 0 || file.closes != 0 {
					t.Fatal("pre-cancel acquired ownership")
				}
			} else if stage == "absent-open" {
				if opens != 1 || file.closes != 0 {
					t.Fatal("absent open invented ownership")
				}
			} else if opens != 1 || file.closes != 1 {
				t.Fatal("cancellation leaked ownership")
			}
			cancel()
		}
	}
	for _, terminal := range []error{io.EOF, fs.ErrPermission} {
		file := &scalarHandle{info: &scalarInformation{}, closeErr: fs.ErrPermission, read: func(p []byte) (int, error) { return copy(p, "data"), terminal }}
		data, found, err := procAcquire(t.Context(), procMemory, file)
		requireScalarFailure(t, data, found, err, ErrBulkClose)
	}
}

func TestInventoryProcProductionDescriptorClosedForEveryOutcome(t *testing.T) {
	for _, mode := range []string{"success", "overflow", "directory"} {
		name := filepath.Join(t.TempDir(), "owned")
		var want error
		if mode == "directory" {
			if err := os.Mkdir(name, 0700); err != nil {
				t.Fatal(err)
			}
			want = ErrBulkRead
		} else {
			value := []byte("MemTotal: 4096 kB\n")
			if mode == "overflow" {
				value = bytes.Repeat([]byte{' '}, 262145)
				want = ErrBulkLimit
			}
			if err := os.WriteFile(name, value, 0600); err != nil {
				t.Fatal(err)
			}
		}
		var opened *os.File
		data, found, err := readInventoryProc(t.Context(), name, procMemory, func(p string) (inventoryScalar, error) {
			f, e := openInventoryScalar(p)
			if f != nil {
				opened = f.(*os.File)
			}
			return f, e
		})
		if want == nil {
			if err != nil || !found || string(data) != "MemTotal: 4096 kB\n" {
				t.Fatal("real file control failed", err)
			}
		} else {
			requireScalarFailure(t, data, found, err, want)
		}
		if opened == nil || !errors.Is(opened.Close(), os.ErrClosed) {
			t.Fatal("real production descriptor remained open")
		}
	}
}
