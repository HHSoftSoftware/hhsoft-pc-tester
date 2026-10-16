package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/inventory"
)

const maximumFixtureComponentsBytes = 4 << 10

func loadFixtureComponents(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	file, metadata, err := openRegularNoSymlink(path, maximumFixtureComponentsBytes)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReader(io.LimitReader(file, maximumFixtureComponentsBytes+1))
	seen := make(map[string]bool)
	var result []string
	for {
		line, readErr := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" {
			if len(result) >= 8 || !validFixtureComponentID(line) || seen[line] {
				return nil, errors.New("invalid fixture component list")
			}
			seen[line] = true
			result = append(result, line)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	if len(result) == 0 {
		return nil, errors.New("fixture component list is empty")
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(metadata, after) || metadata.Size() != after.Size() {
		return nil, errors.New("fixture component list changed while reading")
	}
	return result, nil
}

func excludeFixtureComponents(snapshot inventory.Snapshot, excluded []string) (inventory.Snapshot, error) {
	if len(excluded) == 0 {
		return snapshot, nil
	}
	wanted := make(map[string]bool, len(excluded))
	for _, id := range excluded {
		if !validFixtureComponentID(id) || wanted[id] {
			return inventory.Snapshot{}, errors.New("invalid fixture component ID")
		}
		wanted[id] = true
	}
	filtered := make([]inventory.Component, 0, len(snapshot.Components))
	for _, component := range snapshot.Components {
		if wanted[component.ID] {
			delete(wanted, component.ID)
			continue
		}
		filtered = append(filtered, component)
	}
	if len(wanted) != 0 {
		return inventory.Snapshot{}, fmt.Errorf("deployment-key fixture changed before inventory")
	}
	return inventory.NewSnapshot(filtered), nil
}

func excludeFixtureComponentsFromFile(snapshot inventory.Snapshot, path string) (inventory.Snapshot, error) {
	excluded, err := loadFixtureComponents(path)
	if err != nil {
		return inventory.Snapshot{}, err
	}
	return excludeFixtureComponents(snapshot, excluded)
}

func validFixtureComponentID(value string) bool {
	if len(value) < 5 || len(value) > 128 || (!strings.HasPrefix(value, "block:") && !strings.HasPrefix(value, "usb:")) {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("._:-", character) {
			continue
		}
		return false
	}
	return true
}
