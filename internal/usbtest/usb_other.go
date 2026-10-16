//go:build !linux

package usbtest

import "context"

func runPlatform(context.Context, Options) Result {
	return Result{Schema: Schema, Status: StatusError, Detail: "USB readiness is available only in the Linux boot environment"}
}
