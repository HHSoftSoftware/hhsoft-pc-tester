package inventory

import (
	"encoding/binary"
	"path"
	"strconv"
)

type firmwareSecurityField struct {
	input, name string
	variable    bool
}

var firmwareSecurityFields = [...]firmwareSecurityField{
	{"firmware/efi/fw_platform_size", "uefi_platform_bits", false},
	{"firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c", "secure_boot", true},
	{"firmware/efi/efivars/SetupMode-8be4df61-93ca-11d2-aa0d-00e098032b8c", "setup_mode_flag", true},
}

func (collector *linuxCollection) collectFirmwareSecurity() []Component {
	var components []Component
	attributes := make(map[string]string, 6)
	anyObserved := false
	for _, field := range collectionItems(collector, firmwareSecurityFields[:]) {
		data, observed := collector.readScalar(path.Join(collector.Paths.Sys, field.input))
		if !collector.check() {
			return nil
		}
		value, state := "", "unavailable"
		if observed {
			anyObserved = true
			value, state = parseFirmwareSecurityScalar(field, data)
		}
		attributes[field.name+"_state"] = state
		if state == "reported" {
			attributes[field.name] = value
		}
	}
	if !collector.check() {
		return nil
	}
	if anyObserved {
		components = append(components, NewComponent(ComponentFirmwareSecurity, "firmware_security:uefi", attributes))
	}
	root := path.Join(collector.Paths.Sys, "class/tpm")
	for index, entry := range collectionItems(collector, collector.readDir(root)) {
		data, observed := collector.readScalar(path.Join(root, entry.Name(), "tpm_version_major"))
		if !collector.check() {
			return nil
		}
		value, state := "", "unavailable"
		if observed {
			value, state = parseFirmwareSecurityScalar(firmwareSecurityField{name: "version_major"}, data)
		}
		attributes := map[string]string{"version_major_state": state}
		if state == "reported" {
			attributes["version_major"] = value
		}
		// Class names are private implementation details, not persistent identity.
		components = append(components, NewComponent(ComponentTPM, "tpm:"+strconv.Itoa(index+1), attributes))
	}
	if !collector.check() {
		return nil
	}
	return components
}

// Decode completed raw bytes before generic attribute normalization. These
// flags describe reported firmware state; they do not attest platform health.
func parseFirmwareSecurityScalar(field firmwareSecurityField, data []byte) (string, string) {
	if field.variable {
		if len(data) != 5 {
			return "", "malformed"
		}
		if binary.LittleEndian.Uint32(data[:4]) != 6 || data[4] > 1 {
			return "", "unsupported"
		}
		return strconv.Itoa(int(data[4])), "reported"
	}
	if len(data) > 0 && data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}
	if len(data) == 0 || len(data) > 1 && data[0] == '0' {
		return "", "malformed"
	}
	for _, digit := range data {
		if digit < '0' || digit > '9' {
			return "", "malformed"
		}
	}
	value := string(data)
	if field.name == "uefi_platform_bits" && (value == "32" || value == "64") ||
		field.name == "version_major" && (value == "1" || value == "2") {
		return value, "reported"
	}
	return "", "unsupported"
}
