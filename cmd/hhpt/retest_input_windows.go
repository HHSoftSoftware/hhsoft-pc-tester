//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var retestReOpenFile = syscall.NewLazyDLL("kernel32.dll").NewProc("ReOpenFile")

type retestWindowsFile struct {
	*os.File
}

func (file *retestWindowsFile) Stat() (os.FileInfo, error) {
	handle := syscall.Handle(file.Fd())
	kind, err := syscall.GetFileType(handle)
	if err != nil || kind != syscall.FILE_TYPE_DISK {
		return nil, errRetestInput
	}
	var info syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(handle, &info) != nil || info.FileAttributes&(syscall.FILE_ATTRIBUTE_REPARSE_POINT|syscall.FILE_ATTRIBUTE_DIRECTORY|0x40) != 0 || info.NumberOfLinks == 0 {
		return nil, errRetestInput
	}
	return file.File.Stat()
}

func realRetestInputOperations() retestInputOperations {
	wrap := func(handle syscall.Handle) retestInputReadable {
		file := os.NewFile(uintptr(handle), "report input")
		if file == nil {
			return nil
		}
		return &retestWindowsFile{File: file}
	}
	return retestInputOperations{
		openMetadata: func(path string) (retestInputMetadata, error) {
			return openRetestWindowsMetadata(path, func(path string) (syscall.Handle, error) {
				name, err := syscall.UTF16PtrFromString(path)
				if err != nil {
					return syscall.InvalidHandle, errRetestInput
				}
				// FILE_READ_ATTRIBUTES grants metadata access, not file data.
				// Request read sharing; object proof does not rely on pathname immutability.
				return syscall.CreateFile(name, 0x80, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING,
					syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
			}, wrap, syscall.CloseHandle)
		},
		openReadable: func(_ string, metadata retestInputMetadata) (retestInputReadable, error) {
			file, ok := metadata.(*retestWindowsFile)
			if !ok || file == nil || file.File == nil {
				return nil, errRetestInput
			}
			if err := retestReOpenFile.Find(); err != nil {
				return nil, errRetestInput
			}
			// ReOpenFile opens the proven handle's object, never the pathname.
			value, _, callErr := retestReOpenFile.Call(file.Fd(), uintptr(syscall.GENERIC_READ),
				uintptr(syscall.FILE_SHARE_READ), uintptr(syscall.FILE_FLAG_OPEN_REPARSE_POINT))
			handle := syscall.Handle(value)
			var err error
			if handle == syscall.InvalidHandle {
				err = callErr
			}
			return adoptRetestWindowsHandle(handle, err, wrap, syscall.CloseHandle)
		},
		sameFile: os.SameFile,
	}
}

func openRetestWindowsMetadata(path string, open func(string) (syscall.Handle, error), wrap func(syscall.Handle) retestInputReadable, closeHandle func(syscall.Handle) error) (retestInputMetadata, error) {
	// Restrict the adapter to conventional local-drive file paths. Device,
	// volume, UNC, extended namespace and alternate-stream spellings are not
	// eligible. This lexical check does not replace descriptor metadata proof.
	// FullPath may remove trailing dots/spaces or dot components. Reject
	// unsafe original components before that normalization can erase them.
	spelling := strings.ReplaceAll(path, "/", `\`)
	if spelling == "" || strings.HasPrefix(spelling, `\\`) {
		return nil, errRetestInput
	}
	components := spelling
	if len(components) >= 2 && components[1] == ':' {
		if !((components[0] >= 'A' && components[0] <= 'Z') || (components[0] >= 'a' && components[0] <= 'z')) {
			return nil, errRetestInput
		}
		components = components[2:]
	}
	for _, part := range strings.Split(components, `\`) {
		if part == "" || part == "." || part == ".." {
			continue
		}
		if !retestWindowsInputComponent(part) {
			return nil, errRetestInput
		}
	}
	absolute, err := filepath.Abs(path)
	if err != nil || len(absolute) < 4 || absolute[1] != ':' || absolute[2] != '\\' || !((absolute[0] >= 'A' && absolute[0] <= 'Z') || (absolute[0] >= 'a' && absolute[0] <= 'z')) {
		return nil, errRetestInput
	}
	for _, part := range strings.Split(absolute[3:], `\`) {
		if !retestWindowsInputComponent(part) {
			return nil, errRetestInput
		}
	}
	handle, failure := open(absolute)
	return adoptRetestWindowsHandle(handle, failure, wrap, closeHandle)
}

func retestWindowsInputComponent(part string) bool {
	base := strings.SplitN(part, ".", 2)[0]
	return filepath.IsLocal(part) && (base == "" || filepath.IsLocal(base)) && !strings.HasSuffix(part, " ") && !strings.HasSuffix(part, ".") && strings.IndexFunc(part, func(r rune) bool { return r < 32 }) < 0
}

// An acquired raw handle is either transferred to the returned owner, even
// alongside an error, or closed here with its result preserved safely.
func adoptRetestWindowsHandle(handle syscall.Handle, failure error, wrap func(syscall.Handle) retestInputReadable, closeHandle func(syscall.Handle) error) (retestInputReadable, error) {
	if handle == syscall.InvalidHandle {
		return nil, errRetestInput
	}
	file := wrap(handle)
	if nilRetestInputValue(file) {
		if closeHandle(handle) != nil {
			return nil, errors.Join(errRetestInput, errRetestInputCleanup)
		}
		return nil, errRetestInput
	}
	return file, failure
}
