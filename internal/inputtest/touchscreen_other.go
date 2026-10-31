//go:build !linux

package inputtest

import "context"

func runTouchscreenPlatform(ctx context.Context, options TouchscreenOptions) TouchscreenResult {
	return unsupportedTouchscreenPlatform(ctx, options)
}
