//go:build !linux

package main

import (
	"context"
	"errors"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/deploymentkey"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

func loadDeploymentKeyMediaPlatform(context.Context, reporttarget.Options, reporttarget.Candidate) (deploymentkey.Record, func() error, error) {
	return deploymentkey.Record{}, nil, errors.New("deployment-key media activation is supported only in the bootable Linux environment")
}
