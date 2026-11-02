package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

type retestInputReader func(context.Context, string, retestInputKind) ([]byte, error)

var errRetestVerification = errors.New("retest: report verification failed")

type reportTrustFlags struct {
	prefix, publicKeyPath, keyID string
	selfIssued                   bool
	keySet, idSet, selfSet       bool
}

func (trust *reportTrustFlags) register(flags *flag.FlagSet, prefix string) {
	trust.prefix = prefix
	flags.StringVar(&trust.publicKeyPath, prefix+"public-key", "", "file containing exactly 32 raw Ed25519 public-key bytes")
	flags.StringVar(&trust.keyID, prefix+"key-id", "", "expected non-secret signing-key identifier")
	flags.BoolVar(&trust.selfIssued, prefix+"self-issued", false, "explicitly accept this report's embedded signer at reduced identity assurance")
}

func (trust *reportTrustFlags) recordPresence(flags *flag.FlagSet) {
	flags.Visit(func(value *flag.Flag) {
		switch value.Name {
		case trust.prefix + "public-key":
			trust.keySet = true
		case trust.prefix + "key-id":
			trust.idSet = true
		case trust.prefix + "self-issued":
			trust.selfSet = true
		}
	})
}

func (trust reportTrustFlags) present() bool { return trust.keySet || trust.idSet || trust.selfSet }

func (trust reportTrustFlags) valid() bool {
	if trust.selfSet {
		return trust.selfIssued && !trust.keySet && !trust.idSet
	}
	return trust.keySet && trust.idSet && strings.TrimSpace(trust.publicKeyPath) != "" && strings.TrimSpace(trust.keyID) != ""
}

type previousReportFlags struct {
	path    string
	pathSet bool
	trust   reportTrustFlags
}

func (previous *previousReportFlags) register(flags *flag.FlagSet) {
	flags.StringVar(&previous.path, "previous-report", "", "explicit previous .hhpcr report for a linked retest")
	previous.trust.register(flags, "previous-")
}

func (previous *previousReportFlags) recordPresence(flags *flag.FlagSet) {
	previous.trust.recordPresence(flags)
	flags.Visit(func(value *flag.Flag) {
		if value.Name == "previous-report" {
			previous.pathSet = true
		}
	})
}

func (previous previousReportFlags) present() bool {
	return previous.pathSet || previous.trust.present()
}

func (previous previousReportFlags) valid() bool {
	return !previous.present() || previous.pathSet && strings.TrimSpace(previous.path) != "" && previous.trust.valid()
}

// Neither type is a serializable authorization. Only verified acquisition
// constructs a populated summary; paths, raw payloads and keys are dropped.
type verifiedReportSummary struct {
	verified                    bool
	sessionID, product, profile string
	reference                   report.PreviousReport
	linked                      bool
	previous                    report.PreviousReport
}

type verifiedPreviousReport struct {
	verified  bool
	sessionID string
	reference report.PreviousReport
}

func (previous verifiedPreviousReport) valid() bool {
	return previous.verified && strings.TrimSpace(previous.sessionID) != "" && previous.reference.Validate() == nil
}

// All inputs are acquired and closed before parsing. The canonical bundle hash
// comes only from successful full verification, never a separate pathname hash.
func readVerifiedRetestReport(ctx context.Context, path string, trust reportTrustFlags, readInput retestInputReader) (verifiedReportSummary, error) {
	if ctx == nil || readInput == nil || !trust.valid() {
		return verifiedReportSummary{}, errRetestVerification
	}
	if err := ctx.Err(); err != nil {
		return verifiedReportSummary{}, err
	}
	encoded, err := readInput(ctx, path, retestArtifactInput)
	if err != nil {
		return verifiedReportSummary{}, err
	}
	var keys report.Keyring
	if !trust.selfIssued {
		key, err := readInput(ctx, trust.publicKeyPath, retestPublicKeyInput)
		if err != nil {
			return verifiedReportSummary{}, err
		}
		if len(key) != ed25519.PublicKeySize {
			return verifiedReportSummary{}, errRetestVerification
		}
		keys = report.Keyring{strings.TrimSpace(trust.keyID): ed25519.PublicKey(key)}
	}
	if err := ctx.Err(); err != nil {
		return verifiedReportSummary{}, err
	}
	var artifact bundle.Artifact
	if trust.selfIssued {
		artifact, err = bundle.VerifySelfIssued(bytes.NewReader(encoded), int64(len(encoded)))
	} else {
		artifact, err = bundle.Verify(bytes.NewReader(encoded), int64(len(encoded)), keys)
	}
	if err != nil {
		return verifiedReportSummary{}, errRetestVerification
	}
	if err := ctx.Err(); err != nil {
		return verifiedReportSummary{}, err
	}
	summary := verifiedReportSummary{verified: true, sessionID: artifact.Envelope.SessionID, product: artifact.Envelope.Product, profile: artifact.Manifest.Schema,
		reference: report.PreviousReport{ManifestDigest: artifact.Envelope.ManifestDigest, BundleSHA256: "sha256:" + hex.EncodeToString(artifact.BundleSHA256[:])}}
	if artifact.Manifest.PreviousReport != nil {
		summary.linked, summary.previous = true, *artifact.Manifest.PreviousReport
	}
	return summary, nil
}

func (previous previousReportFlags) load(ctx context.Context, readInput retestInputReader) (*verifiedPreviousReport, error) {
	if !previous.valid() {
		return nil, errRetestVerification
	}
	if !previous.present() {
		return nil, nil
	}
	summary, err := readVerifiedRetestReport(ctx, previous.path, previous.trust, readInput)
	if err != nil {
		return nil, err
	}
	if !summary.verified || summary.product != product.Name || (summary.profile != bundle.ManifestSchema && summary.profile != bundle.RetestManifestSchema && summary.profile != bundle.MemoryCapacityManifestSchema && summary.profile != bundle.MemoryCapacityRetestManifestSchema) {
		return nil, errRetestVerification
	}
	parent := verifiedPreviousReport{verified: true, sessionID: summary.sessionID, reference: summary.reference}
	if !parent.valid() {
		return nil, errRetestVerification
	}
	return &parent, nil
}

func randomRetestSessionID(random io.Reader) (string, error) {
	var value [16]byte
	if random == nil {
		return "", errRetestVerification
	}
	for offset := 0; offset < len(value); {
		count, err := random.Read(value[offset:])
		if count <= 0 || count > len(value)-offset || err != nil {
			return "", errRetestVerification
		}
		offset += count
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	text := hex.EncodeToString(value[:])
	return text[:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:], nil
}

func runVerifyWithInput(ctx context.Context, arguments []string, stdout, stderr io.Writer, readInput retestInputReader) int {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var trust reportTrustFlags
	trust.register(flags, "")
	var previous previousReportFlags
	previous.register(flags)
	if flags.Parse(arguments) != nil {
		fmt.Fprintln(stderr, "verify: invalid arguments")
		return 2
	}
	trust.recordPresence(flags)
	previous.recordPresence(flags)
	if flags.NArg() != 1 || !trust.valid() || !previous.valid() {
		fmt.Fprintln(stderr, "verify requires explicit independent trust for each supplied report")
		return 2
	}
	child, err := readVerifiedRetestReport(ctx, flags.Arg(0), trust, readInput)
	if err != nil {
		fmt.Fprintln(stderr, "verify: child input or verification failed")
		return 1
	}
	var parent *verifiedPreviousReport
	if previous.present() {
		if !child.linked {
			fmt.Fprintln(stderr, "verify: supplied parent requires a linked child")
			return 1
		}
		parent, err = previous.load(ctx, readInput)
		if err != nil {
			fmt.Fprintln(stderr, "verify: parent input or verification failed")
			return 1
		}
		if !parent.valid() || child.previous != parent.reference || child.sessionID == parent.sessionID {
			fmt.Fprintln(stderr, "verify: exact predecessor relation mismatch")
			return 1
		}
	}
	if ctx == nil || ctx.Err() != nil {
		fmt.Fprintln(stderr, "verify: cancelled before success")
		return 1
	}
	// No acquired artifact/key handle remains open at any successful output.
	var message bytes.Buffer
	if trust.selfIssued {
		fmt.Fprintln(&message, "verified: contents/signature valid; signer is self-issued and does not prove operator identity")
	} else {
		fmt.Fprintln(&message, "verified: report signature and contents are valid")
	}
	if child.linked {
		fmt.Fprintf(&message, "previous_report manifest_digest=%s bundle_sha256=%s\n", child.previous.ManifestDigest, child.previous.BundleSHA256)
		if parent == nil {
			fmt.Fprintln(&message, "predecessor relation: unchecked; no parent was supplied")
		} else {
			fmt.Fprintln(&message, "predecessor relation: exact supplied parent matched; no asset, chronology, supersession or complete-chain claim")
		}
	}
	if err := writeAll(stdout, message.Bytes()); err != nil {
		fmt.Fprintln(stderr, "verify: output incomplete")
		return 1
	}
	return 0
}
