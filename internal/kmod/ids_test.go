package kmod

import (
	"os"
	"strings"
	"testing"
)

func TestParseIDs(t *testing.T) {
	in := `#	List of PCI IDs
1002  Advanced Micro Devices, Inc. [AMD/ATI]
	1114  Krackan [Radeon 840M / 860M Graphics]
		17aa 380d  ThinkPad subsystem (ignored)
	1640  Radeon High Definition Audio Controller
15b7  Sandisk Corp
	5044  WD PC SN7100S M.2 2242 NVMe SSD (DRAM-less)
C 03  Display controller
	00  VGA compatible controller
		00  VGA controller (prog-if, ignored)
	80  Display controller
C 01  Mass storage controller
	08  Non-Volatile memory controller
HID 01  Pointer
	01  not a device line
`
	db, err := ParseIDs(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, got, want string
	}{
		{"vendor", db.Vendor("1002"), "Advanced Micro Devices, Inc. [AMD/ATI]"},
		{"vendor upper-case id", db.Vendor("15B7"), "Sandisk Corp"},
		{"device", db.Device("1002", "1114"), "Krackan [Radeon 840M / 860M Graphics]"},
		{"device under second vendor", db.Device("15b7", "5044"), "WD PC SN7100S M.2 2242 NVMe SSD (DRAM-less)"},
		{"unknown device", db.Device("1002", "ffff"), ""},
		{"subclass", db.Class("038000"), "Display controller"},
		{"subclass with 0x", db.Class("0x010802"), "Non-Volatile memory controller"},
		{"class only", db.Class("03"), "Display controller"},
		{"unknown subclass falls back to class", db.Class("0377"), "Display controller"},
		{"unknown class falls back to builtin table", db.Class("0d1100"), "Wireless controller"},
		{"keyword sections are not vendors", db.Device("01", "01"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertEqual(t, tt.name, tt.got, tt.want)
		})
	}
}

func TestParseIDsFixtures(t *testing.T) {
	tests := []struct {
		file, vendor, device, wantVendor, wantDevice string
	}{
		{"pci.ids", "14c3", "7925", "MEDIATEK Corp.", "MT7925 802.11be 160MHz 2x2 PCIe Wireless Network Adapter [Filogic 360]"},
		{"usb.ids", "1d6b", "0003", "Linux Foundation", "3.0 root hub"},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			f, err := os.Open("testdata/root/usr/share/misc/" + tt.file)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			db, err := ParseIDs(f)
			if err != nil {
				t.Fatal(err)
			}
			assertEqual(t, "vendor", db.Vendor(tt.vendor), tt.wantVendor)
			assertEqual(t, "device", db.Device(tt.vendor, tt.device), tt.wantDevice)
		})
	}
}

func TestNilIDDB(t *testing.T) {
	var db *IDDB
	assertEqual(t, "vendor", db.Vendor("1002"), "")
	assertEqual(t, "class fallback", db.Class("020000"), "Network controller")
}
