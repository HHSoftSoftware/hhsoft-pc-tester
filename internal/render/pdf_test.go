package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestInventoryPDFIsDeterministicAndStatic(t *testing.T) {
	payload := testPayload()
	payload.Components[0].ID = `) /JavaScript (unsafe)`
	payload.Components[0].Attributes = []inventory.Attribute{{Name: strings.Repeat("network/path", 20), Value: `\\server\\share`}}
	payload.TestResults = []app.TestResult{{ID: "cpu.quick", Version: 1, Status: app.TestPass, Evidence: []byte(`{"detail":"/OpenAction"}`)}}

	first, err := InventoryPDF(payload)
	if err != nil {
		t.Fatal(err)
	}
	second, err := InventoryPDF(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("identical payload produced different PDF")
	}
	if !bytes.HasPrefix(first, []byte("%PDF-1.4\n")) || !bytes.HasSuffix(first, []byte("%%EOF\n")) {
		t.Fatal("PDF header or trailer is invalid")
	}
	for _, required := range []string{"HHSoft PC Tester", "SYSTEM FINGERPRINT", "FUNCTIONAL TEST EVIDENCE", "cpu.quick", "xref", "startxref"} {
		if !bytes.Contains(first, []byte(required)) {
			t.Fatalf("PDF does not contain %q", required)
		}
	}
	for _, forbidden := range []string{"/JavaScript", "/OpenAction", "/AA", "/Launch", "/URI", "/EmbeddedFile", "/RichMedia", "/AcroForm", "/XFA"} {
		if bytes.Contains(first, []byte(forbidden)) {
			t.Fatalf("static PDF contains active-content marker %q", forbidden)
		}
	}
}

func TestInventoryPDFPaginatesAndSorts(t *testing.T) {
	payload := testPayload()
	for index := 0; index < 80; index++ {
		payload.Components = append(payload.Components, app.ComponentRecord{Type: inventory.ComponentUSB, ID: strings.Repeat("device", 30)})
	}
	document, err := InventoryPDF(payload)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(document, []byte("/Type /Page ")) < 2 {
		t.Fatal("long inventory did not create multiple pages")
	}
	if bytes.Index(document, []byte("disk-0")) > bytes.Index(document, []byte("cpu-0")) {
		t.Fatal("components are not sorted")
	}
}
