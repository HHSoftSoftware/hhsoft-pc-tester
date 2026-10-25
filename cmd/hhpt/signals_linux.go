//go:build linux

package main

import (
	"os"
	"syscall"
)

// Supervisors request cooperative cleanup before considering any hard stop.
func terminationSignals() []os.Signal { return []os.Signal{os.Interrupt, syscall.SIGTERM} }
