package ui

import (
	"testing"

	"github.com/jesseduffield/gocui"

	"github.com/cristianradulescu/kmodtui/internal/kmod"
)

func TestAllFilesTab(t *testing.T) {
	a := newTestApp(t, fixtureRoot, 140, 50)
	v, _ := a.g.View(viewBoot)
	_, y0, _, y1 := v.Dimensions()
	collapsed := y1 - y0

	a.mustPress(t, '3', ']')
	assertContains(t, "screen", a.screen(), "[3]─Boot config - All files")
	_, y0, _, y1 = v.Dimensions()
	if y1-y0 <= collapsed {
		t.Errorf("Boot config panel did not grow when focused: %d rows, collapsed %d", y1-y0, collapsed)
	}
	rows := a.viewText(t, viewBoot)
	assertContains(t, "all files", rows,
		"options snd-pcsp index=-2 aliases.conf",
		"blacklist usbmouse", "blacklist.conf",
		// Long entries keep their text and drop the file name (Details has it).
		"remove iwlwifi (/sbin/lsmod",
		"softdep btusb post: rfcomm",
		"load at boot lp", "cups-filters.conf",
		"blacklist vga16fb")
	assertNotContains(t, "all files", rows, "arkfb", "must-not-appear")

	// Filter the tab, then clear it.
	a.mustPress(t, '/')
	a.typeText(t, "usb")
	a.mustPress(t, gocui.KeyEnter)
	assertContains(t, "filtered", a.viewText(t, viewBoot), "blacklist usbmouse", "blacklist usbkbd", "softdep btusb")
	assertNotContains(t, "filtered", a.viewText(t, viewBoot), "iwlwifi")
	assertContains(t, "footer", a.screen(), "filter: usb")

	// Entries in other files are read-only.
	a.allList.selectKey("/etc/modprobe.d/blacklist.conf:9")
	a.syncBootSelection()
	a.renderDetails()
	a.renderBar()
	assertContains(t, "details", a.viewText(t, viewDetails), "blacklist usbmouse", "/etc/modprobe.d/blacklist.conf (line 9)",
		"not managed by kmodtui; edit by hand with sudoedit /etc/modprobe.d/blacklist.conf", "$ sed -n '9p' /etc/modprobe.d/blacklist.conf")
	assertNotContains(t, "bar", a.viewText(t, viewOptions), "Remove: d")
	a.mustPress(t, 'd')
	if a.topPopup() != "" {
		t.Errorf("d opened %q for a file kmodtui does not manage", a.topPopup())
	}

	a.mustPress(t, gocui.KeyEsc)
	assertNotContains(t, "footer", a.screen(), "filter: usb")
	// The kmodtui tab keeps its own (empty) filter.
	a.mustPress(t, '[')
	assertContains(t, "kmodtui tab", a.viewText(t, viewBoot), "blacklist", "uvcvideo", "vboxdrv")
}

func TestModuleConfigAndWhy(t *testing.T) {
	a := newTestApp(t, fixtureRoot, 140, 60)

	a.selectModule(t, "rfcomm")
	assertContains(t, "rfcomm", a.viewText(t, viewDetails),
		"Why loaded", "soft dependency of btusb (/etc/modprobe.d/bluetooth-local.conf)",
		"Config", "softdep pulled in by btusb (post: rfcomm) /etc/modprobe.d/bluetooth-local.conf:2")

	a.selectModule(t, "vboxdrv")
	assertContains(t, "vboxdrv", a.viewText(t, viewDetails),
		"loaded at boot: listed in /etc/modules-load.d/kmodtui.conf",
		"needed by vboxnetadp, vboxnetflt",
		"load at boot /etc/modules-load.d/kmodtui.conf:2 (kmodtui)")

	a.selectModule(t, "cfg80211")
	assertContains(t, "cfg80211", a.viewText(t, viewDetails),
		"needed by mt7925_common, mt76_connac_lib, mt76, mac80211",
		"for mt7925e, which drives network device pci 0000:c1:00.0")

	a.selectModule(t, "bridge")
	assertContains(t, "bridge", a.viewText(t, viewDetails), "no device, dependent module or config entry explains it")
}

func TestKernelLogInDetails(t *testing.T) {
	a := newTestApp(t, fixtureRoot, 140, 60)
	a.selectModule(t, "nvme")
	assertContains(t, "nvme", a.viewText(t, viewDetails),
		"Kernel log (this boot, newest first)",
		"Oct 31 07:48:02 nvme nvme0: 16/0/0 default/read/poll queues",
		"Oct 24 18:24:08 nvme 0000:bf:00.0: platform quirk: setting simple suspend",
		`$ journalctl -k -b -r -n 20 --grep '\b(?:nvme)\b'`)
	assertContains(t, "log", a.logText(), "# Kernel messages about nvme")

	a.selectModule(t, "e1000e")
	assertContains(t, "e1000e", a.viewText(t, viewDetails), "No kernel messages mention e1000e in this boot.", "adm or systemd-journal")

	a.mustPress(t, ']')
	a.devs.selectKey("pci0000:c2:00.0")
	a.syncModulesSelection()
	a.renderDetails()
	assertContains(t, "device", a.viewText(t, viewDetails), "amdgpu 0000:c2:00.0: enabling device (0006 -> 0007)")
}

func TestBlacklistNotesOtherFiles(t *testing.T) {
	a := newTestApp(t, fixtureRoot, 140, 45)
	a.allConf = append(a.allConf, kmod.BootEntry{Kind: kmod.BootBlacklist, Module: "btusb", File: "/etc/modprobe.d/blacklist-bt.conf", Line: 1})
	on := a.blacklistAction("btusb", true)
	assertContains(t, "notes", stripSGR(flat(joinNotes(on))), "Already blacklisted in /etc/modprobe.d/blacklist-bt.conf:1")
	off := a.blacklistAction("btusb", false)
	assertContains(t, "notes", stripSGR(flat(joinNotes(off))), "btusb stays blacklisted by /etc/modprobe.d/blacklist-bt.conf:1")
}

func joinNotes(act *action) string {
	s := ""
	for _, n := range act.notes {
		s += n + "\n"
	}
	return s
}

func TestAliasRowsReadPatternToModule(t *testing.T) {
	a := newTestApp(t, fixtureRoot, 140, 50)
	a.allConf = append(a.allConf, kmod.BootEntry{Kind: kmod.BootAlias, Module: "off", Args: "net-pf-3", File: "/etc/modprobe.d/blacklist-rare-network.conf", Line: 3})
	a.allList.set(a.allConf)
	a.mustPress(t, '3', ']')
	assertContains(t, "alias", a.viewText(t, viewBoot), "alias net-pf-3 → off")
}
