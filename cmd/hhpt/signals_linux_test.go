//go:build linux

package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

func TestTerminationSignalHelper(t *testing.T) {
	if os.Getenv("HHPT_TEST_TERMINATION_HELPER") != "1" {
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), terminationSignals()...)
	defer stop()
	fmt.Println("ready")
	<-ctx.Done()
	fmt.Println("cleanup-context-cancelled")
}

func TestTerminationSignalsCancelOwnedChildContext(t *testing.T) {
	for _, termination := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(termination.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTerminationSignalHelper$")
			command.Env = []string{"HHPT_TEST_TERMINATION_HELPER=1"}
			output, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { cancel(); _ = command.Wait() }()
			reader := bufio.NewReader(output)
			if line, err := reader.ReadString('\n'); err != nil || line != "ready\n" {
				t.Fatal("owned helper did not become ready")
			}
			if err := command.Process.Signal(termination); err != nil {
				t.Fatal(err)
			}
			if line, err := reader.ReadString('\n'); err != nil || line != "cleanup-context-cancelled\n" {
				t.Fatal("termination signal did not request cooperative cleanup")
			}
			if err := command.Wait(); err != nil {
				t.Fatal("owned helper did not exit successfully")
			}
		})
	}
}
