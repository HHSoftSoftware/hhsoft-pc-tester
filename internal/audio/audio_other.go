//go:build !linux

package audio

import "context"

func runPlatform(ctx context.Context, _ Options) Result {
	if ctx.Err() != nil {
		return Result{Schema: Schema, Status: StatusAborted, Readiness: ReadinessUnknown, Detail: "cancelled"}
	}
	return Result{Schema: Schema, Status: StatusError, Readiness: ReadinessUnknown, Detail: "audio readiness is available only in the Linux boot environment"}
}
