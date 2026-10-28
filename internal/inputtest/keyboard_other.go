//go:build !linux

package inputtest

import (
	"context"
	"time"
)

func runKeyboardPlatform(ctx context.Context, options KeyboardOptions) KeyboardResult {
	result := newKeyboardResult(options)
	if nilInputValue(ctx) || options.Duration < 10*time.Millisecond || options.Duration > 90*time.Second {
		result.LimitMilliseconds, result.Detail = 0, "invalid_options"
		return result
	}
	if stopped := ctx.Err(); stopped != nil {
		result.MarkInterrupted(stopped)
		return result
	}
	result.Detail = "unsupported_platform"
	return result
}
