package reporttarget

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const (
	CatalogSchema                = "hhsoft.pc-tester.report-target-catalog/v1"
	AuthorizationSchema          = "hhsoft.pc-tester.report-target-authorization/v1"
	ReceiptSchema                = "hhsoft.pc-tester.report-persist-receipt/v1"
	MountPoint                   = "/run/hhpt/report-target"
	MaximumReportBytes           = 64 << 20
	AuthorizationLifetimeSeconds = 15 * 60
)

type Status string

const (
	StatusPass         Status = "pass"
	StatusNotPresent   Status = "not_present"
	StatusInconclusive Status = "inconclusive"
	StatusError        Status = "error"
	StatusAborted      Status = "aborted"
)

type Options struct {
	Root           string
	FingerprintKey []byte
}

type Candidate struct {
	Device              string `json:"device"`
	Parent              string `json:"parent"`
	Topology            string `json:"topology"`
	DeviceMajorMinor    string `json:"device_major_minor"`
	ParentMajorMinor    string `json:"parent_major_minor"`
	SizeBytes           uint64 `json:"size_bytes"`
	ParentSizeBytes     uint64 `json:"parent_size_bytes"`
	FingerprintKeyID    string `json:"fingerprint_key_id,omitempty"`
	MediaIdentity       string `json:"media_identity,omitempty"`
	MediaIdentitySource string `json:"media_identity_source,omitempty"`
	ConfirmationCode    string `json:"confirmation_code,omitempty"`
	ReadOnly            bool   `json:"read_only"`
	EligibleReadOnly    bool   `json:"eligible_read_only"`
	Mounted             bool   `json:"mounted"`
	NodeReady           bool   `json:"node_ready"`
	Eligible            bool   `json:"eligible"`
	Exclusion           string `json:"exclusion,omitempty"`
}

type Catalog struct {
	Schema     string      `json:"schema"`
	Status     Status      `json:"status"`
	Candidates []Candidate `json:"candidates,omitempty"`
	Detail     string      `json:"detail,omitempty"`
}

type AuthorizationRequest struct {
	Device           string
	ConfirmationCode string
}

type DeviceBinding struct {
	Device              string `json:"device"`
	Parent              string `json:"parent"`
	Topology            string `json:"topology"`
	DeviceMajorMinor    string `json:"device_major_minor"`
	ParentMajorMinor    string `json:"parent_major_minor"`
	SizeBytes           uint64 `json:"size_bytes"`
	ParentSizeBytes     uint64 `json:"parent_size_bytes"`
	FingerprintKeyID    string `json:"fingerprint_key_id"`
	MediaIdentity       string `json:"media_identity"`
	MediaIdentitySource string `json:"media_identity_source"`
}

type Authorization struct {
	Schema                 string        `json:"schema"`
	ID                     string        `json:"id"`
	BootID                 string        `json:"boot_id"`
	IssuedBootSeconds      uint64        `json:"issued_boot_seconds"`
	ExpiresBootSeconds     uint64        `json:"expires_boot_seconds"`
	Device                 DeviceBinding `json:"device"`
	MountPoint             string        `json:"mount_point"`
	RequiredMountOptions   []string      `json:"required_mount_options"`
	AllowedFileSystemTypes []string      `json:"allowed_filesystem_types"`
}

type Receipt struct {
	Schema           string `json:"schema"`
	AuthorizationID  string `json:"authorization_id"`
	Device           string `json:"device"`
	File             string `json:"file"`
	Bytes            int64  `json:"bytes"`
	SHA256           string `json:"sha256"`
	FileSynced       bool   `json:"file_synced"`
	ReadBackVerified bool   `json:"read_back_verified"`
	DirectorySynced  bool   `json:"directory_synced"`
}

func Discover(ctx context.Context, options Options) Catalog {
	return discoverPlatform(ctx, options)
}

func Authorize(ctx context.Context, options Options, request AuthorizationRequest) (Authorization, error) {
	return authorizePlatform(ctx, options, request)
}

// ValidateMountAuthorization rechecks the authorization and unmounted media
// immediately before a caller mounts its separately bound device descriptor.
func ValidateMountAuthorization(ctx context.Context, options Options, authorization Authorization) error {
	return validateMountAuthorizationPlatform(ctx, options, authorization)
}

func Persist(ctx context.Context, options Options, authorization Authorization, source *os.File, sourceSize int64, name string, expectedSHA256 [sha256.Size]byte) (Receipt, error) {
	return persistPlatform(ctx, options, authorization, source, sourceSize, name, expectedSHA256)
}

func DecodeAuthorization(encoded []byte) (Authorization, error) {
	if len(encoded) == 0 || len(encoded) > 64<<10 {
		return Authorization{}, errors.New("report-target authorization size is invalid")
	}
	var authorization Authorization
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&authorization); err != nil {
		return Authorization{}, fmt.Errorf("decode report-target authorization: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return Authorization{}, err
	}
	if err := ValidateAuthorization(authorization); err != nil {
		return Authorization{}, err
	}
	return authorization, nil
}

func ValidateAuthorization(authorization Authorization) error {
	if authorization.Schema != AuthorizationSchema || authorization.MountPoint != MountPoint {
		return errors.New("invalid report-target authorization schema or mount point")
	}
	if !equalStrings(authorization.RequiredMountOptions, []string{"nodev", "noexec", "nosuid", "rw"}) ||
		!equalStrings(authorization.AllowedFileSystemTypes, []string{"exfat", "ext4", "vfat"}) {
		return errors.New("invalid report-target authorization policy")
	}
	if !validBinding(authorization.Device) {
		return errors.New("invalid report-target device binding")
	}
	if len(authorization.BootID) != 36 || authorization.ExpiresBootSeconds <= authorization.IssuedBootSeconds ||
		authorization.ExpiresBootSeconds-authorization.IssuedBootSeconds != AuthorizationLifetimeSeconds {
		return errors.New("invalid report-target authorization lifetime")
	}
	if !strings.HasPrefix(authorization.ID, "hmac-sha256:") || len(authorization.ID) != 76 {
		return errors.New("report-target authorization ID is invalid")
	}
	return nil
}

func NewAuthorization(candidate Candidate, bootID string, issuedBootSeconds uint64, fingerprintKey []byte) (Authorization, error) {
	binding := DeviceBinding{
		Device: candidate.Device, Parent: candidate.Parent, Topology: candidate.Topology,
		DeviceMajorMinor: candidate.DeviceMajorMinor, ParentMajorMinor: candidate.ParentMajorMinor,
		SizeBytes: candidate.SizeBytes, ParentSizeBytes: candidate.ParentSizeBytes,
		FingerprintKeyID: candidate.FingerprintKeyID, MediaIdentity: candidate.MediaIdentity,
		MediaIdentitySource: candidate.MediaIdentitySource,
	}
	result := Authorization{
		Schema: AuthorizationSchema, BootID: bootID, IssuedBootSeconds: issuedBootSeconds,
		ExpiresBootSeconds: issuedBootSeconds + AuthorizationLifetimeSeconds, Device: binding, MountPoint: MountPoint,
		RequiredMountOptions:   []string{"nodev", "noexec", "nosuid", "rw"},
		AllowedFileSystemTypes: []string{"exfat", "ext4", "vfat"},
	}
	identifier, err := authorizationID(result, fingerprintKey)
	if err != nil {
		return Authorization{}, err
	}
	result.ID = identifier
	return result, nil
}

func validBinding(binding DeviceBinding) bool {
	return validDevicePath(binding.Device) && validDevicePath(binding.Parent) &&
		strings.HasPrefix(binding.Topology, "/sys/devices/") && len(binding.Topology) <= 1024 &&
		validMajorMinor(binding.DeviceMajorMinor) && validMajorMinor(binding.ParentMajorMinor) &&
		binding.SizeBytes >= 16<<20 && binding.ParentSizeBytes >= binding.SizeBytes &&
		strings.HasPrefix(binding.FingerprintKeyID, "hmac-sha256:") && len(binding.FingerprintKeyID) == 76 &&
		strings.HasPrefix(binding.MediaIdentity, "hmac-sha256:") && len(binding.MediaIdentity) == 76 &&
		binding.MediaIdentitySource != "" && len(binding.MediaIdentitySource) <= 128
}

func validDevicePath(value string) bool {
	return strings.HasPrefix(value, "/dev/") && len(value) <= 69 &&
		value == "/dev/"+strings.TrimPrefix(value, "/dev/") &&
		!strings.ContainsAny(strings.TrimPrefix(value, "/dev/"), `/\\`)
}

func validMajorMinor(value string) bool {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return false
	}
	major, majorErr := strconv.ParseUint(parts[0], 10, 64)
	minor, minorErr := strconv.ParseUint(parts[1], 10, 64)
	return majorErr == nil && minorErr == nil && major <= 0xfffff && minor <= 0xfffff
}

func authorizationID(authorization Authorization, fingerprintKey []byte) (string, error) {
	if len(fingerprintKey) != 32 {
		return "", errors.New("authorization HMAC requires exactly 32 fingerprint-key bytes")
	}
	encoded, err := json.Marshal(struct {
		Device             DeviceBinding `json:"device"`
		BootID             string        `json:"boot_id"`
		IssuedBootSeconds  uint64        `json:"issued_boot_seconds"`
		ExpiresBootSeconds uint64        `json:"expires_boot_seconds"`
	}{authorization.Device, authorization.BootID, authorization.IssuedBootSeconds, authorization.ExpiresBootSeconds})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, fingerprintKey)
	_, _ = mac.Write([]byte("HHSoft report-target authorization v1"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(encoded)
	return "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil)), nil
}

func validateAuthorizationMAC(authorization Authorization, fingerprintKey []byte) error {
	expected, err := authorizationID(authorization, fingerprintKey)
	if err != nil || !hmac.Equal([]byte(authorization.ID), []byte(expected)) {
		return errors.New("report-target authorization HMAC is invalid")
	}
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("report-target authorization contains trailing JSON")
	}
	return nil
}
