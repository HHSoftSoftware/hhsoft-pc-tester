//go:build linux || windows

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/reporttarget"
)

func TestReportTargetFingerprintRealRegularFileSizes(t *testing.T) {
	for _, mode := range []string{"catalog", "authorization"} {
		for _, shape := range []string{"short", "exact", "long"} {
			t.Run(mode+"/"+shape, func(t *testing.T) {
				key := []byte("12345678901234567890123456789012")
				contents := append([]byte(nil), key...)
				if shape == "short" {
					contents = contents[:31]
				}
				if shape == "long" {
					contents = append(contents, 1)
				}
				path := filepath.Join(t.TempDir(), "fingerprint.key")
				if err := os.WriteFile(path, contents, 0600); err != nil {
					t.Fatal(err)
				}
				ctx := context.WithValue(context.Background(), struct{}{}, "real-file-command")
				arguments := []string{"--fingerprint-key", path}
				if mode == "authorization" {
					arguments = append(arguments, "--device", "/dev/sdb1", "--confirm", "RPT-15B6C5")
				}
				reads, discoveries, authorizations, outputs := 0, 0, 0, 0
				reader := func(got context.Context, gotPath string, kind retestInputKind) ([]byte, error) {
					reads++
					if got != ctx || gotPath != path || kind != retestFingerprintKeyInput {
						t.Fatal("real reader arguments changed")
					}
					data, err := readRetestInput(got, gotPath, kind)
					if shape == "exact" {
						if err != nil || !bytes.Equal(data, key) {
							t.Fatal("real captured bytes changed", err)
						}
					} else if data != nil || err == nil {
						t.Fatal("real key size was accepted")
					}
					return data, err
				}
				check := func(got context.Context, options reporttarget.Options) {
					if got != ctx || options.Root != "/" || !bytes.Equal(options.FingerprintKey, key) || shape != "exact" {
						t.Fatal("invalid real capture reached consumer")
					}
				}
				discover := func(got context.Context, options reporttarget.Options) reporttarget.Catalog {
					discoveries++
					check(got, options)
					if mode != "catalog" {
						t.Fatal("wrong consumer")
					}
					return reportTargetFingerprintCatalog()
				}
				authorize := func(got context.Context, options reporttarget.Options, request reporttarget.AuthorizationRequest) (reporttarget.Authorization, error) {
					authorizations++
					check(got, options)
					if mode != "authorization" || request != (reporttarget.AuthorizationRequest{Device: "/dev/sdb1", ConfirmationCode: "RPT-15B6C5"}) {
						t.Fatal("wrong authorization request")
					}
					return reporttarget.NewAuthorization(reportTargetFingerprintCatalog().Candidates[0], "11111111-1111-4111-8111-111111111111", 12, options.FingerprintKey)
				}
				destination := func(path string, stdout io.Writer, write func(io.Writer) error) error {
					outputs++
					if path != "-" {
						t.Fatal("output path changed")
					}
					return writeDestination(path, stdout, write)
				}
				var stdout, stderr bytes.Buffer
				code := runReportTargetWith(ctx, arguments, &stdout, &stderr, reader, discover, authorize, destination)
				if reads != 1 {
					t.Fatal("real reader call count", reads)
				}
				if shape == "exact" {
					if code != 0 || discoveries+authorizations != 1 || outputs != 1 || stderr.Len() != 0 {
						t.Fatal("real input completion failed", code, stderr.String())
					}
					assertReportTargetFingerprintJSON(t, stdout.Bytes(), mode == "authorization", reportTargetFingerprintCatalog())
					if bytes.Contains(stdout.Bytes(), key) || bytes.Contains(stdout.Bytes(), []byte(path)) {
						t.Fatal("result disclosed input")
					}
				} else if code != 1 || discoveries != 0 || authorizations != 0 || outputs != 0 || stdout.Len() != 0 || stderr.String() != "read fingerprint key: input acquisition failed\n" {
					t.Fatal("real size failure crossed the input boundary", code, stderr.String())
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(after, contents) {
					t.Fatal("key fixture was modified", err)
				}
			})
		}
	}
}

func TestReportTargetFingerprintRealDispatchRejectsNonRegularInput(t *testing.T) {
	for _, mode := range []string{"catalog", "authorization"} {
		t.Run(mode, func(t *testing.T) {
			// A directory cannot satisfy the real loader's ordinary-file proof.
			// No valid key is supplied to either real topology consumer.
			arguments := []string{"report-target", "--fingerprint-key", t.TempDir()}
			if mode == "authorization" {
				arguments = append(arguments, "--device", "/dev/sdb1", "--confirm", "RPT-15B6C5")
			}
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), arguments, bytes.NewReader(nil), &stdout, &stderr)
			if code != 1 || stdout.Len() != 0 || stderr.String() != "read fingerprint key: input acquisition failed\n" {
				t.Fatal("real dispatch did not stop at owned input acquisition", code, stderr.String())
			}
		})
	}
}
