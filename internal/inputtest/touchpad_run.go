package inputtest

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"slices"
	"sort"
	"time"
)

var errTouchpadIdle = errors.New("touchpad queue idle")
var errTouchpadRetry = errors.New("touchpad read interrupted")

// The adapter transfers one concrete pointer owner for the whole acquisition.
// It must not wrap an already-transferred handle in another ownership identity.
type touchpadOpened struct {
	owner                 io.Closer
	metadata              touchpadMetadata
	eventSize, typeOffset int
	read                  func([]byte) (int, error)
	snapshot              func() (touchpadMetadata, error)
	finalSnapshot         func() (touchpadMetadata, error)
	neutral               func() (bool, error)
}

type touchpadBackend struct {
	discover func(context.Context, string) ([]string, error)
	open     func(context.Context, string, string) (touchpadOpened, error)
}

type observedTouchpad struct {
	source         touchpadOpened
	expected       touchpadMetadata
	state          *touchpadState
	pending        []byte
	baselineBytes  int
	baselineFrame  bool
	baselineEvents int
	records        uint64
	candidate      bool
}

type ownedTouchpad struct {
	owner io.Closer
	rows  []int
}

// Advance both quotas together; a rejected record changes neither counter.
func touchpadRecordQuota(device, total *uint64) bool {
	if *device >= maximumTouchpadRecords || *total >= maximumTouchpadTotalRecords {
		return false
	}
	*device++
	*total++
	return true
}

func touchpadNames(names []string) ([]string, bool) {
	if len(names) > maximumTouchpadDevices {
		return nil, false
	}
	owned := append([]string{}, names...)
	sort.Strings(owned)
	for i, name := range owned {
		if !pointerInterface(name) || i > 0 && name == owned[i-1] {
			return nil, false
		}
	}
	return owned, true
}

func touchpadRecheck(device *observedTouchpad, terminal bool) error {
	check := device.source.snapshot
	if terminal && device.source.finalSnapshot != nil {
		check = device.source.finalSnapshot
	}
	metadata, err := check()
	// Copy callback-owned storage before any later callback, including ctx.Err.
	frozen, bounded := copyTouchpadMetadata(metadata)
	if err != nil {
		return err
	}
	if !bounded || !sameTouchpadMetadata(frozen, device.expected) {
		return errInputIdentity
	}
	return nil
}

// This function never consumes terminal bytes or starts another observation
// window. observeStop records cancellation without bypassing owned cleanup.
func recheckTouchpadPass(device *observedTouchpad, observeStop func()) error {
	revoke := func(detail string) {
		device.candidate = false
		device.state.result.Status, device.state.result.Detail = StatusInconclusive, detail
		device.state.result.FinalNeutral = false
	}
	observeStop()
	if err := touchpadRecheck(device, true); err != nil {
		revoke("identity_changed")
		return err
	}
	observeStop()
	var buffer [4096]byte
	count, err := device.source.read(buffer[:])
	if count < 0 || count > len(buffer) || count > 0 && err != nil || count == 0 && err == nil {
		revoke("malformed_events")
		return errInputStream
	}
	observeStop()
	if count != 0 || err != errTouchpadIdle || len(device.pending) != 0 || !device.state.complete() {
		revoke("terminal_not_idle")
		if count > 0 && err == nil || count == 0 && err == errTouchpadRetry {
			return nil
		}
		if err == errTouchpadIdle {
			return errInputStream
		}
		return err
	}
	observeStop()
	neutral, err := device.source.neutral()
	observeStop()
	if err != nil || !neutral {
		revoke("terminal_not_neutral")
		return err
	}
	observeStop()
	if err := touchpadRecheck(device, true); err != nil {
		revoke("identity_changed")
		return err
	}
	observeStop()
	device.state.result.FinalNeutral = true
	return nil
}

// runTouchpadWith is the complete backend-driven core. The platform wrapper
// and real descriptor acquisition are a separate connected source partition.
func runTouchpadWith(ctx context.Context, options TouchpadOptions, backend touchpadBackend) (result TouchpadResult) {
	result = TouchpadResult{Schema: TouchpadSchema, Status: StatusError, StopReason: "error",
		LimitMilliseconds: options.Duration.Milliseconds(), CleanupConfirmed: true, Devices: []TouchpadDeviceResult{}}
	if nilInputValue(ctx) || options.Duration < 10*time.Millisecond || options.Duration > 90*time.Second ||
		backend.discover == nil || backend.open == nil {
		result.LimitMilliseconds, result.Detail = 0, "invalid_options"
		return
	}
	result.Status, result.Detail = StatusInconclusive, "incomplete_actions"
	started := time.Now()
	deadline := started.Add(options.Duration)
	if options.Root == "" {
		options.Root = "/"
	}
	var owners []ownedTouchpad
	uncertainRows := make(map[*TouchpadDeviceResult]bool)
	ownerIDs := make(map[io.Closer]int)
	var devices []observedTouchpad
	var initial []string
	wantPass := false
	hardDetail := ""
	hard := func(detail string) {
		if hardDetail == "" {
			hardDetail = detail
		}
		result.Status, result.StopReason, result.Detail = StatusError, "error", hardDetail
	}
	noteError := func(err error, detail string) {
		if errors.Is(err, errInputClose) {
			result.CleanupConfirmed = false
			hard("cleanup_unconfirmed")
		} else if err == context.Canceled || err == context.DeadlineExceeded {
			if stopped := pointerContextError(ctx); stopped != nil {
				result.MarkInterrupted(stopped)
			} else {
				hard(detail)
			}
		} else {
			hard(detail)
		}
	}
	deviceError := func(d *observedTouchpad, err error, detail string) {
		if errors.Is(err, errInputClose) {
			uncertainRows[d.state.result] = true
		}
		d.candidate = false
		if err == context.Canceled || err == context.DeadlineExceeded {
			if stopped := pointerContextError(ctx); stopped != nil {
				d.state.result.Status, d.state.result.Detail = StatusInconclusive, detail
				d.state.result.FinalNeutral = false
				result.MarkInterrupted(stopped)
				return
			}
		}
		d.state.reject(detail, false)
		noteError(err, detail)
	}
	observeStop := func() {
		result.MarkInterrupted(pointerContextError(ctx))
		if hardDetail == "" && result.Status != StatusError && result.Status != StatusAborted && !time.Now().Before(deadline) {
			result.Status, result.StopReason, result.Detail = StatusInconclusive, "observation_timeout", "observation_timeout"
		}
	}
	stop := func() bool {
		observeStop()
		return hardDetail != "" || !result.CleanupConfirmed || result.Status == StatusAborted ||
			result.Status == StatusError || result.StopReason == "observation_timeout"
	}
	defer func() {
		// Active observation has ended globally before any terminal key query.
		for i := range devices {
			d := &devices[i]
			if !d.state.failed && (len(d.pending) != 0 || d.state.frameEvents != 0) {
				// Interruption can leave a valid record/frame prefix. Only an
				// observed read/protocol fault may classify it as malformed.
				d.candidate = false
				d.state.result.Status, d.state.result.Detail = StatusInconclusive, "incomplete_events"
				d.state.result.FinalNeutral = false
			}
		}
		anyCandidate := false
		for i := range devices {
			anyCandidate = anyCandidate || devices[i].candidate
		}
		if anyCandidate {
			observeStop()
			raw, err := backend.discover(ctx, options.Root)
			current, valid := touchpadNames(raw)
			if err != nil || !valid || !slices.Equal(current, initial) {
				for i := range devices {
					devices[i].candidate = false
					devices[i].state.result.FinalNeutral = false
					devices[i].state.result.Detail = "registry_changed"
				}
				noteError(err, "registry_changed")
			}
			observeStop()
		}
		for i := range devices {
			d := &devices[i]
			if d.candidate && !d.state.failed {
				if err := recheckTouchpadPass(d, observeStop); err != nil {
					if errors.Is(err, errInputClose) {
						uncertainRows[d.state.result] = true
					}
					noteError(err, d.state.result.Detail)
				}
			}
		}
		for i := len(owners) - 1; i >= 0; i-- {
			owned := owners[i]
			err := owned.owner.Close()
			for _, row := range owned.rows {
				result.Devices[row].CleanupConfirmed = err == nil && !uncertainRows[&result.Devices[row]]
				if err != nil || uncertainRows[&result.Devices[row]] {
					result.Devices[row].Status, result.Devices[row].Detail = StatusError, "cleanup_unconfirmed"
				}
			}
			if err != nil {
				result.CleanupConfirmed = false
			}
			observeStop()
		}
		passed := 0
		for i := range devices {
			d := &devices[i]
			if d.candidate && !d.state.failed && d.state.result.FinalNeutral && d.state.result.CleanupConfirmed {
				d.state.result.Status, d.state.result.Detail = StatusPass, ""
				passed++
			}
		}
		result.DurationMilliseconds = time.Since(started).Milliseconds()
		if !result.CleanupConfirmed {
			result.Status, result.StopReason, result.Detail = StatusError, "cleanup_unconfirmed", "cleanup_unconfirmed"
			return
		}
		observeStop()
		if wantPass && hardDetail == "" && result.Status != StatusError && result.Status != StatusAborted &&
			result.StopReason != "observation_timeout" && passed == len(result.Devices) {
			result.Status, result.StopReason, result.Detail = StatusPass, "completed", ""
		}
	}()
	if stop() {
		return
	}
	raw, err := backend.discover(ctx, options.Root)
	var valid bool
	initial, valid = touchpadNames(raw) // Freeze before ctx or any provider runs.
	if err != nil {
		noteError(err, "registry_unavailable")
		return
	}
	if !valid {
		hard("registry_unavailable")
		return
	}
	if stop() {
		return
	}
	if len(initial) == 0 {
		result.StopReason, result.Detail = "no_touchpads", "no_touchpads"
		return
	}
	for _, name := range initial {
		result.Devices = append(result.Devices, TouchpadDeviceResult{Interface: name, Status: StatusInconclusive,
			Buttons: []PointerButtonResult{}, CleanupConfirmed: true, Detail: "incomplete_actions"})
	}
	for index, name := range initial {
		if stop() {
			return
		}
		row := &result.Devices[index]
		source, openErr := backend.open(ctx, options.Root, name)
		// Register ownership before interpreting any accompanied error or
		// invoking a context callback. Only concrete pointer owners are admitted.
		ownerPresent := !nilInputValue(source.owner)
		if ownerPresent {
			value := reflect.ValueOf(source.owner)
			if value.Kind() != reflect.Pointer || !value.Comparable() {
				owners = append(owners, ownedTouchpad{source.owner, []int{index}})
				row.CleanupConfirmed = false
				hard("invalid_owner")
				return
			}
			if prior, duplicate := ownerIDs[source.owner]; duplicate {
				owners[prior].rows = append(owners[prior].rows, index)
				row.CleanupConfirmed = false
				hard("ownership_alias")
				return
			}
			ownerIDs[source.owner] = len(owners)
			owners = append(owners, ownedTouchpad{source.owner, []int{index}})
			row.CleanupConfirmed = false
		}
		frozen, bounded := copyTouchpadMetadata(source.metadata)
		if openErr != nil {
			if errors.Is(openErr, errInputClose) {
				uncertainRows[row] = true
				row.CleanupConfirmed = false
			}
			row.Detail = "identity_unavailable"
			empty := !ownerPresent && source.read == nil && source.snapshot == nil && source.finalSnapshot == nil && source.neutral == nil
			if openErr == errInputUnavailable && empty {
				continue
			}
			if !ownerPresent && !empty {
				row.CleanupConfirmed = false
				result.CleanupConfirmed = false
			}
			noteError(openErr, "identity_unavailable")
			return
		}
		if !ownerPresent || source.read == nil || source.snapshot == nil || source.neutral == nil {
			if !ownerPresent && (source.read != nil || source.snapshot != nil || source.finalSnapshot != nil || source.neutral != nil) {
				row.CleanupConfirmed = false
				result.CleanupConfirmed = false
			}
			hard("identity_unavailable")
			return
		}
		if stop() {
			return
		}
		row.Readable = true
		identity := source.metadata.identity
		row.Identity = &identity
		if !bounded {
			row.Detail = "unsupported_capabilities"
			continue
		}
		evidence, _ := copyTouchpadMetadata(frozen)
		row.Capabilities = &evidence.capabilities
		_, _, row.Supported = ValidateTouchpadCapabilities(frozen.capabilities)
		if !row.Supported {
			row.Detail = "unsupported_capabilities"
			continue
		}
		if source.eventSize < 8 || source.eventSize > 64 || source.typeOffset != source.eventSize-8 {
			hard("unsupported_record_layout")
			return
		}
		state := newTouchpadState(row)
		devices = append(devices, observedTouchpad{source: source, expected: frozen, state: state})
	}
	buffer := make([]byte, 4096)
	var totalRecords uint64
	for {
		if stop() {
			return
		}
		unfinished := false
		for i := range devices {
			d := &devices[i]
			if stop() {
				return
			}
			if err := touchpadRecheck(d, false); err != nil {
				deviceError(d, err, "identity_changed")
				return
			}
			if stop() {
				return
			}
			count, readErr := d.source.read(buffer)
			d.candidate = false
			if count < 0 || count > len(buffer) || count > 0 && readErr != nil || count == 0 && readErr == nil {
				d.state.reject("malformed_events", false)
				hard("malformed_events")
				return
			}
			if readErr != nil && readErr != errTouchpadIdle && readErr != errTouchpadRetry {
				deviceError(d, readErr, "disconnected")
				return
			}
			// The callback may retain its destination. Copy before ctx.Err or
			// any other callback can mutate a retained alias.
			fresh := append([]byte{}, buffer[:count]...)
			if stop() {
				return
			}
			d.pending = append(d.pending, fresh...)
			if !d.state.result.BaselineNeutral {
				d.baselineBytes += count
				if d.baselineBytes > maximumTouchpadBaselineBytes {
					d.state.reject("baseline_unavailable", false)
					hard("baseline_unavailable")
					return
				}
			}
			for len(d.pending) >= d.source.eventSize {
				if stop() {
					return
				}
				if !touchpadRecordQuota(&d.records, &totalRecords) {
					d.state.reject("record_limit", false)
					hard("record_limit")
					return
				}
				record := d.pending[d.source.typeOffset:d.source.eventSize]
				event := touchpadEvent{binary.NativeEndian.Uint16(record), binary.NativeEndian.Uint16(record[2:]), int32(binary.NativeEndian.Uint32(record[4:]))}
				if d.state.result.BaselineNeutral {
					d.state.consume(event)
				} else {
					if event.typeCode == 0 && event.code != 0 {
						d.state.reject("malformed_events", event.code == 3)
						if event.code == 3 {
							d.state.result.Detail = "event_loss"
						}
					}
					d.baselineFrame = event.typeCode != 0 || event.code != 0
					if d.baselineFrame {
						d.baselineEvents++
						if d.baselineEvents > maximumTouchpadFrameEvents {
							d.state.reject("frame_limit", false)
						}
					} else {
						d.baselineEvents = 0
					}
				}
				d.pending = d.pending[d.source.eventSize:]
				if d.state.failed {
					hard(d.state.result.Detail)
					return
				}
			}
			idle := count == 0 && readErr == errTouchpadIdle
			if idle && !d.state.result.BaselineNeutral {
				if len(d.pending) != 0 || d.baselineFrame {
					d.state.reject("malformed_events", false)
					hard("malformed_events")
					return
				}
				if stop() {
					return
				}
				neutral, err := d.source.neutral()
				if err != nil {
					if errors.Is(err, errInputClose) {
						uncertainRows[d.state.result] = true
					}
					noteError(err, "baseline_unavailable")
					return
				}
				if stop() {
					return
				}
				d.state.result.BaselineNeutral = neutral
			}
			d.state.result.Detail = d.state.incompleteReason()
			if idle && len(d.pending) == 0 && d.state.complete() {
				if err := touchpadRecheck(d, false); err != nil {
					deviceError(d, err, "identity_changed")
					return
				}
				if stop() {
					return
				}
				d.candidate = true // FinalNeutral stays false until terminal proof.
			}
			unfinished = unfinished || !d.candidate
		}
		if !unfinished {
			wantPass = len(devices) == len(result.Devices)
			if !wantPass {
				result.Detail = "unsupported_or_unreadable_device"
			}
			return
		}
		if stop() {
			return
		}
		timer := time.NewTimer(min(5*time.Millisecond, time.Until(deadline)))
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
	}
}
