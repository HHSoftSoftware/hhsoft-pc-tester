package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/touchpadevidence"
)

// Independent public wire literal. Two directions on each axis share frames;
// no collector or validator constructs its expected counts.
const touchpadCommandPassLiteral = `{"schema":"hhsoft.pc-tester.touchpad-functional/v1","status":"pass","stop_reason":"completed","limit_ms":30000,"duration_ms":100,"cleanup_confirmed":true,"devices":[{"interface":"event4","status":"pass","supported":true,"readable":true,"identity":{"bus":3,"vendor":4660,"product":22136,"version":1},"capabilities":{"protocol_version":65537,"events":[0,1,3,4],"keys":[272,273,274,325,330],"relative":[],"properties":[0,2],"misc":[4,5],"axes":[{"code":0,"minimum":0,"maximum":1000,"fuzz":0,"flat":0,"resolution":1},{"code":1,"minimum":-500,"maximum":500,"fuzz":0,"flat":0,"resolution":1}]},"x_threshold":50,"y_threshold":50,"frames":6,"contact_starts":1,"contact_releases":1,"xy_baselines":1,"qualifying_contacts":1,"x_positive":2,"x_negative":2,"y_positive":2,"y_negative":2,"buttons":[{"button":"left","presses":1,"releases":1,"pairs":1},{"button":"right","presses":1,"releases":1,"pairs":1},{"button":"middle","presses":1,"releases":1,"pairs":1}],"baseline_neutral":true,"final_neutral":true,"evidence_loss":false,"cleanup_confirmed":true}]}`

func touchpadCommandLiteral(t *testing.T) inputtest.TouchpadResult {
	t.Helper()
	var result inputtest.TouchpadResult
	if json.Unmarshal([]byte(touchpadCommandPassLiteral), &result) != nil {
		t.Fatal("invalid independent fixture")
	}
	return result
}

type touchpadCommandWriter struct {
	bytes.Buffer
	writeErr, closeErr error
	short              bool
	writes, closes     int
	onWrite            func()
	onClose            func()
}

func (writer *touchpadCommandWriter) Write(data []byte) (int, error) {
	writer.writes++
	if writer.onWrite != nil {
		writer.onWrite()
	}
	if writer.writeErr != nil {
		return 0, writer.writeErr
	}
	if writer.short {
		return len(data) - 1, nil
	}
	return writer.Buffer.Write(data)
}

func (writer *touchpadCommandWriter) Close() error {
	writer.closes++
	if writer.onClose != nil {
		writer.onClose()
	}
	return writer.closeErr
}

type touchpadCommandNilContext struct{}

func (*touchpadCommandNilContext) Deadline() (time.Time, bool) { panic("nil context used") }
func (*touchpadCommandNilContext) Done() <-chan struct{}       { panic("nil context used") }
func (*touchpadCommandNilContext) Err() error                  { panic("nil context used") }
func (*touchpadCommandNilContext) Value(any) any               { panic("nil context used") }

func TestTouchpadCommandFlagsAndFixedCollectorOptions(t *testing.T) {
	for _, row := range []struct {
		name              string
		args              []string
		duration, timeout time.Duration
	}{
		{"defaults", nil, 30 * time.Second, 90 * time.Second},
		{"minimum_duration", []string{"-duration", "10ms"}, 10 * time.Millisecond, 90 * time.Second},
		{"equal_limits", []string{"-duration", "1s", "-timeout", "1s"}, time.Second, time.Second},
		{"maximum_limits", []string{"-duration", "90s", "-timeout", "90s"}, 90 * time.Second, 90 * time.Second},
		{"fractional_milliseconds", []string{"-duration", "10.5ms"}, 10500 * time.Microsecond, 90 * time.Second},
	} {
		t.Run(row.name, func(t *testing.T) {
			expected := touchpadCommandLiteral(t)
			expected.LimitMilliseconds, expected.DurationMilliseconds = row.duration.Milliseconds(), 0
			calls := 0
			var out, diagnostic bytes.Buffer
			collect := func(ctx context.Context, options inputtest.TouchpadOptions) inputtest.TouchpadResult {
				calls++
				deadline, ok := ctx.Deadline()
				if options.Root != "/" || options.Duration != row.duration || !ok ||
					time.Until(deadline) <= 0 || time.Until(deadline) > row.timeout {
					t.Fatal("command changed fixed root or duration/deadline contract")
				}
				return expected
			}
			if code := runTouchpadWith(t.Context(), row.args, &out, &diagnostic, collect); code != 0 || calls != 1 {
				t.Fatalf("valid options rejected: code=%d calls=%d", code, calls)
			}
			actual, err := touchpadevidence.Decode(out.Bytes())
			if err != nil || !reflect.DeepEqual(actual, expected) || diagnostic.String() != touchpadCommandGuide {
				t.Fatal("literal command result or checked guidance changed")
			}
		})
	}
}

func TestTouchpadCommandRejectsFlagsWithoutCollection(t *testing.T) {
	for _, row := range []struct {
		name string
		args []string
	}{
		{"unknown", []string{"-canary-device-path"}},
		{"positional", []string{"canary-device-path"}},
		{"trailing", []string{"-duration", "1s", "canary-device-path"}},
		{"bad_duration", []string{"-duration", "canary-device-path"}},
		{"missing_duration", []string{"-duration"}},
		{"duration_zero", []string{"-duration", "0"}},
		{"duration_negative", []string{"-duration", "-1s"}},
		{"duration_low", []string{"-duration", "9.999ms"}},
		{"duration_high", []string{"-duration", "90.001s"}},
		{"timeout_zero", []string{"-timeout", "0"}},
		{"timeout_low", []string{"-duration", "10ms", "-timeout", "9ms"}},
		{"timeout_high", []string{"-timeout", "90.001s"}},
		{"duration_exceeds_timeout", []string{"-duration", "2s", "-timeout", "1s"}},
		{"output_empty", []string{"-output", ""}},
		{"root_selector", []string{"-root", "canary-device-path"}},
		{"device_selector", []string{"-device", "canary-device-path"}},
		{"help_is_not_collection", []string{"-help"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			calls := 0
			code := runTouchpadWith(t.Context(), row.args, &out, &diagnostic,
				func(context.Context, inputtest.TouchpadOptions) inputtest.TouchpadResult {
					calls++
					return inputtest.TouchpadResult{}
				})
			if code != 2 || calls != 0 || out.Len() != 0 || strings.Contains(diagnostic.String(), "canary") ||
				strings.Contains(diagnostic.String(), touchpadCommandGuide) {
				t.Fatal("invalid caller input reached collection or leaked a caller string")
			}
		})
	}
}

func TestTouchpadCommandNilBoundaries(t *testing.T) {
	for _, name := range []string{"context", "typed_context", "stdout", "typed_stdout", "stderr", "typed_stderr", "collector", "destination"} {
		t.Run(name, func(t *testing.T) {
			var ctx context.Context = t.Context()
			var out, diagnostic io.Writer = io.Discard, io.Discard
			calls := 0
			var collect touchpadCollector = func(context.Context, inputtest.TouchpadOptions) inputtest.TouchpadResult {
				calls++
				return inputtest.TouchpadResult{}
			}
			var destination destinationWriter = writeDestination
			switch name {
			case "context":
				ctx = nil
			case "typed_context":
				ctx = (*touchpadCommandNilContext)(nil)
			case "stdout":
				out = nil
			case "typed_stdout":
				out = (*touchpadCommandWriter)(nil)
			case "stderr":
				diagnostic = nil
			case "typed_stderr":
				diagnostic = (*touchpadCommandWriter)(nil)
			case "collector":
				collect = nil
			case "destination":
				destination = nil
			}
			if code := runTouchpadWithDestination(ctx, nil, out, diagnostic, collect, destination); code != 2 || calls != 0 {
				t.Fatal("nil boundary reached a callback")
			}
		})
	}
}

func TestTouchpadCommandStopsBeforeCollection(t *testing.T) {
	for _, name := range []string{"precancelled", "expired_deadline", "guidance_cancels"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if name == "precancelled" {
				cancel()
			}
			if name == "expired_deadline" {
				var stop context.CancelFunc
				ctx, stop = context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
				defer stop()
			}
			diagnostic := &touchpadCommandWriter{}
			if name == "guidance_cancels" {
				diagnostic.onWrite = cancel
			}
			calls := 0
			var out bytes.Buffer
			code := runTouchpadWith(ctx, nil, &out, diagnostic,
				func(context.Context, inputtest.TouchpadOptions) inputtest.TouchpadResult {
					calls++
					return inputtest.TouchpadResult{}
				})
			actual, err := touchpadevidence.Decode(out.Bytes())
			wantStatus, wantStop := inputtest.StatusAborted, "operator_cancelled"
			if name == "expired_deadline" {
				wantStatus, wantStop = inputtest.StatusError, "deadline"
			}
			if code != 0 || calls != 0 || err != nil || actual.Status != wantStatus || actual.StopReason != wantStop ||
				actual.LimitMilliseconds != 30000 || len(actual.Devices) != 0 || !actual.CleanupConfirmed {
				t.Fatal("pre-collection stop did not produce an honest complete diagnostic")
			}
			if name != "guidance_cancels" && diagnostic.writes != 0 {
				t.Fatal("pre-stopped command wrote guidance")
			}
		})
	}
}

func TestTouchpadCommandGuidanceWriteFailure(t *testing.T) {
	for _, name := range []string{"short_nil", "writer_error"} {
		t.Run(name, func(t *testing.T) {
			writer := &touchpadCommandWriter{short: name == "short_nil"}
			if name == "writer_error" {
				writer.writeErr = errors.New("canary-raw-error")
			}
			calls := 0
			var out bytes.Buffer
			if code := runTouchpadWith(t.Context(), nil, &out, writer,
				func(context.Context, inputtest.TouchpadOptions) inputtest.TouchpadResult {
					calls++
					return inputtest.TouchpadResult{}
				}); code != 1 || calls != 0 || out.Len() != 0 || writer.closes != 0 {
				t.Fatal("failed instruction write acquired devices or closed caller output")
			}
		})
	}
}

func TestTouchpadCommandRejectsMalformedCollector(t *testing.T) {
	for _, name := range []string{
		"limit_mismatch", "unknown_schema", "nil_devices", "unknown_detail", "path_interface",
		"bad_threshold", "missing_final_neutral", "contact_frame_overclaim", "axis_frame_overclaim",
		"unreleased_motion", "unknown_abs_nonzero", "oversized_codes",
	} {
		t.Run(name, func(t *testing.T) {
			result := touchpadCommandLiteral(t)
			d := &result.Devices[0]
			switch name {
			case "limit_mismatch":
				result.LimitMilliseconds = 29999
			case "unknown_schema":
				result.Schema = "canary-schema"
			case "nil_devices":
				result.Devices = nil
			case "unknown_detail":
				result.Detail = "canary-device-name"
			case "path_interface":
				d.Interface = "canary-device-path"
			case "bad_threshold":
				d.XThreshold = 49
			case "missing_final_neutral":
				d.FinalNeutral = false
			case "contact_frame_overclaim":
				d.ContactStarts, d.ContactReleases = 4, 4
			case "axis_frame_overclaim":
				d.XPositive, d.XNegative = 3, 3
			case "unreleased_motion":
				d.ContactReleases = 0
			case "unknown_abs_nonzero":
				d.Capabilities.Axes = append(d.Capabilities.Axes, inputtest.TouchpadAxis{Code: 47, Maximum: 10})
			case "oversized_codes":
				d.Capabilities.Keys = make([]uint16, 9)
			}
			var out, diagnostic bytes.Buffer
			calls := 0
			code := runTouchpadWith(t.Context(), nil, &out, &diagnostic,
				func(context.Context, inputtest.TouchpadOptions) inputtest.TouchpadResult { calls++; return result })
			if code != 1 || calls != 1 || out.Len() != 0 || strings.Contains(diagnostic.String(), "canary") {
				t.Fatal("malformed collector result was published or exposed raw text")
			}
		})
	}
}

func TestTouchpadCommandCleanupWinsOtherFailures(t *testing.T) {
	for _, name := range []string{
		"valid_diagnostic", "empty_no_owner", "aggregate_false_with_pass", "row_false_only", "invalid_schema",
		"limit_mismatch", "cancellation", "output_error", "output_short", "destination_close_error", "oversized_rows",
	} {
		t.Run(name, func(t *testing.T) {
			result := touchpadCommandLiteral(t)
			result.Status, result.StopReason, result.Detail = inputtest.StatusError, "cleanup_unconfirmed", "cleanup_unconfirmed"
			result.CleanupConfirmed = false
			result.Devices[0].Status, result.Devices[0].Detail = inputtest.StatusError, "cleanup_unconfirmed"
			result.Devices[0].CleanupConfirmed, result.Devices[0].FinalNeutral = false, false
			switch name {
			case "empty_no_owner":
				result.Devices = []inputtest.TouchpadDeviceResult{}
			case "aggregate_false_with_pass":
				result = touchpadCommandLiteral(t)
				result.CleanupConfirmed = false
			case "row_false_only":
				result = touchpadCommandLiteral(t)
				result.Devices[0].CleanupConfirmed = false
			case "invalid_schema":
				result.Schema = "canary-schema"
			case "limit_mismatch":
				result.LimitMilliseconds = 100
			case "oversized_rows":
				result = touchpadCommandLiteral(t)
				result.Devices = make([]inputtest.TouchpadDeviceResult, 129)
				for i := range result.Devices {
					result.Devices[i].CleanupConfirmed = true
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			out := &touchpadCommandWriter{}
			if name == "output_error" {
				out.writeErr = errors.New("canary-write")
			}
			if name == "output_short" {
				out.short = true
			}
			if name == "destination_close_error" {
				out.closeErr = errors.New("canary-close")
			}
			var diagnostic bytes.Buffer
			destination := writeDestination
			if name == "destination_close_error" {
				destination = func(path string, stdout io.Writer, write func(io.Writer) error) error {
					return writeDestinationWith("owned.json", stdout, write, func(string, int, os.FileMode) (io.WriteCloser, error) { return out, nil })
				}
			}
			collect := func(context.Context, inputtest.TouchpadOptions) inputtest.TouchpadResult {
				if name == "cancellation" {
					cancel()
				}
				return result
			}
			code := runTouchpadWithDestination(ctx, nil, out, &diagnostic, collect, destination)
			if code != workerCleanupUncertainExitCode || !strings.Contains(diagnostic.String(), "stop the session") ||
				strings.Contains(diagnostic.String(), "canary") {
				t.Fatal("cleanup uncertainty lost precedence or raw error escaped")
			}
			if name == "destination_close_error" && out.closes != 1 {
				t.Fatal("destination close was not attempted once")
			}
			if name == "valid_diagnostic" || name == "empty_no_owner" || name == "cancellation" {
				actual, err := touchpadevidence.Decode(out.Bytes())
				if !errors.Is(err, touchpadevidence.ErrCleanupUnconfirmed) || actual.CleanupConfirmed ||
					actual.StopReason != "cleanup_unconfirmed" {
					t.Fatal("valid bounded cleanup diagnostic was not preserved")
				}
			}
		})
	}
}

func TestTouchpadCommandDestinationOwnership(t *testing.T) {
	for _, name := range []string{"stdout", "exclusive_file", "write_error", "short_nil", "close_error", "write_and_close", "open_with_owner_error", "nil_owner", "open_error"} {
		t.Run(name, func(t *testing.T) {
			result := touchpadCommandLiteral(t)
			writer := &touchpadCommandWriter{}
			if name == "write_error" || name == "write_and_close" {
				writer.writeErr = errors.New("canary-write")
			}
			if name == "short_nil" {
				writer.short = true
			}
			if name == "close_error" || name == "write_and_close" {
				writer.closeErr = errors.New("canary-close")
			}
			opens := 0
			open := func(path string, flags int, mode os.FileMode) (io.WriteCloser, error) {
				opens++
				if path != "owned-output.json" || flags != os.O_WRONLY|os.O_CREATE|os.O_EXCL || mode != 0600 {
					t.Fatal("exclusive owned destination contract changed")
				}
				switch name {
				case "open_with_owner_error":
					return writer, errors.New("canary-open")
				case "nil_owner":
					return nil, nil
				case "open_error":
					return nil, errors.New("canary-open")
				default:
					return writer, nil
				}
			}
			args := []string{"-output", "owned-output.json"}
			if name == "stdout" {
				args = nil
			}
			var diagnostic bytes.Buffer
			destination := func(path string, stdout io.Writer, write func(io.Writer) error) error {
				return writeDestinationWith(path, stdout, write, open)
			}
			code := runTouchpadWithDestination(t.Context(), args, writer, &diagnostic,
				func(context.Context, inputtest.TouchpadOptions) inputtest.TouchpadResult { return result }, destination)
			want := 1
			if name == "stdout" || name == "exclusive_file" {
				want = 0
			}
			if code != want || strings.Contains(diagnostic.String(), "canary") {
				t.Fatal("destination outcome/diagnostic mismatch")
			}
			wantCloses := 1
			if name == "stdout" || name == "nil_owner" || name == "open_error" {
				wantCloses = 0
			}
			if writer.closes != wantCloses || opens != map[bool]int{true: 0, false: 1}[name == "stdout"] {
				t.Fatal("destination ownership was lost")
			}
			if want == 0 {
				actual, err := touchpadevidence.Decode(writer.Bytes())
				if err != nil || !reflect.DeepEqual(actual, result) {
					t.Fatal("successful output changed the collector result")
				}
			}
		})
	}
}

func TestTouchpadCommandDestinationCallbackContract(t *testing.T) {
	for _, name := range []string{"missing", "duplicate", "nil_writer", "typed_nil_writer", "ignored_write_error"} {
		t.Run(name, func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			destination := func(_ string, _ io.Writer, write func(io.Writer) error) error {
				switch name {
				case "missing":
				case "duplicate":
					_ = write(&out)
					_ = write(&out)
				case "nil_writer":
					_ = write(nil)
				case "typed_nil_writer":
					_ = write((*touchpadCommandWriter)(nil))
				case "ignored_write_error":
					_ = write(&touchpadCommandWriter{writeErr: errors.New("canary-write")})
				}
				return nil
			}
			result := touchpadCommandLiteral(t)
			if code := runTouchpadWithDestination(t.Context(), nil, &out, &diagnostic,
				func(context.Context, inputtest.TouchpadOptions) inputtest.TouchpadResult { return result }, destination); code != 1 ||
				strings.Contains(diagnostic.String(), "canary") {
				t.Fatal("destination adapter hid a failed/missing/duplicate write")
			}
		})
	}
}

func TestTouchpadCommandFreezesNestedResultBeforeDestination(t *testing.T) {
	expected := touchpadCommandLiteral(t)
	result := touchpadCommandLiteral(t)
	var out, diagnostic bytes.Buffer
	destination := func(_ string, _ io.Writer, write func(io.Writer) error) error {
		result.Devices[0].Identity.Product = 0
		result.Devices[0].Capabilities.Axes[0].Maximum = 7
		result.Devices[0].Capabilities.Keys[0] = 0
		result.Devices[0].Buttons[0].Pairs = 0
		result.Devices[0].CleanupConfirmed = false
		result.Devices[0].Interface = "canary-mutated-path"
		return write(&out)
	}
	if code := runTouchpadWithDestination(t.Context(), nil, &out, &diagnostic,
		func(context.Context, inputtest.TouchpadOptions) inputtest.TouchpadResult { return result }, destination); code != 0 {
		t.Fatal("owned snapshot was replaced by destination callback alias")
	}
	actual, err := touchpadevidence.Decode(out.Bytes())
	if err != nil || !reflect.DeepEqual(actual, expected) || strings.Contains(out.String()+diagnostic.String(), "canary") {
		t.Fatal("nested collector alias reached the frozen output")
	}
}

func TestTouchpadCommandPublicationInterruption(t *testing.T) {
	for _, name := range []string{"during_write", "during_close", "cleanup_and_write_cancel"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := touchpadCommandLiteral(t)
			want := 1
			if name == "cleanup_and_write_cancel" {
				result.Status, result.StopReason, result.Detail = inputtest.StatusError, "cleanup_unconfirmed", "cleanup_unconfirmed"
				result.CleanupConfirmed = false
				result.Devices = []inputtest.TouchpadDeviceResult{}
				want = workerCleanupUncertainExitCode
			}
			writer := &touchpadCommandWriter{}
			var diagnostic bytes.Buffer
			destination := func(_ string, stdout io.Writer, write func(io.Writer) error) error {
				if name == "during_close" {
					writer.onClose = cancel
					return writeDestinationWith("owned.json", stdout, write, func(string, int, os.FileMode) (io.WriteCloser, error) { return writer, nil })
				}
				writer.onWrite = cancel
				return write(writer)
			}
			code := runTouchpadWithDestination(ctx, nil, writer, &diagnostic,
				func(context.Context, inputtest.TouchpadOptions) inputtest.TouchpadResult { return result }, destination)
			if code != want || writer.Len() == 0 {
				t.Fatal("late interruption returned success or erased already written diagnostics")
			}
		})
	}
}

func TestTouchpadCommandDispatchAndUsage(t *testing.T) {
	var out, diagnostic bytes.Buffer
	if code := run(t.Context(), []string{"touchpad", "-duration", "0"}, strings.NewReader(""), &out, &diagnostic); code != 2 ||
		out.Len() != 0 || !strings.Contains(diagnostic.String(), "touchpad accepts only") {
		t.Fatal("standalone command dispatch is not connected")
	}
	out.Reset()
	diagnostic.Reset()
	if code := run(t.Context(), nil, strings.NewReader(""), &out, &diagnostic); code != 2 ||
		!strings.Contains(diagnostic.String(), "hhpt touchpad [-duration 30s] [-timeout 90s] [-output FILE]") {
		t.Fatal("usage omits the bounded standalone command")
	}
}
