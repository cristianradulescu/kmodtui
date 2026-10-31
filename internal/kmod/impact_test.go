package kmod

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDryRun(t *testing.T) {
	r := &fakeRunner{outputs: map[string]string{
		"modprobe -n -v -d testdata/root e1000e":    "insmod /lib/modules/7.0.0-38-generic/kernel/drivers/net/ethernet/intel/e1000e/e1000e.ko.zst \n",
		"modprobe -n -v -r -d testdata/root bridge": "rmmod bridge\nrmmod stp\nrmmod llc\n",
	}}
	src := &Source{Root: fixtureRoot, Runner: r}
	out, op := src.DryRun("e1000e", false)
	if op.Err != nil {
		t.Fatal(op.Err)
	}
	assertEqual(t, "load cmd", op.Cmd, "modprobe -n -v e1000e")
	assertEqual(t, "load out", out, "insmod /lib/modules/7.0.0-38-generic/kernel/drivers/net/ethernet/intel/e1000e/e1000e.ko.zst")
	out, op = src.DryRun("bridge", true)
	assertEqual(t, "unload cmd", op.Cmd, "modprobe -n -v -r bridge")
	assertEqual(t, "unload out", out, "rmmod bridge\nrmmod stp\nrmmod llc")
}

func TestMounts(t *testing.T) {
	src, _ := fixtureSource(t)
	m, op := src.Mounts()
	if op.Err != nil {
		t.Fatal(op.Err)
	}
	assertEqual(t, "ext4", m["ext4"], []string{"/", "/boot"})
	assertEqual(t, "vfat", m["vfat"], []string{"/boot/efi"})
	assertEqual(t, "fuse", m["fuse.portal"], []string{"/run/user/1000/doc"})
}

func TestInitramfsPending(t *testing.T) {
	base := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		initrd  string // path of the image, "" for none
		imgAge  time.Duration
		confAge time.Duration // 0: no kmodtui.conf
		dirAge  time.Duration
		want    bool
	}{
		{"image newer than config", "boot/initrd.img", 0, 2 * time.Hour, 2 * time.Hour, false},
		{"config edited after the image", "boot/initrd.img", 2 * time.Hour, time.Hour, 3 * time.Hour, true},
		{"file removed after the image (directory changed)", "boot/initrd.img", 2 * time.Hour, 0, time.Hour, true},
		{"per-release image name", "boot/initrd.img-7.0.0-38-generic", 2 * time.Hour, time.Hour, 3 * time.Hour, true},
		{"no initramfs at all", "", 0, time.Hour, time.Hour, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			touch := func(rel string, age time.Duration, dir bool) {
				p := filepath.Join(root, rel)
				if dir {
					if err := os.MkdirAll(p, 0o755); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(p, nil, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				ts := base.Add(-age)
				if err := os.Chtimes(p, ts, ts); err != nil {
					t.Fatal(err)
				}
			}
			if tt.confAge > 0 {
				touch(ModprobeConf, tt.confAge, false)
			}
			touch("etc/modprobe.d", tt.dirAge, true)
			if tt.initrd != "" {
				touch(tt.initrd, tt.imgAge, false)
			}
			got, op := (&Source{Root: root}).InitramfsPending("7.0.0-38-generic")
			if op.Err != nil {
				t.Fatal(op.Err)
			}
			assertEqual(t, "pending", got, tt.want)
		})
	}
}

func TestImpact(t *testing.T) {
	src, _ := fixtureSource(t)
	loaded, _ := src.LoadedModules()
	aliases, _ := src.LoadAliasIndex(release)
	devs, _ := src.Devices(aliases)
	byMod := DevicesByModule(devs)
	mounts, _ := src.Mounts()

	tests := []struct {
		module string
		want   []string
	}{
		{"nvme", []string{
			"in use (use count 3)",
			"drives storage device pci 0000:bf:00.0 (WD PC SN7100S M.2 2242 NVMe SSD (DRAM-less))",
		}},
		// nvme_core drives nothing itself, but nvme depends on it.
		{"nvme_core", []string{
			"in use by nvme",
			"drives storage device pci 0000:bf:00.0 (WD PC SN7100S M.2 2242 NVMe SSD (DRAM-less)) through nvme",
		}},
		{"amdgpu", []string{
			"in use (use count 31)",
			"drives display device pci 0000:c2:00.0 (Krackan [Radeon 840M / 860M Graphics])",
		}},
		{"btusb", []string{"drives wireless device usb 3-5 (Wireless_Device)"}},
		{"mt7925e", []string{"drives network device pci 0000:c1:00.0 (MT7925 802.11be 160MHz 2x2 PCIe Wireless Network Adapter [Filogic 360])"}},
		{"vfat", []string{"provides the vfat filesystem mounted at /boot/efi"}},
		{"fuse", []string{"provides the fuse.portal filesystem mounted at /run/user/1000/doc"}},
		// Unused, drives a camera: nothing to warn about.
		{"e1000e", nil},
	}
	for _, tt := range tests {
		t.Run(tt.module, func(t *testing.T) {
			got := Impact(tt.module, loaded, byMod, mounts)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("Impact(%s) =\n  %q\nwant\n  %q", tt.module, got, tt.want)
			}
		})
	}
}

func TestParamsWritable(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sys/module/nvme/parameters")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, mode := range map[string]os.FileMode{"io_queue_depth": 0o644, "max_host_mem_size_mb": 0o444} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("1\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	params, _ := (&Source{Root: root}).Params("nvme")
	assertEqual(t, "params", params, []Param{
		{Name: "io_queue_depth", Value: "1", Writable: true},
		{Name: "max_host_mem_size_mb", Value: "1"},
	})
}
