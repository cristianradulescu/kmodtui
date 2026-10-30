package kmod

import (
	"bufio"
	"bytes"
	"io"
	"strings"
)

// IDDB holds vendor, device and class names from pci.ids / usb.ids.
type IDDB struct {
	vendors map[string]string // "1002" -> name
	devices map[string]string // "1002:1114" -> name
	classes map[string]string // "03" -> class, "0380" -> subclass
}

// ParseIDs parses the pci.ids / usb.ids format:
//
//	vvvv  Vendor name
//	<TAB>dddd  Device name
//	<TAB><TAB>...  (subsystems, ignored)
//	C cc  Class name
//	<TAB>ss  Subclass name
//
// usb.ids has further keyword sections (AT, HID, R, ...) that are skipped.
func ParseIDs(r io.Reader) (*IDDB, error) {
	db := &IDDB{vendors: map[string]string{}, devices: map[string]string{}, classes: map[string]string{}}
	const (
		inNone = iota
		inVendor
		inClass
		inOther
	)
	section, cur := inNone, ""
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		if line[0] != '\t' {
			id, name, ok := splitID(line)
			switch {
			case ok && len(id) == 4 && isHex(id):
				section, cur = inVendor, strings.ToLower(id)
				db.vendors[cur] = name
			case strings.HasPrefix(line, "C "):
				id, name, ok = splitID(line[2:])
				if ok {
					section, cur = inClass, strings.ToLower(id)
					db.classes[cur] = name
				}
			default:
				section = inOther
			}
			continue
		}
		if strings.HasPrefix(line, "\t\t") {
			continue
		}
		id, name, ok := splitID(line[1:])
		if !ok {
			continue
		}
		switch section {
		case inVendor:
			db.devices[cur+":"+strings.ToLower(id)] = name
		case inClass:
			db.classes[cur+strings.ToLower(id)] = name
		}
	}
	return db, sc.Err()
}

// splitID splits "1002  Advanced Micro Devices" at the two-space separator.
func splitID(s string) (id, name string, ok bool) {
	id, name, ok = strings.Cut(s, "  ")
	return strings.TrimSpace(id), strings.TrimSpace(name), ok
}

func isHex(s string) bool {
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return s != ""
}

// Vendor returns the vendor name for a hex ID, or "".
func (db *IDDB) Vendor(vendor string) string {
	if db == nil {
		return ""
	}
	return db.vendors[strings.ToLower(vendor)]
}

// Device returns the device name for vendor and device hex IDs, or "".
func (db *IDDB) Device(vendor, device string) string {
	if db == nil {
		return ""
	}
	return db.devices[strings.ToLower(vendor+":"+device)]
}

// Class returns the most specific name for a class code given as "cc",
// "ccss" or "ccsspp" (PCI's 0x038000 is "038000").
func (db *IDDB) Class(code string) string {
	code = strings.ToLower(strings.TrimPrefix(code, "0x"))
	if db != nil {
		if len(code) >= 4 {
			if n := db.classes[code[:4]]; n != "" {
				return n
			}
		}
		if len(code) >= 2 {
			if n := db.classes[code[:2]]; n != "" {
				return n
			}
		}
	}
	if len(code) >= 2 {
		return pciBaseClasses[code[:2]]
	}
	return ""
}

// pciBaseClasses is the fallback when pci.ids is not installed.
var pciBaseClasses = map[string]string{
	"00": "Unclassified device",
	"01": "Mass storage controller",
	"02": "Network controller",
	"03": "Display controller",
	"04": "Multimedia controller",
	"05": "Memory controller",
	"06": "Bridge",
	"07": "Communication controller",
	"08": "Generic system peripheral",
	"09": "Input device controller",
	"0a": "Docking station",
	"0b": "Processor",
	"0c": "Serial bus controller",
	"0d": "Wireless controller",
	"0e": "Intelligent controller",
	"0f": "Satellite communications controller",
	"10": "Encryption controller",
	"11": "Signal processing controller",
	"12": "Processing accelerators",
	"13": "Non-Essential Instrumentation",
	"40": "Coprocessor",
	"ff": "Unassigned class",
}

// loadIDs reads the first ID database that exists; nil if none does (names
// then fall back to numeric IDs).
func (s *Source) loadIDs(paths ...string) *IDDB {
	for _, p := range paths {
		b, err := s.readFile(p)
		if err != nil {
			continue
		}
		if db, err := ParseIDs(bytes.NewReader(b)); err == nil {
			return db
		}
	}
	return nil
}
