package kmod

import (
	"path"
	"testing"
)

func TestAllConfig(t *testing.T) {
	src, _ := fixtureSource(t)
	all, op := src.AllConfig()
	if op.Err != nil {
		t.Fatal(op.Err)
	}
	assertEqual(t, "cmd", op.Cmd, "systemd-analyze cat-config modprobe.d; systemd-analyze cat-config modules-load.d")

	// Files come in file-name order across directories, /etc winning over
	// /usr/lib, and /usr/lib over /lib, for the same name; non-.conf files
	// are ignored.
	var files []string
	for _, e := range all {
		if len(files) == 0 || files[len(files)-1] != e.File {
			files = append(files, e.File)
		}
	}
	assertEqual(t, "files", files, []string{
		"/usr/lib/modprobe.d/aliases.conf",
		"/etc/modprobe.d/alsa-base.conf",
		"/etc/modprobe.d/blacklist.conf",
		"/etc/modprobe.d/bluetooth-local.conf",
		"/etc/modprobe.d/fbdev-blacklist.conf",
		"/etc/modprobe.d/iwlwifi.conf",
		"/etc/modprobe.d/kmodtui.conf",
		"/usr/lib/modprobe.d/systemd.conf",
		"/etc/modules-load.d/cups-filters.conf",
		"/usr/lib/modules-load.d/ddcutil.conf",
		"/usr/lib/modules-load.d/fwupd-i2c.conf",
		"/etc/modules-load.d/kmodtui.conf",
		// modules.conf is a symlink to /etc/modules, which has only comments.
	})

	tests := []struct {
		module string
		want   []BootEntry
	}{
		{"usbmouse", []BootEntry{{Kind: BootBlacklist, Module: "usbmouse", File: "/etc/modprobe.d/blacklist.conf", Line: 9}}},
		// The /etc file of the same name hides the packaged one.
		{"vga16fb", []BootEntry{{Kind: BootBlacklist, Module: "vga16fb", File: "/etc/modprobe.d/fbdev-blacklist.conf", Line: 2}}},
		{"arkfb", nil},
		{"iwlwifi", []BootEntry{{Kind: BootRemove, Module: "iwlwifi", File: "/etc/modprobe.d/iwlwifi.conf", Line: 5,
			Args: "(/sbin/lsmod | grep -o -e ^iwlmvm -e ^iwldvm -e ^iwlwifi | xargs /sbin/rmmod) && /sbin/modprobe -r mac80211"}}},
		{"dummy", []BootEntry{{Kind: BootOptions, Module: "dummy", Args: "numdummies=0", File: "/usr/lib/modprobe.d/systemd.conf", Line: 20}}},
		{"snd-pcsp", []BootEntry{{Kind: BootOptions, Module: "snd-pcsp", Args: "index=-2", File: "/usr/lib/modprobe.d/aliases.conf", Line: 5}}},
		// Both spellings in modules-load.d.
		{"i2c_dev", []BootEntry{
			{Kind: BootLoad, Module: "i2c-dev", File: "/usr/lib/modules-load.d/ddcutil.conf", Line: 1},
			{Kind: BootLoad, Module: "i2c_dev", File: "/usr/lib/modules-load.d/fwupd-i2c.conf", Line: 1},
		}},
		{"fuse", nil}, // the /lib copy of aliases.conf is hidden
	}
	for _, tt := range tests {
		t.Run(tt.module, func(t *testing.T) {
			assertEqual(t, "entries", ConfigFor(all, tt.module), tt.want)
		})
	}

	sd := SoftdepsOn(all, "rfcomm")
	if len(sd) != 1 || sd[0].Module != "btusb" || path.Base(sd[0].File) != "bluetooth-local.conf" {
		t.Errorf("softdeps on rfcomm = %#v", sd)
	}
	if len(SoftdepsOn(all, "btusb")) != 0 {
		t.Error("btusb is the module declaring the softdep, not a target")
	}
}

func TestAllConfigEmptyRoot(t *testing.T) {
	all, op := emptySource(t).AllConfig()
	if op.Err != nil || len(all) != 0 {
		t.Errorf("all = %v, err = %v; want nothing and no error", all, op.Err)
	}
}
