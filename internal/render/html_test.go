package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

func TestInventoryHTMLEscapesAllPayloadText(t *testing.T) {
	payload := testPayload()
	payload.Components[0].ID = `<script>alert("id")</script>`
	payload.Components[0].Attributes = []inventory.Attribute{
		{Name: `<img src=x onerror=alert(1)>`, Value: `</td><script>alert("value")</script>`},
	}
	payload.Components[0].Identity.Value = `<unsafe-fingerprint>`
	payload.SystemFingerprint.Value = `<unsafe-system-fingerprint>`
	payload.TestResults = []app.TestResult{{ID: "graphics.quick", Version: 1, Status: app.TestPass, Evidence: json.RawMessage(`{"detail":"</pre><script>alert(1)</script>"}`)}}

	document, err := InventoryHTML(payload)
	if err != nil {
		t.Fatal(err)
	}
	html := string(document)
	for _, unsafe := range []string{`<script>alert("id")</script>`, `<img src=x onerror=alert(1)>`, `</td><script>alert("value")</script>`, `<unsafe-fingerprint>`, `<unsafe-system-fingerprint>`} {
		if strings.Contains(html, unsafe) {
			t.Fatalf("output contains unescaped payload text %q", unsafe)
		}
	}
	for _, escaped := range []string{`&lt;script&gt;alert(&#34;id&#34;)&lt;/script&gt;`, `&lt;img src=x onerror=alert(1)&gt;`, `&lt;unsafe-fingerprint&gt;`, `&lt;unsafe-system-fingerprint&gt;`} {
		if !strings.Contains(html, escaped) {
			t.Fatalf("output does not contain escaped payload text %q", escaped)
		}
	}
}

func TestInventoryHTMLIsDeterministicAndSortsComponents(t *testing.T) {
	payload := testPayload()

	first, err := InventoryHTML(payload)
	if err != nil {
		t.Fatal(err)
	}
	second, err := InventoryHTML(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("identical payload produced different HTML")
	}
	if strings.Index(string(first), "disk-0") > strings.Index(string(first), "cpu-0") {
		t.Fatal("components are not rendered in canonical type and ID order")
	}
	if payload.Components[0].ID != "cpu-0" {
		t.Fatal("renderer mutated the source payload")
	}
}

func TestInventoryHTMLShowsFingerprintKeyFieldsAndIsStandalone(t *testing.T) {
	document, err := InventoryHTML(testPayload())
	if err != nil {
		t.Fatal(err)
	}
	html := string(document)
	for _, expected := range []string{
		"<!doctype html>",
		"HHSoft PC Tester Inventory Report",
		"hmac-sha256:system",
		"hmac-sha256:cpu",
		"hmac-sha256:disk",
		"hmac-sha256",
		"seller-key-7",
		"durable",
		"indicative",
		"Identified parts",
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("output is missing required field %q", expected)
		}
	}
	lower := strings.ToLower(html)
	for _, forbidden := range []string{"<script", "<link", "<iframe", "src=", "url(", "http://", "https://"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("standalone output contains forbidden resource or script marker %q", forbidden)
		}
	}
}

func testPayload() app.InventoryPayload {
	return app.InventoryPayload{
		SchemaVersion: 1,
		Components: []app.ComponentRecord{
			{
				Type:       inventory.ComponentCPU,
				ID:         "cpu-0",
				Attributes: []inventory.Attribute{{Name: "model", Value: "Fast CPU"}},
				Identity:   app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "seller-key-7", Value: "hmac-sha256:cpu", Confidence: "indicative"},
			},
			{
				Type:       inventory.ComponentBlock,
				ID:         "disk-0",
				Attributes: []inventory.Attribute{{Name: "model", Value: "Fast Disk"}},
				Identity:   app.ComponentIdentity{Algorithm: "hmac-sha256", KeyEpoch: "seller-key-7", Value: "hmac-sha256:disk", Confidence: "durable"},
			},
		},
		SystemFingerprint: app.SystemFingerprint{
			Algorithm:         "hmac-sha256",
			KeyEpoch:          "seller-key-7",
			Value:             "hmac-sha256:system",
			IdentifiedParts:   2,
			UnidentifiedParts: 0,
		},
	}
}
