package storagetest

import (
	"errors"
	"sort"
	"strconv"
	"strings"
)

var ErrAdmission = errors.New("storage sampling admission is incomplete or ambiguous")

func canonicalNumber(value string, allowZero bool) bool {
	if len(value) < 3 || len(value) > 21 {
		return false
	}
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return false
	}
	for index, part := range parts {
		if part == "" || len(part) > 10 || len(part) > 1 && part[0] == '0' {
			return false
		}
		for _, c := range part {
			if c < '0' || c > '9' {
				return false
			}
		}
		n, err := strconv.ParseUint(part, 10, 32)
		if err != nil || index == 0 && n == 0 && !allowZero {
			return false
		}
	}
	return true
}

func componentID(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) <= len(prefix) || len(value) > len(prefix)+64 {
		return false
	}
	for _, c := range value[len(prefix):] {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._:-", c)) {
			return false
		}
	}
	return value != prefix+"." && value != prefix+".." && !(prefix == "block:" && strings.ContainsRune(value[len(prefix):], ':'))
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func sortedUnique(values []string, limit int, valid func(string) bool) bool {
	if values == nil || len(values) > limit {
		return false
	}
	for index, value := range values {
		if !valid(value) || index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}

func contains(values []string, wanted string) bool {
	index := sort.SearchStrings(values, wanted)
	return index < len(values) && values[index] == wanted
}

type topology struct {
	ids          map[string]int
	numbers      map[string]int
	memory       map[string]string
	adjacent     [][]int
	reasons      []map[string]bool
	fixtureMasks []uint8
}

func validateNodes(discovery Discovery) (topology, error) {
	if !discovery.Complete || !discovery.BackingComplete || discovery.SwapState != "none" || discovery.Nodes == nil || len(discovery.Nodes) > MaximumDevices {
		return topology{}, ErrAdmission
	}
	graph := topology{ids: map[string]int{}, numbers: map[string]int{}, memory: map[string]string{}, adjacent: make([][]int, len(discovery.Nodes)), reasons: make([]map[string]bool, len(discovery.Nodes)), fixtureMasks: make([]uint8, len(discovery.Nodes))}
	edges := 0
	for i, node := range discovery.Nodes {
		if node.Geometry == nil || !componentID(node.ID, "block:") || !canonicalNumber(node.Number, false) || i > 0 && discovery.Nodes[i-1].ID >= node.ID {
			return topology{}, ErrAdmission
		}
		if _, exists := graph.numbers[node.Number]; exists {
			return topology{}, ErrAdmission
		}
		graph.numbers[node.Number] = i
		graph.ids[node.ID] = i
		graph.reasons[i] = map[string]bool{}
		blockID := func(s string) bool { return componentID(s, "block:") }
		if !sortedUnique(node.USBAncestors, MaximumEdges, func(s string) bool { return componentID(s, "usb:") }) || !sortedUnique(node.PhysicalChildren, MaximumDevices, blockID) || !sortedUnique(node.Holders, MaximumEdges, blockID) || !sortedUnique(node.Slaves, MaximumEdges, blockID) {
			return topology{}, ErrAdmission
		}
		edges += len(node.USBAncestors) + len(node.PhysicalChildren) + len(node.Holders) + len(node.Slaves)
		if node.LoopBacking != nil && node.LoopBacking.Kind == "block" {
			edges++
		}
		if edges > MaximumEdges {
			return topology{}, ErrAdmission
		}
		if node.Kind != "loop" && (node.LoopState != "" || node.LoopBacking != nil) {
			return topology{}, ErrAdmission
		}
		switch node.Kind {
		case "physical":
			if node.Parent != "" {
				return topology{}, ErrAdmission
			}
		case "partition":
			if !blockID(node.Parent) || len(node.PhysicalChildren) != 0 {
				return topology{}, ErrAdmission
			}
		case "loop":
			if node.Parent != "" {
				return topology{}, ErrAdmission
			}
			switch node.LoopState {
			case "configured":
				backing := node.LoopBacking
				if backing == nil || !validDigest(backing.BindingSHA256) {
					return topology{}, ErrAdmission
				}
				if backing.Kind == "block" {
					if !canonicalNumber(backing.Device, false) {
						return topology{}, ErrAdmission
					}
				} else if backing.Kind == "tmpfs" || backing.Kind == "ramfs" {
					if !canonicalNumber(backing.Device, true) || !strings.HasPrefix(backing.Device, "0:") {
						return topology{}, ErrAdmission
					}
				} else {
					return topology{}, ErrAdmission
				}
			case "unbound":
				if node.LoopBacking != nil || node.Geometry.CapacityBytes != 0 || len(node.Holders) != 0 || len(node.Slaves) != 0 || len(node.PhysicalChildren) != 0 || node.Removable || len(node.USBAncestors) != 0 {
					return topology{}, ErrAdmission
				}
			default:
				return topology{}, ErrAdmission
			}
		case "mapper":
			if node.Parent != "" || len(node.Slaves) == 0 {
				return topology{}, ErrAdmission
			}
		default:
			return topology{}, ErrAdmission
		}
		if node.Kind == "physical" || node.Kind == "partition" {
			switch node.Transport {
			case "ata", "scsi", "nvme", "virtio", "xen", "mmc", "usb", "optical", "floppy", "unknown":
			default:
				return topology{}, ErrAdmission
			}
		} else if node.Transport != "virtual" {
			return topology{}, ErrAdmission
		}
		if node.Kind == "loop" && node.LoopState == "unbound" {
			if node.Geometry.LogicalBlockBytes < 512 || node.Geometry.LogicalBlockBytes > MaximumSampleBytes || node.Geometry.LogicalBlockBytes&(node.Geometry.LogicalBlockBytes-1) != 0 {
				return topology{}, ErrAdmission
			}
		} else if node.Geometry.CapacityBytes == 0 {
			if node.Kind == "loop" {
				return topology{}, ErrAdmission
			}
			graph.reasons[i]["empty_media"] = true
		} else if _, err := SampleRanges(node.Geometry.CapacityBytes, node.Geometry.LogicalBlockBytes); err != nil {
			graph.reasons[i]["unsupported_geometry"] = true
		}
	}
	if !sortedUnique(discovery.MountedNumbers, MaximumDevices, func(s string) bool { return canonicalNumber(s, false) }) || !sortedUnique(discovery.BootBackingNumbers, MaximumDevices, func(s string) bool { return canonicalNumber(s, true) }) || discovery.MemoryMounts == nil || len(discovery.MemoryMounts) > MaximumDevices {
		return topology{}, ErrAdmission
	}
	for i, mount := range discovery.MemoryMounts {
		if !canonicalNumber(mount.Number, true) || !strings.HasPrefix(mount.Number, "0:") || mount.Filesystem != "tmpfs" && mount.Filesystem != "ramfs" || i > 0 && discovery.MemoryMounts[i-1].Number >= mount.Number {
			return topology{}, ErrAdmission
		}
		graph.memory[mount.Number] = mount.Filesystem
	}
	return graph, nil
}

func (graph *topology) link(a, b int) error {
	if a == b {
		return ErrAdmission
	}
	graph.adjacent[a] = append(graph.adjacent[a], b)
	graph.adjacent[b] = append(graph.adjacent[b], a)
	return nil
}

func (graph *topology) connect(nodes []Node) error {
	for i, node := range nodes {
		if node.Parent != "" {
			parent, ok := graph.ids[node.Parent]
			if !ok || nodes[parent].Kind == "partition" || !contains(nodes[parent].PhysicalChildren, node.ID) || graph.link(i, parent) != nil {
				return ErrAdmission
			}
		}
		for _, id := range node.PhysicalChildren {
			child, ok := graph.ids[id]
			if !ok || nodes[child].Parent != node.ID || nodes[child].Kind != "partition" {
				return ErrAdmission
			}
		}
		for _, relation := range []struct {
			values  []string
			reverse bool
		}{{node.Holders, true}, {node.Slaves, false}} {
			for _, id := range relation.values {
				other, ok := graph.ids[id]
				if !ok {
					return ErrAdmission
				}
				back := nodes[other].Holders
				if relation.reverse {
					back = nodes[other].Slaves
				}
				if !contains(back, node.ID) || graph.link(i, other) != nil {
					return ErrAdmission
				}
				graph.reasons[i]["stacked"] = true
				graph.reasons[other]["stacked"] = true
			}
		}
		if node.Kind == "loop" {
			graph.reasons[i]["loop"] = true
			if node.LoopState == "unbound" {
				graph.reasons[i]["unbound"] = true
			} else {
				backing := node.LoopBacking
				if backing.Kind == "block" {
					other, ok := graph.numbers[backing.Device]
					if !ok || nodes[other].LoopState == "unbound" || graph.link(i, other) != nil {
						return ErrAdmission
					}
				} else if graph.memory[backing.Device] != backing.Kind {
					return ErrAdmission
				}
			}
		}
		if node.Kind == "mapper" {
			graph.reasons[i]["stacked"] = true
		}
		if node.Removable {
			graph.reasons[i]["removable"] = true
		}
		if node.Transport == "usb" || len(node.USBAncestors) > 0 {
			graph.reasons[i]["usb"] = true
		}
		if node.Transport == "optical" || node.Transport == "floppy" {
			graph.reasons[i][node.Transport] = true
		}
		if node.Transport == "unknown" {
			graph.reasons[i]["transport_unknown"] = true
		}
	}
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
		node := nodes[i]
		dependencies := append([]string{}, node.Slaves...)
		if node.Parent != "" {
			dependencies = append(dependencies, node.Parent)
		}
		if node.LoopBacking != nil && node.LoopBacking.Kind == "block" {
			dependencies = append(dependencies, nodes[graph.numbers[node.LoopBacking.Device]].ID)
		}
		for _, id := range dependencies {
			if !visit(graph.ids[id]) {
				return false
			}
		}
		state[i] = 2
		return true
	}
	for i := range state {
		if !visit(i) {
			return ErrAdmission
		}
	}
	return nil
}

func (graph *topology) excludeFamilies() {
	seen := make([]bool, len(graph.adjacent))
	for start := range graph.adjacent {
		if seen[start] {
			continue
		}
		family := []int{start}
		seen[start] = true
		all := map[string]bool{}
		var mask uint8
		for next := 0; next < len(family); next++ {
			i := family[next]
			mask |= graph.fixtureMasks[i]
			for reason := range graph.reasons[i] {
				all[reason] = true
			}
			for _, other := range graph.adjacent[i] {
				if !seen[other] {
					seen[other] = true
					family = append(family, other)
				}
			}
		}
		for _, i := range family {
			graph.reasons[i] = all
			graph.fixtureMasks[i] = mask
		}
	}
}
