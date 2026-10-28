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

func (collector *linuxCollection) collectInputDevices() []Component {
	inputDevices := path.Join(collector.Paths.Proc, "bus/input/devices")
	if data, ok := collector.readProc(inputDevices, procInput); ok {
		if components := collector.parseInputDevices(string(data)); len(components) > 0 {
			return components
		}
	}

	inputRoot := path.Join(collector.Paths.Sys, "class/input")
	entries := collector.readDir(inputRoot)
	useEvents := false
	for _, entry := range collectionItems(collector, entries) {
		if !collector.check() {
			return nil
		}
		if eventNodePattern.MatchString(entry.Name()) {
			useEvents = true
			break
		}
	}
	var components []Component
	for _, entry := range collectionItems(collector, entries) {
		if !collector.check() {
			return nil
		}
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
		if !collector.check() {
			return nil
		}
		components = append(components, NewComponent(ComponentInput, "input:"+name, attributes))
	}
	return components
}

func (collector *linuxCollection) parseInputDevices(content string) []Component {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	var components []Component
	for recordIndex, block := range collectionItems(collector, strings.Split(content, "\n\n")) {
		if !collector.check() {
			return nil
		}
		attributes := make(map[string]string)
		for _, line := range collectionItems(collector, strings.Split(block, "\n")) {
			if !collector.check() {
				return nil
			}
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "I:"):
				for _, field := range collectionItems(collector, strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "I:")))) {
					if !collector.check() {
						return nil
					}
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
		if !collector.check() {
			return nil
		}
		components = append(components, NewComponent(ComponentInput, "input:"+strconv.Itoa(recordIndex), attributes))
	}
	return components
}

func ClassifyInput(name, handlers, keyCapabilities, relativeCapabilities, absoluteCapabilities, properties string) string {
	return ClassifyInputWithBitmapWidth(name, handlers, keyCapabilities, relativeCapabilities, absoluteCapabilities, properties, strconv.IntSize)
}

// ClassifyInputWithBitmapWidth uses an independently observed kernel word size.
// Device names remain discovery hints; functional tests check descriptor ioctls.
func ClassifyInputWithBitmapWidth(name, handlers, keyCapabilities, relativeCapabilities, absoluteCapabilities, properties string, wordBits int) string {
	identity := strings.ToLower(name + " " + handlers)
	propertyWordBits := wordBits
	if wordBits != 32 && wordBits != 64 && len(strings.Fields(properties)) == 1 {
		// A single 32-bit property word has the same low property bits on
		// both kernel layouts. This discovery hint does not establish the
		// kernel width required to validate functional keyboard evidence.
		propertyWordBits = 32
	}
	switch {
	case strings.Contains(identity, "touchpad"):
		return "touchpad"
	case strings.Contains(identity, "trackpad"):
		return "touchpad"
	case strings.Contains(identity, "touchscreen") || strings.Contains(identity, "touch screen"):
		return "touchscreen"
	case absoluteCapabilities != "" && inputCapabilityBitWidth(properties, 1, propertyWordBits):
		return "touchscreen"
	case absoluteCapabilities != "" && (inputCapabilityBitWidth(properties, 2, propertyWordBits) || inputCapabilityBitWidth(properties, 3, propertyWordBits) || inputCapabilityBitWidth(properties, 4, propertyWordBits)):
		return "touchpad"
	case strings.Contains(identity, "mouse") || strings.Contains(identity, "trackball") || strings.Contains(identity, "pointing") || handlerHasPrefix(handlers, "mouse"):
		return "pointer"
	case strings.Contains(identity, "keyboard"):
		return "keyboard"
	case handlerHas(handlers, "kbd") && keyboardKeyMapWidth(keyCapabilities, wordBits):
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

func keyboardKeyMapWidth(value string, wordBits int) bool {
	matches := 0
	for _, code := range []int{2, 16, 28, 30, 44, 57} {
		if inputCapabilityBitWidth(value, code, wordBits) {
			matches++
		}
	}
	return matches >= 2
}

func inputCapabilityBitWidth(value string, code, wordBits int) bool {
	if wordBits != 32 && wordBits != 64 || code < 0 {
		return false
	}
	words := strings.Fields(value)
	wordIndex := code / wordBits
	if wordIndex >= len(words) {
		return false
	}
	word, err := strconv.ParseUint(words[len(words)-1-wordIndex], 16, wordBits)
	return err == nil && word&(1<<uint(code%wordBits)) != 0
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

func (collector *linuxCollection) collectSound() []Component {
	soundRoot := path.Join(collector.Paths.Sys, "class/sound")
	cards := make(map[string]map[string]string)
	for _, entry := range collectionItems(collector, collector.readDir(soundRoot)) {
		if !collector.check() {
			return nil
		}
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
		if !collector.check() {
			return nil
		}
		components = append(components, NewComponent(ComponentSound, "sound:card"+cardNumber, attributes))
	}
	return components
}

func (collector *linuxCollection) collectVideo() []Component {
	var components []Component
	drmRoot := path.Join(collector.Paths.Sys, "class/drm")
	for _, entry := range collectionItems(collector, collector.readDir(drmRoot)) {
		if !collector.check() {
			return nil
		}
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
		fields := []attributePath{
			{name: "device_number", relativePath: "dev"},
			{name: "vendor_id", relativePath: "device/vendor"},
			{name: "device_id", relativePath: "device/device"},
			{name: "subsystem_vendor_id", relativePath: "device/subsystem_vendor"},
			{name: "subsystem_device_id", relativePath: "device/subsystem_device"},
		}
		if category == "display_connector" {
			// A connector's device link names its DRM card, not the card's
			// bus device. device/device can therefore be a directory.
			fields = []attributePath{
				{name: "status", relativePath: "status"},
				{name: "enabled", relativePath: "enabled"},
			}
		}
		collector.addAttributes(attributes, base, fields)
		if !collector.check() {
			return nil
		}
		components = append(components, NewComponent(ComponentVideo, "video:drm:"+name, attributes))
	}

	videoRoot := path.Join(collector.Paths.Sys, "class/video4linux")
	for _, entry := range collectionItems(collector, collector.readDir(videoRoot)) {
		if !collector.check() {
			return nil
		}
		name := entry.Name()
		attributes := map[string]string{"interface": name, "category": "video_capture"}
		collector.addAttributes(attributes, path.Join(videoRoot, name), []attributePath{
			{name: "name", relativePath: "name"},
			{name: "index", relativePath: "index"},
			{name: "device_number", relativePath: "dev"},
			{name: "modalias", relativePath: "device/modalias"},
		})
		if !collector.check() {
			return nil
		}
		components = append(components, NewComponent(ComponentVideo, "video:capture:"+name, attributes))
	}

	graphicsRoot := path.Join(collector.Paths.Sys, "class/graphics")
	for _, entry := range collectionItems(collector, collector.readDir(graphicsRoot)) {
		if !collector.check() {
			return nil
		}
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
		if !collector.check() {
			return nil
		}
		components = append(components, NewComponent(ComponentVideo, "video:framebuffer:"+name, attributes))
	}
	return components
}
