//go:build linux

package inputtest

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unsafe"

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
	result := Result{Schema: Schema, Status: StatusError, Mode: "readiness"}
	if ctx.Err() != nil {
		result.Status = StatusAborted
		result.Detail = "cancelled"
		return result
	}
	if options.Duration < 0 || options.Duration > 5*time.Minute {
		result.Detail = "duration must be between 0 and 5m"
		return result
	}
	if options.Duration > 0 {
		result.Mode = "activity"
	}
	if options.Root == "" {
		options.Root = "/"
	}
	automaticCategories := len(options.Categories) == 0
	categories, err := requestedCategories(options.Categories)
	if err != nil {
		result.Detail = err.Error()
		return result
	}
	result.RequestedCategories = categories
	procPath := filepath.Join(filepath.Clean(options.Root), "proc/bus/input/devices")
	devicesFile, err := os.Open(procPath)
	if err != nil {
		result.Detail = "cannot read input device registry"
		return result
	}
	devices, err := parseDevices(devicesFile, categories)
	_ = devicesFile.Close()
	if err != nil {
		result.Detail = err.Error()
		return result
	}
	if len(devices) == 0 {
		result.Status = StatusInconclusive
		result.Detail = "no requested input event devices were discovered"
		return result
	}
	if automaticCategories {
		categories = deviceCategories(devices)
		result.RequestedCategories = categories
	}

	openedFiles := make([]*os.File, 0, len(devices))
	openedIndexes := make([]int, 0, len(devices))
	defer func() {
		for _, file := range openedFiles {
			_ = file.Close()
		}
	}()
	deviceRoot := filepath.Join(filepath.Clean(options.Root), "dev/input")
	for _, device := range devices {
		if ctx.Err() != nil {
			result.Status = StatusAborted
			result.Detail = "cancelled"
			return result
		}
		entry := DeviceResult{Interface: device.interfaceName, Category: device.category, Name: device.name}
		result.Devices = append(result.Devices, entry)
		devicePath := filepath.Join(deviceRoot, device.interfaceName)
		metadata, lstatErr := os.Lstat(devicePath)
		if lstatErr != nil || metadata.Mode()&os.ModeCharDevice == 0 || metadata.Mode()&os.ModeSymlink != 0 {
			continue
		}
		file, openErr := os.OpenFile(devicePath, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		if openErr != nil {
			continue
		}
		result.Devices[len(result.Devices)-1].Readable = true
		openedFiles = append(openedFiles, file)
		openedIndexes = append(openedIndexes, len(result.Devices)-1)
	}

	result.ObservedCategories = readableCategories(result.Devices)
	if len(openedFiles) != len(result.Devices) || !hasEveryCategory(result.ObservedCategories, categories) {
		result.Status = StatusInconclusive
		result.Detail = "one or more requested input categories are absent or unreadable"
		return result
	}
	if options.Duration == 0 {
		if ctx.Err() != nil {
			result.Status = StatusAborted
			result.Detail = "cancelled"
			return result
		}
		result.Status = StatusPass
		return result
	}

	started := time.Now()
	deadline := started.Add(options.Duration)
	eventSize := int(unsafe.Sizeof(inputEvent{}))
	eventTypeOffset := int(unsafe.Offsetof(inputEvent{}.Type))
	buffers := make([][]byte, len(openedFiles))
	readBuffers := make([][]byte, len(openedFiles))
	for fileIndex := range readBuffers {
		readBuffers[fileIndex] = make([]byte, 4096)
	}
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			result.Status = StatusAborted
			result.Detail = "cancelled"
			result.DurationMilliseconds = time.Since(started).Milliseconds()
			return result
		default:
		}
		for fileIndex, file := range openedFiles {
			buffer := readBuffers[fileIndex]
			count, readErr := syscall.Read(int(file.Fd()), buffer)
			if count > 0 {
				buffers[fileIndex] = append(buffers[fileIndex], buffer[:count]...)
				complete := len(buffers[fileIndex]) / eventSize * eventSize
				for offset := 0; offset < complete; offset += eventSize {
					record := buffers[fileIndex][offset : offset+eventSize]
					event := inputEvent{
						Type:  binary.LittleEndian.Uint16(record[eventTypeOffset:]),
						Code:  binary.LittleEndian.Uint16(record[eventTypeOffset+2:]),
						Value: int32(binary.LittleEndian.Uint32(record[eventTypeOffset+4:])),
					}
					if accountEvent(&result.Devices[openedIndexes[fileIndex]], event) {
						result.Status = StatusInconclusive
						result.Detail = "input event stream lost records"
						result.DurationMilliseconds = time.Since(started).Milliseconds()
						return result
					}
				}
				buffers[fileIndex] = append(buffers[fileIndex][:0], buffers[fileIndex][complete:]...)
			}
			if readErr != nil && !errors.Is(readErr, syscall.EAGAIN) && !errors.Is(readErr, syscall.EINTR) {
				result.Status = StatusInconclusive
				result.Detail = "input event stream became unreadable"
				result.DurationMilliseconds = time.Since(started).Milliseconds()
				return result
			}
		}
		if activityComplete(result.Devices, categories) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	result.DurationMilliseconds = time.Since(started).Milliseconds()
	result.ObservedCategories = activeCategories(result.Devices)
	if activityComplete(result.Devices, categories) {
		result.Status = StatusPass
		return result
	}
	result.Status = StatusInconclusive
	result.Detail = "not every requested input category produced activity"
	return result
}

func parseDevices(source io.Reader, categories []string) ([]discoveredDevice, error) {
	wanted := make(map[string]bool, len(categories))
	for _, category := range categories {
		wanted[category] = true
	}
	var devices []discoveredDevice
	name, handlers := "", ""
	keyCapabilities, relativeCapabilities, absoluteCapabilities, properties := "", "", "", ""
	seenInterfaces := make(map[string]bool)
	registryError := ""
	flush := func() {
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
	scanner := bufio.NewScanner(source)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
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
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if registryError != "" {
		return nil, errors.New(registryError)
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

func activeCategories(devices []DeviceResult) []string {
	return categoryList(devices, qualifyingActivity)
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
