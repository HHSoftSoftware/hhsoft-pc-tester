package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/correction"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

func runCorrection(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	return runCorrectionWith(ctx, arguments, stdout, stderr, readRetestInput, writeDestination, rand.Reader)
}

func runCorrectionWith(ctx context.Context, arguments []string, stdout, stderr io.Writer, readInput retestInputReader, writeOutput destinationWriter, random io.Reader) int {
	fail := func(message string, code int) int { fmt.Fprintln(stderr, message); return code }
	if len(arguments) == 0 || arguments[0] != "create" && arguments[0] != "verify" && arguments[0] != "view" {
		return fail("correction requires create, verify or view", 2)
	}
	mode := arguments[0]
	flags := flag.NewFlagSet("correction "+mode, flag.ContinueOnError)
	// Parser/provider errors can contain supplied values; diagnostics are fixed.
	flags.SetOutput(io.Discard)
	reportPath := flags.String("report", "", "original signed .hhpcr report")
	var sourceTrust, addendumTrust reportTrustFlags
	sourceTrust.register(flags, "source-")
	var changesPath, signingPath, signingID, addendumPath, outputPath string
	if mode == "create" {
		flags.StringVar(&changesPath, "changes", "", "bounded public display-change request JSON")
		flags.StringVar(&signingPath, "signing-key", "", "exact 64-byte Ed25519 signing key")
		flags.StringVar(&signingID, "key-id", "", "non-secret public addendum signer label")
	} else {
		flags.StringVar(&addendumPath, "addendum", "", "signed correction JSON")
		addendumTrust.register(flags, "addendum-")
	}
	if mode != "verify" {
		flags.StringVar(&outputPath, "output", "", "new output file, or - for stdout")
	}
	if err := flags.Parse(arguments[1:]); err != nil {
		if err == flag.ErrHelp {
			if nilRetestInputValue(ctx) || ctx.Err() != nil {
				return fail("correction help cancelled", 1)
			}
			help := "correction create: -report FILE -source-public-key FILE -source-key-id ID -changes FILE -signing-key FILE -key-id ID -output FILE\n" +
				"correction verify/view: -report FILE -source-public-key FILE -source-key-id ID -addendum FILE -addendum-public-key FILE -addendum-key-id ID; view also requires -output FILE\n" +
				"For either trust choice, replace its public-key/key-id pair with the corresponding explicit source-self-issued or addendum-self-issued flag.\n"
			if err := writeAll(completeOutputWriter{stdout}, []byte(help)); err != nil || ctx.Err() != nil {
				return fail("correction help output failed", 1)
			}
			return 0
		}
		return fail("invalid correction arguments", 2)
	}
	if flags.NArg() != 0 {
		return fail("invalid correction arguments", 2)
	}
	sourceTrust.recordPresence(flags)
	addendumTrust.recordPresence(flags)
	if *reportPath == "" || !sourceTrust.valid() ||
		mode == "create" && (changesPath == "" || signingPath == "" || signingID == "") ||
		mode != "create" && (addendumPath == "" || !addendumTrust.valid()) || mode != "verify" && outputPath == "" {
		return fail("correction requires its inputs, output and independent explicit trust selections", 2)
	}
	if nilRetestInputValue(ctx) || readInput == nil || writeOutput == nil || ctx.Err() != nil {
		return fail("correction input acquisition failed", 1)
	}
	// Each reader completes owned-input cleanup before releasing bytes to parse.
	rawSource, err := readInput(ctx, *reportPath, retestArtifactInput)
	if err != nil || ctx.Err() != nil {
		return fail("correction source acquisition failed", 1)
	}
	sourceKeys, err := correctionTrustKeys(ctx, sourceTrust, readInput)
	if err != nil || ctx.Err() != nil {
		return fail("correction source trust acquisition failed", 1)
	}
	source, err := correction.VerifySource(rawSource, sourceKeys, sourceTrust.selfIssued)
	if err != nil || ctx.Err() != nil {
		return fail("correction source verification failed", 1)
	}
	var encoded []byte
	if mode == "create" {
		request, err := readInput(ctx, changesPath, retestCorrectionRequestInput)
		if err != nil || ctx.Err() != nil {
			return fail("correction request acquisition failed", 1)
		}
		key, err := readInput(ctx, signingPath, retestSigningKeyInput)
		defer clear(key)
		if err != nil || ctx.Err() != nil {
			return fail("correction signing-key acquisition failed", 1)
		}
		id, err := randomRetestSessionID(random)
		if err != nil || ctx.Err() != nil {
			return fail("correction identifier generation failed", 1)
		}
		encoded, err = correction.Create(source, request, id, signingID, ed25519.PrivateKey(key))
		if err != nil || ctx.Err() != nil {
			return fail("correction creation failed", 1)
		}
	} else {
		rawAddendum, err := readInput(ctx, addendumPath, retestCorrectionArtifactInput)
		if err != nil || ctx.Err() != nil {
			return fail("correction addendum acquisition failed", 1)
		}
		keys, err := correctionTrustKeys(ctx, addendumTrust, readInput)
		if err != nil || ctx.Err() != nil {
			return fail("correction addendum trust acquisition failed", 1)
		}
		verified, err := correction.Verify(source, rawAddendum, keys, addendumTrust.selfIssued)
		if err != nil || ctx.Err() != nil {
			return fail("correction addendum verification failed", 1)
		}
		if mode == "verify" {
			encoded, err = correction.VerificationText(verified)
		} else {
			encoded, err = correction.RenderHTML(verified)
		}
		if err != nil || ctx.Err() != nil {
			return fail("correction display failed", 1)
		}
	}
	if mode == "verify" {
		outputPath = "-"
	}
	if ctx.Err() != nil {
		return fail("correction cancelled before output", 1)
	}
	err = writeOutput(outputPath, stdout, func(output io.Writer) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := writeAll(output, encoded); err != nil {
			return err
		}
		return ctx.Err()
	})
	if err != nil || ctx.Err() != nil {
		return fail("correction output failed", 1)
	}
	return 0
}

func correctionTrustKeys(ctx context.Context, trust reportTrustFlags, readInput retestInputReader) (report.Keyring, error) {
	if trust.selfIssued {
		return nil, nil
	}
	public, err := readInput(ctx, trust.publicKeyPath, retestPublicKeyInput)
	if err != nil || ctx.Err() != nil || len(public) != ed25519.PublicKeySize {
		return nil, errRetestVerification
	}
	return report.Keyring{strings.TrimSpace(trust.keyID): ed25519.PublicKey(append([]byte(nil), public...))}, nil
}
