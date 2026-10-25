package workerexec

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// Own the output pipes rather than letting exec.Cmd.Wait wait for inherited
// descriptors. A worker's exit does not prove its descendants closed them.
type outputCapture struct {
	stdout, stderr          *boundedBuffer
	stdoutRead, stdoutWrite *os.File
	stderrRead, stderrWrite *os.File
	done                    chan error
}

func newOutputCapture(stdoutLimit, stderrLimit int) (*outputCapture, error) {
	capture := &outputCapture{
		stdout: newBoundedBuffer(stdoutLimit), stderr: newBoundedBuffer(stderrLimit),
		done: make(chan error, 2),
	}
	var err error
	capture.stdoutRead, capture.stdoutWrite, err = os.Pipe()
	if err == nil {
		capture.stderrRead, capture.stderrWrite, err = os.Pipe()
	}
	if err != nil {
		capture.close()
		return nil, err
	}
	return capture, nil
}

func (capture *outputCapture) start() {
	_ = capture.stdoutWrite.Close()
	_ = capture.stderrWrite.Close()
	for _, pair := range []struct {
		input  *os.File
		output *boundedBuffer
	}{{capture.stdoutRead, capture.stdout}, {capture.stderrRead, capture.stderr}} {
		go func(input *os.File, output *boundedBuffer) {
			_, err := io.Copy(output, input)
			_ = input.Close()
			capture.done <- err
		}(pair.input, pair.output)
	}
}

func (capture *outputCapture) finish(deadline time.Time) error {
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	var failure error
	for completed := 0; completed < 2; completed++ {
		var err error
		select {
		case err = <-capture.done:
		default:
			select {
			case err = <-capture.done:
			case <-timer.C:
				capture.close()
				return errors.Join(ErrCleanupUncertain, failure, errors.New("workerexec: output drain deadline exceeded"))
			}
		}
		if err != nil {
			failure = errors.Join(failure, fmt.Errorf("workerexec: read output pipe: %w", err))
		}
	}
	if failure != nil {
		return errors.Join(ErrCleanupUncertain, failure)
	}
	return nil
}

func (capture *outputCapture) close() {
	closePipe(capture.stdoutRead, capture.stdoutWrite)
	closePipe(capture.stderrRead, capture.stderrWrite)
}

func (capture *outputCapture) result(exitCode int) Result {
	stdout, stdoutTruncated := capture.stdout.snapshot()
	stderr, stderrTruncated := capture.stderr.snapshot()
	return Result{Stdout: stdout, Stderr: stderr, ExitCode: exitCode,
		StdoutTruncated: stdoutTruncated, StderrTruncated: stderrTruncated}
}
