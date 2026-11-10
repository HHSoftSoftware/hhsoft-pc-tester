package inventory

import (
	"path"
	"strings"
)

func (collector *linuxCollection) collectPCI() []Component {
	pciRoot := path.Join(collector.Paths.Sys, "bus/pci/devices")
	var components []Component
	for _, entry := range collectionItems(collector, collector.readDir(pciRoot)) {
		if !collector.check() {
			return nil
		}
		address := entry.Name()
		attributes := map[string]string{"address": address}
		collector.addAttributes(attributes, path.Join(pciRoot, address), []attributePath{
			{name: "class", relativePath: "class"},
			{name: "vendor_id", relativePath: "vendor"},
			{name: "device_id", relativePath: "device"},
			{name: "subsystem_vendor_id", relativePath: "subsystem_vendor"},
			{name: "subsystem_device_id", relativePath: "subsystem_device"},
			{name: "revision", relativePath: "revision"},
			{name: "irq", relativePath: "irq"},
			{name: "numa_node", relativePath: "numa_node"},
			{name: "modalias", relativePath: "modalias"},
		})
		if !collector.check() {
			return nil
		}
		collector.addPCIeCapabilities(attributes, path.Join(pciRoot, address))
		if !collector.check() {
			return nil
		}
		collector.addPCIeNegotiatedObservations(attributes, path.Join(pciRoot, address))
		if !collector.check() {
			return nil
		}
		components = append(components, NewComponent(ComponentPCI, "pci:"+address, attributes))
	}
	return components
}

func (collector *linuxCollection) collectNetwork() []Component {
	networkRoot := path.Join(collector.Paths.Sys, "class/net")
	var components []Component
	for _, entry := range collectionItems(collector, collector.readDir(networkRoot)) {
		if !collector.check() {
			return nil
		}
		name := entry.Name()
		attributes := map[string]string{"name": name}
		collector.addAttributes(attributes, path.Join(networkRoot, name), []attributePath{
			{name: "address", relativePath: "address"},
			{name: "type", relativePath: "type"},
			{name: "interface_index", relativePath: "ifindex"},
			{name: "link_index", relativePath: "iflink"},
			{name: "mtu", relativePath: "mtu"},
			{name: "operational_state", relativePath: "operstate"},
			{name: "carrier", relativePath: "carrier"},
			{name: "speed_mbps", relativePath: "speed"},
			{name: "duplex", relativePath: "duplex"},
			{name: "vendor_id", relativePath: "device/vendor"},
			{name: "device_id", relativePath: "device/device"},
		})
		if !collector.check() {
			return nil
		}
		components = append(components, NewComponent(ComponentNetwork, "network:"+name, attributes))
	}
	return components
}

func (collector *linuxCollection) collectUSB() []Component {
	usbRoot := path.Join(collector.Paths.Sys, "bus/usb/devices")
	entries := collector.readDir(usbRoot)
	var components []Component
	for _, entry := range collectionItems(collector, entries) {
		if !collector.check() {
			return nil
		}
		name := entry.Name()
		base := path.Join(usbRoot, name)
		vendorID, ok := collector.readValue(path.Join(base, "idVendor"))
		if !ok {
			continue
		}
		attributes := map[string]string{"path": name, "vendor_id": vendorID}
		collector.addAttributes(attributes, base, []attributePath{
			{name: "product_id", relativePath: "idProduct"},
			{name: "manufacturer", relativePath: "manufacturer"},
			{name: "product", relativePath: "product"},
			{name: "serial", relativePath: "serial"},
			{name: "device_version", relativePath: "bcdDevice"},
			{name: "class", relativePath: "bDeviceClass"},
			{name: "subclass", relativePath: "bDeviceSubClass"},
			{name: "protocol", relativePath: "bDeviceProtocol"},
			{name: "bus_number", relativePath: "busnum"},
			{name: "device_number", relativePath: "devnum"},
			{name: "speed_mbps", relativePath: "speed"},
			{name: "authorized", relativePath: "authorized"},
		})
		for _, interfaceEntry := range collectionItems(collector, entries) {
			if !collector.check() {
				return nil
			}
			if !strings.HasPrefix(interfaceEntry.Name(), name+":") {
				continue
			}
			if class, found := collector.readValue(path.Join(usbRoot, interfaceEntry.Name(), "bInterfaceClass")); found && class == "01" {
				attributes["audio_interface"] = "true"
				break
			}
		}
		if !collector.check() {
			return nil
		}
		components = append(components, NewComponent(ComponentUSB, "usb:"+name, attributes))
	}
	return components
}
