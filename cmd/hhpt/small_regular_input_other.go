//go:build !linux

package main

import "os"

func openSmallRegularInput(path string, maximum int64) (retestInputReadable, os.FileInfo, error) {
	return openSmallRegularInputLegacy(path, maximum, smallRegularLegacyOperations{
		lstat: os.Lstat,
		open: func(path string) (retestInputReadable, error) {
			file, err := os.Open(path)
			if file == nil {
				return nil, err
			}
			return file, err
		},
		sameFile: os.SameFile,
	})
}
