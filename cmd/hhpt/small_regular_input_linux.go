//go:build linux

package main

import "os"

func openSmallRegularInput(path string, maximum int64) (retestInputReadable, os.FileInfo, error) {
	return openSmallRegularInputWithOperations(path, maximum, realRetestInputOperations())
}
