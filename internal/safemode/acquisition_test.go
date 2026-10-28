package safemode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type safetyReadFunc func([]byte) (int, error)

func (fn safetyReadFunc) Read(p []byte) (int, error) { return fn(p) }

type safetyDirectoryFunc func(int) ([]os.DirEntry, error)

func (fn safetyDirectoryFunc) ReadDir(n int) ([]os.DirEntry, error) { return fn(n) }

type safetyLiteralEntry struct {
	name  string
	calls int
	hook  func()
}

func (entry *safetyLiteralEntry) Name() string {
	entry.calls++
	if entry.hook != nil {
		entry.hook()
	}
	if entry.calls > 1 {
		return "changed/unsafe"
	}
	return entry.name
}
func (*safetyLiteralEntry) IsDir() bool                { return false }
func (*safetyLiteralEntry) Type() os.FileMode          { return os.ModeSymlink }
func (*safetyLiteralEntry) Info() (os.FileInfo, error) { return nil, errors.New("unused info") }

type safetyNilContext struct{ context.Context }

func TestSafetyAcquisitionPublicCancelledAndUnsupportedBoundary(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// Cancellation is established before Linux Observe can acquire a root.
	// Other platforms retain their existing unsupported public observation.
	result := Observe(ctx)
	if result.Safe() || result.StorageObserved || result.NetworkObserved || result.WritableBlockMounts != 0 || result.ActiveNonLoopInterfaces != 0 {
		t.Fatal("unobserved public call acquired safety credit")
	}
	if runtime.GOOS == "linux" {
		if result.Status != "aborted" {
			t.Fatal("cancelled public Linux dispatch changed")
		}
	} else if result.Status != "error" || result.StorageDetail != "Linux kernel observations required" || result.NetworkDetail != "Linux kernel observations required" {
		t.Fatal("unsupported public observation changed")
	}
}

func TestSafetyAcquisitionByteCapsRequireExactEOF(t *testing.T) {
	for _, limit := range []int64{64, 4 << 20} {
		for _, size := range []int{0, int(limit) - 1, int(limit), int(limit) + 1} {
			t.Run(fmt.Sprintf("limit-%d-size-%d", limit, size), func(t *testing.T) {
				input := bytes.Repeat([]byte{'x'}, size)
				reader := bytes.NewReader(input)
				calls, requested, consumed := 0, 0, 0
				got, err := readSafetyBytes(t.Context(), safetyReadFunc(func(p []byte) (int, error) {
					calls++
					if len(p) < 1 || len(p) > 4096 {
						t.Fatal("request escaped finite byte batch")
					}
					requested += len(p)
					n, err := reader.Read(p)
					consumed += n
					return n, err
				}), limit)
				if size <= int(limit) {
					if err != nil || !bytes.Equal(got, input) || calls != (size+4095)/4096+1 {
						t.Fatalf("exact completion missing: %d %v", calls, err)
					}
				} else if err == nil || got != nil || consumed != int(limit)+1 {
					t.Fatal("overflow was truncated or accepted")
				}
				if consumed > int(limit)+1 || requested < consumed {
					t.Fatal("invalid bounded accounting")
				}
			})
		}
	}
	for _, limit := range []int64{-1, 0, (4 << 20) + 1} {
		if _, err := readSafetyBytes(t.Context(), safetyReadFunc(func([]byte) (int, error) { t.Fatal("invalid limit read"); return 0, io.EOF }), limit); err == nil {
			t.Fatal("invalid cap accepted")
		}
	}
}

func TestSafetyAcquisitionRejectsAmbiguousReadProgress(t *testing.T) {
	for _, mode := range []string{"negative", "oversized", "zero", "read-error", "joined-eof", "wrapped-eof", "data-error"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			got, err := readSafetyBytes(t.Context(), safetyReadFunc(func(p []byte) (int, error) {
				calls++
				if calls > 1 {
					t.Fatal("read continued after fault")
				}
				switch mode {
				case "negative":
					return -1, nil
				case "oversized":
					return len(p) + 1, nil
				case "zero":
					return 0, nil
				case "read-error":
					return 0, errors.New("read")
				case "joined-eof":
					return 0, errors.Join(io.EOF, errors.New("other"))
				case "wrapped-eof":
					return 0, fmt.Errorf("wrapper: %w", io.EOF)
				default:
					p[0] = 'x'
					return 1, errors.New("partial")
				}
			}), 64)
			if err == nil || got != nil || calls != 1 {
				t.Fatal("ambiguous read accepted")
			}
		})
	}
	step := 0
	got, err := readSafetyBytes(t.Context(), safetyReadFunc(func(p []byte) (int, error) {
		step++
		if step == 1 {
			copy(p, "abc")
			return 3, nil
		}
		if step == 2 {
			p[0] = 'd'
			return 1, io.EOF
		}
		t.Fatal("read after EOF")
		return 0, io.EOF
	}), 64)
	if err != nil || string(got) != "abcd" || step != 2 {
		t.Fatal("short progress or data with exact EOF rejected")
	}
}

func TestSafetyAcquisitionRejectsNilAndCancelledReaders(t *testing.T) {
	var reader safetyReadFunc
	var ctx *safetyNilContext
	for _, test := range []struct {
		ctx    context.Context
		reader io.Reader
	}{{nil, bytes.NewReader(nil)}, {ctx, bytes.NewReader(nil)}, {t.Context(), nil}, {t.Context(), reader}} {
		if _, err := readSafetyBytes(test.ctx, test.reader, 64); err == nil {
			t.Fatal("nil provider accepted")
		}
	}
	for _, when := range []string{"before", "during"} {
		t.Run(when, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			if when == "before" {
				cancel()
			}
			got, err := readSafetyBytes(ctx, safetyReadFunc(func(p []byte) (int, error) { calls++; cancel(); p[0] = 'x'; return 1, io.EOF }), 64)
			want := 1
			if when == "before" {
				want = 0
			}
			if err != context.Canceled || got != nil || calls != want {
				t.Fatal("cancelled read acquired data")
			}
		})
	}
}

func TestSafetyAcquisitionRegistryBoundAndCachedNames(t *testing.T) {
	for _, count := range []int{0, 1, 4096, 4097} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			entries := make([]os.DirEntry, count)
			for i := range entries {
				entries[i] = &safetyLiteralEntry{name: fmt.Sprintf("n%04d", count-i)}
			}
			used, calls := 0, 0
			names, err := readSafetyNames(t.Context(), safetyDirectoryFunc(func(n int) ([]os.DirEntry, error) {
				calls++
				if n < 1 || n > 32 {
					t.Fatal("registry request unbounded")
				}
				end := min(count, used+n)
				batch := entries[used:end]
				used = end
				if len(batch) == 0 {
					return batch, io.EOF
				}
				return batch, nil
			}))
			if count <= 4096 {
				if err != nil || len(names) != count || calls != (count+31)/32+1 {
					t.Fatal("EOF or exact count missing")
				}
				for i, name := range names {
					if name != fmt.Sprintf("n%04d", i+1) {
						t.Fatal("names not cached and ordered")
					}
				}
				for _, entry := range entries {
					if entry.(*safetyLiteralEntry).calls != 1 {
						t.Fatal("name re-read")
					}
				}
			} else if err == nil || names != nil || used != 4097 {
				t.Fatal("registry overflow accepted or truncated")
			}
		})
	}
}

func TestSafetyAcquisitionRegistryRejectsProtocolAndNameFaults(t *testing.T) {
	for _, mode := range []string{"zero", "oversized", "joined-eof", "wrapped-eof", "error", "nil-entry", "typed-nil-entry", "duplicate", "dot", "dotdot", "slash", "backslash", "nul", "nonascii", "long"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			names, err := readSafetyNames(t.Context(), safetyDirectoryFunc(func(n int) ([]os.DirEntry, error) {
				calls++
				if calls > 1 {
					t.Fatal("continued after invalid registry batch")
				}
				switch mode {
				case "zero":
					return nil, nil
				case "oversized":
					return make([]os.DirEntry, n+1), io.EOF
				case "joined-eof":
					return nil, errors.Join(io.EOF, errors.New("other"))
				case "wrapped-eof":
					return nil, fmt.Errorf("wrapper: %w", io.EOF)
				case "error":
					return nil, errors.New("read")
				case "nil-entry":
					return []os.DirEntry{nil}, io.EOF
				case "typed-nil-entry":
					var entry *safetyLiteralEntry
					return []os.DirEntry{entry}, io.EOF
				case "duplicate":
					return []os.DirEntry{&safetyLiteralEntry{name: "lo"}, &safetyLiteralEntry{name: "lo"}}, io.EOF
				}
				name := map[string]string{"dot": ".", "dotdot": "..", "slash": "a/b", "backslash": "a\\b", "nul": "a\x00", "nonascii": "\u00e9", "long": strings.Repeat("a", 65)}[mode]
				return []os.DirEntry{&safetyLiteralEntry{name: name}}, io.EOF
			}))
			if err == nil || names != nil || calls != 1 {
				t.Fatal("malformed registry accepted")
			}
		})
	}
	step := 0
	names, err := readSafetyNames(t.Context(), safetyDirectoryFunc(func(int) ([]os.DirEntry, error) {
		step++
		if step == 1 {
			return []os.DirEntry{&safetyLiteralEntry{name: "eth0"}}, nil
		}
		return []os.DirEntry{&safetyLiteralEntry{name: "lo"}}, io.EOF
	}))
	if err != nil || step != 2 || !reflect.DeepEqual(names, []string{"eth0", "lo"}) {
		t.Fatal("short successful batch mistaken for completion")
	}
	step = 0
	if _, err := readSafetyNames(t.Context(), safetyDirectoryFunc(func(int) ([]os.DirEntry, error) { step++; return []os.DirEntry{&safetyLiteralEntry{name: "eth0"}}, nil })); err == nil || step != 2 {
		t.Fatal("duplicate across batches accepted")
	}
}

func TestSafetyAcquisitionRegistryCancellationAndNilProviders(t *testing.T) {
	var directory safetyDirectoryFunc
	if _, err := readSafetyNames(t.Context(), directory); err == nil {
		t.Fatal("typed-nil directory accepted")
	}
	for _, when := range []string{"before", "read", "name"} {
		t.Run(when, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			if when == "before" {
				cancel()
			}
			names, err := readSafetyNames(ctx, safetyDirectoryFunc(func(int) ([]os.DirEntry, error) {
				calls++
				entry := &safetyLiteralEntry{name: "lo"}
				if when == "read" {
					cancel()
				} else {
					entry.hook = cancel
				}
				return []os.DirEntry{entry}, io.EOF
			}))
			want := 1
			if when == "before" {
				want = 0
			}
			if err != context.Canceled || names != nil || calls != want {
				t.Fatal("cancelled registry accepted")
			}
		})
	}
}
