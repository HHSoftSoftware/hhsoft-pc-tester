package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

type saveOperations struct {
	discover  func(context.Context, reporttarget.Options) reporttarget.Catalog
	authorize func(context.Context, reporttarget.Options, reporttarget.AuthorizationRequest) (reporttarget.Authorization, error)
	mount     func(context.Context, reporttarget.Options, reporttarget.Authorization) (func() error, error)
	persist   func(context.Context, reporttarget.Options, reporttarget.Authorization, *os.File, int64, string, [sha256.Size]byte) (reporttarget.Receipt, error)
	openAudit func(string) (io.Writer, func(), error)
}

func runSave(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer) int {
	return runSaveWith(ctx, arguments, input, stdout, stderr, saveOperations{
		discover: reporttarget.Discover, authorize: reporttarget.Authorize,
		mount: mountAuthorizedReportTarget, persist: reporttarget.Persist, openAudit: openSaveAudit,
	})
}

func runSaveWith(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer, operations saveOperations) int {
	flags := flag.NewFlagSet("save", flag.ContinueOnError)
	flags.SetOutput(stderr)
	fingerprintKeyPath := flags.String("fingerprint-key", "", "file containing the session's exact 32-byte fingerprint key")
	reportName := flags.String("name", "", "plain destination .hhpcr filename")
	auditOutput := flags.String("audit-output", "", "optional fixed /dev/console audit output")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 1 || *fingerprintKeyPath == "" {
		fmt.Fprintln(stderr, "save requires -fingerprint-key and one self-issued .hhpcr report")
		return 2
	}
	audit, closeAudit, err := operations.openAudit(*auditOutput)
	if err != nil {
		fmt.Fprintf(stderr, "open report-save audit output: %v\n", err)
		return 1
	}
	defer closeAudit()
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
		catalog := operations.discover(ctx, options)
		eligible := eligibleCandidates(catalog.Candidates)
		printSaveCandidates(stdout, catalog, eligible)
		for index, candidate := range eligible {
			if err := writeSaveCandidateAudit(audit, "CANDIDATE", index+1, candidate); err != nil {
				fmt.Fprintln(stderr, "report-save candidate audit failed; no storage device was written")
				return 1
			}
		}
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
		if err := writeSaveCandidateAudit(audit, "CONFIRM", index, candidate); err != nil {
			fmt.Fprintln(stderr, "report-save confirmation audit failed; no storage device was written")
			return 1
		}
		confirmation, readErr := reader.ReadString('\n')
		if readErr != nil {
			fmt.Fprintf(stderr, "read target confirmation: %v\n", readErr)
			return 1
		}
		confirmation = strings.TrimSpace(confirmation)
		if confirmation != candidate.ConfirmationCode {
			if _, err := fmt.Fprintln(audit, "HHPT_SAVE_REJECTED reason=confirmation"); err != nil {
				fmt.Fprintln(stderr, "report-save rejection audit failed; no storage device was written")
				return 1
			}
			fmt.Fprintln(stderr, "typed report-target confirmation code does not match selected media; no storage device was written")
			continue
		}
		authorization, authorizeErr := operations.authorize(ctx, options, reporttarget.AuthorizationRequest{Device: candidate.Device, ConfirmationCode: confirmation})
		if authorizeErr != nil {
			fmt.Fprintf(stderr, "authorize report target: %v; no storage device was written\n", authorizeErr)
			continue
		}
		unmount, mountErr := operations.mount(ctx, options, authorization)
		if mountErr != nil {
			fmt.Fprintf(stderr, "mount report target: %v; no report file was written\n", mountErr)
			continue
		}
		receipt, persistErr := operations.persist(ctx, options, authorization, source, metadata.Size(), name, artifact.BundleSHA256)
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
		if err := writeSaveReceiptAudit(audit, receipt); err != nil {
			fmt.Fprintln(stderr, "report was saved but the report-save receipt audit failed")
			return 1
		}
		return 0
	}
}

func openSaveAudit(path string) (io.Writer, func(), error) {
	if path == "" {
		return io.Discard, func() {}, nil
	}
	if path != "/dev/console" {
		return nil, func() {}, errors.New("audit output must be /dev/console")
	}
	file, err := openSaveAuditConsole()
	if err != nil {
		return nil, func() {}, err
	}
	return file, func() { _ = file.Close() }, nil
}

func writeSaveCandidateAudit(destination io.Writer, phase string, index int, candidate reporttarget.Candidate) error {
	deviceName := strings.TrimPrefix(candidate.Device, "/dev/")
	code := strings.TrimPrefix(candidate.ConfirmationCode, "RPT-")
	if (phase != "CANDIDATE" && phase != "CONFIRM") || index < 1 || index > 4096 || !candidate.Eligible ||
		candidate.Device != "/dev/"+deviceName || len(deviceName) > 64 || !validNameToken(deviceName) ||
		candidate.ConfirmationCode != "RPT-"+code || len(code) != 6 || !allHexCharacters(code, "0123456789ABCDEF") {
		return errors.New("report-save candidate audit fields are invalid")
	}
	_, err := fmt.Fprintf(destination, "HHPT_SAVE_%s index=%d device=%s confirmation=%s\n", phase, index, candidate.Device, candidate.ConfirmationCode)
	return err
}

func writeSaveReceiptAudit(destination io.Writer, receipt reporttarget.Receipt) error {
	base := strings.TrimSuffix(receipt.File, ".hhpcr")
	digest := strings.TrimPrefix(receipt.SHA256, "sha256:")
	// Persistence accepts a plain ASCII basename of at most 128 bytes, including
	// internal dots. Keep the optional marker compatible with those valid names.
	if receipt.File != base+".hhpcr" || len(receipt.File) > 128 || !validNameToken(strings.ReplaceAll(base, ".", "_")) ||
		receipt.Bytes <= 0 || receipt.Bytes > reporttarget.MaximumReportBytes ||
		receipt.SHA256 != "sha256:"+digest || len(digest) != 64 || !allHexCharacters(digest, "0123456789abcdef") ||
		!receipt.FileSynced || !receipt.ReadBackVerified || !receipt.DirectorySynced {
		return errors.New("report-save receipt audit fields are invalid")
	}
	_, err := fmt.Fprintf(destination, "HHPT_SAVE_SAVED file=%s bytes=%d sha256=%s\n", receipt.File, receipt.Bytes, digest)
	return err
}

func allHexCharacters(value, alphabet string) bool {
	for _, character := range value {
		if !strings.ContainsRune(alphabet, character) {
			return false
		}
	}
	return true
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
