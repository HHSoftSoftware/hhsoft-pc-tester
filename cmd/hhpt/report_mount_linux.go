//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

type reportMountRequest struct {
	Authorization  reporttarget.Authorization `json:"authorization"`
	FingerprintKey []byte                     `json:"fingerprint_key"`
}

type reportMountOperations struct {
	validate func(context.Context, reporttarget.Options, reporttarget.Authorization) error
	check    func(*os.File, *os.File, reporttarget.Authorization) error
	mount    func(string, string, string, uintptr, string) error
}

func mountAuthorizedReportTarget(ctx context.Context, options reporttarget.Options, authorization reporttarget.Authorization) (func() error, error) {
	if options.Root != "" && options.Root != "/" {
		return nil, errors.New("report mounting requires the live filesystem")
	}
	if err := reporttarget.ValidateMountAuthorization(ctx, options, authorization); err != nil {
		return nil, err
	}
	deviceFD, err := syscall.Open(authorization.Device.Device, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("cannot bind the authorized report block device")
	}
	device := os.NewFile(uintptr(deviceFD), "authorized-report-device")
	defer device.Close()
	directoryFD, err := syscall.Open(reporttarget.MountPoint, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("cannot bind the fixed report mount point")
	}
	directory := os.NewFile(uintptr(directoryFD), "authorized-report-mount-point")
	defer directory.Close()
	if err := checkReportMountDescriptors(device, directory, authorization); err != nil {
		return nil, err
	}
	request, err := json.Marshal(reportMountRequest{Authorization: authorization, FingerprintKey: options.FingerprintKey})
	if err != nil {
		return nil, errors.New("cannot encode report mount authorization")
	}
	mountContext, cancelMount := context.WithTimeout(ctx, 15*time.Second)
	defer cancelMount()
	// Re-exec our own image and pass open descriptors. External mount helpers
	// may canonicalize /proc/self/fd into a device pathname and reintroduce a race.
	command := exec.CommandContext(mountContext, "/proc/self/exe", "_mount-report")
	command.Env = []string{"PATH=/sbin:/bin", "LANG=C"}
	command.ExtraFiles = []*os.File{device, directory}
	command.Stdin = bytes.NewReader(request)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := command.Run(); err != nil {
		// A timeout may occur after the kernel completed mounting but before
		// the child reported success. The reserved mount point must be cleaned up.
		_ = unmountReportTarget()
		return nil, errors.New("authorized report partition could not be mounted with the fixed safe policy")
	}
	return unmountReportTarget, nil
}

func runInternalReportMount(ctx context.Context, arguments []string, input io.Reader, stderr io.Writer) int {
	if len(arguments) != 0 {
		return 2
	}
	encoded, err := io.ReadAll(io.LimitReader(input, (64<<10)+1))
	if err != nil || len(encoded) == 0 || len(encoded) > 64<<10 {
		return 2
	}
	var request reportMountRequest
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		return 2
	}
	var trailing any
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		return 2
	}
	device := os.NewFile(3, "authorized-report-device")
	directory := os.NewFile(4, "authorized-report-mount-point")
	defer device.Close()
	defer directory.Close()
	options := reporttarget.Options{Root: "/", FingerprintKey: request.FingerprintKey}
	err = performReportMount(ctx, options, request.Authorization, device, directory, reportMountOperations{
		validate: reporttarget.ValidateMountAuthorization, check: checkReportMountDescriptors, mount: syscall.Mount,
	})
	if err != nil {
		// Never print the request, key, device content, or raw kernel diagnostics.
		fmt.Fprintln(stderr, "report mount authorization or bound device validation failed")
		return 1
	}
	return 0
}

func performReportMount(ctx context.Context, options reporttarget.Options, authorization reporttarget.Authorization, device, directory *os.File, operations reportMountOperations) error {
	if err := operations.validate(ctx, options, authorization); err != nil {
		return err
	}
	if err := operations.check(device, directory, authorization); err != nil {
		return err
	}
	filesystem, err := probeReportFilesystem(device)
	if err != nil {
		return err
	}
	// Repeat the MAC, lifetime, topology, stable-media and open-node checks
	// after probing. The eventual mount uses these FDs, never a reopened path.
	if err := operations.validate(ctx, options, authorization); err != nil {
		return err
	}
	if err := operations.check(device, directory, authorization); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	source := fmt.Sprintf("/proc/self/fd/%d", device.Fd())
	target := fmt.Sprintf("/proc/self/fd/%d", directory.Fd())
	return operations.mount(source, target, filesystem, syscall.MS_NODEV|syscall.MS_NOSUID|syscall.MS_NOEXEC, "")
}

func checkReportMountDescriptors(device, directory *os.File, authorization reporttarget.Authorization) error {
	opened, err := device.Stat()
	current, currentErr := os.Lstat(authorization.Device.Device)
	if err != nil || currentErr != nil || !directBlockMetadata(opened) || !directBlockMetadata(current) || !os.SameFile(opened, current) {
		return errors.New("authorized report device pathname no longer identifies the bound block node")
	}
	stat, ok := opened.Sys().(*syscall.Stat_t)
	if !ok || reportDeviceNumber(uint64(stat.Rdev)) != authorization.Device.DeviceMajorMinor {
		return errors.New("bound report block device number changed")
	}
	openedDirectory, err := directory.Stat()
	currentDirectory, currentErr := os.Lstat(reporttarget.MountPoint)
	if err != nil || currentErr != nil || !openedDirectory.IsDir() || !currentDirectory.IsDir() ||
		currentDirectory.Mode()&os.ModeSymlink != 0 || !os.SameFile(openedDirectory, currentDirectory) {
		return errors.New("fixed report mount point no longer identifies the bound directory")
	}
	return nil
}

func directBlockMetadata(metadata os.FileInfo) bool {
	return metadata.Mode()&os.ModeSymlink == 0 && metadata.Mode()&os.ModeDevice != 0 && metadata.Mode()&os.ModeCharDevice == 0
}

func reportDeviceNumber(device uint64) string {
	major := (device>>8)&0xfff | (device >> 32 & 0xfffff000)
	minor := device&0xff | (device >> 12 & 0xffffff00)
	return fmt.Sprintf("%d:%d", major, minor)
}

func unmountReportTarget() error {
	unmountContext, cancelUnmount := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelUnmount()
	command := exec.CommandContext(unmountContext, "/bin/umount", reporttarget.MountPoint)
	command.Env = []string{"PATH=/sbin:/bin", "LANG=C"}
	command.Stdin = nil
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return errors.New("report partition could not be unmounted after persistence")
	}
	return nil
}
