package bundle

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"
)

type zipSizeCounter int64

func (c *zipSizeCounter) Write(p []byte) (int, error) {
	*c += zipSizeCounter(len(p))
	return len(p), nil
}

// Independently encode the prescribed stored-member headers to measure overhead.
// This unit fixture tests size admission, not the complete report schema.
func canonicalZIPSize(t *testing.T, members []sourceFile) int64 {
	t.Helper()
	var size zipSizeCounter
	z := zip.NewWriter(&size)
	for _, m := range members {
		h := &zip.FileHeader{Name: m.name, Method: zip.Store}
		h.SetMode(0600)
		h.SetModTime(time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC))
		w, err := z.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(m.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return int64(size)
}

func TestWriteExactAggregateArchiveBoundary(t *testing.T) {
	const limit = 64 << 20
	empty := []sourceFile{{name: RenderedReportName}, {name: RenderedPDFName}}
	overhead := canonicalZIPSize(t, empty)
	data := bytes.Repeat([]byte{'x'}, 32<<20)
	for _, delta := range []int{-1, 0, 1} {
		t.Run(fmt.Sprintf("delta_%d", delta), func(t *testing.T) {
			members := []sourceFile{{name: RenderedReportName, data: data}, {name: RenderedPDFName, data: data[:limit-(32<<20)-int(overhead)+delta]}}
			if got := canonicalZIPSize(t, members); got != limit+int64(delta) {
				t.Fatalf("independent canonical size=%d", got)
			}
			err := validateWriteMembers(members)
			if delta > 0 {
				if !errors.Is(err, ErrInvalidBundle) {
					t.Fatalf("oversized aggregate accepted: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWritePreflightUsesEveryMemberLimit(t *testing.T) {
	for _, c := range []struct {
		name  string
		limit int
	}{{ManifestName, 64 << 10}, {InventoryName, 16 << 20}, {EnvelopeName, 1 << 20}, {RenderedReportName, 32 << 20}, {RenderedPDFName, 32 << 20}, {SelfIssuedKeyName, 32}} {
		t.Run(c.name, func(t *testing.T) {
			if err := validateWriteMembers([]sourceFile{{name: c.name, data: make([]byte, c.limit+1)}}); !errors.Is(err, ErrInvalidBundle) {
				t.Fatalf("member cap not enforced: %v", err)
			}
		})
	}
	if err := validateWriteMembers([]sourceFile{{name: "unknown"}}); !errors.Is(err, ErrInvalidBundle) {
		t.Fatal("unknown member admitted")
	}
}
