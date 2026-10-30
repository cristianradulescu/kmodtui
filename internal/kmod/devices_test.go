package kmod

import (
	"testing"
)

func TestDevicesFixture(t *testing.T) {
	src, _ := fixtureSource(t)
	aliases, op := src.LoadAliasIndex(release)
	if op.Err != nil {
		t.Fatal(op.Err)
	}
	devs, ops := src.Devices(aliases)
	for _, op := range ops {
		if op.Err != nil {
			t.Fatalf("%s: %v", op.Desc, op.Err)
		}
	}
	assertEqual(t, "commands", []string{ops[0].Cmd, ops[1].Cmd}, []string{"lspci -nnk", "lsusb -t"})

	byAddr := map[string]Device{}
	var addrs []string
	for _, d := range devs {
		byAddr[d.Addr] = d
		addrs = append(addrs, d.Addr)
	}
	// Root hubs (usb1) are skipped; interfaces are folded into their device.
	assertEqual(t, "addresses", addrs, []string{
		"0000:00:00.0", "0000:00:01.1", "0000:bf:00.0", "0000:c1:00.0",
		"0000:c2:00.0", "0000:c2:00.1", "0000:c2:00.4", "0000:c3:00.1",
		"1-1", "3-5",
	})

	tests := []struct {
		addr       string
		vendor     string
		product    string
		className  string
		drivers    []string
		candidates []string
		unbound    bool
		binding0   Binding
	}{
		{
			addr:      "0000:c2:00.0",
			vendor:    "Advanced Micro Devices, Inc. [AMD/ATI]",
			product:   "Krackan [Radeon 840M / 860M Graphics]",
			className: "Display controller",
			drivers:   []string{"amdgpu"},
			binding0: Binding{Path: "0000:c2:00.0", Modalias: "pci:v00001002d00001114sv000017AAsd00003801bc03sc80i00",
				Class: "038000", ClassName: "Display controller", Driver: "amdgpu", Module: "amdgpu"},
		},
		{
			// Driver name differs from the module, and the module is built in.
			addr:      "0000:c2:00.4",
			vendor:    "Advanced Micro Devices, Inc. [AMD]",
			className: "USB controller",
			drivers:   []string{"xhci_pci"},
			binding0: Binding{Path: "0000:c2:00.4", Modalias: "pci:v00001022d00001128sv000017AAsd00003817bc0Csc03i30",
				Class: "0c0330", ClassName: "USB controller", Driver: "xhci_hcd", Module: "xhci_pci", Builtin: true},
		},
		{
			// Built-in driver whose "module" is not in modules.builtin.
			addr:      "0000:00:01.1",
			vendor:    "Advanced Micro Devices, Inc. [AMD]",
			className: "PCI bridge",
			drivers:   []string{"pcieportdrv"},
			binding0: Binding{Path: "0000:00:01.1", Modalias: "pci:v00001022d00001125sv00001022sd00001125bc06sc04i00",
				Class: "060400", ClassName: "PCI bridge", Driver: "pcieport", Module: "pcieportdrv", Builtin: true},
		},
		{
			// No driver, no matching alias.
			addr:      "0000:00:00.0",
			vendor:    "Advanced Micro Devices, Inc. [AMD]",
			product:   "Krackan Root Complex",
			className: "Host bridge",
			unbound:   true,
			binding0: Binding{Path: "0000:00:00.0", Modalias: "pci:v00001022d00001122sv000017AAsd0000380Dbc06sc00i00",
				Class: "060000", ClassName: "Host bridge"},
		},
		{
			// No driver, module found through modules.alias.
			addr:       "0000:c3:00.1",
			vendor:     "Advanced Micro Devices, Inc. [AMD]",
			product:    "Strix/Krackan/Strix Halo Neural Processing Unit",
			className:  "Signal processing controller",
			candidates: []string{"amdxdna"},
			unbound:    true,
			binding0: Binding{Path: "0000:c3:00.1", Modalias: "pci:v00001022d000017F0sv000017AAsd00003820bc11sc80i00",
				Class: "118000", ClassName: "Signal processing controller", Candidates: []string{"amdxdna"}},
		},
		{
			// USB: names from sysfs; one interface has no driver.
			addr:      "1-1",
			vendor:    "Syntek", // no manufacturer attribute: falls back to usb.ids
			product:   "Integrated Camera",
			className: "Video",
			drivers:   []string{"uvcvideo"},
			unbound:   true,
			binding0: Binding{Path: "1-1:1.0", Modalias: "usb:v174Fp1826d1010dcEFdsc02dp01ic0Eisc01ip01in00",
				Class: "0e", ClassName: "Video", Driver: "uvcvideo", Module: "uvcvideo"},
		},
		{
			addr:      "3-5",
			vendor:    "MediaTek Inc.",
			product:   "Wireless_Device",
			className: "Wireless",
			drivers:   []string{"btusb"},
			binding0: Binding{Path: "3-5:1.0", Modalias: "usb:v0489pE111d0100dcEFdsc02dp01icE0isc01ip01in00",
				Class: "e0", ClassName: "Wireless", Driver: "btusb", Module: "btusb"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			d, ok := byAddr[tt.addr]
			if !ok {
				t.Fatalf("device %s not found", tt.addr)
			}
			assertEqual(t, "vendor", d.Vendor, tt.vendor)
			assertEqual(t, "product", d.Product, tt.product)
			assertEqual(t, "class", d.ClassName, tt.className)
			assertEqual(t, "drivers", d.Drivers(), tt.drivers)
			assertEqual(t, "candidates", d.Candidates(), tt.candidates)
			assertEqual(t, "unbound", d.Unbound(), tt.unbound)
			assertEqual(t, "binding[0]", d.Bindings[0], tt.binding0)
		})
	}

	assertEqual(t, "camera interfaces", len(byAddr["1-1"].Bindings), 3)

	byMod := DevicesByModule(devs)
	assertEqual(t, "uvcvideo devices", len(byMod["uvcvideo"]), 1) // three interfaces, one device
	assertEqual(t, "amdgpu device", byMod["amdgpu"][0].Addr, "0000:c2:00.0")
}

func TestDevicesEmptyRoot(t *testing.T) {
	devs, ops := emptySource(t).Devices(nil)
	if ops[0].Err == nil {
		t.Error("want error for missing /sys/bus/pci/devices")
	}
	if ops[1].Err != nil {
		t.Errorf("missing USB bus should not be an error: %v", ops[1].Err)
	}
	if len(devs) != 0 {
		t.Errorf("devs = %v", devs)
	}
}
