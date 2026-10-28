package main

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

type retestIDReader struct {
	call  func([]byte) (int, error)
	calls int
}

func (reader *retestIDReader) Read(p []byte) (int, error) { reader.calls++; return reader.call(p) }

func TestRetestRandomIDRequiresFiniteUnambiguousEntropy(t *testing.T) {
	value, err := randomRetestSessionID(bytes.NewReader(bytes.Repeat([]byte{0xab}, 16)))
	if err != nil || value != "abababab-abab-4bab-abab-abababababab" || !report.ValidRetestID(value) {
		t.Fatal("random ID did not retain entropy with canonical version/variant")
	}
	for _, mode := range []string{"no-progress", "negative", "oversize", "bytes-with-EOF", "bytes-with-error", "error", "short-then-stop"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			reader := &retestIDReader{call: func(p []byte) (int, error) {
				calls++
				switch mode {
				case "no-progress":
					return 0, nil
				case "negative":
					return -1, nil
				case "oversize":
					return len(p) + 1, nil
				case "bytes-with-EOF":
					return len(p), io.EOF
				case "bytes-with-error":
					return len(p), errors.New("private fixture marker")
				case "error":
					return 0, io.ErrUnexpectedEOF
				case "short-then-stop":
					if calls == 1 {
						p[0] = 1
						return 1, nil
					}
					return 0, nil
				}
				panic("unreachable test mode")
			}}
			id, err := randomRetestSessionID(reader)
			if id != "" || err != errRetestVerification || reader.calls > 2 {
				t.Fatal("ambiguous entropy produced an ID or unbounded retry")
			}
		})
	}
	if id, err := randomRetestSessionID(nil); id != "" || err != errRetestVerification {
		t.Fatal("nil entropy source accepted")
	}
	reader := &retestIDReader{call: func(p []byte) (int, error) { p[0] = 0x11; return 1, nil }}
	id, err := randomRetestSessionID(reader)
	if err != nil || !report.ValidRetestID(id) || reader.calls != 16 {
		t.Fatal("finite short reads rejected")
	}
}
