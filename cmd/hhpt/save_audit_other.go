//go:build !linux

package main

import (
	"errors"
	"os"
)

func openSaveAuditConsole() (*os.File, error) {
	return nil, errors.New("report-save console audit output requires Linux")
}
