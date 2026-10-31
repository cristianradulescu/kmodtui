package kmod

import (
	"strings"
	"testing"
)

func TestParseModprobeConf(t *testing.T) {
	const f = ModprobeConf
	tests := []struct {
		name string
		in   string
		want []BootEntry
	}{
		{
			name: "blacklist and options",
			in:   "# Managed by kmodtui\nblacklist pcspkr\noptions snd_hda_intel power_save=1 power_save_controller=Y\n",
			want: []BootEntry{
				{Kind: BootBlacklist, Module: "pcspkr", File: f, Line: 2},
				{Kind: BootOptions, Module: "snd_hda_intel", Args: "power_save=1 power_save_controller=Y", File: f, Line: 3},
			},
		},
		{
			name: "line continuation keeps the first line number",
			in:   "\noptions iwlwifi \\\n  power_save=1\n",
			want: []BootEntry{{Kind: BootOptions, Module: "iwlwifi", Args: "power_save=1", File: f, Line: 2}},
		},
		{
			name: "install, remove, softdep and alias",
			in:   "install floppy /bin/false\nsoftdep nvme pre: nvme_core\nalias char-major-10-229 fuse\nremove iwlwifi \\\n(/sbin/lsmod | grep -o ^iwlmvm | xargs /sbin/rmmod) \\\n&& /sbin/modprobe -r mac80211\n",
			want: []BootEntry{
				{Kind: BootInstall, Module: "floppy", Args: "/bin/false", File: f, Line: 1},
				{Kind: BootSoftdep, Module: "nvme", Args: "pre: nvme_core", File: f, Line: 2},
				{Kind: BootAlias, Module: "fuse", Args: "char-major-10-229", File: f, Line: 3},
				{Kind: BootRemove, Module: "iwlwifi", Args: "(/sbin/lsmod | grep -o ^iwlmvm | xargs /sbin/rmmod) && /sbin/modprobe -r mac80211", File: f, Line: 4},
			},
		},
		{
			name: "unknown command kept verbatim",
			in:   "frobnicate nvme now\n",
			want: []BootEntry{{Kind: BootOther, Module: "nvme", Args: "frobnicate nvme now", File: f, Line: 1}},
		},
		{
			name: "indented comment and blank lines",
			in:   "   # comment\n\n\t\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseModprobeConf(strings.NewReader(tt.in), f)
			if err != nil {
				t.Fatal(err)
			}
			assertEqual(t, "entries", got, tt.want)
		})
	}
}

func TestParseModulesLoad(t *testing.T) {
	const f = ModulesLoadConf
	tests := []struct {
		name    string
		in      string
		want    []BootEntry
		wantErr bool
	}{
		{
			name: "modules and both comment styles",
			in:   "# Managed by kmodtui\n; also a comment\nvboxdrv\n\n  tcp_bbr  \n",
			want: []BootEntry{
				{Kind: BootLoad, Module: "vboxdrv", File: f, Line: 3},
				{Kind: BootLoad, Module: "tcp_bbr", File: f, Line: 5},
			},
		},
		{
			name:    "two words on a line",
			in:      "vboxdrv\nvboxnetflt vboxnetadp\n",
			want:    []BootEntry{{Kind: BootLoad, Module: "vboxdrv", File: f, Line: 1}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseModulesLoad(strings.NewReader(tt.in), f)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			assertEqual(t, "entries", got, tt.want)
		})
	}
}

func TestBootConfig(t *testing.T) {
	src, _ := fixtureSource(t)
	entries, op := src.BootConfig()
	if op.Err != nil {
		t.Fatal(op.Err)
	}
	assertEqual(t, "entries", entries, []BootEntry{
		{Kind: BootBlacklist, Module: "pcspkr", File: ModprobeConf, Line: 2},
		{Kind: BootBlacklist, Module: "uvcvideo", File: ModprobeConf, Line: 3},
		{Kind: BootLoad, Module: "vboxdrv", File: ModulesLoadConf, Line: 2},
	})

	entries, op = emptySource(t).BootConfig()
	if op.Err != nil || len(entries) != 0 {
		t.Errorf("missing files: entries=%v err=%v, want none", entries, op.Err)
	}
}
