package inventory

import (
	"context"
	"errors"
	"iter"
	"path"
	"regexp"
	"strconv"
	"strings"
)

var ErrNoPathSource = errors.New("inventory: no path source")

var cpuDirectoryPattern = regexp.MustCompile(`^cpu[0-9]+$`)

type LinuxPaths struct {
	Proc string
	Sys  string
}

type LinuxCollector struct {
	Source              PathSource
	Paths               LinuxPaths
	inputBitmapWordBits func() int
}

func NewLinuxCollector(root string) *LinuxCollector {
	return NewLinuxCollectorWithSource(NewRootPathSource(root))
}

func NewLinuxCollectorWithSource(source PathSource) *LinuxCollector {
	return &LinuxCollector{
		Source:              source,
		inputBitmapWordBits: kernelInputBitmapWordBits,
		Paths: LinuxPaths{
			Proc: "proc",
			Sys:  "sys",
		},
	}
}

func (collector *LinuxCollector) Collect(ctx context.Context) (Snapshot, error) {
	if collector == nil || nilInventoryValue(collector.Source) {
		return Snapshot{}, ErrNoPathSource
	}
	if ctx == nil {
		return Snapshot{}, ErrInvalidContext
	}
	collection := collector.newCollection(ctx)
	if !collection.check() {
		return Snapshot{}, collection.err
	}
	groups := []func() []Component{
		collection.collectCPU, collection.collectMemory, collection.collectDMI,
		collection.collectBlock, collection.collectPCI, collection.collectNetwork,
		collection.collectInput, collection.collectSound, collection.collectVideo,
		collection.collectUSB,
	}
	var components []Component
	for _, collect := range groups {
		if !collection.check() {
			return Snapshot{}, collection.err
		}
		group := collect()
		if !collection.check() {
			return Snapshot{}, collection.err
		}
		components = append(components, group...)
	}
	return collection.snapshot(components, NewSnapshot)
}

func (collector *linuxCollection) collectMemory() []Component {
	data, ok := collector.readFile(path.Join(collector.Paths.Proc, "meminfo"))
	if !ok {
		return nil
	}
	attributes := make(map[string]string)
	for sourceName, targetName := range map[string]string{
		"memtotal":     "total_kib",
		"memavailable": "available_kib",
	} {
		if !collector.check() {
			return nil
		}
		for _, line := range collectionItems(collector, strings.Split(string(data), "\n")) {
			if !collector.check() {
				return nil
			}
			name, value, found := strings.Cut(line, ":")
			if found && strings.EqualFold(strings.TrimSpace(name), sourceName) {
				attributes[targetName] = strings.TrimSuffix(normalizeValue(value), " kB")
				break
			}
		}
	}
	if attributes["total_kib"] == "" {
		return nil
	}
	if !collector.check() {
		return nil
	}
	return []Component{NewComponent(ComponentMemory, "memory:system", attributes)}
}

func (collector *linuxCollection) collectCPU() []Component {
	processors := make(map[string]map[string]string)
	if data, ok := collector.readFile(path.Join(collector.Paths.Proc, "cpuinfo")); ok {
		for recordIndex, record := range collector.parseRecords(string(data)) {
			if !collector.check() {
				return nil
			}
			logicalIndex := record["processor"]
			if logicalIndex == "" {
				logicalIndex = strconv.Itoa(recordIndex)
			}
			attributes := processors[logicalIndex]
			if attributes == nil {
				attributes = make(map[string]string)
				processors[logicalIndex] = attributes
			}
			attributes["logical_index"] = logicalIndex
			collector.copyAliases(attributes, record, []alias{
				{from: "vendor_id", to: "vendor"},
				{from: "model name", to: "model"},
				{from: "cpu family", to: "family"},
				{from: "model", to: "model_number"},
				{from: "stepping", to: "stepping"},
				{from: "microcode", to: "microcode"},
				{from: "physical id", to: "package_id"},
				{from: "core id", to: "core_id"},
				{from: "cpu cores", to: "cores"},
				{from: "siblings", to: "siblings"},
				{from: "flags", to: "features"},
				{from: "features", to: "features"},
				{from: "cpu implementer", to: "implementer"},
				{from: "cpu architecture", to: "architecture"},
				{from: "cpu variant", to: "variant"},
				{from: "cpu part", to: "part"},
				{from: "cpu revision", to: "revision"},
				{from: "isa", to: "isa"},
				{from: "uarch", to: "microarchitecture"},
				{from: "hardware", to: "hardware"},
			})
		}
	}

	cpuRoot := path.Join(collector.Paths.Sys, "devices/system/cpu")
	for _, entry := range collectionItems(collector, collector.readDir(cpuRoot)) {
		if !collector.check() {
			return nil
		}
		if !cpuDirectoryPattern.MatchString(entry.Name()) {
			continue
		}
		logicalIndex := strings.TrimPrefix(entry.Name(), "cpu")
		attributes := processors[logicalIndex]
		if attributes == nil {
			attributes = make(map[string]string)
			processors[logicalIndex] = attributes
		}
		attributes["logical_index"] = logicalIndex
		collector.addAttributes(attributes, path.Join(cpuRoot, entry.Name()), []attributePath{
			{name: "online", relativePath: "online"},
			{name: "core_id", relativePath: "topology/core_id"},
			{name: "package_id", relativePath: "topology/physical_package_id"},
			{name: "thread_siblings", relativePath: "topology/thread_siblings_list"},
			{name: "core_siblings", relativePath: "topology/core_siblings_list"},
			{name: "maximum_frequency_khz", relativePath: "cpufreq/cpuinfo_max_freq"},
			{name: "capacity", relativePath: "cpu_capacity"},
		})
	}

	components := make([]Component, 0, len(processors))
	for logicalIndex, attributes := range processors {
		if !collector.check() {
			return nil
		}
		components = append(components, NewComponent(ComponentCPU, "cpu:"+logicalIndex, attributes))
	}
	return components
}

func (collector *linuxCollection) collectDMI() []Component {
	dmiRoot := path.Join(collector.Paths.Sys, "class/dmi/id")
	system := make(map[string]string)
	collector.addAttributes(system, dmiRoot, []attributePath{
		{name: "vendor", relativePath: "sys_vendor"},
		{name: "product_name", relativePath: "product_name"},
		{name: "product_family", relativePath: "product_family"},
		{name: "product_version", relativePath: "product_version"},
		{name: "product_serial", relativePath: "product_serial"},
		{name: "product_uuid", relativePath: "product_uuid"},
		{name: "product_sku", relativePath: "product_sku"},
		{name: "chassis_vendor", relativePath: "chassis_vendor"},
		{name: "chassis_type", relativePath: "chassis_type"},
		{name: "chassis_version", relativePath: "chassis_version"},
		{name: "chassis_serial", relativePath: "chassis_serial"},
	})
	baseboard := make(map[string]string)
	collector.addAttributes(baseboard, dmiRoot, []attributePath{
		{name: "vendor", relativePath: "board_vendor"},
		{name: "name", relativePath: "board_name"},
		{name: "version", relativePath: "board_version"},
		{name: "serial", relativePath: "board_serial"},
		{name: "asset_tag", relativePath: "board_asset_tag"},
	})

	var components []Component
	if !collector.check() {
		return nil
	}
	if len(system) > 0 {
		if !collector.check() {
			return nil
		}
		components = append(components, NewComponent(ComponentSystem, "system", system))
	}
	if len(baseboard) > 0 {
		if !collector.check() {
			return nil
		}
		components = append(components, NewComponent(ComponentBaseboard, "baseboard", baseboard))
	}
	return components
}

func (collector *linuxCollection) collectBlock() []Component {
	blockRoot := path.Join(collector.Paths.Sys, "class/block")
	var components []Component
	for _, entry := range collectionItems(collector, collector.readDir(blockRoot)) {
		if !collector.check() {
			return nil
		}
		name := entry.Name()
		attributes := map[string]string{"name": name, "kind": "device"}
		collector.addAttributes(attributes, path.Join(blockRoot, name), []attributePath{
			{name: "device_number", relativePath: "dev"},
			{name: "size_sectors", relativePath: "size"},
			{name: "read_only", relativePath: "ro"},
			{name: "removable", relativePath: "removable"},
			{name: "logical_block_size", relativePath: "queue/logical_block_size"},
			{name: "physical_block_size", relativePath: "queue/physical_block_size"},
			{name: "rotational", relativePath: "queue/rotational"},
			{name: "vendor", relativePath: "device/vendor"},
			{name: "model", relativePath: "device/model"},
			{name: "serial", relativePath: "device/serial"},
			{name: "wwid", relativePath: "wwid"},
			{name: "transport_type", relativePath: "device/type"},
		})
		if partition, ok := collector.readValue(path.Join(blockRoot, name, "partition")); ok {
			attributes["kind"] = "partition"
			attributes["partition_number"] = partition
		}
		if !collector.check() {
			return nil
		}
		components = append(components, NewComponent(ComponentBlock, "block:"+name, attributes))
	}
	return components
}

type attributePath struct {
	name         string
	relativePath string
}

type alias struct {
	from string
	to   string
}

func (collector *linuxCollection) addAttributes(attributes map[string]string, base string, fields []attributePath) {
	for _, field := range collectionItems(collector, fields) {
		if !collector.check() {
			return
		}
		if value, ok := collector.readValue(path.Join(base, field.relativePath)); ok {
			attributes[field.name] = value
		}
	}
}

func (collector *linuxCollection) readValue(name string) (string, bool) {
	data, ok := collector.readFile(name)
	if !ok {
		return "", false
	}
	value := normalizeValue(string(data))
	if !collector.check() {
		return "", false
	}
	return value, value != ""
}

func (collector *linuxCollection) parseRecords(content string) iter.Seq2[int, map[string]string] {
	return func(yield func(int, map[string]string) bool) {
		// CPU fallback IDs used the compacted nonempty-record slice index.
		// Preserve that contract rather than exposing raw split-block indices.
		recordIndex := 0
		for _, block := range collectionItems(collector, strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n\n")) {
			if !collector.check() {
				return
			}
			record := make(map[string]string)
			for _, line := range collectionItems(collector, strings.Split(block, "\n")) {
				if !collector.check() {
					return
				}
				name, value, found := strings.Cut(line, ":")
				if found {
					record[strings.ToLower(strings.TrimSpace(name))] = normalizeValue(value)
				}
			}
			if len(record) > 0 {
				if !collector.check() {
					return
				}
				keepGoing := yield(recordIndex, record)
				recordIndex++
				if !collector.check() || !keepGoing {
					return
				}
			}
		}
	}
}

func (collector *linuxCollection) copyAliases(destination, source map[string]string, aliases []alias) {
	for _, item := range collectionItems(collector, aliases) {
		if !collector.check() {
			return
		}
		if value := source[item.from]; value != "" {
			destination[item.to] = value
		}
	}
}
