//go:build !linux

package inputtest

import "context"

func runTouchpadPlatform(ctx context.Context, options TouchpadOptions) TouchpadResult {
	return unsupportedTouchpadPlatform(ctx, options)
}
