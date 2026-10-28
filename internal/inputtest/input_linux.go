//go:build linux

package inputtest

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"unicode"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

const maximumInputDevices = 128

var eventInterfacePattern = regexp.MustCompile(`^event[0-9]+$`)

type inputEvent struct {
	Time  syscall.Timeval
	Type  uint16
	Code  uint16
	Value int32
}

type discoveredDevice struct {
	interfaceName string
	category      string
	name          string
}

func runPlatform(ctx context.Context, options Options) Result {
	return runInputWithOperations(ctx, options, systemInputReadinessOperations())
}

func parseInputRegistry(ctx context.Context, data []byte, categories []string) ([]discoveredDevice, error) {
	if nilInputValue(ctx) || len(data) > maximumInputRegistryBytes {
		return nil, errInputRead
	}

	wanted := make(map[string]bool, len(categories))
	for _, category := range categories {
		wanted[category] = true
	}
	var devices []discoveredDevice
	name, handlers := "", ""
	keyCapabilities, relativeCapabilities, absoluteCapabilities, properties := "", "", "", ""
	seenInterfaces := make(map[string]bool)
	registryError := ""
	records, lines := 0, 0
	insideRecord := false
	flush := func() {
		if !insideRecord {
			return
		}
		insideRecord = false
		records++
		if records > maximumInputRegistryRecords {
			registryError = "input device registry exceeds the supported record limit"
			return
		}

		category := inventory.ClassifyInput(name, handlers, keyCapabilities, relativeCapabilities, absoluteCapabilities, properties)
		if !wanted[category] {
			name, handlers, keyCapabilities, relativeCapabilities, absoluteCapabilities, properties = "", "", "", "", "", ""
			return
		}
		for _, handler := range strings.Fields(handlers) {
			if !eventInterfacePattern.MatchString(handler) {
				continue
			}
			if seenInterfaces[handler] {
				registryError = "input device registry contains a duplicate event interface"
				break
			}
			if len(devices) >= maximumInputDevices {
				registryError = "input device registry exceeds the supported limit"
				break
			}
			seenInterfaces[handler] = true
			if len(devices) < maximumInputDevices {
				devices = append(devices, discoveredDevice{interfaceName: handler, category: category, name: sanitizeName(name)})
			}
			break
		}
		name, handlers, keyCapabilities, relativeCapabilities, absoluteCapabilities, properties = "", "", "", "", "", ""
	}
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := bytes.IndexByte(data, '\n')
		if end < 0 {
			end = len(data)
		}
		if end > maximumInputRegistryLineBytes {
			return nil, errInputRead
		}
		line := strings.TrimSuffix(string(data[:end]), "\r")
		if end < len(data) {
			data = data[end+1:]
		} else {
			data = nil
		}
		lines++
		if lines > maximumInputRegistryLines {
			return nil, errInputRead
		}
		if line == "" {
			flush()
			if registryError != "" {
				return nil, errors.New(registryError)
			}
			continue
		}
		insideRecord = true
		if strings.HasPrefix(line, "N: Name=") {
			name = strings.Trim(strings.TrimPrefix(line, "N: Name="), "\"")
		}
		if strings.HasPrefix(line, "H: Handlers=") {
			handlers = strings.TrimSpace(strings.TrimPrefix(line, "H: Handlers="))
		}
		if strings.HasPrefix(line, "B: KEY=") {
			keyCapabilities = strings.TrimSpace(strings.TrimPrefix(line, "B: KEY="))
		}
		if strings.HasPrefix(line, "B: REL=") {
			relativeCapabilities = strings.TrimSpace(strings.TrimPrefix(line, "B: REL="))
		}
		if strings.HasPrefix(line, "B: ABS=") {
			absoluteCapabilities = strings.TrimSpace(strings.TrimPrefix(line, "B: ABS="))
		}
		if strings.HasPrefix(line, "B: PROP=") {
			properties = strings.TrimSpace(strings.TrimPrefix(line, "B: PROP="))
		}
	}
	flush()
	if registryError != "" {
		return nil, errors.New(registryError)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].interfaceName < devices[j].interfaceName })
	return devices, nil
}

func requestedCategories(categories []string) ([]string, error) {
	if len(categories) == 0 {
		categories = []string{"keyboard", "pointer", "touchpad", "touchscreen"}
	}
	wanted := make(map[string]bool)
	for _, category := range categories {
		category = strings.ToLower(strings.TrimSpace(category))
		switch category {
		case "keyboard", "pointer", "touchpad", "touchscreen":
			wanted[category] = true
		default:
			return nil, errors.New("unknown input category")
		}
	}
	result := make([]string, 0, len(wanted))
	for category := range wanted {
		result = append(result, category)
	}
	sort.Strings(result)
	return result, nil
}

func sanitizeName(name string) string {
	name = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, strings.TrimSpace(name))
	if len(name) > 128 {
		name = name[:128]
	}
	return name
}

func accountEvent(result *DeviceResult, event inputEvent) bool {
	if event.Type == 0 {
		if event.Code == 3 {
			result.EvidenceLoss = true
			return true
		}
		return false
	}
	switch event.Type {
	case 1:
		if event.Value == 0 {
			result.Events++
			result.KeyReleases++
		} else if event.Value == 1 {
			result.Events++
			result.KeyPresses++
		}
	case 2:
		if event.Value != 0 {
			result.Events++
			result.RelativeEvents++
		}
	case 3:
		result.Events++
		result.AbsoluteEvents++
	case 5:
		if event.Value == 0 || event.Value == 1 {
			result.Events++
			result.SwitchEvents++
		}
	}
	return false
}

func activityComplete(devices []DeviceResult, categories []string) bool {
	observed := make(map[string]bool)
	for _, device := range devices {
		if qualifyingActivity(device) {
			observed[device.Category] = true
		}
	}
	for _, category := range categories {
		if !observed[category] {
			return false
		}
	}
	return true
}

func qualifyingActivity(device DeviceResult) bool {
	switch device.Category {
	case "keyboard":
		return device.KeyPresses > 0 && device.KeyReleases > 0
	case "pointer":
		return device.RelativeEvents > 0 || device.AbsoluteEvents > 0 || device.KeyPresses > 0
	case "touchpad", "touchscreen":
		return device.AbsoluteEvents > 0
	default:
		return false
	}
}

func hasEveryCategory(available, requested []string) bool {
	present := make(map[string]bool, len(available))
	for _, category := range available {
		present[category] = true
	}
	for _, category := range requested {
		if !present[category] {
			return false
		}
	}
	return true
}

func readableCategories(devices []DeviceResult) []string {
	return categoryList(devices, func(device DeviceResult) bool { return device.Readable })
}

func categoryList(devices []DeviceResult, include func(DeviceResult) bool) []string {
	wanted := make(map[string]bool)
	for _, device := range devices {
		if include(device) {
			wanted[device.Category] = true
		}
	}
	result := make([]string, 0, len(wanted))
	for category := range wanted {
		result = append(result, category)
	}
	sort.Strings(result)
	return result
}

func deviceCategories(devices []discoveredDevice) []string {
	wanted := make(map[string]bool)
	for _, device := range devices {
		wanted[device.category] = true
	}
	result := make([]string, 0, len(wanted))
	for category := range wanted {
		result = append(result, category)
	}
	sort.Strings(result)
	return result
}
