//go:build !linux

package inputtest

import (
	"context"
	"time"
)

func runPointerPlatform(ctx context.Context, options PointerOptions) PointerResult {
	result := PointerResult{Schema: PointerSchema, Status: StatusError, StopReason: "error", LimitMilliseconds: options.Duration.Milliseconds(),
		Detail: "pointer functional testing is available only in the Linux boot environment"}
	if ctx == nil || options.Duration < 10*time.Millisecond || options.Duration > 90*time.Second {
		result.LimitMilliseconds = 0
		return result
	}
	if ctx != nil {
		result.MarkInterrupted(ctx.Err())
	}
	return result
}
