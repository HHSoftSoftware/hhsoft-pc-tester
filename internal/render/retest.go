package render

import (
	"bytes"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

// These views show a signed reference. Local verification/trust decisions never
// enter the archived text, and legacy renderer entry points keep their bytes.
func InventoryRetestHTMLForVersion(payload app.InventoryPayload, version string, previous report.PreviousReport) ([]byte, error) {
	var output bytes.Buffer
	if err := renderInventoryHTMLWithPrevious(&output, payload, version, &previous); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func InventoryRetestPDFForVersion(payload app.InventoryPayload, version string, previous report.PreviousReport) ([]byte, error) {
	return inventoryPDFWithPrevious(payload, version, &previous)
}

// These explicit entry points belong to the authenticated capacity view profile.
// Legacy entry points above retain their complete historical output bytes.
func InventoryMemoryCapacityHTMLForVersion(payload app.InventoryPayload, version string) ([]byte, error) {
	var output bytes.Buffer
	if err := renderInventoryHTMLWithCapacity(&output, payload, version, nil, true); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func InventoryMemoryCapacityPDFForVersion(payload app.InventoryPayload, version string) ([]byte, error) {
	return inventoryPDFWithCapacity(payload, version, nil, true)
}

func InventoryMemoryCapacityRetestHTMLForVersion(payload app.InventoryPayload, version string, previous report.PreviousReport) ([]byte, error) {
	var output bytes.Buffer
	if err := renderInventoryHTMLWithCapacity(&output, payload, version, &previous, true); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func InventoryMemoryCapacityRetestPDFForVersion(payload app.InventoryPayload, version string, previous report.PreviousReport) ([]byte, error) {
	return inventoryPDFWithCapacity(payload, version, &previous, true)
}
