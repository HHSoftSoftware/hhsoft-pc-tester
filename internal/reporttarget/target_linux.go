//go:build linux

package reporttarget

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/identity"
)

const maximumBlockEntries = 512

var usbControllerSegment = regexp.MustCompile(`^usb[0-9]+$`)
var usbDeviceSegment = regexp.MustCompile(`^[0-9]+-[0-9]+(\.[0-9]+)*$`)

type mountRecord struct {
	Device     string
	Root       string
	Point      string
	Options    []string
	Filesystem string
}

type blockRecord struct {
	Name   string
	Parent string
	Device string
}

type nodeInspector func(string, string, string) bool

func discoverPlatform(ctx context.Context, options Options) Catalog {
	return discoverWith(ctx, options, inspectDirectBlockNode)
}

func discoverWith(ctx context.Context, options Options, inspectNode nodeInspector) Catalog {
	result := Catalog{Schema: CatalogSchema, Status: StatusError}
	root, keyID, err := validateOptions(options)
	if err != nil {
		result.Detail = err.Error()
		return result
	}
	mounts, err := readMountRegistry(root)
	if err != nil {
		result.Detail = "cannot verify report-media mount safety"
		return result
	}
	candidates, blocks, err := collectCandidates(ctx, root, options.FingerprintKey, keyID, inspectNode)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			result.Status = StatusAborted
			result.Detail = "cancelled"
		} else {
			result.Detail = err.Error()
		}
		return result
	}
	for index := range candidates {
		candidate := &candidates[index]
		if parentHasMount(candidate.Parent, mounts, blocks, "") {
			candidate.Mounted = true
			candidate.Eligible = false
			candidate.EligibleReadOnly = false
			candidate.Exclusion = "a partition on the report-media parent is mounted"
		}
	}
	result.Candidates = candidates
	for _, candidate := range candidates {
		if candidate.Eligible {
			result.Status = StatusPass
			return result
		}
	}
	if len(candidates) == 0 {
		result.Status = StatusNotPresent
		result.Detail = "no removable USB report partitions were discovered"
		return result
	}
	result.Status = StatusInconclusive
	result.Detail = "removable USB partitions were discovered but none are safe authorization targets"
	return result
}

func authorizePlatform(ctx context.Context, options Options, request AuthorizationRequest) (Authorization, error) {
	return authorizeWith(ctx, options, request, inspectDirectBlockNode)
}

func authorizeWith(ctx context.Context, options Options, request AuthorizationRequest, inspectNode nodeInspector) (Authorization, error) {
	catalog := discoverWith(ctx, options, inspectNode)
	if catalog.Status == StatusError || catalog.Status == StatusAborted {
		return Authorization{}, errors.New(catalog.Detail)
	}
	for _, candidate := range catalog.Candidates {
		if candidate.Device != request.Device {
			continue
		}
		if !candidate.Eligible {
			return Authorization{}, fmt.Errorf("report target is ineligible: %s", candidate.Exclusion)
		}
		if request.ConfirmationCode == "" || request.ConfirmationCode != candidate.ConfirmationCode {
			return Authorization{}, errors.New("typed report-target confirmation code does not match current media")
		}
		if err := prepareMountPoint(options.Root); err != nil {
			return Authorization{}, err
		}
		root, _, err := validateOptions(options)
		if err != nil {
			return Authorization{}, err
		}
		bootID, bootSeconds, err := currentBoot(root)
		if err != nil {
			return Authorization{}, err
		}
		return NewAuthorization(candidate, bootID, bootSeconds, options.FingerprintKey)
	}
	return Authorization{}, errors.New("requested report target was not discovered")
}

func persistPlatform(ctx context.Context, options Options, authorization Authorization, source *os.File, sourceSize int64, name string, expectedSHA256 [sha256.Size]byte) (Receipt, error) {
	return persistWith(ctx, options, authorization, source, sourceSize, name, expectedSHA256, inspectDirectBlockNode)
}

func persistWith(ctx context.Context, options Options, authorization Authorization, source *os.File, sourceSize int64, name string, expectedSHA256 [sha256.Size]byte, inspectNode nodeInspector) (Receipt, error) {
	if err := ValidateAuthorization(authorization); err != nil {
		return Receipt{}, err
	}
	if !validReportName(name) {
		return Receipt{}, errors.New("report name must be a plain .hhpcr filename")
	}
	root, keyID, err := validateOptions(options)
	if err != nil {
		return Receipt{}, err
	}
	if err := validateAuthorizationMAC(authorization, options.FingerprintKey); err != nil {
		return Receipt{}, err
	}
	if ctx.Err() != nil {
		return Receipt{}, ctx.Err()
	}
	if err := validateCurrentBoot(root, authorization); err != nil {
		return Receipt{}, err
	}
	metadata, err := source.Stat()
	if err != nil || !metadata.Mode().IsRegular() || metadata.Size() != sourceSize || sourceSize <= 0 || sourceSize > MaximumReportBytes {
		return Receipt{}, errors.New("source report is unavailable, changed, or exceeds the persistence limit")
	}
	candidates, blocks, err := collectCandidates(ctx, root, options.FingerprintKey, keyID, inspectNode)
	if err != nil {
		return Receipt{}, err
	}
	candidate, ok := matchingBinding(candidates, authorization.Device)
	if !ok || !candidate.NodeReady || candidate.Exclusion != "" {
		return Receipt{}, errors.New("authorized report media changed or is no longer usable")
	}
	mounts, err := readMountRegistry(root)
	if err != nil || !validReportMount(authorization, mounts, blocks) {
		return Receipt{}, errors.New("report media is not the sole safe mounted partition on its parent")
	}
	mountPath := filepath.Join(root, strings.TrimPrefix(MountPoint, "/"))
	mountMetadata, err := os.Lstat(mountPath)
	if err != nil || mountMetadata.Mode()&os.ModeSymlink != 0 || !mountMetadata.IsDir() {
		return Receipt{}, errors.New("report mount point is not a direct directory")
	}
	directoryFD, err := syscall.Open(mountPath, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return Receipt{}, errors.New("cannot open report mount point")
	}
	directory := os.NewFile(uintptr(directoryFD), mountPath)
	defer directory.Close()
	if !openedDirectoryMatches(directory, authorization.Device.DeviceMajorMinor) {
		return Receipt{}, errors.New("report mount point device changed")
	}
	destinationFD, err := syscall.Openat(directoryFD, name, syscall.O_RDWR|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return Receipt{}, fmt.Errorf("create report exclusively: %w", err)
	}
	destination := os.NewFile(uintptr(destinationFD), name)
	digest := sha256.New()
	written, writeErr := copyReport(ctx, io.MultiWriter(destination, digest), source, sourceSize)
	syncErr := destination.Sync()
	actualSHA256 := digest.Sum(nil)
	if writeErr != nil || syncErr != nil || written != sourceSize || !hmac.Equal(expectedSHA256[:], actualSHA256) {
		_ = destination.Close()
		return Receipt{}, errors.New("report copy did not preserve the verified bytes and sync")
	}
	readBackDigest := sha256.New()
	readBackBytes, readBackErr := copyReport(ctx, readBackDigest, destination, sourceSize)
	destinationMetadata, destinationStatErr := destination.Stat()
	closeErr := destination.Close()
	if readBackErr != nil || destinationStatErr != nil || closeErr != nil || readBackBytes != sourceSize || destinationMetadata.Size() != sourceSize ||
		!hmac.Equal(expectedSHA256[:], readBackDigest.Sum(nil)) {
		return Receipt{}, errors.New("report media did not read back the verified bytes")
	}
	if changedSource(source, metadata) {
		return Receipt{}, errors.New("source report changed during persistence")
	}
	refreshedCandidates, refreshedBlocks, refreshErr := collectCandidates(ctx, root, options.FingerprintKey, keyID, inspectNode)
	refreshedCandidate, stillMatches := matchingBinding(refreshedCandidates, authorization.Device)
	if mounts, err = readMountRegistry(root); err != nil || refreshErr != nil || !stillMatches || refreshedCandidate.Exclusion != "" || !refreshedCandidate.NodeReady ||
		!validReportMount(authorization, mounts, refreshedBlocks) || !openedDirectoryMatches(directory, authorization.Device.DeviceMajorMinor) ||
		validateCurrentBoot(root, authorization) != nil {
		return Receipt{}, errors.New("report media changed during persistence")
	}
	directorySynced := directory.Sync() == nil
	return Receipt{
		Schema: ReceiptSchema, AuthorizationID: authorization.ID, Device: authorization.Device.Device,
		File: name, Bytes: written, SHA256: "sha256:" + hex.EncodeToString(actualSHA256),
		FileSynced: true, ReadBackVerified: true, DirectorySynced: directorySynced,
	}, nil
}

func validateOptions(options Options) (string, string, error) {
	if len(options.FingerprintKey) != 32 {
		return "", "", errors.New("report-target operations require exactly 32 fingerprint-key bytes")
	}
	root := options.Root
	if root == "" {
		root = "/"
	}
	root = filepath.Clean(root)
	keyDigest, err := identity.DeploymentKeyID(options.FingerprintKey)
	if err != nil {
		return "", "", err
	}
	return root, "hmac-sha256:" + hex.EncodeToString(keyDigest[:]), nil
}

func collectCandidates(ctx context.Context, root string, fingerprintKey []byte, keyID string, inspectNode nodeInspector) ([]Candidate, map[string]blockRecord, error) {
	entries, err := os.ReadDir(filepath.Join(root, "sys/class/block"))
	if err != nil {
		return nil, nil, errors.New("cannot read block device registry")
	}
	if len(entries) > maximumBlockEntries {
		return nil, nil, errors.New("block device registry exceeds the supported limit")
	}
	blocks := make(map[string]blockRecord)
	type topology struct {
		name, parent, path, canonical, dev string
	}
	var partitions []topology
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		name := entry.Name()
		if !validBlockName(name) {
			continue
		}
		canonical, err := filepath.EvalSymlinks(filepath.Join(root, "sys/class/block", name))
		if err != nil || !withinDevices(root, canonical) {
			continue
		}
		dev := readSmallValue(filepath.Join(canonical, "dev"))
		if !validMajorMinor(dev) {
			continue
		}
		parent := name
		if fileExists(filepath.Join(canonical, "partition")) {
			parent = filepath.Base(filepath.Dir(canonical))
			partitions = append(partitions, topology{name: name, parent: parent, path: exportedPath(root, canonical), canonical: canonical, dev: dev})
		}
		blocks[dev] = blockRecord{Name: name, Parent: parent, Device: dev}
	}
	var candidates []Candidate
	for _, partition := range partitions {
		parentPath := filepath.Join(root, "sys/class/block", partition.parent)
		parentCanonical, err := filepath.EvalSymlinks(parentPath)
		if err != nil || !withinDevices(root, parentCanonical) || !isUSBTopology(root, parentCanonical) || readSmallValue(filepath.Join(parentCanonical, "removable")) != "1" {
			continue
		}
		deviceSectors, deviceOK := readUint(filepath.Join(partition.canonical, "size"))
		parentSectors, parentOK := readUint(filepath.Join(parentCanonical, "size"))
		parentDev := readSmallValue(filepath.Join(parentCanonical, "dev"))
		candidate := Candidate{
			Device: "/dev/" + partition.name, Parent: "/dev/" + partition.parent, Topology: partition.path,
			DeviceMajorMinor: partition.dev, ParentMajorMinor: parentDev,
			SizeBytes: sectorsToBytes(deviceSectors), ParentSizeBytes: sectorsToBytes(parentSectors),
			FingerprintKeyID: keyID,
		}
		candidate.MediaIdentity, candidate.MediaIdentitySource = mediaIdentity(fingerprintKey, parentCanonical)
		candidate.NodeReady = inspectNode(root, partition.name, partition.dev)
		candidate.ReadOnly = readSmallValue(filepath.Join(partition.canonical, "ro")) != "0" || readSmallValue(filepath.Join(parentCanonical, "ro")) != "0"
		candidate.Eligible = true
		candidate.EligibleReadOnly = true
		switch {
		case !deviceOK || !parentOK || candidate.SizeBytes < 16<<20 || candidate.ParentSizeBytes < candidate.SizeBytes:
			candidate.Eligible = false
			candidate.EligibleReadOnly = false
			candidate.Exclusion = "report partition capacity is invalid or too small"
		case !validMajorMinor(parentDev):
			candidate.Eligible = false
			candidate.EligibleReadOnly = false
			candidate.Exclusion = "report-media parent identity is unavailable"
		case !emptyDirectory(filepath.Join(partition.canonical, "holders")) || !emptyDirectory(filepath.Join(parentCanonical, "holders")):
			candidate.Eligible = false
			candidate.EligibleReadOnly = false
			candidate.Exclusion = "report media participates in another block mapping"
		case candidate.MediaIdentity == "":
			candidate.Eligible = false
			candidate.EligibleReadOnly = false
			candidate.Exclusion = "report media has no stable identifier for hot-plug binding"
		case !candidate.NodeReady:
			candidate.Eligible = false
			candidate.EligibleReadOnly = false
			candidate.Exclusion = "report partition is not a direct readable block node"
		case candidate.ReadOnly:
			candidate.Eligible = false
			candidate.Exclusion = "report media is read-only"
		}
		candidate.ConfirmationCode = confirmationCode(candidate)
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Device < candidates[j].Device })
	return candidates, blocks, nil
}

func matchingBinding(candidates []Candidate, binding DeviceBinding) (Candidate, bool) {
	for _, candidate := range candidates {
		if candidate.Device == binding.Device && candidate.Parent == binding.Parent && candidate.Topology == binding.Topology &&
			candidate.DeviceMajorMinor == binding.DeviceMajorMinor && candidate.ParentMajorMinor == binding.ParentMajorMinor &&
			candidate.SizeBytes == binding.SizeBytes && candidate.ParentSizeBytes == binding.ParentSizeBytes &&
			candidate.FingerprintKeyID == binding.FingerprintKeyID && candidate.MediaIdentity == binding.MediaIdentity &&
			candidate.MediaIdentitySource == binding.MediaIdentitySource {
			return candidate, true
		}
	}
	return Candidate{}, false
}

func validReportMount(authorization Authorization, mounts []mountRecord, blocks map[string]blockRecord) bool {
	found := false
	for _, mount := range mounts {
		block, isBlock := blocks[mount.Device]
		if !isBlock || "/dev/"+block.Parent != authorization.Device.Parent {
			continue
		}
		if found || mount.Device != authorization.Device.DeviceMajorMinor || mount.Point != authorization.MountPoint || mount.Root != "/" ||
			!containsAll(mount.Options, authorization.RequiredMountOptions) || !contains(authorization.AllowedFileSystemTypes, mount.Filesystem) {
			return false
		}
		found = true
	}
	return found
}

func parentHasMount(parent string, mounts []mountRecord, blocks map[string]blockRecord, exceptPoint string) bool {
	for _, mount := range mounts {
		block, ok := blocks[mount.Device]
		if ok && "/dev/"+block.Parent == parent && mount.Point != exceptPoint {
			return true
		}
	}
	return false
}

func prepareMountPoint(optionRoot string) error {
	root := optionRoot
	if root == "" {
		root = "/"
	}
	path := filepath.Clean(root)
	for _, segment := range strings.Split(strings.TrimPrefix(MountPoint, "/"), "/") {
		path = filepath.Join(path, segment)
		if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return errors.New("cannot prepare fixed report mount point")
		}
		metadata, err := os.Lstat(path)
		if err != nil || metadata.Mode()&os.ModeSymlink != 0 || !metadata.IsDir() {
			return errors.New("fixed report mount point is unsafe")
		}
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		return errors.New("fixed report mount point is not empty")
	}
	return nil
}

func currentBoot(root string) (string, uint64, error) {
	bootID := readSmallValue(filepath.Join(root, "proc/sys/kernel/random/boot_id"))
	uptimeFields := strings.Fields(readSmallValue(filepath.Join(root, "proc/uptime")))
	if len(bootID) != 36 || len(uptimeFields) != 2 {
		return "", 0, errors.New("cannot bind report authorization to the current boot")
	}
	wholeSeconds := strings.SplitN(uptimeFields[0], ".", 2)[0]
	bootSeconds, err := strconv.ParseUint(wholeSeconds, 10, 64)
	if err != nil || bootSeconds > ^uint64(0)-AuthorizationLifetimeSeconds {
		return "", 0, errors.New("cannot bind report authorization to the current boot")
	}
	return bootID, bootSeconds, nil
}

func validateCurrentBoot(root string, authorization Authorization) error {
	bootID, bootSeconds, err := currentBoot(root)
	if err != nil || bootID != authorization.BootID || bootSeconds < authorization.IssuedBootSeconds || bootSeconds >= authorization.ExpiresBootSeconds {
		return errors.New("report-target authorization expired or belongs to another boot")
	}
	return nil
}

func mediaIdentity(fingerprintKey []byte, parentCanonical string) (string, string) {
	type material struct{ name, value string }
	seen := make(map[string]struct{})
	var materials []material
	current := parentCanonical
	for depth := 0; depth < 20; depth++ {
		for _, name := range []string{"wwid", "serial"} {
			value := readSmallValue(filepath.Join(current, name))
			if !useStableValue(value) {
				continue
			}
			key := name + "=" + value
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			materials = append(materials, material{name: name, value: value})
		}
		if filepath.Base(current) == "devices" || filepath.Dir(current) == current {
			break
		}
		current = filepath.Dir(current)
	}
	if len(materials) == 0 {
		return "", ""
	}
	sort.Slice(materials, func(i, j int) bool {
		if materials[i].name != materials[j].name {
			return materials[i].name < materials[j].name
		}
		return materials[i].value < materials[j].value
	})
	var encoded strings.Builder
	var sources []string
	for _, value := range materials {
		encoded.WriteString(value.name)
		encoded.WriteByte(0)
		encoded.WriteString(value.value)
		encoded.WriteByte(0)
		if !contains(sources, value.name) {
			sources = append(sources, value.name)
		}
	}
	computed, err := identity.Compute(fingerprintKey, []identity.ComponentMaterial{{Domain: "report-target", Material: []byte(encoded.String())}})
	if err != nil {
		return "", ""
	}
	return "hmac-sha256:" + hex.EncodeToString(computed.ComponentPseudonyms[0].Digest[:]), strings.Join(sources, "+")
}

func confirmationCode(candidate Candidate) string {
	encoded, _ := json.Marshal(DeviceBinding{
		Device: candidate.Device, Parent: candidate.Parent, Topology: candidate.Topology,
		DeviceMajorMinor: candidate.DeviceMajorMinor, ParentMajorMinor: candidate.ParentMajorMinor,
		SizeBytes: candidate.SizeBytes, ParentSizeBytes: candidate.ParentSizeBytes,
		FingerprintKeyID: candidate.FingerprintKeyID, MediaIdentity: candidate.MediaIdentity,
		MediaIdentitySource: candidate.MediaIdentitySource,
	})
	digest := sha256.Sum256(encoded)
	return "RPT-" + strings.ToUpper(hex.EncodeToString(digest[:3]))
}

func inspectDirectBlockNode(root, name, expectedDevice string) bool {
	path := filepath.Join(root, "dev", name)
	metadata, err := os.Lstat(path)
	if err != nil || metadata.Mode()&os.ModeSymlink != 0 || metadata.Mode()&os.ModeDevice == 0 || metadata.Mode()&os.ModeCharDevice != 0 {
		return false
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return false
	}
	opened, statErr := file.Stat()
	_ = file.Close()
	return statErr == nil && opened.Mode()&os.ModeDevice != 0 && opened.Mode()&os.ModeCharDevice == 0 &&
		fileDevice(metadata, true) == expectedDevice && fileDevice(opened, true) == expectedDevice
}

func openedDirectoryMatches(directory *os.File, expectedDevice string) bool {
	metadata, err := directory.Stat()
	return err == nil && metadata.IsDir() && fileDevice(metadata, false) == expectedDevice
}

func fileDevice(metadata os.FileInfo, special bool) string {
	stat, ok := metadata.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	value := uint64(stat.Dev)
	if special {
		value = uint64(stat.Rdev)
	}
	return fmt.Sprintf("%d:%d", linuxMajor(value), linuxMinor(value))
}

func linuxMajor(device uint64) uint64 {
	return (device>>8)&0xfff | (device >> 32 & 0xfffff000)
}

func linuxMinor(device uint64) uint64 {
	return device&0xff | (device >> 12 & 0xffffff00)
}

func copyReport(ctx context.Context, destination io.Writer, source *os.File, size int64) (int64, error) {
	reader := io.NewSectionReader(source, 0, size)
	buffer := make([]byte, 64<<10)
	var written int64
	for written < size {
		if ctx.Err() != nil {
			return written, ctx.Err()
		}
		remaining := size - written
		chunk := int64(len(buffer))
		if remaining < chunk {
			chunk = remaining
		}
		count, err := io.CopyBuffer(destination, io.LimitReader(reader, chunk), buffer)
		written += count
		if err != nil || count != chunk {
			if err == nil {
				err = io.ErrUnexpectedEOF
			}
			return written, err
		}
	}
	return written, nil
}

func changedSource(source *os.File, before os.FileInfo) bool {
	after, err := source.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return true
	}
	return false
}

func validReportName(name string) bool {
	if len(name) < len("a.hhpcr") || len(name) > 128 || !strings.HasSuffix(name, ".hhpcr") || filepath.Base(name) != name {
		return false
	}
	for _, character := range name {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') &&
			!(character >= '0' && character <= '9') && character != '.' && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func readMountRegistry(root string) ([]mountRecord, error) {
	file, err := os.Open(filepath.Join(root, "proc/self/mountinfo"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return parseMountRegistry(file)
}

func parseMountRegistry(source io.Reader) ([]mountRecord, error) {
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 1024), 1<<20)
	var result []mountRecord
	for scanner.Scan() {
		if len(result) >= 4096 {
			return nil, errors.New("mount registry exceeds the supported limit")
		}
		fields := strings.Fields(scanner.Text())
		separator := -1
		for index, value := range fields {
			if value == "-" {
				separator = index
				break
			}
		}
		if len(fields) < 10 || separator < 6 || separator+3 >= len(fields) || !validMajorMinor(fields[2]) {
			return nil, errors.New("malformed mount registry")
		}
		result = append(result, mountRecord{Device: fields[2], Root: unescapeMount(fields[3]), Point: unescapeMount(fields[4]), Options: strings.Split(fields[5], ","), Filesystem: fields[separator+1]})
	}
	return result, scanner.Err()
}

func unescapeMount(value string) string {
	for encoded, decoded := range map[string]string{"\\040": " ", "\\011": "\t", "\\012": "\n", "\\134": "\\"} {
		value = strings.ReplaceAll(value, encoded, decoded)
	}
	return value
}

func isUSBTopology(root, canonical string) bool {
	relative, err := filepath.Rel(filepath.Join(root, "sys/devices"), canonical)
	if err != nil || relative == "." || strings.HasPrefix(relative, "..") {
		return false
	}
	controller := false
	for _, segment := range strings.Split(filepath.ToSlash(relative), "/") {
		if usbControllerSegment.MatchString(segment) {
			controller = true
		} else if controller && usbDeviceSegment.MatchString(segment) {
			return true
		}
	}
	return false
}

func withinDevices(root, canonical string) bool {
	relative, err := filepath.Rel(filepath.Join(root, "sys/devices"), canonical)
	return err == nil && relative != "." && !strings.HasPrefix(relative, "..")
}

func exportedPath(root, canonical string) string {
	relative, err := filepath.Rel(root, canonical)
	if err != nil || strings.HasPrefix(relative, "..") {
		return ""
	}
	return "/" + filepath.ToSlash(relative)
}

func validBlockName(value string) bool {
	return value != "" && len(value) <= 64 && value != "." && value != ".." && !strings.ContainsAny(value, `/\\`)
}

func readSmallValue(path string) string {
	encoded, err := os.ReadFile(path)
	if err != nil || len(encoded) > 256 {
		return ""
	}
	return strings.TrimSpace(string(encoded))
}

func readUint(path string) (uint64, bool) {
	parsed, err := strconv.ParseUint(readSmallValue(path), 10, 64)
	return parsed, err == nil
}

func sectorsToBytes(sectors uint64) uint64 {
	if sectors > ^uint64(0)/512 {
		return 0
	}
	return sectors * 512
}

func emptyDirectory(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) == 0
}

func useStableValue(value string) bool {
	value = strings.TrimSpace(value)
	normalized := strings.ToLower(value)
	if len(value) < 4 || normalized == "unknown" || normalized == "none" || normalized == "n/a" || normalized == "not specified" {
		return false
	}
	allPlaceholder := true
	for _, character := range normalized {
		if !strings.ContainsRune("0f-: ", character) {
			allPlaceholder = false
			break
		}
	}
	return !allPlaceholder
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsAll(values, required []string) bool {
	for _, wanted := range required {
		if !contains(values, wanted) {
			return false
		}
	}
	return true
}
