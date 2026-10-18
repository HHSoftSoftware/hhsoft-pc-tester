package render

import (
	"bytes"
	"errors"
	"io"
)

const maximumDocumentBytes = 32 << 20

// Keep metadata supplied by a signed envelope inert and bounded. Product names
// remain fixed by this renderer; a version is a label, not HTML or PDF syntax.
func validateVersion(version string) error {
	if len(version) == 0 || len(version) > 128 {
		return errors.New("report version is outside the supported bounds")
	}
	for _, character := range version {
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'z') &&
			!(character >= 'A' && character <= 'Z') && character != '.' && character != '-' && character != '+' {
			return errors.New("report version contains unsupported characters")
		}
	}
	return nil
}

type limitedWriter struct {
	destination io.Writer
	written     int
}

func (writer *limitedWriter) Write(value []byte) (int, error) {
	if len(value) > maximumDocumentBytes-writer.written {
		return 0, errors.New("rendered report exceeds the supported size")
	}
	count, err := writer.destination.Write(value)
	writer.written += count
	if err == nil && count != len(value) {
		err = io.ErrShortWrite
	}
	return count, err
}

type limitedBuffer struct {
	bytes.Buffer
	err error
}

func (buffer *limitedBuffer) Write(value []byte) (int, error) {
	if buffer.err != nil {
		return 0, buffer.err
	}
	if len(value) > maximumDocumentBytes-buffer.Len() {
		buffer.err = errors.New("rendered report exceeds the supported size")
		return 0, buffer.err
	}
	return buffer.Buffer.Write(value)
}

func (buffer *limitedBuffer) WriteString(value string) (int, error) {
	return buffer.Write([]byte(value))
}
