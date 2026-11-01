package render

import (
	"bytes"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"sort"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/app"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/report"
)

type inventoryDocument struct {
	PreviousReport      *report.PreviousReport
	ProductName         string
	ProductVersion      string
	SchemaVersion       int
	TotalComponents     int
	IdentifiedParts     int
	UnidentifiedParts   int
	SystemFingerprint   app.SystemFingerprint
	Components          []app.ComponentRecord
	TestResults         []testDocument
	CoverageDescription string
	Thermal             *thermalDocument
	Keyboard            *keyboardDocument
	CPUScalar           *cpuScalarDocument
	Memory              *memoryDocument
	Pointer             *pointerDocument
	Touchpad            *touchpadDocument
	Touchscreen         *touchscreenDocument
	Storage             *storageDocument
	CPUStress           *cpuStressDocument
	MemoryStress        *memoryStressDocument
	Inspection          *inspectionDocument
}

type testDocument struct {
	ID                 string
	Version            int
	Status             app.TestStatus
	PlanID             string
	HardwareGraph      string
	Evidence           string
	PlannedStatus      app.PlannedStatus
	PlannedDescription string
}

var inventoryTemplate = template.Must(template.New("inventory").Funcs(template.FuncMap{
	"display": func(value string) string {
		if strings.TrimSpace(value) == "" {
			return "Unavailable"
		}
		return value
	},
}).Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.ProductName}} Inventory Report</title>
<style>
:root{color-scheme:light;--ink:#17202a;--muted:#536270;--line:#8795a1;--paper:#f5f7f8;--panel:#fff;--accent:#125e70;--shade:#e4eaed}
*{box-sizing:border-box}
body{margin:0;background:var(--paper);color:var(--ink);font:14px/1.45 system-ui,-apple-system,"Segoe UI",sans-serif}
main{max-width:1100px;margin:0 auto;padding:24px}
h1,h2{line-height:1.15;margin:0 0 12px}h1{font-size:28px}h2{border-bottom:3px solid var(--accent);font-size:19px;padding-bottom:6px}
.subtitle{color:var(--muted);margin:0 0 24px}.section{margin:0 0 26px}.notice{background:var(--shade);border-left:5px solid var(--accent);padding:10px 12px}
table{background:var(--panel);border-collapse:collapse;border:1px solid var(--line);width:100%}th,td{border:1px solid var(--line);padding:7px 9px;text-align:left;vertical-align:top}th{background:var(--shade)}
.label{font-weight:650;width:210px}.fingerprint,pre{font-family:ui-monospace,"SFMono-Regular",Consolas,monospace;overflow-wrap:anywhere}.component,.test{margin:0 0 18px}.component h3,.test h3{background:var(--ink);color:#fff;font-size:15px;margin:0;padding:8px 10px}.attributes{margin-top:-1px}.attributes th:first-child{width:35%}pre{background:var(--panel);border:1px solid var(--line);margin:0;padding:10px;white-space:pre-wrap}
@media print{body{background:#fff}main{max-width:none;padding:0}.section,.component{break-inside:avoid}}
</style>
</head>
<body>
<main>
<header>
<h1>{{.ProductName}} Inventory Report</h1>
<p class="subtitle">Version {{.ProductVersion}} · Inventory schema {{.SchemaVersion}}</p>
</header>
{{if .PreviousReport}}<section class="section" aria-labelledby="retest-heading">
<h2 id="retest-heading">Linked retest reference</h2>
<p>A signed reference, not proof of shared hardware, chronology, supersession or inherited results. Verify the predecessor separately.</p>
<p>Previous manifest: {{.PreviousReport.ManifestDigest}}</p>
<p>Previous canonical bundle: {{.PreviousReport.BundleSHA256}}</p>
</section>
{{end}}<section class="section" aria-labelledby="summary-heading">
<h2 id="summary-heading">Summary</h2>
<table>
<tbody>
<tr><th class="label" scope="row">Components</th><td>{{.TotalComponents}}</td></tr>
<tr><th class="label" scope="row">Identified parts</th><td>{{.IdentifiedParts}}</td></tr>
<tr><th class="label" scope="row">Unidentified parts</th><td>{{.UnidentifiedParts}}</td></tr>
<tr><th class="label" scope="row">Functional test results</th><td>{{len .TestResults}}</td></tr>{{if .CoverageDescription}}
<tr><th class="label" scope="row">Coverage</th><td>{{.CoverageDescription}}</td></tr>{{end}}
</tbody>
</table>
</section>
<section class="section" aria-labelledby="system-heading">
<h2 id="system-heading">System Fingerprint</h2>
<table>
<tbody>
<tr><th class="label" scope="row">Algorithm</th><td>{{display .SystemFingerprint.Algorithm}}</td></tr>
<tr><th class="label" scope="row">Key epoch</th><td>{{display .SystemFingerprint.KeyEpoch}}</td></tr>
<tr><th class="label" scope="row">Fingerprint key ID</th><td class="fingerprint">{{display .SystemFingerprint.KeyID}}</td></tr>
<tr><th class="label" scope="row">Fingerprint</th><td class="fingerprint">{{display .SystemFingerprint.Value}}</td></tr>
</tbody>
</table>
</section>
{{if .MemoryStress}}<section class="section" aria-labelledby="memory-stress-heading">
<h2 id="memory-stress-heading">MEMORY STRESS (EXTENDED PROFILE)</h2>
<p>{{.MemoryStress.Scope}}</p>
<table><tbody>
<tr><th class="label" scope="row">Outcome</th><td>{{.MemoryStress.Outcome}}</td></tr>
<tr><th class="label" scope="row">Stop reason</th><td>{{.MemoryStress.StopReason}}</td></tr>
<tr><th class="label" scope="row">Requested load (ms)</th><td>{{.MemoryStress.Requested}}</td></tr>
<tr><th class="label" scope="row">Observed load window (ms)</th><td>{{.MemoryStress.Observed}}</td></tr>
<tr><th class="label" scope="row">Working-set bytes</th><td>{{.MemoryStress.Allocated}}</td></tr>
<tr><th class="label" scope="row">Verified load cycles</th><td>{{.MemoryStress.LoadCycles}}</td></tr>
<tr><th class="label" scope="row">Completed cycles including finishing cycle</th><td>{{.MemoryStress.CompletedCycles}}</td></tr>
<tr><th class="label" scope="row">Completed cycle phases</th><td>{{.MemoryStress.CompletedPhases}}</td></tr>
<tr><th class="label" scope="row">Accepted current-phase bytes</th><td>{{.MemoryStress.PhaseBytes}}</td></tr>
<tr><th class="label" scope="row">Accepted cycle read / write bytes</th><td>{{.MemoryStress.ReadBytes}} / {{.MemoryStress.WrittenBytes}}</td></tr>
<tr><th class="label" scope="row">Uncredited cycle prefix bytes</th><td>{{.MemoryStress.UncreditedBytes}}</td></tr>
<tr><th class="label" scope="row">Accepted terminal read bytes</th><td>{{.MemoryStress.TerminalReadBytes}}</td></tr>
<tr><th class="label" scope="row">Uncredited terminal prefix bytes</th><td>{{.MemoryStress.TerminalUncreditedBytes}}</td></tr>
<tr><th class="label" scope="row">Mismatch</th><td>{{.MemoryStress.Mismatch}}</td></tr>
<tr><th class="label" scope="row">Cleanup confirmed</th><td>{{.MemoryStress.Cleanup}}</td></tr>
</tbody></table></section>
{{end}}{{if .CPUStress}}<section class="section" aria-labelledby="cpu-stress-heading">
<h2 id="cpu-stress-heading">CPU STRESS (STANDARD PROFILE)</h2>
<p>{{.CPUStress.Scope}}</p>
<table><tbody>
<tr><th class="label" scope="row">Outcome</th><td>{{.CPUStress.Outcome}}</td></tr>
<tr><th class="label" scope="row">Stop reason</th><td>{{.CPUStress.StopReason}}</td></tr>
<tr><th class="label" scope="row">Requested load (ms)</th><td>{{.CPUStress.Requested}}</td></tr>
<tr><th class="label" scope="row">Observed load window (ms)</th><td>{{.CPUStress.Observed}}</td></tr>
<tr><th class="label" scope="row">Ready workers</th><td>{{.CPUStress.Ready}}/{{.CPUStress.RequestedWorkers}}</td></tr>
<tr><th class="label" scope="row">Load scalar checks</th><td>{{.CPUStress.LoadChecks}}</td></tr>
<tr><th class="label" scope="row">Cleanup confirmed</th><td>{{.CPUStress.Cleanup}}</td></tr>
</tbody></table>
<table><thead><tr><th>Worker</th><th>Load entry (ms)</th><th>Stop (ms)</th><th>Load iterations</th><th>Load checks</th><th>Stop reason</th></tr></thead><tbody>
{{range .CPUStress.Workers}}<tr><td>{{.Index}}</td><td>{{.Entry}}</td><td>{{.Stop}}</td><td>{{.Iterations}}</td><td>{{.Checks}}</td><td>{{.Reason}}</td></tr>{{end}}
</tbody></table></section>{{end}}{{if .Inspection}}<section class="section" aria-labelledby="exterior-inspection-heading">
<h2 id="exterior-inspection-heading">Operator-reported exterior inspection</h2>
<p>{{.Inspection.Scope}}</p>
<table><tbody>
<tr><th class="label" scope="row">Finding</th><td>{{.Inspection.Finding}}</td></tr>
<tr><th class="label" scope="row">Status</th><td>{{.Inspection.Status}}</td></tr>
<tr><th class="label" scope="row">Response complete</th><td>{{.Inspection.ResponseComplete}}</td></tr>
<tr><th class="label" scope="row">Stop reason</th><td>{{.Inspection.StopReason}}</td></tr>
<tr><th class="label" scope="row">Bounded observation time (ms)</th><td>{{.Inspection.ObservationMilliseconds}}</td></tr>
</tbody></table>
<p>{{.Inspection.TimingNote}}</p>
</section>
{{end}}{{if .Storage}}<section class="section" aria-labelledby="storage-sample-heading">
<h2 id="storage-sample-heading">Storage Sample Observations</h2>
<p>{{.Storage.ScopeNote}}</p>
<p>{{.Storage.Finding}}</p>
<table><tbody>
<tr><th scope="row">Selection scope</th><td>{{.Storage.View.Scope}}</td></tr>
<tr><th scope="row">Status / stop</th><td>{{.Storage.View.Status}} / {{.Storage.View.StopReason}}</td></tr>
<tr><th scope="row">Accepted read bytes</th><td>{{.Storage.View.AcceptedReadBytes}}</td></tr>
<tr><th scope="row">Accepted compared bytes</th><td>{{.Storage.View.AcceptedComparedBytes}}</td></tr>
<tr><th scope="row">Accepted matched ranges</th><td>{{.Storage.View.AcceptedMatchedRanges}}</td></tr>
<tr><th scope="row">Diagnostic read bytes</th><td>{{.Storage.View.DiagnosticReadBytes}}</td></tr>
<tr><th scope="row">Diagnostic compared bytes</th><td>{{.Storage.View.DiagnosticComparedBytes}}</td></tr>
<tr><th scope="row">Cleanup confirmed</th><td>{{.Storage.View.CleanupConfirmed}}</td></tr>
</tbody></table>
<table><thead><tr><th>Device</th><th>Status</th><th>Exclusion</th><th>Accepted</th><th>Observed read bytes</th><th>Observed compared bytes</th><th>Observed matched ranges</th></tr></thead><tbody>
{{range .Storage.View.Devices}}<tr><td>{{.Interface}}</td><td>{{.Status}}</td><td>{{.Exclusion}}</td><td>{{.Accepted}}</td><td>{{.ReadBytes}}</td><td>{{.ComparedBytes}}</td><td>{{.MatchedRanges}}</td></tr>
{{end}}</tbody></table>
</section>
{{end}}{{if .CPUScalar}}<section class="section" aria-labelledby="cpu-scalar-heading">
<h2 id="cpu-scalar-heading">CPU Scalar Calculation Observations</h2>
<p>{{.CPUScalar.Scope}}</p><p>{{.CPUScalar.Finding}}</p>
<table><tbody>
<tr><th class="label" scope="row">Suite</th><td>{{.CPUScalar.Suite}}</td></tr>
<tr><th class="label" scope="row">Accepted scalar checks</th><td>{{.CPUScalar.Checks}}</td></tr>
<tr><th class="label" scope="row">Started workers</th><td>{{.CPUScalar.Started}}</td></tr>
<tr><th class="label" scope="row">Workers completing at least one suite</th><td>{{.CPUScalar.Completed}}</td></tr>
<tr><th class="label" scope="row">Workers with scalar mismatches</th><td>{{.CPUScalar.Mismatched}}</td></tr>
<tr><th class="label" scope="row">Integer mismatch</th><td>{{.CPUScalar.IntegerFailure}}</td></tr>
<tr><th class="label" scope="row">SHA-256 mismatch</th><td>{{.CPUScalar.SHA256Failure}}</td></tr>
<tr><th class="label" scope="row">Scalar mismatch</th><td>{{.CPUScalar.ScalarFailure}}</td></tr>
</tbody></table></section>{{end}}{{if .Memory}}<section class="section" aria-labelledby="memory-heading">
<h2 id="memory-heading">RAM Pattern and Inversion Observations</h2>
<p>{{.Memory.Scope}}</p><p>{{.Memory.Finding}}</p>
<table><tbody>
<tr><th class="label" scope="row">Suite</th><td>{{.Memory.Suite}}</td></tr>
<tr><th class="label" scope="row">Allocated bytes</th><td>{{.Memory.Allocated}}</td></tr>
<tr><th class="label" scope="row">Completed/requested passes</th><td>{{.Memory.CompletedPasses}}/{{.Memory.Passes}}</td></tr>
<tr><th class="label" scope="row">Completed phases</th><td>{{.Memory.CompletedPhases}}</td></tr>
<tr><th class="label" scope="row">Phase / direction</th><td>{{.Memory.Phase}} / {{.Memory.Direction}}</td></tr>
<tr><th class="label" scope="row">Accepted matching read bytes</th><td>{{.Memory.AcceptedReads}}</td></tr>
<tr><th class="label" scope="row">Accepted write bytes</th><td>{{.Memory.AcceptedWrites}}</td></tr>
<tr><th class="label" scope="row">Observed matching read bytes</th><td>{{.Memory.ObservedMatches}}</td></tr>
<tr><th class="label" scope="row">Attempted read bytes (including mismatch)</th><td>{{.Memory.AttemptedReads}}</td></tr>
<tr><th class="label" scope="row">Observed write bytes</th><td>{{.Memory.ObservedWrites}}</td></tr>
<tr><th class="label" scope="row">Uncredited terminal units</th><td>{{.Memory.Uncredited}}</td></tr>
</tbody></table></section>{{end}}{{if .Thermal}}<section class="section" aria-labelledby="thermal-heading">
<h2 id="thermal-heading">Session Temperature Observations</h2>
<table><tbody>
<tr><th class="label" scope="row">Scope</th><td>{{.Thermal.Scope}}</td></tr>
<tr><th class="label" scope="row">Finding</th><td>{{.Thermal.Finding}}</td></tr>
<tr><th class="label" scope="row">Sensor coverage</th><td>{{.Thermal.Coverage}}</td></tr>
<tr><th class="label" scope="row">Global observed peak</th><td>{{.Thermal.Peak}}</td></tr>
<tr><th class="label" scope="row">Stop limit</th><td>{{.Thermal.Limit}}</td></tr>
<tr><th class="label" scope="row">Accepted global observations</th><td>{{.Thermal.Observations}}</td></tr>
<tr><th class="label" scope="row">Maximum sampling gap</th><td>{{.Thermal.MaximumGapMilliseconds}} ms</td></tr>
<tr><th class="label" scope="row">Monitor cleanup</th><td>{{.Thermal.Cleanup}}</td></tr>
<tr><th class="label" scope="row">Session stop reason</th><td>{{.Thermal.StopReason}}</td></tr>
</tbody></table>
<p>Sampling is not continuous coverage. Sensor availability does not establish hardware health.</p>
{{if .Thermal.LocalSources}}<h3>Local CPU/RAM Temperature Findings</h3>
<p>Local peaks do not change global sensor coverage or observation counts.</p>
<table><thead><tr><th>Worker</th><th>Local observed peak</th><th>Stop limit</th><th>Execution interval</th></tr></thead><tbody>
{{range .Thermal.LocalSources}}<tr><td>{{.Worker}}</td><td>{{.Peak}}</td><td>{{.Limit}}</td><td>{{.Interval}}</td></tr>
{{end}}</tbody></table>
{{end}}</section>
{{end}}{{if .Keyboard}}<section class="section" aria-labelledby="keyboard-heading">
<h2 id="keyboard-heading">Guided Keyboard Observations</h2>
<p>{{.Keyboard.Scope}}</p>
<table><tbody>
<tr><th class="label" scope="row">Status</th><td>{{.Keyboard.Status}}</td></tr>
<tr><th class="label" scope="row">Finding</th><td>{{.Keyboard.Finding}}</td></tr>
<tr><th class="label" scope="row">Stop reason</th><td>{{.Keyboard.StopReason}}</td></tr>
</tbody></table>
{{if .Keyboard.Devices}}<table><thead><tr><th>Interface</th><th>Status</th><th>Completed controls</th><th>Missing controls</th></tr></thead><tbody>
{{range .Keyboard.Devices}}<tr><td>{{.Interface}}</td><td>{{.Status}}</td><td>{{.Completed}}</td><td>{{.Missing}}</td></tr>
{{end}}</tbody></table>
{{else}}<p>No keyboard interface observations were obtained.</p>
{{end}}</section>
{{end}}{{if .Touchpad}}<section class="section" aria-labelledby="touchpad-heading">
<h2 id="touchpad-heading">Guided Single-Contact Touchpad Observations</h2>
<p>{{.Touchpad.Scope}}</p>
<table><tbody>
<tr><th class="label" scope="row">Status</th><td>{{.Touchpad.Status}}</td></tr>
<tr><th class="label" scope="row">Finding</th><td>{{.Touchpad.Finding}}</td></tr>
<tr><th class="label" scope="row">Stop reason</th><td>{{.Touchpad.StopReason}}</td></tr>
<tr><th class="label" scope="row">Duration / limit (ms)</th><td>{{.Touchpad.DurationMilliseconds}} / {{.Touchpad.LimitMilliseconds}}</td></tr>
<tr><th class="label" scope="row">Cleanup confirmed</th><td>{{.Touchpad.CleanupConfirmed}}</td></tr>
</tbody></table>
{{range .Touchpad.Devices}}<h3>Interface {{.Interface}}</h3>
<p>Status: {{.Status}}. {{.Finding}}</p>
<table><tbody>
<tr><th class="label" scope="row">Readable / supported</th><td>{{.Readable}} / {{.Supported}}</td></tr>
<tr><th class="label" scope="row">Numeric identity</th><td>{{.Identity}}</td></tr>
<tr><th class="label" scope="row">Advertised codes</th><td>{{.Capabilities}}</td></tr>
<tr><th class="label" scope="row">Configuration</th><td>{{.Configuration}}</td></tr>
<tr><th class="label" scope="row">Frames</th><td>{{.Frames}}</td></tr>
<tr><th class="label" scope="row">Contact starts / releases</th><td>{{.ContactStarts}} / {{.ContactReleases}}</td></tr>
<tr><th class="label" scope="row">Fresh XY baselines / qualifying contacts</th><td>{{.XYBaselines}} / {{.QualifyingContacts}}</td></tr>
<tr><th class="label" scope="row">X positive / negative</th><td>{{.XPositive}} / {{.XNegative}}</td></tr>
<tr><th class="label" scope="row">Y positive / negative</th><td>{{.YPositive}} / {{.YNegative}}</td></tr>
<tr><th class="label" scope="row">Baseline / final neutral / cleanup</th><td>{{.BaselineNeutral}} / {{.FinalNeutral}} / {{.CleanupConfirmed}}</td></tr>
</tbody></table>
{{if .Buttons}}<table><thead><tr><th>Button</th><th>Presses</th><th>Releases</th><th>Pairs</th></tr></thead><tbody>
{{range .Buttons}}<tr><td>{{.Name}}</td><td>{{.Presses}}</td><td>{{.Releases}}</td><td>{{.Pairs}}</td></tr>
{{end}}</tbody></table>{{end}}
{{else}}<p>No touchpad interface observations were obtained.</p>
{{end}}</section>
{{end}}{{if .Touchscreen}}<section class="section" aria-labelledby="touchscreen-heading">
<h2 id="touchscreen-heading">Guided Single-Contact Touchscreen Observations</h2>
<p>{{.Touchscreen.Scope}}</p>
<table><tbody>
<tr><th class="label" scope="row">Status</th><td>{{.Touchscreen.Status}}</td></tr>
<tr><th class="label" scope="row">Finding</th><td>{{.Touchscreen.Finding}}</td></tr>
<tr><th class="label" scope="row">Stop reason</th><td>{{.Touchscreen.StopReason}}</td></tr>
<tr><th class="label" scope="row">Duration / limit (ms)</th><td>{{.Touchscreen.DurationMilliseconds}} / {{.Touchscreen.LimitMilliseconds}}</td></tr>
<tr><th class="label" scope="row">Cleanup confirmed</th><td>{{.Touchscreen.CleanupConfirmed}}</td></tr>
</tbody></table>
{{range .Touchscreen.Devices}}<h3>Interface {{.Interface}}</h3>
<p>Status: {{.Status}}. {{.Finding}}</p>
<table><tbody>
<tr><th class="label" scope="row">Readable / supported</th><td>{{.Readable}} / {{.Supported}}</td></tr>
<tr><th class="label" scope="row">Numeric identity</th><td>{{.Identity}}</td></tr>
<tr><th class="label" scope="row">Advertised codes</th><td>{{.Capabilities}}</td></tr>
<tr><th class="label" scope="row">Configuration</th><td>{{.Configuration}}</td></tr>
<tr><th class="label" scope="row">Frames</th><td>{{.Frames}}</td></tr>
<tr><th class="label" scope="row">Contact starts / releases</th><td>{{.ContactStarts}} / {{.ContactReleases}}</td></tr>
<tr><th class="label" scope="row">Fresh XY baselines / qualifying contacts</th><td>{{.XYBaselines}} / {{.QualifyingContacts}}</td></tr>
<tr><th class="label" scope="row">X positive / negative</th><td>{{.XPositive}} / {{.XNegative}}</td></tr>
<tr><th class="label" scope="row">Y positive / negative</th><td>{{.YPositive}} / {{.YNegative}}</td></tr>
<tr><th class="label" scope="row">Baseline / final neutral / cleanup</th><td>{{.BaselineNeutral}} / {{.FinalNeutral}} / {{.CleanupConfirmed}}</td></tr>
</tbody></table>
{{else}}<p>No touchscreen interface observations were obtained.</p>
{{end}}</section>
{{end}}{{if .Pointer}}<section class="section" aria-labelledby="pointer-heading">
<h2 id="pointer-heading">Guided Pointer Wheel Observations</h2>
<p>{{.Pointer.Scope}}</p>
<table><tbody>
<tr><th class="label" scope="row">Status</th><td>{{.Pointer.Status}}</td></tr>
<tr><th class="label" scope="row">Finding</th><td>{{.Pointer.Finding}}</td></tr>
<tr><th class="label" scope="row">Stop reason</th><td>{{.Pointer.StopReason}}</td></tr>
</tbody></table>
{{range .Pointer.Devices}}<h3>Interface {{.Interface}}</h3>
<p>Status: {{.Status}}. {{.WheelCoverage}}</p>
{{if .Wheels}}<table><thead><tr><th>Wheel axis</th><th>Credited stream</th><th>Positive frames</th><th>Negative frames</th></tr></thead><tbody>
{{range .Wheels}}<tr><td>{{.Axis}}</td><td>{{.Stream}}</td><td>{{.Positive}}</td><td>{{.Negative}}</td></tr>
{{end}}</tbody></table>{{end}}
{{else}}<p>No pointer interface observations were obtained.</p>
{{end}}</section>
{{end}}<section class="section" aria-labelledby="tests-heading">
<h2 id="tests-heading">Functional Test Evidence</h2>
{{if .TestResults}}
{{range .TestResults}}
<article class="test">
<h3>{{display .ID}} Â· {{.Status}}</h3>
<table><tbody>
<tr><th class="label" scope="row">{{if .PlannedStatus}}Definition version{{else}}Adapter version{{end}}</th><td>{{.Version}}</td></tr>
<tr><th class="label" scope="row">Status</th><td>{{.Status}}</td></tr>{{if .PlannedStatus}}
<tr><th class="label" scope="row">Planned status</th><td>{{.PlannedStatus}}</td></tr>
<tr><th class="label" scope="row">Disposition</th><td>{{.PlannedDescription}}</td></tr>{{end}}
{{if .PlanID}}<tr><th class="label" scope="row">Frozen plan</th><td class="fingerprint">{{.PlanID}}</td></tr>{{end}}
{{if .HardwareGraph}}<tr><th class="label" scope="row">Planned hardware graph</th><td class="fingerprint">{{.HardwareGraph}}</td></tr>{{end}}
</tbody></table>
<pre>{{.Evidence}}</pre>
</article>
{{end}}
{{else}}<p class="notice">No functional test evidence is included.</p>{{end}}
</section>
<section class="section" aria-labelledby="components-heading">
<h2 id="components-heading">Components</h2>
{{if .Components}}
{{range $index, $component := .Components}}
<article class="component">
<h3>{{$component.Type}} · {{display $component.ID}}</h3>
<table>
<tbody>
<tr><th class="label" scope="row">Fingerprint</th><td class="fingerprint">{{display $component.Identity.Value}}</td></tr>
<tr><th class="label" scope="row">Algorithm</th><td>{{display $component.Identity.Algorithm}}</td></tr>
<tr><th class="label" scope="row">Key epoch</th><td>{{display $component.Identity.KeyEpoch}}</td></tr>
<tr><th class="label" scope="row">Identity source</th><td>{{display $component.Identity.Source}}</td></tr>
<tr><th class="label" scope="row">Confidence</th><td>{{display $component.Identity.Confidence}}</td></tr>
</tbody>
</table>
<table class="attributes">
<thead><tr><th scope="col">Attribute</th><th scope="col">Value</th></tr></thead>
<tbody>
{{if $component.Attributes}}{{range $component.Attributes}}<tr><th scope="row">{{.Name}}</th><td>{{.Value}}</td></tr>{{end}}{{else}}<tr><td colspan="2">No public attributes reported.</td></tr>{{end}}
</tbody>
</table>
</article>
{{end}}
{{else}}<p class="notice">No components reported.</p>{{end}}
</section>
</main>
</body>
</html>
`))

// InventoryHTML renders a payload as a deterministic, standalone HTML5 document.
func InventoryHTML(payload app.InventoryPayload) ([]byte, error) {
	return InventoryHTMLForVersion(payload, product.Version)
}

// InventoryHTMLForVersion reproduces a view using its signed software version,
// rather than silently substituting the verifier binary's version.
func InventoryHTMLForVersion(payload app.InventoryPayload, version string) ([]byte, error) {
	var output bytes.Buffer
	if err := renderInventoryHTML(&output, payload, version); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

// RenderInventoryHTML writes a deterministic, standalone HTML5 document.
func RenderInventoryHTML(output io.Writer, payload app.InventoryPayload) error {
	return renderInventoryHTML(output, payload, product.Version)
}

func renderInventoryHTML(output io.Writer, payload app.InventoryPayload, version string) error {
	return renderInventoryHTMLWithPrevious(output, payload, version, nil)
}

func renderInventoryHTMLWithPrevious(output io.Writer, payload app.InventoryPayload, version string, previous *report.PreviousReport) error {
	if previous != nil && previous.Validate() != nil {
		return errors.New("invalid previous-report view reference")
	}
	if err := validateVersion(version); err != nil {
		return err
	}
	memoryStress, err := memoryStressReportDocument(payload)
	if err != nil {
		return err
	}
	stress, err := cpuStressReportDocument(payload)
	if err != nil {
		return err
	}
	inspection, err := inspectionReportDocument(payload)
	if err != nil {
		return err
	}
	thermal, err := sessionThermalDocument(payload)
	if err != nil {
		return err
	}
	keyboard, err := keyboardReportDocument(payload)
	if err != nil {
		return err
	}
	touchpad, err := touchpadReportDocument(payload)
	if err != nil {
		return err
	}
	touchscreen, err := touchscreenReportDocument(payload)
	if err != nil {
		return err
	}
	pointer, err := pointerReportDocument(payload)
	if err != nil {
		return err
	}
	cpuScalar, err := cpuScalarReportDocument(payload)
	if err != nil {
		return err
	}
	memory, err := memoryReportDocument(payload)
	if err != nil {
		return err
	}
	storage, err := storageReportDocument(payload)
	if err != nil {
		return err
	}
	components := append([]app.ComponentRecord(nil), payload.Components...)
	for i := range components {
		components[i].Attributes = append([]inventory.Attribute(nil), components[i].Attributes...)
	}
	sort.SliceStable(components, func(left, right int) bool {
		if components[left].Type != components[right].Type {
			return components[left].Type < components[right].Type
		}
		return components[left].ID < components[right].ID
	})
	testResults := make([]testDocument, 0, len(payload.TestResults))
	evidenceBudget := maximumDocumentBytes
	for _, result := range payload.TestResults {
		evidence, err := indentEvidence(result.Evidence, &evidenceBudget)
		if err != nil {
			return err
		}
		testResults = append(testResults, testDocument{ID: result.ID, Version: result.Version, Status: result.Status, PlanID: result.PlanID, HardwareGraph: result.HardwareGraph, Evidence: evidence, PlannedStatus: result.PlannedStatus, PlannedDescription: plannedStatusDescription(result.PlannedStatus)})
	}
	sort.SliceStable(testResults, func(left, right int) bool { return testResults[left].ID < testResults[right].ID })
	document := inventoryDocument{
		PreviousReport:      previous,
		ProductName:         product.Name,
		ProductVersion:      version,
		SchemaVersion:       payload.SchemaVersion,
		TotalComponents:     len(components),
		IdentifiedParts:     payload.SystemFingerprint.IdentifiedParts,
		UnidentifiedParts:   payload.SystemFingerprint.UnidentifiedParts,
		SystemFingerprint:   payload.SystemFingerprint,
		Components:          components,
		TestResults:         testResults,
		CoverageDescription: coverageDescription(payload.CoverageScope),
		Thermal:             thermal,
		Keyboard:            keyboard,
		Pointer:             pointer,
		Touchpad:            touchpad,
		Touchscreen:         touchscreen,
		CPUScalar:           cpuScalar,
		Memory:              memory,
		Storage:             storage,
		CPUStress:           stress,
		MemoryStress:        memoryStress,
		Inspection:          inspection,
	}
	return inventoryTemplate.Execute(&limitedWriter{destination: output}, document)
}

// json.Indent writes to bytes.Buffer, so an output writer limit cannot protect
// this intermediate allocation. Preflight a conservative size bound, counting
// even the newlines omitted for empty containers, before calling it. Evidence
// from implemented workers is shallow; deeper inputs are unsupported.
func indentEvidence(encoded []byte, remaining *int) (string, error) {
	if len(encoded) == 0 || len(encoded) > 1<<20 || !json.Valid(encoded) {
		return "", errors.New("report evidence is outside the supported bounds")
	}
	depth, projected := 0, len(encoded)
	inString, escaped := false, false
	for _, character := range encoded {
		if inString {
			if escaped {
				escaped = false
			} else if character == '\\' {
				escaped = true
			} else if character == '"' {
				inString = false
			}
			continue
		}
		switch character {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > 64 {
				return "", errors.New("report evidence nesting exceeds the supported limit")
			}
			projected += 1 + 2*depth
		case '}', ']':
			depth--
			projected += 1 + 2*depth
		case ',':
			projected += 1 + 2*depth
		case ':':
			projected++
		}
		if projected > *remaining {
			return "", errors.New("indented report evidence exceeds the supported size")
		}
	}
	if projected > *remaining {
		return "", errors.New("indented report evidence exceeds the supported size")
	}
	var output bytes.Buffer
	if err := json.Indent(&output, encoded, "", "  "); err != nil {
		return "", errors.New("report evidence is invalid")
	}
	*remaining -= projected
	return output.String(), nil
}
