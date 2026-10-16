//go:build linux

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/deploymentkey"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

const deploymentKeyMountPoint = "/run/hhpt/deployment-key-media"

func loadDeploymentKeyMediaPlatform(ctx context.Context, options reporttarget.Options, candidate reporttarget.Candidate) (deploymentkey.Record, func() error, error) {
	if !currentDeploymentCandidate(ctx, options, candidate) {
		return deploymentkey.Record{}, nil, errors.New("selected USB key token changed before mounting")
	}
	if err := prepareDeploymentKeyMountPoint(); err != nil {
		return deploymentkey.Record{}, nil, err
	}
	filesystem, err := probeDeploymentFilesystem(ctx, candidate.Device)
	if err != nil {
		return deploymentkey.Record{}, nil, err
	}
	mountOptions := "ro,nodev,nosuid,noexec"
	if filesystem == "ext4" {
		mountOptions += ",noload"
	}
	mountContext, cancelMount := context.WithTimeout(ctx, 15*time.Second)
	defer cancelMount()
	command := exec.CommandContext(mountContext, "/bin/mount", "-t", filesystem, "-o", mountOptions, candidate.Device, deploymentKeyMountPoint)
	command.Env = []string{"PATH=/sbin:/bin", "LANG=C"}
	command.Stdin = nil
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return deploymentkey.Record{}, nil, errors.New("selected USB key token could not be mounted with the fixed read-only policy")
	}
	unmount := unmountDeploymentKeyMedia
	fail := func(message string) (deploymentkey.Record, func() error, error) {
		_ = unmount()
		return deploymentkey.Record{}, nil, errors.New(message)
	}
	if !validDeploymentKeyMount(filesystem, candidate) {
		return fail("mounted USB key token does not match the selected partition and read-only policy")
	}
	if !currentMountedDeploymentCandidate(ctx, options, candidate) {
		return fail("USB key token changed while mounting")
	}
	record, err := readMountedDeploymentKey()
	if err != nil {
		_ = unmount()
		return deploymentkey.Record{}, nil, err
	}
	if !validDeploymentKeyMount(filesystem, candidate) || !currentMountedDeploymentCandidate(ctx, options, candidate) {
		return fail("USB key token changed while reading")
	}
	return record, unmount, nil
}

func currentDeploymentCandidate(ctx context.Context, options reporttarget.Options, selected reporttarget.Candidate) bool {
	for _, candidate := range eligibleDeploymentKeyMedia(reporttarget.Discover(ctx, options).Candidates) {
		if sameDeploymentCandidate(candidate, selected) {
			return true
		}
	}
	return false
}

func currentMountedDeploymentCandidate(ctx context.Context, options reporttarget.Options, selected reporttarget.Candidate) bool {
	for _, candidate := range reporttarget.Discover(ctx, options).Candidates {
		if sameDeploymentCandidate(candidate, selected) && candidate.Mounted && candidate.NodeReady {
			return true
		}
	}
	return false
}

func sameDeploymentCandidate(left, right reporttarget.Candidate) bool {
	return left.Device == right.Device && left.Parent == right.Parent && left.Topology == right.Topology &&
		left.DeviceMajorMinor == right.DeviceMajorMinor && left.ParentMajorMinor == right.ParentMajorMinor &&
		left.SizeBytes == right.SizeBytes && left.ParentSizeBytes == right.ParentSizeBytes &&
		left.FingerprintKeyID == right.FingerprintKeyID && left.MediaIdentity == right.MediaIdentity &&
		left.MediaIdentitySource == right.MediaIdentitySource && left.ReadOnly == right.ReadOnly
}

func probeDeploymentFilesystem(ctx context.Context, device string) (string, error) {
	probeContext, cancelProbe := context.WithTimeout(ctx, 15*time.Second)
	defer cancelProbe()
	command := exec.CommandContext(probeContext, "/sbin/blkid", "-p", "-s", "TYPE", "-o", "value", device)
	command.Env = []string{"PATH=/sbin:/bin", "LANG=C"}
	command.Stdin = nil
	command.Stderr = io.Discard
	var output strings.Builder
	command.Stdout = &output
	if err := command.Run(); err != nil {
		return "", errors.New("cannot identify the USB key token filesystem")
	}
	if output.Len() > 32 {
		return "", errors.New("USB key token filesystem identifier is invalid")
	}
	filesystem := strings.TrimSpace(output.String())
	if filesystem != "vfat" && filesystem != "exfat" && filesystem != "ext4" {
		return "", errors.New("USB key token filesystem must be vfat, exfat, or ext4")
	}
	return filesystem, nil
}

func prepareDeploymentKeyMountPoint() error {
	path := filepath.Clean("/")
	for _, segment := range strings.Split(strings.TrimPrefix(deploymentKeyMountPoint, "/"), "/") {
		path = filepath.Join(path, segment)
		if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return errors.New("cannot prepare the fixed deployment-key mount point")
		}
		metadata, err := os.Lstat(path)
		if err != nil || metadata.Mode()&os.ModeSymlink != 0 || !metadata.IsDir() {
			return errors.New("fixed deployment-key mount point is unsafe")
		}
	}
	entries, err := os.ReadDir(deploymentKeyMountPoint)
	if err != nil || len(entries) != 0 {
		return errors.New("fixed deployment-key mount point is not empty")
	}
	return nil
}

func validDeploymentKeyMount(filesystem string, candidate reporttarget.Candidate) bool {
	registry, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	defer registry.Close()
	return deploymentKeyMountMatches(registry, filesystem, candidate)
}

func deploymentKeyMountMatches(registry io.Reader, filesystem string, candidate reporttarget.Candidate) bool {
	found := false
	scanner := bufio.NewScanner(registry)
	scanner.Buffer(make([]byte, 1024), 1<<20)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		separator := -1
		for index, field := range fields {
			if field == "-" {
				separator = index
				break
			}
		}
		if len(fields) < 10 || separator < 6 || separator+3 >= len(fields) {
			return false
		}
		point := unescapeKeyMount(fields[4])
		if point != deploymentKeyMountPoint {
			continue
		}
		options := strings.Split(fields[5], ",")
		if found || fields[2] != candidate.DeviceMajorMinor || fields[3] != "/" || fields[separator+1] != filesystem || fields[separator+2] != candidate.Device ||
			!hasMountOption(options, "ro") || hasMountOption(options, "rw") || !hasMountOption(options, "nodev") || !hasMountOption(options, "nosuid") || !hasMountOption(options, "noexec") {
			return false
		}
		found = true
	}
	return scanner.Err() == nil && found
}

func readMountedDeploymentKey() (deploymentkey.Record, error) {
	directoryFD, err := syscall.Open(deploymentKeyMountPoint, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return deploymentkey.Record{}, errors.New("cannot open the deployment-key mount point")
	}
	directory := os.NewFile(uintptr(directoryFD), deploymentKeyMountPoint)
	defer directory.Close()
	fileFD, err := syscall.Openat(directoryFD, deploymentkey.Filename, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return deploymentkey.Record{}, fmt.Errorf("open %s: %w", deploymentkey.Filename, err)
	}
	file := os.NewFile(uintptr(fileFD), deploymentkey.Filename)
	defer file.Close()
	metadata, err := file.Stat()
	if err != nil || !metadata.Mode().IsRegular() || metadata.Size() <= 0 || metadata.Size() > deploymentkey.MaximumEncodedSize {
		return deploymentkey.Record{}, deploymentkey.ErrInvalid
	}
	encoded, err := io.ReadAll(io.LimitReader(file, deploymentkey.MaximumEncodedSize+1))
	if err != nil || int64(len(encoded)) != metadata.Size() {
		return deploymentkey.Record{}, deploymentkey.ErrInvalid
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(metadata, after) || metadata.Size() != after.Size() {
		return deploymentkey.Record{}, deploymentkey.ErrInvalid
	}
	return deploymentkey.Decode(encoded)
}

func unmountDeploymentKeyMedia() error {
	unmountContext, cancelUnmount := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelUnmount()
	command := exec.CommandContext(unmountContext, "/bin/umount", deploymentKeyMountPoint)
	command.Env = []string{"PATH=/sbin:/bin", "LANG=C"}
	command.Stdin = nil
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return errors.New("USB key token could not be unmounted")
	}
	return nil
}

func hasMountOption(options []string, wanted string) bool {
	for _, option := range options {
		if option == wanted {
			return true
		}
	}
	return false
}

func unescapeKeyMount(value string) string {
	for encoded, decoded := range map[string]string{"\\040": " ", "\\011": "\t", "\\012": "\n", "\\134": "\\"} {
		value = strings.ReplaceAll(value, encoded, decoded)
	}
	return value
}
