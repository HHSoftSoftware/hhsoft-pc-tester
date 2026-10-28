package usbtest

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
)

const maximumUSBMetadataBytes = 256

var (
	errUSBNodeClose     = errors.New("USB device node closure could not be confirmed")
	errUSBMetadataRead  = errors.New("cannot verify USB registry identity")
	errUSBMetadataClose = errors.New("USB registry metadata closure could not be confirmed")
)

type usbMetadataFile interface {
	Read([]byte) (int, error)
	Stat() (os.FileInfo, error)
	Close() error
}

func nilUSBValue(value any) bool {
	if value == nil {
		return true
	}
	switch reflect.ValueOf(value).Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflect.ValueOf(value).IsNil()
	default:
		return false
	}
}

// This reader bounds the new mandatory identity observations before allocation.
// It does not change the older optional USB attribute acquisition policy.
func readUSBMetadata(ctx context.Context, path string, open func(string) (usbMetadataFile, error)) (data []byte, err error) {
	if nilUSBValue(ctx) || open == nil {
		return nil, errUSBMetadataRead
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, openErr := open(path)
	if nilUSBValue(file) {
		return nil, errUSBMetadataRead
	}
	defer func() {
		if file.Close() != nil {
			data, err = nil, errUSBMetadataClose
			return
		}
		if err != nil {
			data = nil
			return
		}
		if stopped := ctx.Err(); stopped != nil {
			data, err = nil, stopped
		}
	}()
	if openErr != nil {
		return nil, errUSBMetadataRead
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, statErr := file.Stat()
	if statErr != nil || nilUSBValue(info) || !info.Mode().IsRegular() {
		return nil, errUSBMetadataRead
	}
	var buffer [maximumUSBMetadataBytes + 1]byte
	used := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		request := buffer[used:min(len(buffer), used+32)]
		n, readErr := file.Read(request)
		if n < 0 || n > len(request) || n == 0 && readErr == nil || used+n > maximumUSBMetadataBytes {
			return nil, errUSBMetadataRead
		}
		if readErr != nil && readErr != io.EOF {
			return nil, errUSBMetadataRead
		}
		used += n
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if readErr == io.EOF {
			return buffer[:used], nil
		}
	}
}
