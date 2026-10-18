package render

import (
	"bytes"
	stdhtml "html"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestReportViewExpansionCannotExceedDocumentLimit(t *testing.T) {
	// Each input is smaller than the 16 MiB inventory limit, but escaping the
	// actual view would exceed 32 MiB. Exercise the real renderer, not a mocked
	// byte counter or a prebuilt oversized output.
	for _, format := range []string{"HTML", "PDF"} {
		t.Run(format, func(t *testing.T) {
			payload := testPayload()
			if format == "HTML" {
				payload.Components[0].Attributes = []inventory.Attribute{{Name: "model", Value: strings.Repeat("'", 7<<20)}}
				if output, err := InventoryHTML(payload); err == nil || output != nil {
					t.Fatal("HTML expansion returned an oversized or partial successful document")
				}
			} else {
				payload.Components[0].Attributes = []inventory.Attribute{{Name: "model", Value: strings.Repeat("/", 9<<20)}}
				if output, err := InventoryPDF(payload); err == nil || output != nil {
					t.Fatal("PDF escaping returned an oversized or partial successful document")
				}
			}
		})
	}
}

func TestSignedVersionMetadataIsBoundedAndNeverPDFSyntax(t *testing.T) {
	for _, version := range []string{"", "1.0\n/Injected", "1.0\r", "1.0\x00", "<script>", "1.0 (object)", strings.Repeat("1", 129)} {
		if _, err := InventoryHTMLForVersion(testPayload(), version); err == nil {
			t.Fatal("HTML accepted unsupported version metadata")
		}
		if _, err := InventoryPDFForVersion(testPayload(), version); err == nil {
			t.Fatal("PDF accepted unsupported version metadata")
		}
	}
	const version = "0.0.9-alpha.1+fixture"
	html, err := InventoryHTMLForVersion(testPayload(), version)
	if err != nil || !strings.Contains(stdhtml.UnescapeString(string(html)), "Version "+version) {
		t.Fatal("HTML did not preserve the supplied safe version")
	}
	pdf, err := InventoryPDFForVersion(testPayload(), version)
	if err != nil || !bytes.Contains(pdf, []byte("Version: "+version)) {
		t.Fatal("PDF did not preserve the supplied safe version")
	}
}

func TestPDFLongFieldWrapRetainsEveryNonspaceCharacter(t *testing.T) {
	// A long unbroken field must make linear forward progress without truncation
	// or dropping a character at page/line boundaries.
	value := strings.Repeat("0123456789", 8192)
	var builder pdfLines
	builder.field("model", value, "  ")
	if builder.err != nil {
		t.Fatal(builder.err)
	}
	lines := builder.values
	var recovered strings.Builder
	for index, line := range lines {
		if len(line) > pdfLineWidth {
			t.Fatal("wrapped field exceeds the page width")
		}
		if index == 0 {
			recovered.WriteString(strings.TrimPrefix(line, "  model: "))
		} else {
			recovered.WriteString(strings.TrimLeft(line, " "))
		}
	}
	if recovered.String() != value {
		t.Fatal("long field text changed while wrapping")
	}
}

func TestHTMLRejectsDeepOrCumulativelyOversizedEvidenceBeforeIndentation(t *testing.T) {
	for _, scenario := range []string{"depth", "indentation size", "aggregate indentation size"} {
		t.Run(scenario, func(t *testing.T) {
			payload := testPayload()
			depth, items, count := 2000, 1, 1
			if scenario == "indentation size" {
				depth, items = 63, 280000
			}
			if scenario == "aggregate indentation size" {
				depth, items, count = 63, 150000, 2
			}
			evidence := []byte(strings.Repeat("[", depth) + strings.Repeat("0,", items-1) + "0" + strings.Repeat("]", depth))
			for index := 0; index < count; index++ {
				payload.TestResults = append(payload.TestResults, app.TestResult{ID: "fixture", Version: 1, Status: app.TestInconclusive, Evidence: evidence})
			}
			if output, err := InventoryHTML(payload); err == nil || output != nil {
				t.Fatal("unsupported evidence expansion returned an HTML document")
			}
		})
	}
}
