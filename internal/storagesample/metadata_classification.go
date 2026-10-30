package storagesample

import (
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/safemode"
	"github.com/HHSoftSoftware/hhsoft-pc-tester/internal/storagetest"
)

// A canonical chain is descriptive input from a separate owned producer.
// "none" means an observed absent subsystem link; empty means unavailable.
// Paths must describe real /devices directories, never unresolved class links.
// No path string or equal repeated observation is a target/backing lease.
type linuxStorageAncestor struct {
	path, subsystem, deviceType string
	scsiType                    []byte
}
type linuxStorageCanonicalPath struct {
	name      string
	number    []byte
	ancestors []linuxStorageAncestor
}

func linuxStorageCanonicalParts(value string) ([]string, error) {
	if len(value) == 0 || len(value) > 4096 || !strings.HasPrefix(value, "/devices") {
		return nil, errLinuxStorageMetadata
	}
	parts := strings.Split(value[1:], "/")
	if len(parts) == 0 || len(parts) > 64 || parts[0] != "devices" {
		return nil, errLinuxStorageMetadata
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || len(part) > 255 {
			return nil, errLinuxStorageMetadata
		}
		for _, c := range part {
			if c < 32 || c > 126 || c == '\\' {
				return nil, errLinuxStorageMetadata
			}
		}
	}
	return parts, nil
}

func validateLinuxStorageAncestor(value linuxStorageAncestor) error {
	if _, err := linuxStorageCanonicalParts(value.path); err != nil {
		return err
	}
	if !linuxStorageName(value.subsystem) {
		return errLinuxStorageMetadata
	}
	if value.subsystem == "usb" {
		if value.deviceType != "usb_device" && value.deviceType != "usb_interface" {
			return errLinuxStorageMetadata
		}
	} else if value.deviceType != "" {
		return errLinuxStorageMetadata
	}
	if value.scsiType != nil {
		if value.subsystem != "scsi" {
			return errLinuxStorageMetadata
		}
		n, err := linuxStorageScalar(value.scsiType, 32)
		if err != nil || n > 31 {
			return errLinuxStorageMetadata
		}
	}
	return nil
}

// Transport is context derived from observed ancestor subsystems. Missing
// context stays unknown; no disk prefix or fixed parent depth guesses it.
func linuxStorageChainContext(chain []linuxStorageAncestor) (string, []string, error) {
	usb := []string{}
	seenUSB := map[string]bool{}
	transports := map[string]bool{}
	var scsiType *uint64
	for _, ancestor := range chain {
		switch ancestor.subsystem {
		case "usb":
			if ancestor.deviceType == "usb_device" {
				parts, _ := linuxStorageCanonicalParts(ancestor.path)
				name := parts[len(parts)-1]
				if !linuxStorageName(name) || seenUSB[name] {
					return "", nil, errLinuxStorageMetadata
				}
				seenUSB[name] = true
				usb = append(usb, "usb:"+name)
			}
		case "nvme", "virtio", "xen", "mmc", "scsi":
			transports[ancestor.subsystem] = true
		}
		if ancestor.scsiType != nil {
			n, err := linuxStorageScalar(ancestor.scsiType, 32)
			if err != nil || scsiType != nil && *scsiType != n {
				return "", nil, errLinuxStorageMetadata
			}
			scsiType = &n
		}
	}
	sort.Strings(usb)
	for _, ancestor := range chain {
		if ancestor.subsystem == "usb" && len(usb) == 0 {
			return "", nil, errLinuxStorageMetadata
		}
	}
	if len(transports) > 1 {
		return "", nil, errLinuxStorageMetadata
	}
	transport := "unknown"
	for observed := range transports {
		transport = observed
	}
	if transport == "scsi" {
		transport = "unknown"
		if scsiType != nil {
			switch *scsiType {
			case 0:
				transport = "scsi"
			case 5, 7:
				transport = "optical"
			}
		}
	}
	// Optical remains visible even behind USB; USB ancestry is retained for
	// whole-family exclusion. Other USB-backed contexts use USB transport.
	if transport == "optical" {
		return transport, usb, nil
	}
	if len(usb) > 0 {
		return "usb", usb, nil
	}
	return transport, usb, nil
}

// Classify finite captured facts without promoting them to live admission.
// BootBackingNumbers deliberately remains nil: a separate original-boot
// producer is mandatory before the existing strict parser can accept this.
// The caller must not mutate inputs concurrently; there are no callbacks.
func classifyLinuxStorageCapture(capture linuxStorageCapture, paths []linuxStorageCanonicalPath) (linuxStorageMetadataInput, error) {
	fail := func() (linuxStorageMetadataInput, error) { return linuxStorageMetadataInput{}, errLinuxStorageMetadata }
	if capture.registry == nil || capture.nodes == nil || paths == nil || len(capture.registry) > 256 ||
		len(capture.nodes) != len(capture.registry) || len(paths) != len(capture.registry) ||
		len(capture.mountInfo) > 4<<20 || len(capture.swaps) > 64<<10 {
		return fail()
	}
	byName := map[string]int{}
	numbers := map[string]bool{}
	registry := map[string]bool{}
	edges := 0
	for _, name := range capture.registry {
		if !linuxStorageName(name) || registry[name] {
			return fail()
		}
		registry[name] = true
	}
	for i, node := range capture.nodes {
		if !registry[node.name] {
			return fail()
		}
		if _, found := byName[node.name]; found {
			return fail()
		}
		byName[node.name] = i
		for _, raw := range [][]byte{node.number, node.sectors, node.readOnly, node.partition, node.removable, node.logicalBlock} {
			if len(raw) > 256 {
				return fail()
			}
		}
		number, err := linuxStorageNumber(node.number, true)
		if err != nil || numbers[number] {
			return fail()
		}
		if strings.HasPrefix(node.name, "loop") || strings.HasPrefix(number, "7:") {
			return linuxStorageMetadataInput{}, errLinuxStorageLoop
		}
		numbers[number] = true
		for _, relation := range [][]string{node.holders, node.slaves} {
			if relation == nil || len(relation) > 1024-edges {
				return fail()
			}
			edges += len(relation)
		}
		if node.partition == nil {
			if node.partitionSlavesAbsent {
				return fail()
			}
		} else {
			part, err := linuxStorageScalar(node.partition, 32)
			if err != nil || part == 0 || node.removable != nil || node.logicalBlock != nil ||
				node.partitionSlavesAbsent && len(node.slaves) != 0 {
				return fail()
			}
		}
	}
	pathByName := map[string]linuxStorageCanonicalPath{}
	nameByPath := map[string]string{}
	ancestors := map[string]linuxStorageAncestor{}
	usbPaths := map[string]string{}
	for _, row := range paths {
		i, ok := byName[row.name]
		if !ok || len(row.ancestors) < 2 || len(row.ancestors) > 64 || row.ancestors[0].subsystem != "none" {
			return fail()
		}
		if _, duplicate := pathByName[row.name]; duplicate {
			return fail()
		}
		number, err := linuxStorageNumber(row.number, true)
		expected, _ := linuxStorageNumber(capture.nodes[i].number, true)
		if err != nil || number != expected {
			return fail()
		}
		last := row.ancestors[len(row.ancestors)-1]
		parts, err := linuxStorageCanonicalParts(last.path)
		if err != nil || len(parts) != len(row.ancestors) || parts[len(parts)-1] != row.name || last.subsystem != "block" {
			return fail()
		}
		if _, duplicate := nameByPath[last.path]; duplicate {
			return fail()
		}
		for j, ancestor := range row.ancestors {
			if ancestor.path != "/"+strings.Join(parts[:j+1], "/") || validateLinuxStorageAncestor(ancestor) != nil {
				return fail()
			}
			if prior, ok := ancestors[ancestor.path]; ok {
				if !reflect.DeepEqual(prior, ancestor) {
					return fail()
				}
			} else {
				if len(ancestors) >= 1024 {
					return fail()
				}
				ancestors[ancestor.path] = ancestor
			}
			if ancestor.subsystem == "usb" && ancestor.deviceType == "usb_device" {
				key := parts[j]
				if prior, ok := usbPaths[key]; ok && prior != ancestor.path {
					return fail()
				}
				usbPaths[key] = ancestor.path
			}
		}
		pathByName[row.name] = row
		nameByPath[last.path] = row.name
	}
	parents := map[string]string{}
	children := map[string][]string{}
	for _, node := range capture.nodes {
		children[node.name] = []string{}
	}
	for _, node := range capture.nodes {
		chain := pathByName[node.name].ancestors
		parent := ""
		for i := len(chain) - 2; i >= 0; i-- {
			if chain[i].subsystem != "block" {
				continue
			}
			name, ok := nameByPath[chain[i].path]
			if !ok || capture.nodes[byName[name]].partition != nil {
				return fail()
			}
			if parent != "" {
				return fail()
			}
			parent = name
		}
		if (node.partition != nil) != (parent != "") {
			return fail()
		}
		if parent != "" {
			parents[node.name] = parent
			children[parent] = append(children[parent], node.name)
			edges++
			if edges > 1024 {
				return fail()
			}
		}
	}
	input := linuxStorageMetadataInput{Registry: slices.Clone(capture.registry), Nodes: []linuxStorageNodeInput{},
		MountInfo: slices.Clone(capture.mountInfo), Swaps: slices.Clone(capture.swaps)}
	validated := []storagetest.Node{}
	for _, node := range capture.nodes {
		chain := pathByName[node.name].ancestors
		transport, usb, err := linuxStorageChainContext(chain)
		if err != nil || len(usb) > 1024-edges {
			return fail()
		}
		edges += len(usb)
		kind := "physical"
		removable, logical := node.removable, node.logicalBlock
		if parent := parents[node.name]; parent != "" {
			kind = "partition"
			whole := capture.nodes[byName[parent]]
			removable, logical = whole.removable, whole.logicalBlock
		} else if len(node.slaves) > 0 {
			kind, transport = "mapper", "virtual"
		} else if strings.HasPrefix(chain[len(chain)-1].path, "/devices/virtual/") {
			return fail()
		}
		offspring := slices.Clone(children[node.name])
		sort.Strings(offspring)
		out := linuxStorageNodeInput{Name: node.name, Kind: kind, Parent: parents[node.name], Transport: transport,
			Number: slices.Clone(node.number), Sectors: slices.Clone(node.sectors), ReadOnly: slices.Clone(node.readOnly),
			Removable: slices.Clone(removable), LogicalBlock: slices.Clone(logical), Partition: slices.Clone(node.partition),
			USBAncestors: usb, PhysicalChildren: offspring, Holders: slices.Clone(node.holders), Slaves: slices.Clone(node.slaves)}
		checked, err := parseLinuxStorageNode(out)
		if err != nil {
			return linuxStorageMetadataInput{}, err
		}
		validated = append(validated, checked)
		input.Nodes = append(input.Nodes, out)
	}
	if validateLinuxStorageRelations(validated) != nil {
		return fail()
	}
	swap, err := parseLinuxStorageSwap(input.Swaps)
	if err != nil {
		return fail()
	}
	if swap != "none" {
		return linuxStorageMetadataInput{}, errLinuxStorageSwap
	}
	mounts, err := safemode.ParseMountFacts(input.MountInfo)
	if err != nil {
		return fail()
	}
	for number := range mounts.BlockDevices {
		if !numbers[number] {
			return fail()
		}
	}
	return input, nil
}
