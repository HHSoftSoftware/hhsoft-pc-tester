package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/bundle"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/graphics"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inputtest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/networktest"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/quick"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/render"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storage"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/usbtest"
)

const sessionIDLength = 16

type snapshotCollector func(context.Context) (inventory.Snapshot, error)
type graphicsCollector func(context.Context, int) graphics.Result
type cpuCollector func(context.Context, quick.CPUOptions) quick.CPUResult
type memoryCollector func(context.Context, quick.MemoryOptions) quick.MemoryResult
type inputCollector func(context.Context, inputtest.Options) inputtest.Result
type networkCollector func(context.Context, networktest.Options) networktest.Result
type storageCollector func(context.Context, storage.Options) storage.Result
type usbCollector func(context.Context, usbtest.Options) usbtest.Result

func runCapture(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	collector := inventory.NewLinuxCollector("/")
	probe := func(ctx context.Context, frames int) graphics.Result {
		return graphics.Probe(ctx, "/", frames, graphics.NewExecRunner("/run/hhpt", 20*time.Second, 64<<10))
	}
	return runCaptureWith(ctx, arguments, stdout, stderr, collector.Collect, probe, quick.RunCPU, quick.RunMemory, inputtest.Run, networktest.Run, storage.Run, usbtest.Run, cryptorand.Reader, time.Now)
}

func runCaptureWith(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect snapshotCollector, probeGraphics graphicsCollector, probeCPU cpuCollector, probeMemory memoryCollector, probeInput inputCollector, probeNetwork networkCollector, probeStorage storageCollector, probeUSB usbCollector, random io.Reader, now func() time.Time) int {
	return runCaptureWithDestination(ctx, arguments, stdout, stderr, collect, probeGraphics, probeCPU, probeMemory, probeInput, probeNetwork, probeStorage, probeUSB, random, now, writeDestination)
}

func runCaptureWithDestination(ctx context.Context, arguments []string, stdout, stderr io.Writer, collect snapshotCollector, probeGraphics graphicsCollector, probeCPU cpuCollector, probeMemory memoryCollector, probeInput inputCollector, probeNetwork networkCollector, probeStorage storageCollector, probeUSB usbCollector, random io.Reader, now func() time.Time, writeOutput destinationWriter) int {
	flags := flag.NewFlagSet("capture", flag.ContinueOnError)
	flags.SetOutput(stderr)
	fingerprintKeyPath := flags.String("fingerprint-key", "", "file containing exactly 32 raw random bytes")
	keyEpoch := flags.String("key-epoch", "local-1", "non-secret deployment key epoch label")
	signingKeyPath := flags.String("signing-key", "", "file containing exactly 64 raw Ed25519 private-key bytes")
	keyID := flags.String("key-id", "", "non-secret signing-key identifier")
	sessionID := flags.String("session-id", "", "session identifier; random when omitted")
	graphicsPatternFrames := flags.Int("graphics-pattern-frames", 120, "validated KMS frames to include in signed graphics evidence")
	cpuDuration := flags.Duration("cpu-duration", 3*time.Second, "bounded computation duration")
	cpuTimeout := flags.Duration("cpu-timeout", 20*time.Second, "CPU test global deadline")
	memoryBytes := flags.Int64("memory-bytes", 0, "working-set bytes; zero safely chooses up to 64 MiB")
	memoryPasses := flags.Int("memory-passes", 2, "one to eight bounded pattern passes")
	memoryTimeout := flags.Duration("memory-timeout", 30*time.Second, "memory test global deadline")
	inputDuration := flags.Duration("input-duration", 0, "bounded input activity observation; zero checks readiness")
	inputCategories := flags.String("input-categories", "", "comma-separated input categories; blank observes every present category")
	thermalLimit := flags.Int("thermal-limit-c", 90, "abort active tests at this temperature")
	outputPath := flags.String("output", "", "new .hhpcr output file")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "capture accepts no positional arguments")
		return 2
	}
	if *graphicsPatternFrames < 0 || *graphicsPatternFrames > 600 {
		fmt.Fprintln(stderr, "capture requires -graphics-pattern-frames between 0 and 600")
		return 2
	}
	if *cpuDuration <= 0 || *cpuTimeout <= *cpuDuration || *memoryBytes < 0 || *memoryPasses < 1 || *memoryPasses > 8 || *memoryTimeout < time.Second || *inputDuration < 0 || *inputDuration > 5*time.Minute || *thermalLimit < 50 || *thermalLimit > 125 {
		fmt.Fprintln(stderr, "capture requires bounded CPU/memory/input durations and a 50-125C thermal limit")
		return 2
	}
	if *fingerprintKeyPath == "" || *signingKeyPath == "" || strings.TrimSpace(*keyID) == "" || *outputPath == "" {
		fmt.Fprintln(stderr, "capture requires -fingerprint-key, -signing-key, -key-id, and -output")
		return 2
	}

	fingerprintKey, err := readRawKey(*fingerprintKeyPath, 32)
	if err != nil {
		fmt.Fprintf(stderr, "read fingerprint key: %v\n", err)
		return 1
	}
	privateKeyBytes, err := readRawKey(*signingKeyPath, ed25519.PrivateKeySize)
	if err != nil {
		fmt.Fprintf(stderr, "read signing key: %v\n", err)
		return 1
	}
	privateKey := ed25519.PrivateKey(privateKeyBytes)
	if subtle.ConstantTimeCompare(privateKey, ed25519.NewKeyFromSeed(privateKey.Seed())) != 1 {
		fmt.Fprintln(stderr, "read signing key: inconsistent Ed25519 seed and public key")
		return 1
	}
	resolvedSessionID := strings.TrimSpace(*sessionID)
	if resolvedSessionID == "" {
		resolvedSessionID, err = randomSessionID(random)
		if err != nil {
			fmt.Fprintf(stderr, "generate session ID: %v\n", err)
			return 1
		}
	}

	snapshot, err := collect(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "collect inventory: %v\n", err)
		return 1
	}
	cpuEvidence := probeCPU(ctx, quick.CPUOptions{Duration: *cpuDuration, GlobalTimeout: *cpuTimeout, Root: "/", ThermalLimit: *thermalLimit * 1000})
	if quickCleanupUnconfirmed(cpuEvidence.Safety) {
		fmt.Fprintln(stderr, "capture stopped: CPU thermal observer cleanup could not be confirmed")
		return workerCleanupUncertainExitCode
	}
	cpuResult, err := newValidatedQuickTestResult("cpu.quick", 2, app.TestStatus(cpuEvidence.Status), cpuEvidence)
	if err != nil {
		fmt.Fprintf(stderr, "build CPU evidence: %v\n", err)
		return 1
	}
	memoryEvidence := quick.MemoryResult{Schema: quick.MemorySchema, Status: quick.StatusAborted, Detail: "not run after CPU safety or correctness stop",
		PatternsPerPass: 3, Coverage: quick.MemoryCoverage{Suite: quick.MemorySuite},
		Safety: quick.SafetyEvidence{GlobalTimeoutMilliseconds: memoryTimeout.Milliseconds(), ThermalLimitMilliCelsius: *thermalLimit * 1000,
			MonitorStatus: "not_started", CleanupConfirmed: true, ThermalCoverage: "unavailable"}}
	if cpuEvidence.Status == quick.StatusPass {
		memoryEvidence = probeMemory(ctx, quick.MemoryOptions{Bytes: *memoryBytes, Passes: *memoryPasses, GlobalTimeout: *memoryTimeout, Root: "/", ThermalLimit: *thermalLimit * 1000})
	}
	if quickCleanupUnconfirmed(memoryEvidence.Safety) {
		fmt.Fprintln(stderr, "capture stopped: memory thermal observer cleanup could not be confirmed")
		return workerCleanupUncertainExitCode
	}
	memoryResult, err := newValidatedQuickTestResult("memory.quick", 2, app.TestStatus(memoryEvidence.Status), memoryEvidence)
	if err != nil {
		fmt.Fprintf(stderr, "build memory evidence: %v\n", err)
		return 1
	}
	graphicsEvidence := graphics.Result{Schema: graphics.Schema, Status: graphics.StatusAborted, Warnings: []string{"not run after CPU or memory safety or correctness stop"}}
	if cpuEvidence.Status == quick.StatusPass && memoryEvidence.Status == quick.StatusPass {
		graphicsEvidence = probeGraphics(ctx, *graphicsPatternFrames)
	}
	if graphicsEvidence.CleanupUncertain {
		fmt.Fprintln(stderr, "capture stopped: graphics worker cleanup could not be confirmed")
		return workerCleanupUncertainExitCode
	}
	graphicsResult, err := newValidatedQuickTestResult("graphics.quick", 1, app.TestStatus(graphicsEvidence.Status), graphicsEvidence)
	if err != nil {
		fmt.Fprintf(stderr, "build graphics evidence: %v\n", err)
		return 1
	}
	var requestedInput []string
	if strings.TrimSpace(*inputCategories) != "" {
		requestedInput = strings.Split(*inputCategories, ",")
	}
	inputEvidence := probeInput(ctx, inputtest.Options{Root: "/", Duration: *inputDuration, Categories: requestedInput})
	inputResult, err := newValidatedQuickTestResult("input.readiness.quick", 1, app.TestStatus(inputEvidence.Status), inputEvidence)
	if err != nil {
		fmt.Fprintf(stderr, "build input evidence: %v\n", err)
		return 1
	}
	networkEvidence := probeNetwork(ctx, networktest.Options{Root: "/"})
	networkResult, err := newValidatedQuickTestResult("network.readiness.quick", 1, app.TestStatus(networkEvidence.Status), networkEvidence)
	if err != nil {
		fmt.Fprintf(stderr, "build network evidence: %v\n", err)
		return 1
	}
	storageEvidence := probeStorage(ctx, storage.Options{Root: "/"})
	storageResult, err := newValidatedQuickTestResult("storage.readiness.quick", 1, app.TestStatus(storageEvidence.Status), storageEvidence)
	if err != nil {
		fmt.Fprintf(stderr, "build storage evidence: %v\n", err)
		return 1
	}
	usbEvidence := probeUSB(ctx, usbtest.Options{Root: "/"})
	usbResult, err := newValidatedQuickTestResult("usb.readiness.quick", 1, app.TestStatus(usbEvidence.Status), usbEvidence)
	if err != nil {
		fmt.Fprintf(stderr, "build USB evidence: %v\n", err)
		return 1
	}
	encoded, err := captureBundle(snapshot, []app.TestResult{cpuResult, memoryResult, graphicsResult, inputResult, networkResult, storageResult, usbResult}, fingerprintKey, *keyEpoch, privateKey, strings.TrimSpace(*keyID), resolvedSessionID, now(), app.CoverageDirectCapture)
	if err != nil {
		fmt.Fprintf(stderr, "build report: %v\n", err)
		return 1
	}
	if err := writeOutput(*outputPath, stdout, func(destination io.Writer) error {
		return writeAll(destination, encoded)
	}); err != nil {
		fmt.Fprintf(stderr, "write report: %v\n", err)
		return 1
	}
	return 0
}

func runVerify(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	return runVerifyWithInput(ctx, arguments, stdout, stderr, readRetestInput)
}

func captureBundle(snapshot inventory.Snapshot, testResults []app.TestResult, fingerprintKey []byte, keyEpoch string, privateKey ed25519.PrivateKey, keyID, sessionID string, generated time.Time, coverageScope app.CoverageScope) ([]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("signing key must contain exactly %d bytes", ed25519.PrivateKeySize)
	}
	payload, err := app.BuildReportPayload(snapshot, fingerprintKey, keyEpoch, testResults)
	if err != nil {
		return nil, err
	}
	payload.CoverageScope = coverageScope
	inventoryJSON, html, pdf, err := renderSerializedInventory(payload)
	if err != nil {
		return nil, err
	}
	manifestDigest, err := bundle.ManifestDigestWithPDF(inventoryJSON, html, pdf)
	if err != nil {
		return nil, err
	}
	envelope := report.Envelope{
		Schema:         product.ReportSchema,
		Product:        product.Name,
		Version:        product.Version,
		SessionID:      sessionID,
		GeneratedTime:  generated.UTC().Format(time.RFC3339Nano),
		Payload:        append(json.RawMessage(nil), inventoryJSON...),
		ManifestDigest: manifestDigest,
		KeyID:          keyID,
	}
	if err := report.Sign(&envelope, privateKey); err != nil {
		return nil, err
	}
	var encoded bytes.Buffer
	if err := bundle.WriteWithPDF(&encoded, inventoryJSON, envelope, html, pdf); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

func captureSelfIssuedBundle(snapshot inventory.Snapshot, testResults []app.TestResult, fingerprintKey []byte, keyEpoch string, privateKey ed25519.PrivateKey, publicKey ed25519.PublicKey, sessionID string, generated time.Time, coverageScope app.CoverageScope) ([]byte, error) {
	return captureSelfIssuedBundleWithPrevious(snapshot, testResults, fingerprintKey, keyEpoch, privateKey, publicKey, sessionID, generated, coverageScope, nil)
}

func captureSelfIssuedBundleWithPrevious(snapshot inventory.Snapshot, testResults []app.TestResult, fingerprintKey []byte, keyEpoch string, privateKey ed25519.PrivateKey, publicKey ed25519.PublicKey, sessionID string, generated time.Time, coverageScope app.CoverageScope, previous *report.PreviousReport) ([]byte, error) {
	if len(privateKey) != ed25519.PrivateKeySize || len(publicKey) != ed25519.PublicKeySize || !bytes.Equal(privateKey.Public().(ed25519.PublicKey), publicKey) {
		return nil, errors.New("self-issued signing key is invalid or inconsistent")
	}
	payload, err := app.BuildReportPayload(snapshot, fingerprintKey, keyEpoch, testResults)
	if err != nil {
		return nil, err
	}
	payload.CoverageScope = coverageScope
	inventoryJSON, html, pdf, err := renderSerializedInventoryWithPrevious(payload, previous)
	if err != nil {
		return nil, err
	}
	var manifestDigest string
	if previous == nil {
		manifestDigest, err = bundle.SelfIssuedManifestDigestWithPDF(inventoryJSON, html, pdf, publicKey)
	} else {
		manifestDigest, err = bundle.RetestManifestDigestWithPDF(inventoryJSON, html, pdf, *previous, publicKey)
	}
	if err != nil {
		return nil, err
	}
	keyID, err := bundle.SelfIssuedKeyID(publicKey)
	if err != nil {
		return nil, err
	}
	envelope := report.Envelope{
		Schema: product.ReportSchema, Product: product.Name, Version: product.Version,
		SessionID: sessionID, GeneratedTime: generated.UTC().Format(time.RFC3339Nano),
		Payload: append(json.RawMessage(nil), inventoryJSON...), ManifestDigest: manifestDigest, KeyID: keyID,
	}
	if err := report.Sign(&envelope, privateKey); err != nil {
		return nil, err
	}
	var encoded bytes.Buffer
	if previous == nil {
		err = bundle.WriteSelfIssuedWithPDF(&encoded, inventoryJSON, envelope, html, pdf, publicKey)
	} else {
		err = bundle.WriteRetestWithPDF(&encoded, inventoryJSON, envelope, html, pdf, *previous, publicKey)
	}
	if err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

// Render the exact representation authenticated in the report. Marshaling
// compacts and HTML-escapes RawMessage evidence, including worker-file trailing
// newlines. Rendering the earlier in-memory payload would produce views that
// differ from the verifier's reconstruction. The typed round trip retains
// evidence as RawMessage, preserving numeric precision and spelling.
func renderSerializedInventory(payload app.InventoryPayload) (inventoryJSON, html, pdf []byte, err error) {
	return renderSerializedInventoryWithPrevious(payload, nil)
}

func renderSerializedInventoryWithPrevious(payload app.InventoryPayload, previous *report.PreviousReport) (inventoryJSON, html, pdf []byte, err error) {
	inventoryJSON, err = json.Marshal(payload)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("encode inventory: %w", err)
	}
	// Keep the extra decoding allocation within the bundle inventory limit.
	if len(inventoryJSON) > 16<<20 {
		return nil, nil, nil, errors.New("serialized inventory exceeds the report size limit")
	}
	var serialized app.InventoryPayload
	if err := json.Unmarshal(inventoryJSON, &serialized); err != nil {
		return nil, nil, nil, fmt.Errorf("decode serialized inventory: %w", err)
	}
	if previous == nil {
		html, err = render.InventoryHTML(serialized)
	} else {
		html, err = render.InventoryRetestHTMLForVersion(serialized, product.Version, *previous)
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("render report: %w", err)
	}
	if previous == nil {
		pdf, err = render.InventoryPDF(serialized)
	} else {
		pdf, err = render.InventoryRetestPDFForVersion(serialized, product.Version, *previous)
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("render PDF: %w", err)
	}
	return inventoryJSON, html, pdf, nil
}

func randomSessionID(random io.Reader) (string, error) {
	value := make([]byte, sessionIDLength)
	if _, err := io.ReadFull(random, value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func readRawKey(path string, size int) ([]byte, error) {
	source, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	contents, err := io.ReadAll(io.LimitReader(source, int64(size+1)))
	if err != nil {
		return nil, err
	}
	if len(contents) != size {
		return nil, fmt.Errorf("must contain exactly %d raw bytes", size)
	}
	return contents, nil
}

func writeAll(destination io.Writer, contents []byte) error {
	written, err := destination.Write(contents)
	if err != nil {
		return err
	}
	if written != len(contents) {
		return errors.New("short write")
	}
	return nil
}
