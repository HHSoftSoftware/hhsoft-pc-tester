//go:build !linux

package networktest

import "context"

func runPlatform(context.Context, Options) Result {
	return Result{Schema: Schema, Status: StatusError, Detail: "network readiness is available only in the Linux boot environment"}
}
