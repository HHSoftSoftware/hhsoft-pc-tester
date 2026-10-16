package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

func runReportTarget(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("report-target", flag.ContinueOnError)
	flags.SetOutput(stderr)
	fingerprintKeyPath := flags.String("fingerprint-key", "", "file containing exactly 32 raw fingerprint-key bytes")
	device := flags.String("device", "", "eligible removable USB partition to authorize")
	confirmation := flags.String("confirm", "", "typed confirmation code from the current target catalog")
	outputPath := flags.String("output", "-", "new catalog/authorization JSON file, or - for stdout")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *fingerprintKeyPath == "" || (*device == "") != (*confirmation == "") {
		fmt.Fprintln(stderr, "report-target requires -fingerprint-key and either neither or both -device and -confirm")
		return 2
	}
	fingerprintKey, err := readRawKey(*fingerprintKeyPath, 32)
	if err != nil {
		fmt.Fprintf(stderr, "read fingerprint key: %v\n", err)
		return 1
	}
	options := reporttarget.Options{Root: "/", FingerprintKey: fingerprintKey}
	if *device == "" {
		return writeQuickResult(reporttarget.Discover(ctx, options), *outputPath, stdout, stderr, "report-target catalog")
	}
	authorization, err := reporttarget.Authorize(ctx, options, reporttarget.AuthorizationRequest{Device: *device, ConfirmationCode: *confirmation})
	if err != nil {
		fmt.Fprintf(stderr, "authorize report target: %v\n", err)
		return 1
	}
	return writeQuickResult(authorization, *outputPath, stdout, stderr, "report-target authorization")
}

func runPersist(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("persist", flag.ContinueOnError)
	flags.SetOutput(stderr)
	authorizationPath := flags.String("authorization", "", "current RAM-only report-target authorization")
	fingerprintKeyPath := flags.String("fingerprint-key", "", "file containing the authorization's exact 32-byte fingerprint key")
	publicKeyPath := flags.String("public-key", "", "file containing exactly 32 raw Ed25519 public-key bytes")
	keyID := flags.String("key-id", "", "expected non-secret signing-key identifier")
	selfIssued := flags.Bool("self-issued", false, "accept the embedded ephemeral signer at reduced identity assurance")
	reportName := flags.String("name", "", "plain destination .hhpcr filename")
	outputPath := flags.String("output", "-", "new persistence receipt JSON file, or - for stdout")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 1 || *authorizationPath == "" || *fingerprintKeyPath == "" || (*selfIssued && (*publicKeyPath != "" || strings.TrimSpace(*keyID) != "")) || (!*selfIssued && (*publicKeyPath == "" || strings.TrimSpace(*keyID) == "")) {
		fmt.Fprintln(stderr, "persist requires authorization/fingerprint key, either -self-issued or both public key/key ID, and one .hhpcr file")
		return 2
	}
	authorizationEncoded, err := readSmallRegularFile(*authorizationPath, 64<<10)
	if err != nil {
		fmt.Fprintf(stderr, "read report-target authorization: %v\n", err)
		return 1
	}
	authorization, err := reporttarget.DecodeAuthorization(authorizationEncoded)
	if err != nil {
		fmt.Fprintf(stderr, "validate report-target authorization: %v\n", err)
		return 1
	}
	fingerprintKey, err := readRawKey(*fingerprintKeyPath, 32)
	if err != nil {
		fmt.Fprintf(stderr, "read fingerprint key: %v\n", err)
		return 1
	}
	source, metadata, err := openRegularNoSymlink(flags.Arg(0), reporttarget.MaximumReportBytes)
	if err != nil {
		fmt.Fprintf(stderr, "open source report: %v\n", err)
		return 1
	}
	defer source.Close()
	var artifact bundle.Artifact
	if *selfIssued {
		artifact, err = bundle.VerifySelfIssued(source, metadata.Size())
	} else {
		var publicKey []byte
		publicKey, err = readRawKey(*publicKeyPath, ed25519.PublicKeySize)
		if err == nil {
			artifact, err = bundle.Verify(source, metadata.Size(), report.Keyring{strings.TrimSpace(*keyID): ed25519.PublicKey(publicKey)})
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "verify source report: %v\n", err)
		return 1
	}
	name := *reportName
	if name == "" {
		name = defaultReportName(artifact.Envelope.SessionID, artifact.Envelope.ManifestDigest)
	}
	receipt, err := reporttarget.Persist(ctx, reporttarget.Options{Root: "/", FingerprintKey: fingerprintKey}, authorization, source, metadata.Size(), name, artifact.BundleSHA256)
	if err != nil {
		fmt.Fprintf(stderr, "persist report: %v\n", err)
		return 1
	}
	return writeQuickResult(receipt, *outputPath, stdout, stderr, "persistence receipt")
}

func readSmallRegularFile(path string, maximum int64) ([]byte, error) {
	file, metadata, err := openRegularNoSymlink(path, maximum)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	encoded, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(encoded)) != metadata.Size() {
		return nil, errors.New("file changed while reading")
	}
	return encoded, nil
}

func defaultReportName(sessionID, manifestDigest string) string {
	if validNameToken(sessionID) && len(sessionID) <= 100 {
		return "hhpt-" + sessionID + ".hhpcr"
	}
	digest := strings.TrimPrefix(manifestDigest, "sha256:")
	if len(digest) >= 16 && validNameToken(digest) {
		return "hhpt-" + digest[:16] + ".hhpcr"
	}
	return "hhpt-verified-report.hhpcr"
}

func validNameToken(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') && character != '-' && character != '_' {
			return false
		}
	}
	return true
}
