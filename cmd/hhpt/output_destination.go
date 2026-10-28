package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// A command succeeds only after its owned destination has been closed. This
// does not promise a filesystem sync or durable storage after power loss.
type destinationWriter func(string, io.Writer, func(io.Writer) error) error
type destinationOpener func(string, int, os.FileMode) (io.WriteCloser, error)

func writeDestination(path string, stdout io.Writer, write func(io.Writer) error) error {
	return writeDestinationWith(path, stdout, write, openOutputFile)
}

func openOutputFile(path string, flags int, mode os.FileMode) (io.WriteCloser, error) {
	file, err := os.OpenFile(path, flags, mode)
	if err != nil {
		return nil, err
	}
	return file, nil
}

func writeDestinationWith(path string, stdout io.Writer, write func(io.Writer) error, open destinationOpener) (result error) {
	if path == "-" {
		// stdout belongs to the caller, even when it also implements io.Closer.
		return write(completeOutputWriter{stdout})
	}
	destination, err := open(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if destination != nil {
		defer func() {
			if err := destination.Close(); err != nil {
				result = errors.Join(result, fmt.Errorf("close output: %w", err))
			}
		}()
	}
	if err != nil {
		return fmt.Errorf("open output: %w", err)
	}
	if destination == nil {
		return errors.New("open output: no destination returned")
	}
	return write(completeOutputWriter{destination})
}

// Some encoders return only the writer's error. Convert a broken writer's
// short nil-error return into an error before that byte count is discarded.
type completeOutputWriter struct {
	destination io.Writer
}

func (writer completeOutputWriter) Write(contents []byte) (int, error) {
	written, err := writer.destination.Write(contents)
	if err == nil && written != len(contents) {
		err = io.ErrShortWrite
	}
	return written, err
}
