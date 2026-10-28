//go:build linux

package audio

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

const (
	maximumPCMEndpoints = 1024
	maximumCardIndex    = 255
	maximumDeviceIndex  = 255
	maximumMinorNumber  = 1<<20 - 1
	alsaMajorNumber     = 116
	openPath            = 0x200000
)

var (
	cardPattern    = regexp.MustCompile(`^card(0|[1-9][0-9]{0,2})$`)
	controlPattern = regexp.MustCompile(`^controlC(0|[1-9][0-9]{0,2})$`)
	pcmPattern     = regexp.MustCompile(`^pcmC(0|[1-9][0-9]{0,2})D(0|[1-9][0-9]{0,2})([pc])$`)
	devicePattern  = regexp.MustCompile(`^116:(0|[1-9][0-9]{0,6})\n?$`)
)

type deviceNumber struct {
	major uint32
	minor uint32
}

func runPlatform(ctx context.Context, options Options) Result {
	return runWithAudioOperations(ctx, options, systemAudioOperations())
}

func runWithAudioOperations(ctx context.Context, options Options, operations audioOperations) (result Result) {
	result = Result{Schema: Schema, Status: StatusError, Readiness: ReadinessUnknown}
	if nilAudioValue(ctx) || operations.openRoot == nil || operations.openReadable == nil {
		result.Detail = "cannot access audio probe operations"
		return result
	}
	if cancelled(ctx, &result) {
		return result
	}
	if options.Root == "" {
		options.Root = "/"
	}
	if !filepath.IsAbs(options.Root) || len(options.Root) > 4096 || strings.ContainsRune(options.Root, 0) {
		result.Detail = "audio probe root must be an absolute filesystem path"
		return result
	}
	root, err := operations.openRoot(options.Root)
	if nilAudioValue(root) {
		result.Detail = "cannot access audio probe root"
		return result
	}
	defer func() {
		if root.Close() != nil {
			result.Status, result.Readiness, result.Detail = StatusError, ReadinessUnknown, errAudioClose.Error()
		} else if result.Status != StatusError {
			cancelled(ctx, &result)
		}
	}()
	if err != nil {
		result.Detail = "cannot access audio probe root"
		return result
	}
	entries, err := registryEntries(ctx, root)
	if err != nil {
		recordAudioAcquisitionError(&result, err)
		return result
	}
	cards := make(map[uint32]bool)
	controls := make(map[uint32]string)
	for _, name := range entries {
		if cancelled(ctx, &result) {
			return result
		}
		switch {
		case strings.HasPrefix(name, "card"):
			match := cardPattern.FindStringSubmatch(name)
			if match == nil {
				result.Detail = "audio registry contains a malformed card name"
				return result
			}
			card, valid := parseIndex(match[1], maximumCardIndex)
			metadata, statErr := root.Stat("sys/class/sound/" + name)
			if !valid || statErr != nil || nilAudioValue(metadata) || !metadata.IsDir() {
				result.Detail = "audio registry contains an invalid card"
				return result
			}
			cards[card] = true
			result.Cards = append(result.Cards, CardResult{Interface: name, Card: card})
		case strings.HasPrefix(name, "control"):
			match := controlPattern.FindStringSubmatch(name)
			if match == nil {
				result.Detail = "audio registry contains a malformed control name"
				return result
			}
			card, valid := parseIndex(match[1], maximumCardIndex)
			if !valid {
				result.Detail = "audio registry contains an invalid control card index"
				return result
			}
			controls[card] = name
		case strings.HasPrefix(name, "pcm"):
			match := pcmPattern.FindStringSubmatch(name)
			if match == nil {
				result.Detail = "audio registry contains a malformed PCM name"
				return result
			}
			card, cardValid := parseIndex(match[1], maximumCardIndex)
			device, deviceValid := parseIndex(match[2], maximumDeviceIndex)
			if !cardValid || !deviceValid || len(result.Endpoints) >= maximumPCMEndpoints {
				result.Detail = "audio PCM registry exceeds the supported limits"
				return result
			}
			direction := "playback"
			if match[3] == "c" {
				direction = "capture"
			}
			result.Endpoints = append(result.Endpoints, PCMResult{Interface: name, Card: card, Device: device, Direction: direction})
		}
	}
	for card := range controls {
		if _, exists := cards[card]; !exists {
			result.Detail = "audio control references an undiscovered card"
			return result
		}
	}
	for _, endpoint := range result.Endpoints {
		if _, exists := cards[endpoint.Card]; !exists {
			result.Detail = "audio PCM endpoint references an undiscovered card"
			return result
		}
	}
	missingNodes := false
	seenDevices := make(map[deviceNumber]bool)
	for index := range result.Cards {
		if cancelled(ctx, &result) {
			return result
		}
		card := &result.Cards[index]
		name, exists := controls[card.Card]
		if !exists {
			card.Control = NodeResult{Path: "/dev/snd/controlC" + strconv.FormatUint(uint64(card.Card), 10), ReadinessIssue: "ALSA control is absent from sysfs"}
			missingNodes = true
			continue
		}
		card.Control, err = inspectNode(ctx, root, operations, name, seenDevices)
		if err != nil {
			recordAudioAcquisitionError(&result, err)
			return result
		}
		missingNodes = missingNodes || !card.Control.Ready
	}
	for index := range result.Endpoints {
		if cancelled(ctx, &result) {
			return result
		}
		endpoint := &result.Endpoints[index]
		endpoint.Node, err = inspectNode(ctx, root, operations, endpoint.Interface, seenDevices)
		if err != nil {
			recordAudioAcquisitionError(&result, err)
			return result
		}
		if endpoint.Direction == "playback" {
			result.PlaybackEndpoints++
		} else {
			result.CaptureEndpoints++
		}
		if endpoint.Node.Ready {
			result.ReadyEndpoints++
		} else {
			missingNodes = true
		}
	}
	if cancelled(ctx, &result) {
		return result
	}
	result.Status = StatusInconclusive
	switch {
	case len(result.Cards) == 0:
		result.Readiness = ReadinessNoDevice
		result.Detail = "no ALSA sound cards were discovered"
	case missingNodes:
		result.Readiness = ReadinessMissingNodes
		result.Detail = "one or more ALSA control or PCM device nodes are absent"
	case len(result.Endpoints) == 0:
		result.Readiness = ReadinessNoPCM
		result.Detail = "no ALSA PCM endpoints were discovered"
	default:
		result.Status = StatusPass
		result.Readiness = ReadinessReady
		result.Detail = "ALSA control and PCM node metadata match sysfs; playback and capture were not exercised"
	}
	return result
}

func cancelled(ctx context.Context, result *Result) bool {
	if ctx.Err() == nil {
		return false
	}
	result.Status = StatusAborted
	result.Readiness = ReadinessUnknown
	result.Detail = "cancelled"
	return true
}

func recordAudioAcquisitionError(result *Result, err error) {
	result.Status, result.Readiness = StatusError, ReadinessUnknown
	result.Detail = err.Error()
	if err == context.Canceled || err == context.DeadlineExceeded {
		result.Status, result.Detail = StatusAborted, "cancelled"
	}
}

func registryEntries(ctx context.Context, root audioRoot) ([]string, error) {
	return acquireAudioRegistry(ctx, func() (audioDirectory, error) {
		return root.OpenRegistry("sys/class/sound", os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NONBLOCK|syscall.O_CLOEXEC)
	})
}

func inspectNode(ctx context.Context, root audioRoot, operations audioOperations, name string, seen map[deviceNumber]bool) (NodeResult, error) {
	result := NodeResult{Path: "/dev/snd/" + name}
	data, err := readAttribute(ctx, root, operations, "sys/class/sound/"+name+"/dev")
	if err != nil {
		if err == errAudioClose || err == context.Canceled || err == context.DeadlineExceeded {
			return result, err
		}
		return result, errors.New("cannot safely read ALSA device number")
	}
	match := devicePattern.FindStringSubmatch(string(data))
	if match == nil {
		return result, errors.New("ALSA registry contains a malformed device number")
	}
	minor, valid := parseIndex(match[1], maximumMinorNumber)
	if !valid {
		return result, errors.New("ALSA device number exceeds the supported limit")
	}
	expected := deviceNumber{major: alsaMajorNumber, minor: minor}
	if seen[expected] {
		return result, errors.New("ALSA registry contains duplicate device numbers")
	}
	seen[expected] = true
	metadata, err := root.Lstat("dev/snd/" + name)
	if nilAudioValue(metadata) && soleAudioAbsence(err, "statat") {
		result.ReadinessIssue = "ALSA device node is absent"
		return result, nil
	}
	if err != nil || nilAudioValue(metadata) {
		return result, errors.New("cannot inspect ALSA device node")
	}
	result.Present = true
	if !matchesDevice(metadata, expected) {
		result.ReadinessIssue = "ALSA path is not the expected direct character device"
		return result, errors.New(result.ReadinessIssue)
	}
	result.Ready = true
	return result, nil
}

func matchesDevice(metadata os.FileInfo, expected deviceNumber) bool {
	if nilAudioValue(metadata) || metadata.Mode()&os.ModeSymlink != 0 || metadata.Mode()&(os.ModeDevice|os.ModeCharDevice) != os.ModeDevice|os.ModeCharDevice {
		return false
	}
	stat, valid := metadata.Sys().(*syscall.Stat_t)
	if !valid || stat == nil {
		return false
	}
	encoded := uint64(stat.Rdev)
	major := uint32((encoded>>8)&0xfff | (encoded>>32)&0xfffff000)
	minor := uint32(encoded&0xff | (encoded>>12)&0xffffff00)
	return major == expected.major && minor == expected.minor
}

func readAttribute(ctx context.Context, root audioRoot, operations audioOperations, path string) (data []byte, err error) {
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	handle, openErr := root.OpenPath(path, openPath|syscall.O_NOFOLLOW|syscall.O_CLOEXEC)
	if nilAudioValue(handle) {
		return nil, errAudioRead
	}
	defer closeAudioAttribute(ctx, handle, &data, &err)
	if openErr != nil {
		return nil, errAudioRead
	}
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	metadata, statErr := handle.Stat()
	if statErr != nil || nilAudioValue(metadata) || !metadata.Mode().IsRegular() {
		return nil, errors.New("ALSA attribute is not a regular file")
	}
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	file, openErr := operations.openReadable(handle)
	if nilAudioValue(file) {
		return nil, errAudioRead
	}
	defer closeAudioAttribute(ctx, file, &data, &err)
	if openErr != nil {
		return nil, errAudioRead
	}
	if stopped := ctx.Err(); stopped != nil {
		return nil, stopped
	}
	opened, statErr := file.Stat()
	if statErr != nil || nilAudioValue(opened) || !opened.Mode().IsRegular() || !os.SameFile(metadata, opened) {
		return nil, errors.New("ALSA attribute identity changed")
	}
	return readAudioBytes(ctx, file)
}

func parseIndex(value string, maximum uint32) (uint32, bool) {
	parsed, err := strconv.ParseUint(value, 10, 32)
	return uint32(parsed), err == nil && parsed <= uint64(maximum)
}
