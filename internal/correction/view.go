package correction

import (
	"bytes"
	"html/template"
	"strconv"
)

const comparisonTemplate = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Operator display correction</title></head>
<body><h1>Operator display correction</h1>
<p>These are signed operator assertions, not new hardware observations or test results.
The original report, identifiers, fingerprints and test evidence are unchanged.</p>
<dl><dt>Original session</dt><dd>{{.Session}}</dd><dt>Original bundle</dt><dd>{{.Bundle}}</dd>
<dt>Original manifest</dt><dd>{{.Manifest}}</dd><dt>Source trust</dt><dd>{{.SourceTrust}}</dd>
<dt>Addendum ID</dt><dd>{{.ID}}</dd><dt>Addendum SHA-256</dt><dd>{{.Digest}}</dd>
<dt>Addendum signer label</dt><dd>{{.KeyID}}</dd><dt>Addendum trust</dt><dd>{{.Trust}}</dd>
<dt>System component</dt><dd>{{.Component}}</dd></dl>
<table><thead><tr><th>Public field</th><th>Original observation</th><th>Operator value</th></tr></thead>
<tbody>{{range .Changes}}<tr><td>{{.Field}}</td><td><code>{{.Original}}</code></td><td><code>{{.Operator}}</code></td></tr>{{end}}</tbody></table>
<p>Values are quoted text; escapes keep control and non-ASCII characters visible.
An absent original value is marked as not observed. This addendum does not establish
chronology, supersession, revocation, complete correction history or chain of custody.</p>
</body></html>
`

type comparisonRow struct{ Field, Original, Operator string }
type comparisonView struct {
	Session, Bundle, Manifest, SourceTrust, ID, Digest, KeyID, Trust, Component string
	Changes                                                                     []comparisonRow
}

// RenderHTML produces a separate comparison, never a replacement signed view
// inside the original bundle. Both records must already be verified together.
func RenderHTML(verified Verified) ([]byte, error) {
	if !verified.verified {
		return nil, ErrInvalid
	}
	view := comparisonView{
		Session: strconv.QuoteToASCII(verified.source.reference.SessionID),
		Bundle:  verified.source.reference.BundleSHA256, Manifest: verified.source.reference.ManifestDigest,
		SourceTrust: verified.source.trust, ID: verified.record.ID, Digest: verified.digest,
		KeyID: verified.record.KeyID, Trust: verified.trust, Component: strconv.QuoteToASCII(verified.source.componentID),
	}
	for _, value := range verified.record.Changes {
		prior := verified.source.original[fieldIndex(value.Field)]
		original := "(not observed)"
		if prior.present {
			original = strconv.QuoteToASCII(prior.value)
		}
		view.Changes = append(view.Changes, comparisonRow{value.Field, original, strconv.QuoteToASCII(value.Value)})
	}
	parsed, err := template.New("correction").Parse(comparisonTemplate)
	if err != nil {
		return nil, ErrInvalid
	}
	var output limitedView
	if parsed.Execute(&output, view) != nil {
		return nil, ErrInvalid
	}
	return output.buffer.Bytes(), nil
}

type limitedView struct{ buffer bytes.Buffer }

func (writer *limitedView) Write(raw []byte) (int, error) {
	if len(raw) > MaximumViewBytes-writer.buffer.Len() {
		return 0, ErrInvalid
	}
	return writer.buffer.Write(raw)
}

func VerificationText(verified Verified) ([]byte, error) {
	if !verified.verified {
		return nil, ErrInvalid
	}
	return []byte("Correction verified against the exact original report.\nSource trust: " + verified.source.trust +
		"\nAddendum trust: " + verified.trust + "\nAddendum: " + verified.digest +
		"\nOperator assertions only; original evidence unchanged. No custody or supersession claim.\n"), nil
}
