package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
)

const (
	pdfLinesPerPage = 65
	pdfLineWidth    = 100
)

type pdfObject struct {
	id   int
	body []byte
}

func InventoryPDF(payload app.InventoryPayload) ([]byte, error) {
	lines := inventoryPDFLines(payload)
	pageCount := (len(lines) + pdfLinesPerPage - 1) / pdfLinesPerPage
	if pageCount == 0 {
		pageCount = 1
	}
	pageReferences := make([]string, 0, pageCount)
	objects := make([]pdfObject, 0, 3+pageCount*2)
	objects = append(objects,
		pdfObject{id: 1, body: []byte("<< /Type /Catalog /Pages 2 0 R >>")},
		pdfObject{id: 2},
		pdfObject{id: 3, body: []byte("<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>")},
	)
	for page := 0; page < pageCount; page++ {
		pageID := 4 + page*2
		contentID := pageID + 1
		pageReferences = append(pageReferences, fmt.Sprintf("%d 0 R", pageID))
		start := page * pdfLinesPerPage
		end := start + pdfLinesPerPage
		if end > len(lines) {
			end = len(lines)
		}
		content := pdfPageContent(lines[start:end])
		objects = append(objects,
			pdfObject{id: pageID, body: []byte(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", contentID))},
			pdfObject{id: contentID, body: []byte(fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content))},
		)
	}
	objects[1].body = []byte(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(pageReferences, " "), pageCount))

	var output bytes.Buffer
	output.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for _, object := range objects {
		offsets[object.id] = output.Len()
		fmt.Fprintf(&output, "%d 0 obj\n", object.id)
		output.Write(object.body)
		output.WriteString("\nendobj\n")
	}
	xrefOffset := output.Len()
	fmt.Fprintf(&output, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for objectID := 1; objectID <= len(objects); objectID++ {
		fmt.Fprintf(&output, "%010d 00000 n \n", offsets[objectID])
	}
	fmt.Fprintf(&output, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xrefOffset)
	return output.Bytes(), nil
}

func inventoryPDFLines(payload app.InventoryPayload) []string {
	components := append([]app.ComponentRecord(nil), payload.Components...)
	sort.SliceStable(components, func(left, right int) bool {
		if components[left].Type != components[right].Type {
			return components[left].Type < components[right].Type
		}
		return components[left].ID < components[right].ID
	})
	tests := append([]app.TestResult(nil), payload.TestResults...)
	sort.SliceStable(tests, func(left, right int) bool { return tests[left].ID < tests[right].ID })

	lines := []string{
		product.Name + " - Inventory and Functional Test Report",
		"Version: " + product.Version,
		fmt.Sprintf("Inventory schema: %d", payload.SchemaVersion),
		"",
		"SUMMARY",
		fmt.Sprintf("Components: %d", len(components)),
		fmt.Sprintf("Identified parts: %d", payload.SystemFingerprint.IdentifiedParts),
		fmt.Sprintf("Unidentified parts: %d", payload.SystemFingerprint.UnidentifiedParts),
		fmt.Sprintf("Functional test results: %d", len(tests)),
		"",
		"SYSTEM FINGERPRINT",
	}
	lines = appendPDFField(lines, "Algorithm", payload.SystemFingerprint.Algorithm, "  ")
	lines = appendPDFField(lines, "Key epoch", payload.SystemFingerprint.KeyEpoch, "  ")
	lines = appendPDFField(lines, "Fingerprint key ID", payload.SystemFingerprint.KeyID, "  ")
	lines = appendPDFField(lines, "Fingerprint", payload.SystemFingerprint.Value, "  ")
	lines = append(lines, "", "FUNCTIONAL TEST EVIDENCE")
	if len(tests) == 0 {
		lines = append(lines, "No functional test evidence is included.")
	}
	for _, result := range tests {
		lines = append(lines, "")
		lines = appendPDFField(lines, "Test", result.ID, "")
		lines = appendPDFField(lines, "Status", string(result.Status), "  ")
		lines = appendPDFField(lines, "Adapter version", fmt.Sprintf("%d", result.Version), "  ")
		if result.PlanID != "" {
			lines = appendPDFField(lines, "Frozen plan", result.PlanID, "  ")
			lines = appendPDFField(lines, "Planned hardware graph", result.HardwareGraph, "  ")
		}
		var evidence bytes.Buffer
		if json.Compact(&evidence, result.Evidence) != nil {
			evidence.WriteString("Unavailable")
		}
		lines = appendPDFField(lines, "Evidence", evidence.String(), "  ")
	}
	lines = append(lines, "", "COMPONENTS")
	if len(components) == 0 {
		lines = append(lines, "No components reported.")
	}
	for _, component := range components {
		lines = append(lines, "")
		lines = appendPDFField(lines, "Component", string(component.Type)+" - "+component.ID, "")
		lines = appendPDFField(lines, "Fingerprint", component.Identity.Value, "  ")
		lines = appendPDFField(lines, "Algorithm", component.Identity.Algorithm, "  ")
		lines = appendPDFField(lines, "Key epoch", component.Identity.KeyEpoch, "  ")
		lines = appendPDFField(lines, "Identity source", component.Identity.Source, "  ")
		lines = appendPDFField(lines, "Confidence", component.Identity.Confidence, "  ")
		for _, attribute := range component.Attributes {
			lines = appendPDFField(lines, attribute.Name, attribute.Value, "    ")
		}
	}
	return lines
}

func appendPDFField(lines []string, name, value, indent string) []string {
	name = printablePDFText(name)
	value = printablePDFText(value)
	if strings.TrimSpace(value) == "" {
		value = "Unavailable"
	}
	prefix := indent + name + ": "
	if len(prefix) > pdfLineWidth/2 {
		prefix = prefix[:pdfLineWidth/2-4] + "... "
	}
	remaining := prefix + value
	continuation := strings.Repeat(" ", len(prefix))
	for len(remaining) > pdfLineWidth {
		cut := strings.LastIndex(remaining[:pdfLineWidth+1], " ")
		if cut < len(prefix) || cut < pdfLineWidth/2 {
			cut = pdfLineWidth
		}
		lines = append(lines, strings.TrimRight(remaining[:cut], " "))
		remaining = continuation + strings.TrimLeft(remaining[cut:], " ")
	}
	return append(lines, remaining)
}

func printablePDFText(value string) string {
	var result strings.Builder
	result.Grow(len(value))
	for _, character := range value {
		if character >= 0x20 && character <= 0x7e {
			result.WriteRune(character)
		} else {
			result.WriteByte('?')
		}
	}
	return result.String()
}

func pdfPageContent(lines []string) string {
	var content strings.Builder
	content.WriteString("BT\n/F1 8 Tf\n10 TL\n36 756 Td\n")
	for _, line := range lines {
		content.WriteByte('(')
		for index := 0; index < len(line); index++ {
			switch line[index] {
			case '(':
				content.WriteString("\\050")
			case ')':
				content.WriteString("\\051")
			case '\\':
				content.WriteString("\\134")
			case '/':
				content.WriteString("\\057")
			default:
				content.WriteByte(line[index])
			}
		}
		content.WriteString(") Tj\nT*\n")
	}
	content.WriteString("ET\n")
	return content.String()
}
