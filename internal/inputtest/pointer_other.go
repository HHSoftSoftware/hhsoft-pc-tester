//go:build !linux

package inputtest

import "context"

func runPointerPlatform(ctx context.Context, options PointerOptions) PointerResult {
	result := PointerResult{Schema: PointerSchema, Status: StatusError, StopReason: "error", LimitMilliseconds: options.Duration.Milliseconds(),
		Detail: "pointer functional testing is available only in the Linux boot environment"}
	if ctx != nil {
		result.MarkInterrupted(ctx.Err())
	}
	return result
}
