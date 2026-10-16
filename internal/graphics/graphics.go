package graphics

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const Schema = "hhsoft.pc-tester.graphics/v1"

type Status string

const (
	StatusPass         Status = "pass"
	StatusFail         Status = "fail"
	StatusSkip         Status = "skip"
	StatusInconclusive Status = "inconclusive"
	StatusAborted      Status = "aborted"
)

var (
	cardPattern      = regexp.MustCompile(`^card[0-9]+$`)
	renderPattern    = regexp.MustCompile(`^renderD[0-9]+$`)
	softwareRenderer = regexp.MustCompile(`(?i)(llvmpipe|softpipe|swrast|software rasterizer)`)
)

type Runner interface {
	Run(context.Context, string, ...string) (string, error)
}

type Result struct {
	Schema      string         `json:"schema"`
	Status      Status         `json:"status"`
	Cards       []Card         `json:"cards"`
	RenderNodes []string       `json:"render_nodes,omitempty"`
	Renderer    RendererResult `json:"renderer"`
	Pattern     PatternResult  `json:"pattern"`
	Warnings    []string       `json:"warnings,omitempty"`
}

type Card struct {
	Name       string      `json:"name"`
	Device     string      `json:"device"`
	Driver     string      `json:"driver,omitempty"`
	VendorID   string      `json:"vendor_id,omitempty"`
	DeviceID   string      `json:"device_id,omitempty"`
	Connectors []Connector `json:"connectors,omitempty"`
}

type Connector struct {
	Name            string   `json:"name"`
	Status          string   `json:"status,omitempty"`
	Enabled         string   `json:"enabled,omitempty"`
	AdvertisedModes []string `json:"advertised_modes,omitempty"`
}

type RendererResult struct {
	Status           Status `json:"status"`
	Name             string `json:"name,omitempty"`
	HardwareDetected bool   `json:"hardware_detected"`
	Detail           string `json:"detail,omitempty"`
}

type PatternResult struct {
	Status Status `json:"status"`
	Frames int    `json:"frames,omitempty"`
	Device string `json:"device,omitempty"`
	Detail string `json:"detail,omitempty"`
}

func Probe(ctx context.Context, root string, frames int, runner Runner) Result {
	result := Result{Schema: Schema, Status: StatusInconclusive}
	result.Cards, result.RenderNodes = discover(root)
	if len(result.Cards) == 0 {
		result.Warnings = append(result.Warnings, "no DRM card was discovered")
	}

	if runner == nil {
		result.Renderer = RendererResult{Status: StatusSkip, Detail: "renderer probe unavailable"}
		result.Pattern = PatternResult{Status: StatusSkip, Detail: "display pattern unavailable"}
		return result
	}

	rendererOutput, rendererError := runner.Run(ctx, "/usr/bin/eglinfo", "-B", "-p", "gbm")
	result.Renderer = parseRenderer(rendererOutput, rendererError)
	result.Pattern = runPattern(ctx, result.Cards, frames, runner)
	if len(result.Cards) > 0 && result.Renderer.Status == StatusPass && result.Pattern.Status != StatusInconclusive {
		result.Status = StatusPass
	}
	return result
}

func discover(root string) ([]Card, []string) {
	drmRoot := resolve(root, "sys/class/drm")
	entries, _ := os.ReadDir(drmRoot)
	var cards []Card
	var renderNodes []string
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case cardPattern.MatchString(name):
			base := filepath.Join(drmRoot, name)
			properties := readProperties(filepath.Join(base, "device/uevent"))
			cards = append(cards, Card{
				Name:     name,
				Device:   "/dev/dri/" + name,
				Driver:   strings.ToLower(properties["DRIVER"]),
				VendorID: readValue(filepath.Join(base, "device/vendor")),
				DeviceID: readValue(filepath.Join(base, "device/device")),
			})
		case renderPattern.MatchString(name):
			renderNodes = append(renderNodes, "/dev/dri/"+name)
		}
	}
	for cardIndex := range cards {
		prefix := cards[cardIndex].Name + "-"
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), prefix) {
				continue
			}
			base := filepath.Join(drmRoot, entry.Name())
			cards[cardIndex].Connectors = append(cards[cardIndex].Connectors, Connector{
				Name:            entry.Name(),
				Status:          readValue(filepath.Join(base, "status")),
				Enabled:         readValue(filepath.Join(base, "enabled")),
				AdvertisedModes: readLines(filepath.Join(base, "modes"), 64),
			})
		}
		sort.Slice(cards[cardIndex].Connectors, func(left, right int) bool {
			return cards[cardIndex].Connectors[left].Name < cards[cardIndex].Connectors[right].Name
		})
	}
	sort.Slice(cards, func(left, right int) bool { return cards[left].Name < cards[right].Name })
	sort.Strings(renderNodes)
	return cards, renderNodes
}

func parseRenderer(output string, runError error) RendererResult {
	if runError != nil {
		return RendererResult{Status: StatusInconclusive, Detail: normalizeDetail(runError.Error())}
	}
	renderer := field(output, "OpenGL core profile renderer:")
	if renderer == "" {
		renderer = field(output, "OpenGL ES profile renderer:")
	}
	if renderer == "" {
		return RendererResult{Status: StatusInconclusive, Detail: "EGL initialized without a reported renderer"}
	}
	if softwareRenderer.MatchString(renderer) {
		return RendererResult{Status: StatusInconclusive, Name: renderer, Detail: "software rendering fallback"}
	}
	return RendererResult{Status: StatusPass, Name: renderer, HardwareDetected: true}
}

func runPattern(ctx context.Context, cards []Card, frames int, runner Runner) PatternResult {
	if frames <= 0 {
		return PatternResult{Status: StatusSkip, Detail: "not requested"}
	}
	if frames > 600 {
		frames = 600
	}
	device := firstConnectedCard(cards)
	if device == "" {
		return PatternResult{Status: StatusSkip, Detail: "no connected display connector"}
	}
	_, err := runner.Run(ctx, "/usr/bin/kmscube", "-D", device, "-c", strconv.Itoa(frames), "-M", "rgba")
	if err != nil {
		return PatternResult{Status: StatusInconclusive, Frames: frames, Device: device, Detail: normalizeDetail(err.Error())}
	}
	return PatternResult{Status: StatusPass, Frames: frames, Device: device}
}

func firstConnectedCard(cards []Card) string {
	for _, card := range cards {
		for _, connector := range card.Connectors {
			if connector.Status == "connected" {
				return card.Device
			}
		}
	}
	return ""
}

func field(output, label string) string {
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), label) {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), label))
		}
	}
	return ""
}

func resolve(root, name string) string {
	return filepath.Join(filepath.Clean(root), filepath.FromSlash(name))
}

func readValue(name string) string {
	data, err := os.ReadFile(name)
	if err != nil {
		return ""
	}
	return strings.Join(strings.Fields(string(data)), " ")
}

func readLines(name string, limit int) []string {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil
	}
	values := strings.Fields(string(data))
	if len(values) > limit {
		values = values[:limit]
	}
	return values
}

func readProperties(name string) map[string]string {
	result := make(map[string]string)
	data, err := os.ReadFile(name)
	if err != nil {
		return result
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if found {
			result[key] = strings.TrimSpace(value)
		}
	}
	return result
}

func normalizeDetail(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return "diagnostic failed"
	}
	if len(value) > 240 {
		return value[:240]
	}
	return value
}
