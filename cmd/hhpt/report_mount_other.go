//go:build !linux

package main

import (
	"context"
	"errors"
	"io"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

func mountAuthorizedReportTarget(context.Context, reporttarget.Options, reporttarget.Authorization) (func() error, error) {
	return nil, errors.New("report-target mounting is unavailable on this platform")
}

func runInternalReportMount(context.Context, []string, io.Reader, io.Writer) int { return 1 }
