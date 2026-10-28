//go:build !linux && !windows

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

func TestReportTargetFingerprintOtherPlatformRejectsInput(t *testing.T) {
	for _, mode := range []string{"catalog", "authorization"} {
		for _, entry := range []string{"consumer_seam", "dispatch"} {
			t.Run(mode+"/"+entry, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "fingerprint.key")
				key := []byte("12345678901234567890123456789012")
				if err := os.WriteFile(path, key, 0600); err != nil {
					t.Fatal(err)
				}
				arguments := []string{"--fingerprint-key", path}
				if mode == "authorization" {
					arguments = append(arguments, "--device", "/dev/sdb1", "--confirm", "RPT-15B6C5")
				}
				var stdout, stderr bytes.Buffer
				reads := 0
				reader := func(ctx context.Context, gotPath string, kind retestInputKind) ([]byte, error) {
					reads++
					if gotPath != path || kind != retestFingerprintKeyInput {
						t.Fatal("input arguments changed")
					}
					data, err := readRetestInput(ctx, gotPath, kind)
					if data != nil || err != errRetestInput {
						t.Fatal("unsupported adapter did not fail explicitly", err)
					}
					return data, err
				}
				discover := func(context.Context, reporttarget.Options) reporttarget.Catalog {
					t.Fatal("unsupported input reached discovery")
					return reporttarget.Catalog{}
				}
				authorize := func(context.Context, reporttarget.Options, reporttarget.AuthorizationRequest) (reporttarget.Authorization, error) {
					t.Fatal("unsupported input reached authorization")
					return reporttarget.Authorization{}, nil
				}
				destination := func(string, io.Writer, func(io.Writer) error) error {
					t.Fatal("unsupported input reached output")
					return nil
				}
				var code int
				if entry == "consumer_seam" {
					code = runReportTargetWith(context.Background(), arguments, &stdout, &stderr, reader, discover, authorize, destination)
					if reads != 1 {
						t.Fatal("real unsupported reader was not reached")
					}
				} else {
					code = run(context.Background(), append([]string{"report-target"}, arguments...), bytes.NewReader(nil), &stdout, &stderr)
				}
				if code != 1 || stdout.Len() != 0 || stderr.String() != "read fingerprint key: input acquisition failed\n" {
					t.Fatal("unsupported input was not rejected", code, stderr.String())
				}
			})
		}
	}
}
