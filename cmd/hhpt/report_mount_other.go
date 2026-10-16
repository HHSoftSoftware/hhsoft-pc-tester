//go:build !linux

package main

import (
	"context"
	"errors"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

func mountAuthorizedReportTarget(context.Context, reporttarget.Authorization) (func() error, error) {
	return nil, errors.New("report-target mounting is unavailable on this platform")
}
