package safemode

import (
	"context"
	"testing"
	"time"
)

func monitorSafeResult() Result {
	return Result{Schema: Schema, Status: "pass", StorageObserved: true, NetworkObserved: true}
}
func awaitMonitorFailure(t *testing.T, monitor *Monitor) {
	t.Helper()
	select {
	case <-monitor.Failure():
	case <-time.After(5 * time.Second):
		t.Fatal("monitor did not report failure")
	}
}
func stopTestMonitor(t *testing.T, monitor *Monitor) MonitorResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return monitor.Stop(ctx)
}
