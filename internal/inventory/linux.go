package inventory

import (
	"context"
	"errors"
	"iter"
	"path"
	"regexp"
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
	snapshot, _, err := collector.CollectWithDiagnostics(ctx)
	return snapshot, err
}

// CollectWithDiagnostics returns bounded read-failure counts from the same pass.
// Diagnostics are separate from the schema-1 snapshot and are zero on failure.
func (collector *LinuxCollector) CollectWithDiagnostics(ctx context.Context) (Snapshot, CollectionDiagnostics, error) {
	if collector == nil || nilInventoryValue(collector.Source) {
		return Snapshot{}, CollectionDiagnostics{}, ErrNoPathSource
	}
	if ctx == nil {
		return Snapshot{}, CollectionDiagnostics{}, ErrInvalidContext
	}
	collection := collector.newCollection(ctx)
	if !collection.check() {
		return Snapshot{}, CollectionDiagnostics{}, collection.err
	}
	groups := []func() []Component{
		collection.collectCPU, collection.collectMemory, collection.collectDMI,
		collection.collectBlock, collection.collectPCI, collection.collectNetwork,
		collection.collectInput, collection.collectSound, collection.collectVideo,
		collection.collectUSB, collection.collectPowerSupplies, collection.collectFirmwareSecurity,
		collection.collectMemoryDevices,
	}
	var components []Component
	for _, collect := range groups {
		if !collection.check() {
			return Snapshot{}, CollectionDiagnostics{}, collection.err
		}
		group := collect()
		if !collection.check() {
			return Snapshot{}, CollectionDiagnostics{}, collection.err
		}
		components = append(components, group...)
	}
	snapshot, err := collection.snapshot(components, NewSnapshot)
	if err != nil {
		return Snapshot{}, CollectionDiagnostics{}, err
	}
	return snapshot, collection.acquisitionBudget.diagnostics, nil
}

func (collector *linuxCollection) collectMemory() []Component {
	data, ok := collector.readProc(path.Join(collector.Paths.Proc, "meminfo"), procMemory)
	if !ok || !collector.chargeProc(1, 0, 0) {
		return nil
	}
	attributes := make(map[string]string)
	totalSeen, availableSeen := false, false
	for line := range collector.procLines(string(data)) {
		name, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		target := ""
		switch {
		case !totalSeen && strings.EqualFold(strings.TrimSpace(name), "memtotal"):
			totalSeen, target = true, "total_kib"
		case !availableSeen && strings.EqualFold(strings.TrimSpace(name), "memavailable"):
			availableSeen, target = true, "available_kib"
		}
		if target != "" {
			if !collector.storeProcText(attributes, target, value, false, "", true) {
				return nil
			}
			attributes[target] = strings.TrimSuffix(attributes[target], " kB")
		}
		if totalSeen && availableSeen {
			break
		}
	}
	if !collector.check() || attributes["total_kib"] == "" {
		return nil
	}
	if !collector.reserveProcComponent("", "memory:system", attributes) {
		return nil
	}
	return []Component{NewComponent(ComponentMemory, "memory:system", attributes)}
}

func (collector *linuxCollection) collectCPU() []Component {
	processors := make(map[string]map[string]string)
	if data, ok := collector.readProc(path.Join(collector.Paths.Proc, "cpuinfo"), procCPU); ok {
		for recordIndex, record := range collector.parseRecords(string(data)) {
			if !collector.check() {
				return nil
			}
			logicalIndex := record["processor"]
			if logicalIndex == "" {
				var valid bool
				logicalIndex, valid = collector.procIndex(recordIndex)
				if !valid {
					return nil
				}
			}
			attributes := processors[logicalIndex]
			if attributes == nil {
				attributes = make(map[string]string)
				processors[logicalIndex] = attributes
			}
			if !collector.storeProcText(attributes, "logical_index", logicalIndex, false, "", false) {
				return nil
			}
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

	var components []Component
	for logicalIndex, attributes := range processors {
		if !collector.check() {
			return nil
		}
		if !collector.reserveProcComponent("cpu:", logicalIndex, attributes) {
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
	data, ok := collector.readScalar(name)
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
		// Fallback IDs count nonempty parsed records, not raw blocks.
		recordIndex := 0
		for _, block := range collector.procBlocks(content) {
			record := make(map[string]string)
			for line := range collector.procLines(block) {
				name, value, found := strings.Cut(line, ":")
				if found && !collector.storeProcText(record, strings.TrimSpace(name), value, true, "", true) {
					return
				}
			}
			if !collector.check() {
				return
			}
			if len(record) > 0 {
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
			if !collector.storeProcText(destination, item.to, value, false, "", false) {
				return
			}
		}
	}
}
