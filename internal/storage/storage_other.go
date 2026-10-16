//go:build !linux

package storage

import "context"

func runPlatform(context.Context, Options) Result {
	return Result{Schema: Schema, Status: StatusError, Detail: "storage readiness is available only in the Linux boot environment"}
}
