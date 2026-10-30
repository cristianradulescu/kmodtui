package kmod

import (
	"errors"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// Binding is one bindable unit of a device and the driver bound to it: the
// PCI function itself, or one USB interface.
type Binding struct {
	Path      string // sysfs name, e.g. "0000:c2:00.0" or "1-1:1.0"
	Modalias  string
	Class     string // PCI class code ("038000") or USB interface class ("0e")
	ClassName string
	Driver    string // bound driver, "" if none
	// Module is the module providing Driver. It can differ from the driver
	// name (driver xhci_hcd lives in module xhci_pci). Empty if the driver
	// is part of the kernel core.
	Module  string
	Builtin bool // the driver is compiled into the kernel
	// Candidates are modules whose aliases match Modalias; only filled for
	// unbound bindings.
	Candidates []string
}

// Device is a PCI device or a USB device (not root hubs).
type Device struct {
	Bus       string // "pci" or "usb"
	Addr      string // "0000:c2:00.0", "1-1"
	VendorID  string // lowercase hex, "1002"
	ProductID string
	Class     string // PCI: "038000"; USB: bDeviceClass
	Vendor    string // names from sysfs or the ID database; may be ""
	Product   string
	ClassName string
	Bindings  []Binding
}

// Drivers returns the distinct modules (or, for core drivers, driver names)
// bound to the device.
func (d Device) Drivers() []string {
	var out []string
	for _, b := range d.Bindings {
		name := b.Module
		if name == "" {
			name = b.Driver
		}
		if name != "" && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

// Candidates returns the distinct candidate modules of unbound bindings.
func (d Device) Candidates() []string {
	var out []string
	for _, b := range d.Bindings {
		out = append(out, b.Candidates...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Unbound reports whether some binding has no driver.
func (d Device) Unbound() bool {
	for _, b := range d.Bindings {
		if b.Driver == "" {
			return true
		}
	}
	return false
}

// DevicesByModule indexes devices by the (normalized) modules bound to them.
func DevicesByModule(devs []Device) map[string][]Device {
	m := map[string][]Device{}
	for _, d := range devs {
		for _, b := range d.Bindings {
			if b.Module != "" {
				k := Normalize(b.Module)
				if l := m[k]; len(l) == 0 || l[len(l)-1].Addr != d.Addr {
					m[k] = append(l, d)
				}
			}
		}
	}
	return m
}

// Devices lists PCI and USB devices with their drivers and modules. aliases
// may be nil; it is used to suggest modules for unbound devices.
func (s *Source) Devices(aliases *AliasIndex) ([]Device, []Op) {
	pci, pciOp := s.pciDevices(aliases)
	usb, usbOp := s.usbDevices(aliases)
	return append(pci, usb...), []Op{pciOp, usbOp}
}

func (s *Source) pciDevices(aliases *AliasIndex) ([]Device, Op) {
	op := Op{Desc: "List PCI devices and drivers", Cmd: "lspci -nnk"}
	const dir = "/sys/bus/pci/devices"
	entries, err := s.readDir(dir)
	if err != nil {
		op.Err = err
		return nil, op
	}
	ids := s.loadIDs("/usr/share/misc/pci.ids", "/usr/share/hwdata/pci.ids")
	devs := make([]Device, 0, len(entries))
	for _, e := range entries {
		p := path.Join(dir, e.Name())
		d := Device{Bus: "pci", Addr: e.Name()}
		d.VendorID = s.hexAttr(path.Join(p, "vendor"))
		d.ProductID = s.hexAttr(path.Join(p, "device"))
		d.Class = s.hexAttr(path.Join(p, "class"))
		d.Vendor = ids.Vendor(d.VendorID)
		d.Product = ids.Device(d.VendorID, d.ProductID)
		d.ClassName = ids.Class(d.Class)
		b := s.binding("pci", p, e.Name(), aliases)
		b.Class, b.ClassName = d.Class, d.ClassName
		d.Bindings = []Binding{b}
		devs = append(devs, d)
	}
	return devs, op
}

func (s *Source) usbDevices(aliases *AliasIndex) ([]Device, Op) {
	op := Op{Desc: "List USB devices and drivers", Cmd: "lsusb -t"}
	const dir = "/sys/bus/usb/devices"
	entries, err := s.readDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, op // no USB at all (VMs, containers)
	}
	if err != nil {
		op.Err = err
		return nil, op
	}
	ids := s.loadIDs("/usr/share/misc/usb.ids", "/usr/share/hwdata/usb.ids")
	var devs []Device
	for _, e := range entries {
		name := e.Name()
		// Root hubs ("usb1") and interfaces ("1-1:1.0") are not devices here.
		if strings.HasPrefix(name, "usb") || strings.Contains(name, ":") {
			continue
		}
		p := path.Join(dir, name)
		d := Device{Bus: "usb", Addr: name}
		d.VendorID = s.hexAttr(path.Join(p, "idVendor"))
		d.ProductID = s.hexAttr(path.Join(p, "idProduct"))
		d.Class = s.hexAttr(path.Join(p, "bDeviceClass"))
		d.Vendor, _ = s.readString(path.Join(p, "manufacturer"))
		d.Product, _ = s.readString(path.Join(p, "product"))
		if d.Vendor == "" {
			d.Vendor = ids.Vendor(d.VendorID)
		}
		if d.Product == "" {
			d.Product = ids.Device(d.VendorID, d.ProductID)
		}
		for _, ie := range entries {
			iname := ie.Name()
			if !strings.HasPrefix(iname, name+":") {
				continue
			}
			ip := path.Join(dir, iname)
			b := s.binding("usb", ip, iname, aliases)
			b.Class = s.hexAttr(path.Join(ip, "bInterfaceClass"))
			b.ClassName = ids.Class(b.Class)
			d.Bindings = append(d.Bindings, b)
		}
		if len(d.Bindings) > 0 {
			d.ClassName = d.Bindings[0].ClassName
		}
		devs = append(devs, d)
	}
	return devs, op
}

// binding resolves the driver and module bound to a sysfs device directory.
// The module comes from /sys/bus/<bus>/drivers/<driver>/module rather than
// from following the device's driver link, so dangling fixture links work.
func (s *Source) binding(bus, devPath, name string, aliases *AliasIndex) Binding {
	b := Binding{Path: name}
	b.Modalias, _ = s.readString(path.Join(devPath, "modalias"))
	drv, err := s.linkName(path.Join(devPath, "driver"))
	if err != nil {
		b.Candidates = aliases.Lookup(b.Modalias)
		return b
	}
	b.Driver = drv
	mod, err := s.linkName(path.Join("/sys/bus", bus, "drivers", drv, "module"))
	if err != nil {
		b.Builtin = true // e.g. the generic "usb" driver: no module at all
		return b
	}
	b.Module = mod
	// Loadable modules have an initstate file; built-in ones never do.
	b.Builtin = !s.exists(path.Join("/sys/module", mod, "initstate"))
	return b
}

// hexAttr reads a sysfs hex attribute such as "0x1002" as "1002".
func (s *Source) hexAttr(p string) string {
	v, _ := s.readString(p)
	return strings.ToLower(strings.TrimPrefix(v, "0x"))
}
