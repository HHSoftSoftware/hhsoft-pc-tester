//go:build !linux

package inputtest

import "context"

func runPlatform(context.Context, Options) Result {
	return Result{Schema: Schema, Status: StatusError, Detail: "input readiness is available only in the Linux boot environment"}
}
