package storagesample

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
)

var (
	errLinuxStorageMetadata = errors.New("storage metadata is incomplete or ambiguous")
	errLinuxStorageLoop     = errors.New("storage loop lifetime is unsupported")
	errLinuxStorageSwap     = errors.New("storage swap is active")
)

// These are captured facts, not filesystem paths or permission to open media.
// The future acquirer must prove complete enumeration, EOF, identity and closure.
// Nil relation lists mean unavailable; a present empty list means observed empty.
// The caller must not mutate input concurrently with this finite pure parse.
type linuxStorageNodeInput struct {
	Name             string
	Kind             string
	Parent           string
	Transport        string
	Number           []byte
	Sectors          []byte
	LogicalBlock     []byte
	ReadOnly         []byte
	Removable        []byte
	Partition        []byte
	USBAncestors     []string
	PhysicalChildren []string
	Holders          []string
	Slaves           []string
}

type linuxStorageMetadataInput struct {
	Registry           []string
	Nodes              []linuxStorageNodeInput
	MountInfo          []byte
	Swaps              []byte
	BootBackingNumbers []string
}

// A snapshot owns its facts and deliberately leaves both admission gates false.
// No live lifetime, kernel identity, safety interval or sampled work is proved.
type linuxStorageMetadataSnapshot struct {
	facts      storagetest.Discovery
	rootDevice string
}

func (snapshot linuxStorageMetadataSnapshot) discovery() storagetest.Discovery {
	facts := snapshot.facts
	facts.Nodes = make([]storagetest.Node, len(snapshot.facts.Nodes))
	for i, node := range snapshot.facts.Nodes {
		node.USBAncestors = append([]string{}, node.USBAncestors...)
		node.PhysicalChildren = append([]string{}, node.PhysicalChildren...)
		node.Holders = append([]string{}, node.Holders...)
		node.Slaves = append([]string{}, node.Slaves...)
		if node.Geometry != nil {
			geometry := *node.Geometry
			node.Geometry = &geometry
		}
		facts.Nodes[i] = node
	}
	facts.MountedNumbers = append([]string{}, facts.MountedNumbers...)
	facts.MemoryMounts = append([]storagetest.MemoryMount{}, facts.MemoryMounts...)
	facts.BootBackingNumbers = append([]string{}, facts.BootBackingNumbers...)
	facts.Complete, facts.BackingComplete = false, false
	return facts
}

func linuxStorageName(name string) bool {
	if len(name) == 0 || len(name) > 64 || name == "." || name == ".." {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}

func linuxStorageScalar(raw []byte, bits int) (uint64, error) {
	if len(raw) == 0 || len(raw) > 256 {
		return 0, errLinuxStorageMetadata
	}
	if raw[len(raw)-1] == '\n' {
		raw = raw[:len(raw)-1]
	}
	if len(raw) == 0 || len(raw) > 20 || len(raw) > 1 && raw[0] == '0' {
		return 0, errLinuxStorageMetadata
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, errLinuxStorageMetadata
		}
	}
	value, err := strconv.ParseUint(string(raw), 10, bits)
	if err != nil {
		return 0, errLinuxStorageMetadata
	}
	return value, nil
}

func linuxStorageNumber(raw []byte, block bool) (string, error) {
	if len(raw) == 0 || len(raw) > 22 {
		return "", errLinuxStorageMetadata
	}
	if raw[len(raw)-1] == '\n' {
		raw = raw[:len(raw)-1]
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 2 {
		return "", errLinuxStorageMetadata
	}
	major, err := linuxStorageScalar([]byte(parts[0]), 32)
	if err != nil || block && major == 0 {
		return "", errLinuxStorageMetadata
	}
	if _, err := linuxStorageScalar([]byte(parts[1]), 32); err != nil {
		return "", errLinuxStorageMetadata
	}
	// A dev attribute has only one optional terminal newline, not per-field whitespace.
	if strings.ContainsAny(string(raw), "\n\r") {
		return "", errLinuxStorageMetadata
	}
	return string(raw), nil
}

func linuxStorageRelations(values []string, usb bool) ([]string, error) {
	if values == nil || len(values) > storagetest.MaximumEdges {
		return nil, errLinuxStorageMetadata
	}
	result := make([]string, len(values))
	for i, value := range values {
		if usb {
			if !strings.HasPrefix(value, "usb:") || !linuxStorageName(strings.TrimPrefix(value, "usb:")) {
				return nil, errLinuxStorageMetadata
			}
			result[i] = value
		} else {
			if !linuxStorageName(value) {
				return nil, errLinuxStorageMetadata
			}
			result[i] = "block:" + value
		}
	}
	sort.Strings(result)
	for i := 1; i < len(result); i++ {
		if result[i] == result[i-1] {
			return nil, errLinuxStorageMetadata
		}
	}
	return result, nil
}

func parseLinuxStorageNode(raw linuxStorageNodeInput) (storagetest.Node, error) {
	if !linuxStorageName(raw.Name) {
		return storagetest.Node{}, errLinuxStorageMetadata
	}
	if raw.Kind == "loop" || strings.HasPrefix(raw.Name, "loop") {
		return storagetest.Node{}, errLinuxStorageLoop
	}
	number, err := linuxStorageNumber(raw.Number, true)
	if err != nil {
		return storagetest.Node{}, err
	}
	if strings.HasPrefix(number, "7:") {
		return storagetest.Node{}, errLinuxStorageLoop
	}
	geometry, err := storagetest.ParseGeometry(raw.Sectors, raw.LogicalBlock)
	if err != nil {
		return storagetest.Node{}, errLinuxStorageMetadata
	}
	ro, err := linuxStorageScalar(raw.ReadOnly, 1)
	if err != nil {
		return storagetest.Node{}, errLinuxStorageMetadata
	}
	removable, err := linuxStorageScalar(raw.Removable, 1)
	if err != nil {
		return storagetest.Node{}, errLinuxStorageMetadata
	}
	node := storagetest.Node{ID: "block:" + raw.Name, Number: number, Kind: raw.Kind, Transport: raw.Transport,
		Geometry: geometry, ReadOnly: ro == 1, Removable: removable == 1}
	if raw.Parent != "" {
		if !linuxStorageName(raw.Parent) {
			return storagetest.Node{}, errLinuxStorageMetadata
		}
		node.Parent = "block:" + raw.Parent
	}
	if node.Kind == "partition" {
		part, err := linuxStorageScalar(raw.Partition, 32)
		if err != nil || part == 0 || node.Parent == "" || len(raw.PhysicalChildren) != 0 {
			return storagetest.Node{}, errLinuxStorageMetadata
		}
	} else if (node.Kind != "physical" && node.Kind != "mapper") || raw.Partition != nil || node.Parent != "" {
		return storagetest.Node{}, errLinuxStorageMetadata
	}
	if node.Kind == "mapper" {
		if node.Transport != "virtual" || len(raw.Slaves) == 0 {
			return storagetest.Node{}, errLinuxStorageMetadata
		}
	} else {
		switch node.Transport {
		case "ata", "scsi", "nvme", "virtio", "xen", "mmc", "usb", "optical", "floppy", "unknown":
		default:
			return storagetest.Node{}, errLinuxStorageMetadata
		}
	}
	if node.USBAncestors, err = linuxStorageRelations(raw.USBAncestors, true); err != nil {
		return storagetest.Node{}, err
	}
	if node.PhysicalChildren, err = linuxStorageRelations(raw.PhysicalChildren, false); err != nil {
		return storagetest.Node{}, err
	}
	if node.Holders, err = linuxStorageRelations(raw.Holders, false); err != nil {
		return storagetest.Node{}, err
	}
	if node.Slaves, err = linuxStorageRelations(raw.Slaves, false); err != nil {
		return storagetest.Node{}, err
	}
	return node, nil
}

func validateLinuxStorageRelations(nodes []storagetest.Node) error {
	byID := make(map[string]int, len(nodes))
	for i, node := range nodes {
		byID[node.ID] = i
	}
	contains := func(values []string, value string) bool {
		i := sort.SearchStrings(values, value)
		return i < len(values) && values[i] == value
	}
	for _, node := range nodes {
		if node.Parent != "" {
			i, ok := byID[node.Parent]
			if !ok || node.Parent == node.ID || nodes[i].Kind == "partition" || !contains(nodes[i].PhysicalChildren, node.ID) {
				return errLinuxStorageMetadata
			}
		}
		for _, id := range node.PhysicalChildren {
			i, ok := byID[id]
			if !ok || id == node.ID || nodes[i].Kind != "partition" || nodes[i].Parent != node.ID {
				return errLinuxStorageMetadata
			}
		}
		for _, relation := range []struct {
			values  []string
			holders bool
		}{{node.Holders, true}, {node.Slaves, false}} {
			for _, id := range relation.values {
				i, ok := byID[id]
				if !ok || id == node.ID {
					return errLinuxStorageMetadata
				}
				back := nodes[i].Holders
				if relation.holders {
					back = nodes[i].Slaves
				}
				if !contains(back, node.ID) {
					return errLinuxStorageMetadata
				}
			}
		}
	}
	// Only dependency arrows are walked; reciprocal holder rows are not cycles.
	state := make([]uint8, len(nodes))
	var visit func(int) bool
	visit = func(i int) bool {
		if state[i] == 1 {
			return false
		}
		if state[i] == 2 {
			return true
		}
		state[i] = 1
		if nodes[i].Parent != "" && !visit(byID[nodes[i].Parent]) {
			return false
		}
		for _, id := range nodes[i].Slaves {
			if !visit(byID[id]) {
				return false
			}
		}
		state[i] = 2
		return true
	}
	for i := range nodes {
		if !visit(i) {
			return errLinuxStorageMetadata
		}
	}
	return nil
}

func parseLinuxStorageSwap(data []byte) (string, error) {
	if len(data) == 0 || len(data) > 64<<10 || data[len(data)-1] != '\n' {
		return "", errLinuxStorageMetadata
	}
	for _, c := range data {
		if c < 32 && c != '\n' && c != '\t' || c > 126 {
			return "", errLinuxStorageMetadata
		}
	}
	lines := strings.Split(string(data[:len(data)-1]), "\n")
	if len(lines) > storagetest.MaximumDevices+1 || strings.Join(strings.Fields(lines[0]), " ") != "Filename Type Size Used Priority" {
		return "", errLinuxStorageMetadata
	}
	seen := map[string]bool{}
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) != 5 || !strings.HasPrefix(fields[0], "/") || seen[fields[0]] || fields[1] != "partition" && fields[1] != "file" {
			return "", errLinuxStorageMetadata
		}
		seen[fields[0]] = true
		size, err := linuxStorageScalar([]byte(fields[2]), 64)
		if err != nil || size == 0 {
			return "", errLinuxStorageMetadata
		}
		used, err := linuxStorageScalar([]byte(fields[3]), 64)
		if err != nil || used > size {
			return "", errLinuxStorageMetadata
		}
		priority, err := strconv.ParseInt(fields[4], 10, 32)
		if err != nil || strconv.FormatInt(priority, 10) != fields[4] {
			return "", errLinuxStorageMetadata
		}
	}
	if len(lines) != 1 {
		return "active", nil
	}
	return "none", nil
}

func parseLinuxStorageMetadata(input linuxStorageMetadataInput) (linuxStorageMetadataSnapshot, error) {
	fail := func() (linuxStorageMetadataSnapshot, error) {
		return linuxStorageMetadataSnapshot{}, errLinuxStorageMetadata
	}
	if input.Registry == nil || input.Nodes == nil || len(input.Registry) > storagetest.MaximumDevices ||
		len(input.Nodes) != len(input.Registry) || input.BootBackingNumbers == nil ||
		len(input.BootBackingNumbers) > storagetest.MaximumDevices || len(input.MountInfo) > 4<<20 {
		return fail()
	}
	registry := make(map[string]bool, len(input.Registry))
	for _, name := range input.Registry {
		if !linuxStorageName(name) || registry[name] {
			return fail()
		}
		registry[name] = true
	}
	// Reject the cumulative bound before relation allocation or graph traversal.
	edges := 0
	for _, raw := range input.Nodes {
		for _, values := range [][]string{raw.USBAncestors, raw.PhysicalChildren, raw.Holders, raw.Slaves} {
			if values == nil || len(values) > storagetest.MaximumEdges-edges {
				return fail()
			}
			edges += len(values)
		}
	}
	facts := storagetest.Discovery{Nodes: []storagetest.Node{}, MountedNumbers: []string{},
		MemoryMounts: []storagetest.MemoryMount{}, BootBackingNumbers: []string{}}
	numbers := make(map[string]bool, len(input.Nodes))
	for _, raw := range input.Nodes {
		if !registry[raw.Name] {
			return fail()
		}
		delete(registry, raw.Name)
		node, err := parseLinuxStorageNode(raw)
		if err != nil {
			return linuxStorageMetadataSnapshot{}, err
		}
		if numbers[node.Number] {
			return fail()
		}
		numbers[node.Number] = true
		facts.Nodes = append(facts.Nodes, node)
	}
	if len(registry) != 0 {
		return fail()
	}
	sort.Slice(facts.Nodes, func(i, j int) bool { return facts.Nodes[i].ID < facts.Nodes[j].ID })
	if validateLinuxStorageRelations(facts.Nodes) != nil {
		return fail()
	}
	swap, err := parseLinuxStorageSwap(input.Swaps)
	if err != nil {
		return fail()
	}
	if swap != "none" {
		return linuxStorageMetadataSnapshot{}, errLinuxStorageSwap
	}
	facts.SwapState = swap
	mounts, err := safemode.ParseMountFacts(input.MountInfo)
	if err != nil || len(mounts.BlockDevices) > storagetest.MaximumDevices || len(mounts.MemoryFilesystems) > storagetest.MaximumDevices {
		return fail()
	}
	for number := range mounts.BlockDevices {
		if !numbers[number] {
			return fail()
		}
		facts.MountedNumbers = append(facts.MountedNumbers, number)
	}
	sort.Strings(facts.MountedNumbers)
	for number, filesystem := range mounts.MemoryFilesystems {
		facts.MemoryMounts = append(facts.MemoryMounts, storagetest.MemoryMount{Number: number, Filesystem: filesystem})
	}
	sort.Slice(facts.MemoryMounts, func(i, j int) bool { return facts.MemoryMounts[i].Number < facts.MemoryMounts[j].Number })
	boot := map[string]bool{}
	for _, raw := range input.BootBackingNumbers {
		number, err := linuxStorageNumber([]byte(raw), false)
		if err != nil || number != raw || boot[number] || !numbers[number] && mounts.MemoryFilesystems[number] == "" {
			return fail()
		}
		boot[number] = true
		facts.BootBackingNumbers = append(facts.BootBackingNumbers, number)
	}
	sort.Strings(facts.BootBackingNumbers)
	return linuxStorageMetadataSnapshot{facts: facts, rootDevice: mounts.RootDevice}, nil
}
