package kmod

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
)

const release = "7.0.0-38-generic"

func TestModuleNameFromPath(t *testing.T) {
	tests := []struct{ in, want string }{
		{"kernel/drivers/nvme/host/nvme-core.ko.zst", "nvme_core"},
		{"kernel/drivers/usb/host/xhci-hcd.ko", "xhci_hcd"},
		{"kernel/fs/fat/vfat.ko.xz", "vfat"},
		{"updates/dkms/vboxdrv.ko.gz", "vboxdrv"},
		{"snd-hda-intel.ko.zst", "snd_hda_intel"},
	}
	for _, tt := range tests {
		assertEqual(t, tt.in, ModuleNameFromPath(tt.in), tt.want)
	}
}

func TestParseModulesDep(t *testing.T) {
	in := `kernel/crypto/hkdf.ko.zst:
kernel/drivers/nvme/common/nvme-auth.ko.zst: kernel/crypto/hkdf.ko.zst
kernel/drivers/nvme/common/nvme-keyring.ko.zst:
kernel/drivers/nvme/host/nvme-core.ko.zst: kernel/drivers/nvme/common/nvme-keyring.ko.zst kernel/drivers/nvme/common/nvme-auth.ko.zst kernel/crypto/hkdf.ko.zst
kernel/drivers/nvme/host/nvme.ko.zst: kernel/drivers/nvme/host/nvme-core.ko.zst kernel/drivers/nvme/common/nvme-keyring.ko.zst kernel/drivers/nvme/common/nvme-auth.ko.zst kernel/crypto/hkdf.ko.zst
`
	idx, err := ParseModulesDep(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		has        bool
		path       string
		deps       []string
		loadOrder  []string
		requiredBy []string
	}{
		{
			name:      "nvme",
			has:       true,
			path:      "kernel/drivers/nvme/host/nvme.ko.zst",
			deps:      []string{"nvme_core", "nvme_keyring", "nvme_auth", "hkdf"},
			loadOrder: []string{"hkdf", "nvme_auth", "nvme_keyring", "nvme_core", "nvme"},
		},
		{
			name:       "nvme-core", // dashed spelling resolves too
			has:        true,
			path:       "kernel/drivers/nvme/host/nvme-core.ko.zst",
			deps:       []string{"nvme_keyring", "nvme_auth", "hkdf"},
			loadOrder:  []string{"hkdf", "nvme_auth", "nvme_keyring", "nvme_core"},
			requiredBy: []string{"nvme"},
		},
		{
			name:       "hkdf",
			has:        true,
			path:       "kernel/crypto/hkdf.ko.zst",
			loadOrder:  []string{"hkdf"},
			requiredBy: []string{"nvme", "nvme_auth", "nvme_core"},
		},
		{
			name:      "missing",
			loadOrder: []string{"missing"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertEqual(t, "has", idx.Has(tt.name), tt.has)
			assertEqual(t, "path", idx.Path(tt.name), tt.path)
			assertEqual(t, "deps", idx.Deps(tt.name), tt.deps)
			assertEqual(t, "load order", idx.LoadOrder(tt.name), tt.loadOrder)
			assertEqual(t, "required by", idx.RequiredBy(tt.name), tt.requiredBy)
		})
	}
	assertEqual(t, "names", idx.Names(), []string{"hkdf", "nvme", "nvme_auth", "nvme_core", "nvme_keyring"})
}

func TestParseModulesDepMalformed(t *testing.T) {
	_, err := ParseModulesDep(strings.NewReader("kernel/a.ko:\nnot a dep line\n"))
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("err = %v, want line 2 error", err)
	}
}

func TestLoadDepIndex(t *testing.T) {
	src, _ := fixtureSource(t)
	idx, ops := src.LoadDepIndex(release)
	for _, op := range ops {
		if op.Err != nil {
			t.Fatalf("%s: %v", op.Desc, op.Err)
		}
	}
	assertEqual(t, "cmd", ops[0].Cmd, "cat /lib/modules/7.0.0-38-generic/modules.dep")
	assertEqual(t, "vboxnetflt deps", idx.Deps("vboxnetflt"), []string{"vboxdrv"})
	assertEqual(t, "vboxdrv required by", idx.RequiredBy("vboxdrv"), []string{"vboxnetadp", "vboxnetflt"})
	assertEqual(t, "builtin", idx.BuiltinNames(), []string{"ext4", "usbcore", "xhci_hcd", "xhci_pci"})
	assertEqual(t, "xhci_pci builtin", idx.IsBuiltin("xhci-pci"), true)
	assertEqual(t, "nvme builtin", idx.IsBuiltin("nvme"), false)
}

func TestLoadDepIndexMissing(t *testing.T) {
	idx, ops := emptySource(t).LoadDepIndex(release)
	if !errors.Is(ops[0].Err, fs.ErrNotExist) {
		t.Errorf("modules.dep err = %v, want not-exist", ops[0].Err)
	}
	// A missing modules.builtin is normal and not reported.
	if ops[1].Err != nil {
		t.Errorf("modules.builtin err = %v, want nil", ops[1].Err)
	}
	if idx == nil || idx.Has("nvme") {
		t.Errorf("want an empty, usable index")
	}
}

func TestEntries(t *testing.T) {
	src, _ := fixtureSource(t)
	loaded, _ := src.LoadedModules()
	idx, _ := src.LoadDepIndex(release)

	names := func(es []Entry, st State) []string {
		var out []string
		for _, e := range es {
			if e.State == st {
				out = append(out, e.Name)
			}
		}
		return out
	}

	only := Entries(loaded, idx, false)
	assertEqual(t, "loaded only count", len(only), len(loaded))
	assertEqual(t, "first", only[0].Name, "amdgpu")
	if only[0].Module == nil || only[0].Module.Size != 21585920 {
		t.Errorf("amdgpu entry lacks /proc/modules data: %#v", only[0].Module)
	}

	all := Entries(loaded, idx, true)
	assertEqual(t, "available", names(all, Available), []string{"amdxdna", "e1000e", "r8169"})
	assertEqual(t, "builtin", names(all, Builtin), []string{"ext4", "usbcore", "xhci_hcd", "xhci_pci"})
	for i := 1; i < len(all); i++ {
		if all[i-1].Name > all[i].Name {
			t.Fatalf("not sorted: %s before %s", all[i-1].Name, all[i].Name)
		}
	}
}
