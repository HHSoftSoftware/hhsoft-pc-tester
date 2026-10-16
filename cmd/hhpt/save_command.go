package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

func runSave(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("save", flag.ContinueOnError)
	flags.SetOutput(stderr)
	fingerprintKeyPath := flags.String("fingerprint-key", "", "file containing the session's exact 32-byte fingerprint key")
	reportName := flags.String("name", "", "plain destination .hhpcr filename")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 1 || *fingerprintKeyPath == "" {
		fmt.Fprintln(stderr, "save requires -fingerprint-key and one self-issued .hhpcr report")
		return 2
	}
	fingerprintKey, err := readRawKey(*fingerprintKeyPath, 32)
	if err != nil {
		fmt.Fprintf(stderr, "read fingerprint key: %v\n", err)
		return 1
	}
	source, metadata, err := openRegularNoSymlink(flags.Arg(0), reporttarget.MaximumReportBytes)
	if err != nil {
		fmt.Fprintf(stderr, "open report: %v\n", err)
		return 1
	}
	defer source.Close()
	artifact, err := bundle.VerifySelfIssued(source, metadata.Size())
	if err != nil {
		fmt.Fprintf(stderr, "verify report: %v\n", err)
		return 1
	}
	name := *reportName
	if name == "" {
		name = defaultReportName(artifact.Envelope.SessionID, artifact.Envelope.ManifestDigest)
	}
	reader := bufio.NewReader(input)
	options := reporttarget.Options{Root: "/", FingerprintKey: fingerprintKey}
	fmt.Fprintln(stdout, "\nReport complete in RAM. It is self-issued until externally signed/provisioned.")
	for {
		catalog := reporttarget.Discover(ctx, options)
		eligible := eligibleCandidates(catalog.Candidates)
		printSaveCandidates(stdout, catalog, eligible)
		if len(eligible) == 0 {
			fmt.Fprintln(stdout, "Insert one formatted removable USB partition, then press Enter to rescan or Q to finish.")
		} else {
			fmt.Fprintf(stdout, "Select 1-%d to save, Enter to rescan, or Q to finish: ", len(eligible))
		}
		selection, readErr := reader.ReadString('\n')
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return 0
			}
			fmt.Fprintf(stderr, "read save selection: %v\n", readErr)
			return 1
		}
		selection = strings.TrimSpace(selection)
		if strings.EqualFold(selection, "q") {
			fmt.Fprintln(stdout, "Report remains in volatile memory; no storage device was written.")
			return 0
		}
		if selection == "" {
			continue
		}
		index, parseErr := strconv.Atoi(selection)
		if parseErr != nil || index < 1 || index > len(eligible) {
			fmt.Fprintln(stderr, "Invalid selection; no storage device was written.")
			continue
		}
		candidate := eligible[index-1]
		fmt.Fprintf(stdout, "Type %s to authorize a single report write to %s: ", candidate.ConfirmationCode, candidate.Device)
		confirmation, readErr := reader.ReadString('\n')
		if readErr != nil {
			fmt.Fprintf(stderr, "read target confirmation: %v\n", readErr)
			return 1
		}
		authorization, authorizeErr := reporttarget.Authorize(ctx, options, reporttarget.AuthorizationRequest{Device: candidate.Device, ConfirmationCode: strings.TrimSpace(confirmation)})
		if authorizeErr != nil {
			fmt.Fprintf(stderr, "authorize report target: %v; no storage device was written\n", authorizeErr)
			continue
		}
		unmount, mountErr := mountAuthorizedReportTarget(ctx, options, authorization)
		if mountErr != nil {
			fmt.Fprintf(stderr, "mount report target: %v; no report file was written\n", mountErr)
			continue
		}
		receipt, persistErr := reporttarget.Persist(ctx, options, authorization, source, metadata.Size(), name, artifact.BundleSHA256)
		unmountErr := unmount()
		if persistErr != nil {
			fmt.Fprintf(stderr, "save report: %v\n", persistErr)
			return 1
		}
		if unmountErr != nil {
			fmt.Fprintf(stderr, "save report: data was read-back verified but %v\n", unmountErr)
			return 1
		}
		fmt.Fprintf(stdout, "Saved and read-back verified: %s/%s (%d bytes, %s)\n", authorization.MountPoint, receipt.File, receipt.Bytes, receipt.SHA256)
		return 0
	}
}

func eligibleCandidates(candidates []reporttarget.Candidate) []reporttarget.Candidate {
	result := make([]reporttarget.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Eligible {
			result = append(result, candidate)
		}
	}
	return result
}

func printSaveCandidates(destination io.Writer, catalog reporttarget.Catalog, eligible []reporttarget.Candidate) {
	fmt.Fprintf(destination, "\nRemovable report targets: %d eligible", len(eligible))
	if len(catalog.Candidates) != len(eligible) {
		fmt.Fprintf(destination, ", %d excluded", len(catalog.Candidates)-len(eligible))
	}
	fmt.Fprintln(destination)
	for index, candidate := range eligible {
		fmt.Fprintf(destination, "[%d] %s  %d MiB  confirmation %s\n", index+1, candidate.Device, candidate.SizeBytes/(1<<20), candidate.ConfirmationCode)
	}
}
