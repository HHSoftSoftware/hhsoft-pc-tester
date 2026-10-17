//go:build linux

package safemode

import "context"

func Observe(ctx context.Context) Result { return check(ctx, "/") }
