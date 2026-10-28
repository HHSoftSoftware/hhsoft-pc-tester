package storageevidence

import (
	"strconv"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
)

// ValidateInventoryBinding checks every passed device, including an accepted
// pass in a non-pass run, against the available final inventory. It cannot
// prove continuity across same-number/capacity replacement or producer timing.
func ValidateInventoryBinding(result storagetest.Result, components []inventory.Component) error {
	if err := ValidateResult(result); err != nil {
		return err
	}
	selected := map[string]storagetest.Identity{}
	for _, device := range result.Devices {
		if device.Status == "pass" {
			selected["block:"+device.Interface] = device.Observation.Identity
		}
	}
	seen := map[string]bool{}
	for _, component := range components {
		identity, wanted := selected[component.ID]
		if !wanted {
			continue
		}
		if seen[component.ID] || component.Type != inventory.ComponentBlock {
			return ErrInvalid
		}
		seen[component.ID] = true
		attributes := map[string]string{}
		for _, attribute := range component.Attributes {
			if _, exists := attributes[attribute.Name]; exists {
				return ErrInvalid
			}
			attributes[attribute.Name] = attribute.Value
		}
		number := strconv.FormatUint(uint64(identity.Major), 10) + ":" + strconv.FormatUint(uint64(identity.Minor), 10)
		if attributes["device_number"] != number {
			return ErrInvalid
		}
		geometry, err := storagetest.ParseGeometry([]byte(attributes["size_sectors"]), []byte(attributes["logical_block_size"]))
		if err != nil || geometry.CapacityBytes != identity.CapacityBytes || geometry.LogicalBlockBytes != identity.LogicalBlockBytes || attributes["size_sectors"] != strconv.FormatUint(geometry.CapacityBytes/512, 10) || attributes["logical_block_size"] != strconv.FormatUint(uint64(geometry.LogicalBlockBytes), 10) {
			return ErrInvalid
		}
	}
	if len(seen) != len(selected) {
		return ErrInvalid
	}
	return nil
}
