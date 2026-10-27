package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/plan"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/sessionevidence"
)

type sessionFinalOutput struct {
	destination io.Writer
	data        []byte
	truncated   bool
	writeErr    error
}

func (output *sessionFinalOutput) Write(data []byte) (int, error) {
	remaining := 1024 - len(output.data)
	output.data = append(output.data, data[:min(len(data), remaining)]...)
	if len(data) > remaining {
		output.truncated = true
	}
	n, err := output.destination.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	output.writeErr = errors.Join(output.writeErr, err)
	return n, err
}

func writeSessionReady(path, reportPath, keyEpoch string, frozen plan.Plan, journal sessionJournal, finalized sessionIntegrity, output *sessionFinalOutput) error {
	if output.truncated || output.writeErr != nil {
		return errors.New("finalizer output was incomplete")
	}
	var integrity []string
	for _, line := range strings.Split(string(output.data), "\n") {
		if strings.HasPrefix(line, "HHPT_RUN_INTEGRITY") {
			if integrity != nil {
				return errors.New("duplicate finalizer integrity observation")
			}
			integrity = strings.Fields(line)
		}
	}
	if len(integrity) != 8 || integrity[0] != "HHPT_RUN_INTEGRITY" || integrity[2] != "plan="+frozen.ID {
		return errors.New("invalid finalizer integrity observation")
	}
	status := strings.TrimPrefix(integrity[1], "status=")
	if integrity[1] != "status="+status || status != "pass" && status != "fail" {
		return errors.New("invalid finalizer integrity status")
	}
	var counts [5]uint64
	for index, name := range []string{"planned_components", "final_components", "added", "removed", "changed"} {
		value, found := strings.CutPrefix(integrity[index+3], name+"=")
		count, err := strconv.ParseUint(value, 10, 64)
		if !found || err != nil || strconv.FormatUint(count, 10) != value {
			return errors.New("invalid finalizer component count")
		}
		counts[index] = count
	}
	if finalized.PlanID != frozen.ID || finalized.Status != status {
		return errors.New("finalizer integrity disagrees with its typed result")
	}
	for index, count := range []uint64{finalized.PlannedComponents, finalized.FinalComponents, finalized.Added, finalized.Removed, finalized.Changed} {
		if count != counts[index] {
			return errors.New("finalizer component counts disagree with their typed result")
		}
	}
	if counts[0] != uint64(len(frozen.HardwareGraph.Components)) ||
		status == "pass" && (counts[0] != counts[1] || counts[2] != 0 || counts[3] != 0 || counts[4] != 0) ||
		status == "fail" && counts[2] == 0 && counts[3] == 0 && counts[4] == 0 {
		return errors.New("inconsistent finalizer integrity observation")
	}
	outcomes := make(map[string]string, len(journal.Workers))
	for _, worker := range journal.Workers {
		outcomes[worker.ID] = worker.Outcome
	}
	marker := fmt.Sprintf("HHPT_READY report=%s plan=%s components=%d integrity=%s audio=%s cpu=%s memory=%s input=%s pointer=%s network=%s storage=%s usb=%s graphics=%s display=%s writable_block_mounts=0 active_non_loop_interfaces=0",
		sessionMarkerValue(reportPath), frozen.ID, counts[1], status, outcomes["audio.readiness.quick"], outcomes["cpu.quick"], outcomes["memory.quick"],
		outcomes["input.readiness.quick"], outcomes["input.pointer.quick"], outcomes["network.readiness.quick"], outcomes["storage.readiness.quick"], outcomes["usb.readiness.quick"], outcomes["graphics.quick"], outcomes["display.operator.quick"])
	if safeSessionToken(keyEpoch) {
		marker += " key_epoch=" + keyEpoch
	}
	if frozen.Architecture == "legacy-x86" {
		marker += " assurance=legacy"
	}
	view := sessionevidence.ThermalViewOf(journal)
	marker += " session_stop=" + journal.StopReason + " thermal_finding=" + view.Finding + " thermal_coverage=" + view.Coverage + " signature=self-issued\n"
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	return errors.Join(writeAll(file, []byte(marker)), file.Close())
}

func sessionMarkerValue(value string) string {
	for _, character := range value {
		if character <= ' ' || character >= '\x7f' || character == '"' {
			return strconv.Quote(value)
		}
	}
	return value
}

func safeSessionToken(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune("._:-", character) {
			continue
		}
		return false
	}
	return true
}
