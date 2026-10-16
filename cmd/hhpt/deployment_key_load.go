package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/deploymentkey"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

const (
	deploymentActivationSchema = "hhsoft.pc-tester.deployment-key-activation/v1"
	deploymentActivationPath   = "/run/hhpt/deployment-key.json"
	deploymentFingerprintPath  = "/run/hhpt/fingerprint.key"
	deploymentFixturesPath     = "/run/hhpt/fixture-components"
)

type deploymentActivation struct {
	Schema             string   `json:"schema"`
	Mode               string   `json:"mode"`
	Epoch              string   `json:"epoch"`
	KeyID              string   `json:"key_id"`
	Device             string   `json:"device"`
	ReadOnly           bool     `json:"read_only"`
	ExcludedComponents []string `json:"excluded_components"`
}

func runDeploymentKeyLoadMedia(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("deployment-key load-media", flag.ContinueOnError)
	flags.SetOutput(stderr)
	discoveryKeyPath := flags.String("discovery-key", "", "session-only media discovery key")
	auditOutput := flags.String("audit-output", "", "internal fixed console audit output")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *discoveryKeyPath == "" {
		fmt.Fprintln(stderr, "deployment-key load-media requires -discovery-key and no positional arguments")
		return 2
	}
	audit, closeAudit, err := openDeploymentKeyAudit(*auditOutput)
	if err != nil {
		fmt.Fprintf(stderr, "open deployment-key audit output: %v\n", err)
		return 1
	}
	defer closeAudit()
	discoveryKey, err := readRawKey(*discoveryKeyPath, 32)
	if err != nil {
		fmt.Fprintf(stderr, "read media discovery key: %v\n", err)
		return 1
	}
	if err := deploymentActivationAvailable(); err != nil {
		fmt.Fprintf(stderr, "prepare deployment-key activation: %v\n", err)
		return 1
	}
	options := reporttarget.Options{Root: "/", FingerprintKey: discoveryKey}
	reader := bufio.NewReader(input)
	candidate, err := selectDeploymentKeyMedia(ctx, options, reader, stdout, audit)
	if err != nil {
		fmt.Fprintf(audit, "HHPT_KEY_MEDIA_ERROR detail=%s\n", err)
		fmt.Fprintf(stderr, "select deployment-key media: %v\n", err)
		return 1
	}
	fmt.Fprintf(audit, "HHPT_KEY_MEDIA_SELECTED device=%s\n", candidate.Device)
	fmt.Fprintf(stdout, "Type LOAD to mount %s read-only and activate its stable fingerprint epoch: ", candidate.Device)
	confirmation, err := reader.ReadString('\n')
	if err != nil || !strings.EqualFold(strings.TrimSpace(confirmation), "LOAD") {
		fmt.Fprintln(audit, "HHPT_KEY_MEDIA_ERROR detail=activation-not-confirmed")
		fmt.Fprintln(stderr, "deployment-key media activation was not confirmed")
		return 1
	}
	fmt.Fprintln(audit, "HHPT_KEY_MEDIA_CONFIRMED policy=read-only")
	record, unmount, err := loadDeploymentKeyMediaPlatform(ctx, options, candidate)
	if err != nil {
		fmt.Fprintf(audit, "HHPT_KEY_MEDIA_ERROR detail=%s\n", err)
		fmt.Fprintf(stderr, "load deployment-key media: %v\n", err)
		return 1
	}
	keyID, err := deploymentkey.KeyID(record)
	if err != nil {
		_ = unmount()
		fmt.Fprintf(stderr, "identify deployment key: %v\n", err)
		return 1
	}
	fixtures, err := deploymentFixtureComponentIDs(candidate)
	if err != nil {
		_ = unmount()
		fmt.Fprintf(audit, "HHPT_KEY_MEDIA_ERROR detail=fixture-binding-failed\n")
		fmt.Fprintf(stderr, "bind deployment-key fixture: %v\n", err)
		return 1
	}
	activation := deploymentActivation{Schema: deploymentActivationSchema, Mode: "deployment", Epoch: record.Epoch, KeyID: keyID, Device: candidate.Device, ReadOnly: true, ExcludedComponents: fixtures}
	if err := writeDeploymentActivation(record, activation); err != nil {
		_ = unmount()
		fmt.Fprintf(stderr, "activate deployment key: %v\n", err)
		return 1
	}
	fmt.Fprintf(audit, "HHPT_KEY_MEDIA_ACTIVE epoch=%s key_id=%s read_only=true\n", record.Epoch, keyID)
	fmt.Fprintf(stdout, "Deployment key active: epoch=%s key_id=%s; media remains mounted read-only.\n", record.Epoch, keyID)
	return 0
}

func deploymentFixtureComponentIDs(candidate reporttarget.Candidate) ([]string, error) {
	parent := strings.TrimPrefix(candidate.Parent, "/dev/")
	partition := strings.TrimPrefix(candidate.Device, "/dev/")
	usbDevice := ""
	segments := strings.Split(candidate.Topology, "/")
	for index := 0; index+1 < len(segments); index++ {
		if validUSBControllerName(segments[index]) && validUSBDeviceName(segments[index+1]) {
			usbDevice = segments[index+1]
			break
		}
	}
	result := []string{"block:" + parent, "block:" + partition, "usb:" + usbDevice}
	for _, component := range result {
		if !validFixtureComponentID(component) {
			return nil, errors.New("selected token topology cannot be mapped to fixture component IDs")
		}
	}
	return result, nil
}

func validUSBControllerName(value string) bool {
	return strings.HasPrefix(value, "usb") && allDecimal(value[3:])
}

func validUSBDeviceName(value string) bool {
	parts := strings.Split(value, "-")
	if len(parts) != 2 || !allDecimal(parts[0]) {
		return false
	}
	for _, port := range strings.Split(parts[1], ".") {
		if !allDecimal(port) {
			return false
		}
	}
	return true
}

func allDecimal(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func selectDeploymentKeyMedia(ctx context.Context, options reporttarget.Options, reader *bufio.Reader, stdout, audit io.Writer) (reporttarget.Candidate, error) {
	for {
		catalog := reporttarget.Discover(ctx, options)
		candidates := eligibleDeploymentKeyMedia(catalog.Candidates)
		fmt.Fprintf(audit, "HHPT_KEY_MEDIA_SCAN status=%s discovered=%d eligible=%d detail=%s\n", catalog.Status, len(catalog.Candidates), len(candidates), strings.ReplaceAll(catalog.Detail, " ", "-"))
		for _, rejected := range catalog.Candidates {
			if rejected.EligibleReadOnly && !rejected.Mounted {
				continue
			}
			fmt.Fprintf(audit, "HHPT_KEY_MEDIA_REJECTED device=%s node_ready=%t read_only=%t mounted=%t reason=%s\n", rejected.Device, rejected.NodeReady, rejected.ReadOnly, rejected.Mounted, strings.ReplaceAll(rejected.Exclusion, " ", "-"))
		}
		fmt.Fprintf(stdout, "\nDeployment-key media: %d eligible removable partition(s)\n", len(candidates))
		for index, candidate := range candidates {
			readOnly := "writable device; HHPT will mount it read-only"
			if candidate.Exclusion == "report media is read-only" {
				readOnly = "hardware/device read-only"
			}
			fmt.Fprintf(stdout, "[%d] %s  %d MiB  %s\n", index+1, candidate.Device, candidate.SizeBytes/(1<<20), readOnly)
		}
		if len(candidates) == 0 {
			fmt.Fprintln(stdout, "Insert a dedicated USB key token, then press Enter to rescan or Q to cancel.")
		} else {
			fmt.Fprintf(stdout, "Select 1-%d, Enter to rescan, or Q to cancel: ", len(candidates))
		}
		selection, err := reader.ReadString('\n')
		if err != nil {
			return reporttarget.Candidate{}, err
		}
		selection = strings.TrimSpace(selection)
		if strings.EqualFold(selection, "q") {
			return reporttarget.Candidate{}, errors.New("cancelled")
		}
		if selection == "" {
			continue
		}
		index, err := strconv.Atoi(selection)
		if err != nil || index < 1 || index > len(candidates) {
			fmt.Fprintln(stdout, "Invalid selection; no media was mounted.")
			continue
		}
		return candidates[index-1], nil
	}
}

func openDeploymentKeyAudit(path string) (io.Writer, func(), error) {
	if path == "" {
		return io.Discard, func() {}, nil
	}
	if path != "/dev/console" {
		return nil, func() {}, errors.New("audit output must be /dev/console")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return nil, func() {}, err
	}
	return file, func() { _ = file.Close() }, nil
}

func eligibleDeploymentKeyMedia(candidates []reporttarget.Candidate) []reporttarget.Candidate {
	result := make([]reporttarget.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if !candidate.Mounted && candidate.EligibleReadOnly {
			result = append(result, candidate)
		}
	}
	return result
}

func deploymentActivationAvailable() error {
	for _, path := range []string{deploymentFingerprintPath, deploymentActivationPath, deploymentFixturesPath} {
		if _, err := os.Lstat(path); err == nil || !errors.Is(err, os.ErrNotExist) {
			return errors.New("activation outputs already exist")
		}
	}
	metadata, err := os.Lstat(filepath.Dir(deploymentFingerprintPath))
	if err != nil || metadata.Mode()&os.ModeSymlink != 0 || !metadata.IsDir() {
		return errors.New("fixed RAM activation directory is unsafe")
	}
	return nil
}

func writeDeploymentActivation(record deploymentkey.Record, activation deploymentActivation) error {
	encoded, err := json.Marshal(activation)
	if err != nil {
		return err
	}
	if err := writeExclusiveSynced(deploymentFingerprintPath, record.Key[:]); err != nil {
		return err
	}
	fixtures := []byte(strings.Join(activation.ExcludedComponents, "\n") + "\n")
	if err := writeExclusiveSynced(deploymentFixturesPath, fixtures); err != nil {
		_ = os.Remove(deploymentFingerprintPath)
		return err
	}
	if err := writeExclusiveSynced(deploymentActivationPath, encoded); err != nil {
		_ = os.Remove(deploymentFingerprintPath)
		_ = os.Remove(deploymentFixturesPath)
		return err
	}
	return nil
}
