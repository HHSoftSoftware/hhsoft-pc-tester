package render

import (
	"bytes"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"sort"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
)

type inventoryDocument struct {
	ProductName       string
	ProductVersion    string
	SchemaVersion     int
	TotalComponents   int
	IdentifiedParts   int
	UnidentifiedParts int
	SystemFingerprint app.SystemFingerprint
	Components        []app.ComponentRecord
	TestResults       []testDocument
}

type testDocument struct {
	ID            string
	Version       int
	Status        app.TestStatus
	PlanID        string
	HardwareGraph string
	Evidence      string
}

var inventoryTemplate = template.Must(template.New("inventory").Funcs(template.FuncMap{
	"display": func(value string) string {
		if strings.TrimSpace(value) == "" {
			return "Unavailable"
		}
		return value
	},
}).Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.ProductName}} Inventory Report</title>
<style>
:root{color-scheme:light;--ink:#17202a;--muted:#536270;--line:#8795a1;--paper:#f5f7f8;--panel:#fff;--accent:#125e70;--shade:#e4eaed}
*{box-sizing:border-box}
body{margin:0;background:var(--paper);color:var(--ink);font:14px/1.45 system-ui,-apple-system,"Segoe UI",sans-serif}
main{max-width:1100px;margin:0 auto;padding:24px}
h1,h2{line-height:1.15;margin:0 0 12px}h1{font-size:28px}h2{border-bottom:3px solid var(--accent);font-size:19px;padding-bottom:6px}
.subtitle{color:var(--muted);margin:0 0 24px}.section{margin:0 0 26px}.notice{background:var(--shade);border-left:5px solid var(--accent);padding:10px 12px}
table{background:var(--panel);border-collapse:collapse;border:1px solid var(--line);width:100%}th,td{border:1px solid var(--line);padding:7px 9px;text-align:left;vertical-align:top}th{background:var(--shade)}
.label{font-weight:650;width:210px}.fingerprint,pre{font-family:ui-monospace,"SFMono-Regular",Consolas,monospace;overflow-wrap:anywhere}.component,.test{margin:0 0 18px}.component h3,.test h3{background:var(--ink);color:#fff;font-size:15px;margin:0;padding:8px 10px}.attributes{margin-top:-1px}.attributes th:first-child{width:35%}pre{background:var(--panel);border:1px solid var(--line);margin:0;padding:10px;white-space:pre-wrap}
@media print{body{background:#fff}main{max-width:none;padding:0}.section,.component{break-inside:avoid}}
</style>
</head>
<body>
<main>
<header>
<h1>{{.ProductName}} Inventory Report</h1>
<p class="subtitle">Version {{.ProductVersion}} · Inventory schema {{.SchemaVersion}}</p>
</header>
<section class="section" aria-labelledby="summary-heading">
<h2 id="summary-heading">Summary</h2>
<table>
<tbody>
<tr><th class="label" scope="row">Components</th><td>{{.TotalComponents}}</td></tr>
<tr><th class="label" scope="row">Identified parts</th><td>{{.IdentifiedParts}}</td></tr>
<tr><th class="label" scope="row">Unidentified parts</th><td>{{.UnidentifiedParts}}</td></tr>
<tr><th class="label" scope="row">Functional test results</th><td>{{len .TestResults}}</td></tr>
</tbody>
</table>
</section>
<section class="section" aria-labelledby="system-heading">
<h2 id="system-heading">System Fingerprint</h2>
<table>
<tbody>
<tr><th class="label" scope="row">Algorithm</th><td>{{display .SystemFingerprint.Algorithm}}</td></tr>
<tr><th class="label" scope="row">Key epoch</th><td>{{display .SystemFingerprint.KeyEpoch}}</td></tr>
<tr><th class="label" scope="row">Fingerprint key ID</th><td class="fingerprint">{{display .SystemFingerprint.KeyID}}</td></tr>
<tr><th class="label" scope="row">Fingerprint</th><td class="fingerprint">{{display .SystemFingerprint.Value}}</td></tr>
</tbody>
</table>
</section>
<section class="section" aria-labelledby="tests-heading">
<h2 id="tests-heading">Functional Test Evidence</h2>
{{if .TestResults}}
{{range .TestResults}}
<article class="test">
<h3>{{display .ID}} Â· {{.Status}}</h3>
<table><tbody>
<tr><th class="label" scope="row">Adapter version</th><td>{{.Version}}</td></tr>
<tr><th class="label" scope="row">Status</th><td>{{.Status}}</td></tr>
{{if .PlanID}}<tr><th class="label" scope="row">Frozen plan</th><td class="fingerprint">{{.PlanID}}</td></tr>{{end}}
{{if .HardwareGraph}}<tr><th class="label" scope="row">Planned hardware graph</th><td class="fingerprint">{{.HardwareGraph}}</td></tr>{{end}}
</tbody></table>
<pre>{{.Evidence}}</pre>
</article>
{{end}}
{{else}}<p class="notice">No functional test evidence is included.</p>{{end}}
</section>
<section class="section" aria-labelledby="components-heading">
<h2 id="components-heading">Components</h2>
{{if .Components}}
{{range $index, $component := .Components}}
<article class="component">
<h3>{{$component.Type}} · {{display $component.ID}}</h3>
<table>
<tbody>
<tr><th class="label" scope="row">Fingerprint</th><td class="fingerprint">{{display $component.Identity.Value}}</td></tr>
<tr><th class="label" scope="row">Algorithm</th><td>{{display $component.Identity.Algorithm}}</td></tr>
<tr><th class="label" scope="row">Key epoch</th><td>{{display $component.Identity.KeyEpoch}}</td></tr>
<tr><th class="label" scope="row">Identity source</th><td>{{display $component.Identity.Source}}</td></tr>
<tr><th class="label" scope="row">Confidence</th><td>{{display $component.Identity.Confidence}}</td></tr>
</tbody>
</table>
<table class="attributes">
<thead><tr><th scope="col">Attribute</th><th scope="col">Value</th></tr></thead>
<tbody>
{{if $component.Attributes}}{{range $component.Attributes}}<tr><th scope="row">{{.Name}}</th><td>{{.Value}}</td></tr>{{end}}{{else}}<tr><td colspan="2">No public attributes reported.</td></tr>{{end}}
</tbody>
</table>
</article>
{{end}}
{{else}}<p class="notice">No components reported.</p>{{end}}
</section>
</main>
</body>
</html>
`))

// InventoryHTML renders a payload as a deterministic, standalone HTML5 document.
func InventoryHTML(payload app.InventoryPayload) ([]byte, error) {
	return InventoryHTMLForVersion(payload, product.Version)
}

// InventoryHTMLForVersion reproduces a view using its signed software version,
// rather than silently substituting the verifier binary's version.
func InventoryHTMLForVersion(payload app.InventoryPayload, version string) ([]byte, error) {
	var output bytes.Buffer
	if err := renderInventoryHTML(&output, payload, version); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

// RenderInventoryHTML writes a deterministic, standalone HTML5 document.
func RenderInventoryHTML(output io.Writer, payload app.InventoryPayload) error {
	return renderInventoryHTML(output, payload, product.Version)
}

func renderInventoryHTML(output io.Writer, payload app.InventoryPayload, version string) error {
	if err := validateVersion(version); err != nil {
		return err
	}
	components := append([]app.ComponentRecord(nil), payload.Components...)
	sort.SliceStable(components, func(left, right int) bool {
		if components[left].Type != components[right].Type {
			return components[left].Type < components[right].Type
		}
		return components[left].ID < components[right].ID
	})
	testResults := make([]testDocument, 0, len(payload.TestResults))
	evidenceBudget := maximumDocumentBytes
	for _, result := range payload.TestResults {
		evidence, err := indentEvidence(result.Evidence, &evidenceBudget)
		if err != nil {
			return err
		}
		testResults = append(testResults, testDocument{ID: result.ID, Version: result.Version, Status: result.Status, PlanID: result.PlanID, HardwareGraph: result.HardwareGraph, Evidence: evidence})
	}
	sort.SliceStable(testResults, func(left, right int) bool { return testResults[left].ID < testResults[right].ID })
	document := inventoryDocument{
		ProductName:       product.Name,
		ProductVersion:    version,
		SchemaVersion:     payload.SchemaVersion,
		TotalComponents:   len(components),
		IdentifiedParts:   payload.SystemFingerprint.IdentifiedParts,
		UnidentifiedParts: payload.SystemFingerprint.UnidentifiedParts,
		SystemFingerprint: payload.SystemFingerprint,
		Components:        components,
		TestResults:       testResults,
	}
	return inventoryTemplate.Execute(&limitedWriter{destination: output}, document)
}

// json.Indent writes to bytes.Buffer, so an output writer limit cannot protect
// this intermediate allocation. Preflight a conservative size bound, counting
// even the newlines omitted for empty containers, before calling it. Evidence
// from implemented workers is shallow; deeper inputs are unsupported.
func indentEvidence(encoded []byte, remaining *int) (string, error) {
	if len(encoded) == 0 || len(encoded) > 1<<20 || !json.Valid(encoded) {
		return "", errors.New("report evidence is outside the supported bounds")
	}
	depth, projected := 0, len(encoded)
	inString, escaped := false, false
	for _, character := range encoded {
		if inString {
			if escaped {
				escaped = false
			} else if character == '\\' {
				escaped = true
			} else if character == '"' {
				inString = false
			}
			continue
		}
		switch character {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > 64 {
				return "", errors.New("report evidence nesting exceeds the supported limit")
			}
			projected += 1 + 2*depth
		case '}', ']':
			depth--
			projected += 1 + 2*depth
		case ',':
			projected += 1 + 2*depth
		case ':':
			projected++
		}
		if projected > *remaining {
			return "", errors.New("indented report evidence exceeds the supported size")
		}
	}
	if projected > *remaining {
		return "", errors.New("indented report evidence exceeds the supported size")
	}
	var output bytes.Buffer
	if err := json.Indent(&output, encoded, "", "  "); err != nil {
		return "", errors.New("report evidence is invalid")
	}
	*remaining -= projected
	return output.String(), nil
}
