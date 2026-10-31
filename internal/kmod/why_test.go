package kmod

import (
	"strings"
	"testing"
)

func TestWhyLoaded(t *testing.T) {
	src, _ := fixtureSource(t)
	loaded, _ := src.LoadedModules()
	aliases, _ := src.LoadAliasIndex(release)
	devs, _ := src.Devices(aliases)
	conf, _ := src.AllConfig()
	mounts, _ := src.Mounts()
	byMod := DevicesByModule(devs)

	tests := []struct {
		module string
		want   []string
	}{
		{"amdgpu", []string{"drives display device pci 0000:c2:00.0 (Krackan [Radeon 840M / 860M Graphics])"}},
		// Needed by others: name them, then walk up to what they are for.
		{"nvme_core", []string{
			"needed by nvme",
			"  for nvme, which drives storage device pci 0000:bf:00.0 (WD PC SN7100S M.2 2242 NVMe SSD (DRAM-less))",
		}},
		{"cfg80211", []string{
			"needed by mt7925_common, mt76_connac_lib, mt76, mac80211",
			// The libraries in between have no reason of their own; the walk
			// continues up to the module that drives the Wi-Fi card.
			"  for mt7925e, which drives network device pci 0000:c1:00.0",
		}},
		{"vboxdrv", []string{
			"loaded at boot: listed in /etc/modules-load.d/kmodtui.conf",
			"needed by vboxnetadp, vboxnetflt",
		}},
		{"rfcomm", []string{"soft dependency of btusb (/etc/modprobe.d/bluetooth-local.conf)"}},
		{"bridge", []string{"no device, dependent module or config entry explains it"}},
		{"e1000e", nil}, // not loaded
	}
	for _, tt := range tests {
		t.Run(tt.module, func(t *testing.T) {
			got := WhyLoaded(tt.module, loaded, byMod, conf, mounts)
			if tt.want == nil {
				if got != nil {
					t.Errorf("got %q, want nil", got)
				}
				return
			}
			joined := strings.Join(got, "\n")
			for _, w := range tt.want {
				if !strings.Contains(joined, w) {
					t.Errorf("WhyLoaded(%s) =\n%s\nmissing %q", tt.module, joined, w)
				}
			}
		})
	}
}
