package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
)

const maximumMenuLineBytes = 4096

var errMenuLineTooLong = errors.New("menu selection exceeds the input limit")

type uiSelectionFile interface {
	io.Writer
	io.Closer
}

type uiSelectionCreator func(string, int, os.FileMode) (uiSelectionFile, error)

func createUISelectionFile(path string, flags int, mode os.FileMode) (uiSelectionFile, error) {
	return os.OpenFile(path, flags, mode)
}

type quickMenuSelection struct {
	launchSelection bool
	create          uiSelectionCreator
	outputPath      string
	architecture    string
	tests           []string
	excluded        map[string]bool
}

func newQuickMenuSelection(path, architecture string) (*quickMenuSelection, error) {
	if strings.TrimSpace(path) == "" || path == "-" {
		return nil, errors.New("selection output must be a new file")
	}
	tests, err := plan.SelectableQuickTests(architecture)
	if err != nil {
		return nil, err
	}
	return &quickMenuSelection{
		outputPath: path, architecture: architecture, tests: tests, create: createUISelectionFile,
		excluded: make(map[string]bool),
	}, nil
}

func (selection *quickMenuSelection) summary(output io.Writer) {
	excluded := 0
	for _, id := range selection.tests {
		if selection.excluded[id] {
			excluded++
		}
	}
	fmt.Fprintf(output, "Quick tests selected: %d of %d.\n", len(selection.tests)-excluded, len(selection.tests))
	if excluded == len(selection.tests) {
		fmt.Fprintln(output, "No quick tests selected; inventory and safety checks still run.")
	}
	if selection.architecture == "legacy-x86" {
		fmt.Fprintln(output, "Graphics and display tests are unsupported in legacy mode.")
	}
}

func (selection *quickMenuSelection) configure(ctx context.Context, reader *bufio.Reader, output, stderr io.Writer) error {
	for {
		fmt.Fprintln(output, "\nChoose safe quick tests. Unavailable hardware and drivers remain explicit in the report.")
		for index, id := range selection.tests {
			mark := "x"
			if selection.excluded[id] {
				mark = " "
			}
			fmt.Fprintf(output, "[%d] [%s] %s\n", index+1, mark, quickTestLabel(id))
		}
		selection.summary(output)
		fmt.Fprintln(output, "Enter a number to toggle a test, A for all defaults, or Enter to return to the menu.")
		fmt.Fprint(output, "Selection: ")
		line, err := readMenuLine(ctx, reader)
		if errors.Is(err, errMenuLineTooLong) {
			fmt.Fprintln(stderr, "Selection too long; choices unchanged and no test started.")
			continue
		}
		if err != nil {
			return err
		}
		value := strings.ToLower(strings.TrimSpace(line))
		switch value {
		case "":
			return nil
		case "a":
			clear(selection.excluded)
			continue
		}
		number, err := strconv.Atoi(value)
		if err != nil || number < 1 || number > len(selection.tests) {
			fmt.Fprintln(stderr, "Unknown selection; choices unchanged and no test started.")
			continue
		}
		id := selection.tests[number-1]
		selection.excluded[id] = !selection.excluded[id]
	}
}

func (selection *quickMenuSelection) hasExclusions() bool {
	for _, excluded := range selection.excluded {
		if excluded {
			return true
		}
	}
	return false
}

func (selection *quickMenuSelection) write(ctx context.Context, profile plan.Profile) (failure error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if selection == nil {
		return nil
	}
	excluded := make([]string, 0, len(selection.tests))
	for _, id := range selection.tests {
		if selection.excluded[id] {
			excluded = append(excluded, id)
		}
	}
	var encoded []byte
	var err error
	if selection.launchSelection {
		encoded, err = plan.EncodeLaunchSelection(profile, selection.architecture, excluded)
	} else {
		if profile != plan.ProfileQuick {
			if profile == plan.ProfileExtended {
				return errors.New("extended profile requires a launch selection")
			}
			return errors.New("standard profile requires a launch selection")
		}
		payload := struct {
			ExcludedTests []string `json:"excluded_tests"`
		}{excluded}
		encoded, err = json.Marshal(payload)
		if err == nil {
			_, err = plan.DecodeSelection(encoded, selection.architecture)
		}
		encoded = append(encoded, '\n')
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := selection.create(selection.outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if nilRetestInputValue(file) {
		if err != nil {
			return err
		}
		return errors.New("selection output owner unavailable")
	}
	// Every acquired owner, including one returned alongside an error, closes
	// once. Retain created bytes on every failure; publication is never retried.
	defer func() {
		closeErr := file.Close()
		cancelErr := ctx.Err()
		if failure == nil {
			failure = closeErr
		}
		if failure == nil {
			failure = cancelErr
		}
	}()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	n, writeErr := file.Write(encoded)
	cancelErr := ctx.Err()
	if writeErr != nil {
		return writeErr
	}
	if n != len(encoded) {
		return io.ErrShortWrite
	}
	return cancelErr
}

func readMenuLine(ctx context.Context, reader *bufio.Reader) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	line, err := reader.ReadSlice('\n')
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if errors.Is(err, bufio.ErrBufferFull) {
		for errors.Is(err, bufio.ErrBufferFull) {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			_, err = reader.ReadSlice('\n')
		}
		if err != nil {
			return "", err
		}
		return "", errMenuLineTooLong
	}
	if err != nil {
		return "", err
	}
	if len(line) > maximumMenuLineBytes {
		return "", errMenuLineTooLong
	}
	return string(line), nil
}

func quickTestLabel(id string) string {
	labels := map[string]string{
		"audio.readiness.quick":     "Audio device readiness",
		"cpu.quick":                 "CPU computation",
		"display.operator.quick":    "Display inspection",
		"inspection.exterior.quick": "Visible exterior enclosure observation",
		"graphics.quick":            "Graphics pattern",
		"input.readiness.quick":     "Input device readiness",
		"input.keyboard.quick":      "Keyboard shifts and arrow keys",
		"input.pointer.quick":       "Pointer movement, buttons and wheels",
		"input.touchpad.quick":      "Touchpad movement and buttons",
		"input.touchscreen.quick":   "Touchscreen movement and release",
		"memory.quick":              "Memory patterns",
		"network.readiness.quick":   "Network device readiness",
		"storage.readiness.quick":   "Storage device readiness",
		"usb.readiness.quick":       "USB device readiness",
	}
	if label, ok := labels[id]; ok {
		return label
	}
	return id
}
