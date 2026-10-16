package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/deploymentkey"
)

func runDeploymentKey(ctx context.Context, arguments []string, input io.Reader, stdout, stderr io.Writer) int {
	if len(arguments) == 0 {
		fmt.Fprintln(stderr, "deployment-key requires init, inspect, or load-media")
		return 2
	}
	switch arguments[0] {
	case "init":
		return runDeploymentKeyInit(arguments[1:], stdout, stderr, rand.Reader)
	case "inspect":
		return runDeploymentKeyInspect(arguments[1:], stdout, stderr)
	case "load-media":
		return runDeploymentKeyLoadMedia(ctx, arguments[1:], input, stdout, stderr)
	default:
		fmt.Fprintln(stderr, "deployment-key requires init, inspect, or load-media")
		return 2
	}
}

func runDeploymentKeyInit(arguments []string, stdout, stderr io.Writer, random io.Reader) int {
	flags := flag.NewFlagSet("deployment-key init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	epoch := flags.String("epoch", "", "non-secret stable fingerprint epoch")
	output := flags.String("output", "", "new deployment-key file")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || !deploymentkey.ValidEpoch(*epoch) || filepath.Base(*output) != deploymentkey.Filename {
		fmt.Fprintf(stderr, "deployment-key init requires -epoch beginning with a letter and -output ending in %s\n", deploymentkey.Filename)
		return 2
	}
	record, err := deploymentkey.New(*epoch, random)
	if err != nil {
		fmt.Fprintf(stderr, "generate deployment key: %v\n", err)
		return 1
	}
	encoded, err := deploymentkey.Encode(record)
	if err != nil {
		fmt.Fprintf(stderr, "encode deployment key: %v\n", err)
		return 1
	}
	if err := writeExclusiveSynced(*output, encoded); err != nil {
		fmt.Fprintf(stderr, "write deployment key: %v\n", err)
		return 1
	}
	keyID, err := deploymentkey.KeyID(record)
	if err != nil {
		fmt.Fprintf(stderr, "identify deployment key: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "deployment key created: epoch=%s key_id=%s\n", record.Epoch, keyID)
	return 0
}

func runDeploymentKeyInspect(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("deployment-key inspect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "deployment-key inspect requires one key file")
		return 2
	}
	record, err := readDeploymentKey(flags.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "read deployment key: %v\n", err)
		return 1
	}
	keyID, err := deploymentkey.KeyID(record)
	if err != nil {
		fmt.Fprintf(stderr, "identify deployment key: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "deployment key valid: epoch=%s key_id=%s\n", record.Epoch, keyID)
	return 0
}

func readDeploymentKey(path string) (deploymentkey.Record, error) {
	file, metadata, err := openRegularNoSymlink(path, deploymentkey.MaximumEncodedSize)
	if err != nil {
		return deploymentkey.Record{}, err
	}
	defer file.Close()
	encoded, err := io.ReadAll(io.LimitReader(file, deploymentkey.MaximumEncodedSize+1))
	if err != nil || int64(len(encoded)) != metadata.Size() {
		return deploymentkey.Record{}, deploymentkey.ErrInvalid
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(metadata, after) || metadata.Size() != after.Size() {
		return deploymentkey.Record{}, deploymentkey.ErrInvalid
	}
	return deploymentkey.Decode(encoded)
}

func writeExclusiveSynced(path string, contents []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	written, writeErr := file.Write(contents)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || written != len(contents) || syncErr != nil || closeErr != nil {
		return fmt.Errorf("new file could not be written and synced completely")
	}
	return nil
}
