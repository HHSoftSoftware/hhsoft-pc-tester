package inventory

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

var (
	soundCardPattern    = regexp.MustCompile(`^card([0-9]+)$`)
	soundNodePattern    = regexp.MustCompile(`^(?:control|hw|pcm)C([0-9]+)`)
	inputNodePattern    = regexp.MustCompile(`^input[0-9]+$`)
	eventNodePattern    = regexp.MustCompile(`^event[0-9]+$`)
	drmCardPattern      = regexp.MustCompile(`^card[0-9]+$`)
	drmConnectorPattern = regexp.MustCompile(`^card[0-9]+-.+$`)
	drmRenderPattern    = regexp.MustCompile(`^renderD[0-9]+$`)
	framebufferPattern  = regexp.MustCompile(`^fb[0-9]+$`)
)

func (collector *LinuxCollector) collectInput() []Component {
	inputDevices := path.Join(collector.Paths.Proc, "bus/input/devices")
	if data, ok := collector.readFile(inputDevices); ok {
		if components := parseInputDevices(string(data)); len(components) > 0 {
			return components
		}
	}

	inputRoot := path.Join(collector.Paths.Sys, "class/input")
	entries := collector.readDir(inputRoot)
	useEvents := false
	for _, entry := range entries {
		if eventNodePattern.MatchString(entry.Name()) {
			useEvents = true
			break
		}
	}
	var components []Component
	for _, entry := range entries {
		name := entry.Name()
		if useEvents && !eventNodePattern.MatchString(name) {
			continue
		}
		if !useEvents && !inputNodePattern.MatchString(name) {
			continue
		}
		attributes := map[string]string{"interface": name}
		collector.addAttributes(attributes, path.Join(inputRoot, name, "device"), []attributePath{
			{name: "name", relativePath: "name"},
			{name: "physical_location", relativePath: "phys"},
			{name: "unique_id", relativePath: "uniq"},
			{name: "bus", relativePath: "id/bustype"},
			{name: "vendor_id", relativePath: "id/vendor"},
			{name: "product_id", relativePath: "id/product"},
			{name: "version", relativePath: "id/version"},
			{name: "event_capabilities", relativePath: "capabilities/ev"},
			{name: "key_capabilities", relativePath: "capabilities/key"},
			{name: "absolute_capabilities", relativePath: "capabilities/abs"},
			{name: "relative_capabilities", relativePath: "capabilities/rel"},
			{name: "properties", relativePath: "properties"},
		})
		attributes["category"] = ClassifyInput(
			attributes["name"], attributes["handlers"], attributes["key_capabilities"],
			firstNonEmpty(attributes["relative_capabilities"], attributes["rel_capabilities"]),
			firstNonEmpty(attributes["absolute_capabilities"], attributes["abs_capabilities"]),
			firstNonEmpty(attributes["properties"], attributes["prop_capabilities"]),
		)
		components = append(components, NewComponent(ComponentInput, "input:"+name, attributes))
	}
	return components
}

func parseInputDevices(content string) []Component {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	var components []Component
	for recordIndex, block := range strings.Split(content, "\n\n") {
		attributes := make(map[string]string)
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "I:"):
				for _, field := range strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "I:"))) {
					name, value, found := strings.Cut(field, "=")
					if found {
						attributes[strings.ToLower(name)] = value
					}
				}
			case strings.HasPrefix(line, "N: Name="):
				attributes["name"] = strings.Trim(strings.TrimPrefix(line, "N: Name="), "\"")
			case strings.HasPrefix(line, "P: Phys="):
				attributes["physical_location"] = strings.TrimPrefix(line, "P: Phys=")
			case strings.HasPrefix(line, "S: Sysfs="):
				attributes["kernel_path"] = strings.TrimPrefix(line, "S: Sysfs=")
			case strings.HasPrefix(line, "U: Uniq="):
				attributes["unique_id"] = strings.TrimPrefix(line, "U: Uniq=")
			case strings.HasPrefix(line, "H: Handlers="):
				attributes["handlers"] = strings.TrimPrefix(line, "H: Handlers=")
			case strings.HasPrefix(line, "B:"):
				name, value, found := strings.Cut(strings.TrimSpace(strings.TrimPrefix(line, "B:")), "=")
				if found {
					attributes[strings.ToLower(name)+"_capabilities"] = value
				}
			}
		}
		if len(attributes) == 0 {
			continue
		}
		attributes["category"] = ClassifyInput(attributes["name"], attributes["handlers"], attributes["key_capabilities"], attributes["rel_capabilities"], attributes["abs_capabilities"], attributes["prop_capabilities"])
		components = append(components, NewComponent(ComponentInput, "input:"+strconv.Itoa(recordIndex), attributes))
	}
	return components
}

func ClassifyInput(name, handlers, keyCapabilities, relativeCapabilities, absoluteCapabilities, properties string) string {
	identity := strings.ToLower(name + " " + handlers)
	switch {
	case strings.Contains(identity, "touchpad"):
		return "touchpad"
	case strings.Contains(identity, "trackpad"):
		return "touchpad"
	case strings.Contains(identity, "touchscreen") || strings.Contains(identity, "touch screen"):
		return "touchscreen"
	case absoluteCapabilities != "" && inputCapabilityBit(properties, 1):
		return "touchscreen"
	case absoluteCapabilities != "" && (inputCapabilityBit(properties, 2) || inputCapabilityBit(properties, 3) || inputCapabilityBit(properties, 4)):
		return "touchpad"
	case strings.Contains(identity, "mouse") || strings.Contains(identity, "trackball") || strings.Contains(identity, "pointing") || handlerHasPrefix(handlers, "mouse"):
		return "pointer"
	case strings.Contains(identity, "keyboard"):
		return "keyboard"
	case handlerHas(handlers, "kbd") && keyboardKeyMap(keyCapabilities):
		return "keyboard"
	case strings.Contains(identity, "joystick") || strings.Contains(identity, "gamepad"):
		return "controller"
	case strings.Contains(identity, "tablet"):
		return "tablet"
	case relativeCapabilities != "":
		return "pointer"
	case absoluteCapabilities != "":
		return "input"
	default:
		return "input"
	}
}

func keyboardKeyMap(value string) bool {
	matches := 0
	for _, code := range []int{2, 16, 28, 30, 44, 57} {
		if inputCapabilityBit(value, code) {
			matches++
		}
	}
	return matches >= 2
}

func inputCapabilityBit(value string, code int) bool {
	words := strings.Fields(value)
	wordIndex := code / strconv.IntSize
	if wordIndex >= len(words) {
		return false
	}
	word, err := strconv.ParseUint(words[len(words)-1-wordIndex], 16, strconv.IntSize)
	return err == nil && word&(1<<uint(code%strconv.IntSize)) != 0
}

func handlerHas(handlers, wanted string) bool {
	for _, handler := range strings.Fields(handlers) {
		if handler == wanted {
			return true
		}
	}
	return false
}

func handlerHasPrefix(handlers, wanted string) bool {
	for _, handler := range strings.Fields(handlers) {
		if strings.HasPrefix(handler, wanted) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (collector *LinuxCollector) collectSound() []Component {
	soundRoot := path.Join(collector.Paths.Sys, "class/sound")
	cards := make(map[string]map[string]string)
	for _, entry := range collector.readDir(soundRoot) {
		name := entry.Name()
		cardNumber := ""
		if match := soundCardPattern.FindStringSubmatch(name); match != nil {
			cardNumber = match[1]
		} else if match := soundNodePattern.FindStringSubmatch(name); match != nil {
			cardNumber = match[1]
		}
		if cardNumber == "" {
			continue
		}
		attributes := cards[cardNumber]
		if attributes == nil {
			attributes = map[string]string{"card_number": cardNumber}
			cards[cardNumber] = attributes
		}
		if existing := attributes["interfaces"]; existing == "" {
			attributes["interfaces"] = name
		} else {
			attributes["interfaces"] = existing + " " + name
		}
		if soundCardPattern.MatchString(name) {
			collector.addAttributes(attributes, path.Join(soundRoot, name), []attributePath{
				{name: "id", relativePath: "id"},
				{name: "vendor_id", relativePath: "device/vendor"},
				{name: "device_id", relativePath: "device/device"},
			})
		}
	}
	components := make([]Component, 0, len(cards))
	for cardNumber, attributes := range cards {
		components = append(components, NewComponent(ComponentSound, "sound:card"+cardNumber, attributes))
	}
	return components
}

func (collector *LinuxCollector) collectVideo() []Component {
	var components []Component
	drmRoot := path.Join(collector.Paths.Sys, "class/drm")
	for _, entry := range collector.readDir(drmRoot) {
		name := entry.Name()
		category := ""
		switch {
		case drmCardPattern.MatchString(name):
			category = "display_controller"
		case drmConnectorPattern.MatchString(name):
			category = "display_connector"
		case drmRenderPattern.MatchString(name):
			category = "render_interface"
		}
		if category == "" {
			continue
		}
		base := path.Join(drmRoot, name)
		attributes := map[string]string{"interface": name, "category": category}
		collector.addAttributes(attributes, base, []attributePath{
			{name: "device_number", relativePath: "dev"},
			{name: "status", relativePath: "status"},
			{name: "enabled", relativePath: "enabled"},
			{name: "vendor_id", relativePath: "device/vendor"},
			{name: "device_id", relativePath: "device/device"},
			{name: "subsystem_vendor_id", relativePath: "device/subsystem_vendor"},
			{name: "subsystem_device_id", relativePath: "device/subsystem_device"},
		})
		components = append(components, NewComponent(ComponentVideo, "video:drm:"+name, attributes))
	}

	videoRoot := path.Join(collector.Paths.Sys, "class/video4linux")
	for _, entry := range collector.readDir(videoRoot) {
		name := entry.Name()
		attributes := map[string]string{"interface": name, "category": "video_capture"}
		collector.addAttributes(attributes, path.Join(videoRoot, name), []attributePath{
			{name: "name", relativePath: "name"},
			{name: "index", relativePath: "index"},
			{name: "device_number", relativePath: "dev"},
			{name: "modalias", relativePath: "device/modalias"},
		})
		components = append(components, NewComponent(ComponentVideo, "video:capture:"+name, attributes))
	}

	graphicsRoot := path.Join(collector.Paths.Sys, "class/graphics")
	for _, entry := range collector.readDir(graphicsRoot) {
		name := entry.Name()
		if !framebufferPattern.MatchString(name) {
			continue
		}
		attributes := map[string]string{"interface": name, "category": "framebuffer"}
		collector.addAttributes(attributes, path.Join(graphicsRoot, name), []attributePath{
			{name: "name", relativePath: "name"},
			{name: "virtual_size", relativePath: "virtual_size"},
			{name: "bits_per_pixel", relativePath: "bits_per_pixel"},
			{name: "device_number", relativePath: "dev"},
		})
		components = append(components, NewComponent(ComponentVideo, "video:framebuffer:"+name, attributes))
	}
	return components
}
