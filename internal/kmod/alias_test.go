package kmod

import (
	"strings"
	"testing"
)

func TestAliasLookup(t *testing.T) {
	in := `# Aliases extracted from modules themselves.
alias pci:v*d*sv*sd*bc01sc08i02* nvme
alias pci:v00001002d*sv*sd*bc03sc80i00* amdgpu
alias pci:v00001022d000017F0sv*sd*bc*sc*i* amdxdna
alias usb:v*p*d*dc*dsc*dp*icE0isc01ip01in* btusb
alias usb:v0489pE111d*dc*dsc*dp*ic*isc*ip*in* btusb
alias usb:v*p*d*dc*dsc*dp*ic0Eisc01ip00in* uvcvideo
alias usb:v19ABp1000d01[0-1]*dc*dsc*dp*ic0Eisc01ip00in* uvcvideo
alias usb:v*p*d*dc*dsc*dp*icE0isc01ip01in* btusb-compat
alias fs-vfat vfat
malformed line
`
	idx, err := ParseModulesAlias(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		modalias string
		want     []string
	}{
		{"nvme by class", "pci:v000015B7d00005044sv000015B7sd00005044bc01sc08i02", []string{"nvme"}},
		{"gpu by vendor and class", "pci:v00001002d00001114sv000017AAsd0000380Dbc03sc80i00", []string{"amdgpu"}},
		{"npu by exact id", "pci:v00001022d000017F0sv000017AAsd00003820bc11sc80i00", []string{"amdxdna"}},
		{"several patterns, deduped and normalized", "usb:v0489pE111d0100dcEFdsc02dp01icE0isc01ip01in00", []string{"btusb", "btusb_compat"}},
		{"character class", "usb:v19ABp1000d0110dcEFdsc02dp01ic0Eisc01ip00in00", []string{"uvcvideo"}},
		{"no match", "pci:v00001022d00001122sv000017AAsd0000380Dbc06sc00i00", nil},
		{"other bus is not scanned", "acpi:AMDI0010:", nil},
		{"empty", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertEqual(t, "modules", idx.Lookup(tt.modalias), tt.want)
		})
	}
}

func TestLoadAliasIndex(t *testing.T) {
	src, _ := fixtureSource(t)
	idx, op := src.LoadAliasIndex(release)
	if op.Err != nil {
		t.Fatal(op.Err)
	}
	// The unbound NPU in the fixture tree.
	assertEqual(t, "npu", idx.Lookup("pci:v00001022d000017F0sv000017AAsd00003820bc11sc80i00"), []string{"amdxdna"})

	_, op = emptySource(t).LoadAliasIndex(release)
	if op.Err == nil {
		t.Error("want error for missing modules.alias")
	}
}
