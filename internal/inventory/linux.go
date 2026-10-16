package inventory

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"regexp"
	"sort"
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
	Source PathSource
	Paths  LinuxPaths
}

func NewLinuxCollector(root string) *LinuxCollector {
	return NewLinuxCollectorWithSource(NewRootPathSource(root))
}

func NewLinuxCollectorWithSource(source PathSource) *LinuxCollector {
	return &LinuxCollector{
		Source: source,
		Paths: LinuxPaths{
			Proc: "proc",
			Sys:  "sys",
		},
	}
}

func (collector *LinuxCollector) Collect(ctx context.Context) (Snapshot, error) {
	if collector == nil || collector.Source == nil {
		return Snapshot{}, ErrNoPathSource
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	groups := []func() []Component{
		collector.collectCPU,
		collector.collectMemory,
		collector.collectDMI,
		collector.collectBlock,
		collector.collectPCI,
		collector.collectNetwork,
		collector.collectInput,
		collector.collectSound,
		collector.collectVideo,
		collector.collectUSB,
	}
	var components []Component
	for _, collect := range groups {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		components = append(components, collect()...)
	}
	return NewSnapshot(components), nil
}

func (collector *LinuxCollector) collectMemory() []Component {
	data, ok := collector.readFile(path.Join(collector.Paths.Proc, "meminfo"))
	if !ok {
		return nil
	}
	attributes := make(map[string]string)
	for sourceName, targetName := range map[string]string{
		"memtotal":     "total_kib",
		"memavailable": "available_kib",
	} {
		for _, line := range strings.Split(string(data), "\n") {
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
	return []Component{NewComponent(ComponentMemory, "memory:system", attributes)}
}

func (collector *LinuxCollector) collectCPU() []Component {
	processors := make(map[string]map[string]string)
	if data, ok := collector.readFile(path.Join(collector.Paths.Proc, "cpuinfo")); ok {
		for recordIndex, record := range parseRecords(string(data)) {
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
			copyAliases(attributes, record, []alias{
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
	for _, entry := range collector.readDir(cpuRoot) {
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
		components = append(components, NewComponent(ComponentCPU, "cpu:"+logicalIndex, attributes))
	}
	return components
}

func (collector *LinuxCollector) collectDMI() []Component {
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
	if len(system) > 0 {
		components = append(components, NewComponent(ComponentSystem, "system", system))
	}
	if len(baseboard) > 0 {
		components = append(components, NewComponent(ComponentBaseboard, "baseboard", baseboard))
	}
	return components
}

func (collector *LinuxCollector) collectBlock() []Component {
	blockRoot := path.Join(collector.Paths.Sys, "class/block")
	var components []Component
	for _, entry := range collector.readDir(blockRoot) {
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

func (collector *LinuxCollector) addAttributes(attributes map[string]string, base string, fields []attributePath) {
	for _, field := range fields {
		if value, ok := collector.readValue(path.Join(base, field.relativePath)); ok {
			attributes[field.name] = value
		}
	}
}

func (collector *LinuxCollector) readFile(name string) ([]byte, bool) {
	data, err := collector.Source.ReadFile(name)
	return data, err == nil
}

func (collector *LinuxCollector) readValue(name string) (string, bool) {
	data, ok := collector.readFile(name)
	if !ok {
		return "", false
	}
	value := normalizeValue(string(data))
	return value, value != ""
}

func (collector *LinuxCollector) readDir(name string) []fs.DirEntry {
	entries, err := collector.Source.ReadDir(name)
	if err != nil {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	return entries
}

func parseRecords(content string) []map[string]string {
	var records []map[string]string
	for _, block := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n\n") {
		record := make(map[string]string)
		for _, line := range strings.Split(block, "\n") {
			name, value, found := strings.Cut(line, ":")
			if found {
				record[strings.ToLower(strings.TrimSpace(name))] = normalizeValue(value)
			}
		}
		if len(record) > 0 {
			records = append(records, record)
		}
	}
	return records
}

func copyAliases(destination, source map[string]string, aliases []alias) {
	for _, item := range aliases {
		if value := source[item.from]; value != "" {
			destination[item.to] = value
		}
	}
}
