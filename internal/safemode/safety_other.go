//go:build !linux

package safemode

import "context"

func Observe(context.Context) Result {
	return Result{Schema: Schema, Status: "error", StorageDetail: "Linux kernel observations required", NetworkDetail: "Linux kernel observations required"}
}
