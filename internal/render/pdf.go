package render

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
)

const (
	pdfLinesPerPage = 65
	pdfLineWidth    = 100
	maximumPDFLines = maximumDocumentBytes / pdfLineWidth
)

type pdfObject struct {
	id   int
	body []byte
}

func InventoryPDF(payload app.InventoryPayload) ([]byte, error) {
	return InventoryPDFForVersion(payload, product.Version)
}

// InventoryPDFForVersion reproduces the fixed text-only view for the signed
// software version. It does not accept external PDF objects or attachments.
func InventoryPDFForVersion(payload app.InventoryPayload, version string) ([]byte, error) {
	if err := validateVersion(version); err != nil {
		return nil, err
	}
	lines, err := inventoryPDFLines(payload, version)
	if err != nil {
		return nil, err
	}
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
	objectBytes := 0
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
		if len(content) > maximumDocumentBytes-objectBytes {
			return nil, errors.New("rendered PDF exceeds the supported size")
		}
		objectBytes += len(content)
		objects = append(objects,
			pdfObject{id: pageID, body: []byte(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", contentID))},
			pdfObject{id: contentID, body: []byte(fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content))},
		)
	}
	objects[1].body = []byte(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(pageReferences, " "), pageCount))

	var output limitedBuffer
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
	if output.err != nil {
		return nil, output.err
	}
	return output.Bytes(), nil
}

func inventoryPDFLines(payload app.InventoryPayload, version string) ([]string, error) {
	components := append([]app.ComponentRecord(nil), payload.Components...)
	sort.SliceStable(components, func(left, right int) bool {
		if components[left].Type != components[right].Type {
			return components[left].Type < components[right].Type
		}
		return components[left].ID < components[right].ID
	})
	tests := append([]app.TestResult(nil), payload.TestResults...)
	sort.SliceStable(tests, func(left, right int) bool { return tests[left].ID < tests[right].ID })

	var lines pdfLines
	lines.add(
		printablePDFText(product.Name)+" - Inventory and Functional Test Report",
		"Version: "+printablePDFText(version),
		fmt.Sprintf("Inventory schema: %d", payload.SchemaVersion),
		"",
		"SUMMARY",
		fmt.Sprintf("Components: %d", len(components)),
		fmt.Sprintf("Identified parts: %d", payload.SystemFingerprint.IdentifiedParts),
		fmt.Sprintf("Unidentified parts: %d", payload.SystemFingerprint.UnidentifiedParts),
		fmt.Sprintf("Functional test results: %d", len(tests)),
		"",
		"SYSTEM FINGERPRINT",
	)
	lines.field("Algorithm", payload.SystemFingerprint.Algorithm, "  ")
	lines.field("Key epoch", payload.SystemFingerprint.KeyEpoch, "  ")
	lines.field("Fingerprint key ID", payload.SystemFingerprint.KeyID, "  ")
	lines.field("Fingerprint", payload.SystemFingerprint.Value, "  ")
	lines.add("", "FUNCTIONAL TEST EVIDENCE")
	if len(tests) == 0 {
		lines.add("No functional test evidence is included.")
	}
	for _, result := range tests {
		lines.add("")
		lines.field("Test", result.ID, "")
		lines.field("Status", string(result.Status), "  ")
		lines.field("Adapter version", fmt.Sprintf("%d", result.Version), "  ")
		if result.PlanID != "" {
			lines.field("Frozen plan", result.PlanID, "  ")
			lines.field("Planned hardware graph", result.HardwareGraph, "  ")
		}
		if len(result.Evidence) == 0 || len(result.Evidence) > 1<<20 {
			return nil, errors.New("report evidence is outside the supported bounds")
		}
		var evidence bytes.Buffer
		if json.Compact(&evidence, result.Evidence) != nil {
			return nil, errors.New("report evidence is invalid")
		}
		lines.field("Evidence", evidence.String(), "  ")
		if lines.err != nil {
			return nil, lines.err
		}
	}
	lines.add("", "COMPONENTS")
	if len(components) == 0 {
		lines.add("No components reported.")
	}
	for _, component := range components {
		lines.add("")
		lines.field("Component", string(component.Type)+" - "+component.ID, "")
		lines.field("Fingerprint", component.Identity.Value, "  ")
		lines.field("Algorithm", component.Identity.Algorithm, "  ")
		lines.field("Key epoch", component.Identity.KeyEpoch, "  ")
		lines.field("Identity source", component.Identity.Source, "  ")
		lines.field("Confidence", component.Identity.Confidence, "  ")
		for _, attribute := range component.Attributes {
			lines.field(attribute.Name, attribute.Value, "    ")
			if lines.err != nil {
				return nil, lines.err
			}
		}
		if lines.err != nil {
			return nil, lines.err
		}
	}
	return lines.values, lines.err
}

type pdfLines struct {
	values       []string
	encodedBytes int
	err          error
}

func (lines *pdfLines) add(values ...string) {
	if lines.err != nil {
		return
	}
	for _, value := range values {
		encoded := len(value) + len("() Tj\nT*\n")
		for _, character := range value {
			if character == '(' || character == ')' || character == '\\' || character == '/' {
				encoded += 3
			}
		}
		if len(lines.values) >= maximumPDFLines || encoded > maximumDocumentBytes-lines.encodedBytes {
			lines.err = errors.New("PDF line content exceeds the supported bounds")
			return
		}
		lines.values = append(lines.values, value)
		lines.encodedBytes += encoded
	}
}

func (lines *pdfLines) field(name, value, indent string) {
	if lines.err != nil {
		return
	}
	if len(name) > maximumDocumentBytes || len(value) > maximumDocumentBytes {
		lines.err = errors.New("PDF field exceeds the supported bounds")
		return
	}
	name = printablePDFText(name)
	value = printablePDFText(value)
	if strings.TrimSpace(value) == "" {
		value = "Unavailable"
	}
	prefix := indent + name + ": "
	if len(prefix) > pdfLineWidth/2 {
		prefix = prefix[:pdfLineWidth/2-4] + "... "
	}
	remaining := value
	leader := prefix
	continuation := strings.Repeat(" ", len(prefix))
	for len(leader)+len(remaining) > pdfLineWidth {
		available := pdfLineWidth - len(leader)
		cut := strings.LastIndex(remaining[:available+1], " ")
		if cut < 0 || len(leader)+cut < pdfLineWidth/2 {
			cut = available
		}
		lines.add(leader + strings.TrimRight(remaining[:cut], " "))
		if lines.err != nil {
			return
		}
		// Retain a slice of the remaining input, not a new copy of the entire
		// suffix on every line. Verification can receive long untrusted fields.
		remaining = strings.TrimLeft(remaining[cut:], " ")
		leader = continuation
	}
	lines.add(leader + remaining)
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
