package kmod

import (
	"testing"
)

const hdr = ConfHeader + "\n"

func TestValidate(t *testing.T) {
	tests := []struct {
		kind  string
		in    string
		valid bool
	}{
		{"module", "nvme", true},
		{"module", "snd-hda-intel", true},
		{"module", "8021q", true},
		{"module", "_x", true},
		{"module", "-r", false}, // would be an option to modprobe
		{"module", "--help", false},
		{"module", "", false},
		{"module", "nvme core", false},
		{"module", "../etc", false},
		{"module", "nvme\n", false},
		{"module", "a234567890123456789012345678901234567890123456789012345", true},
		{"module", "a2345678901234567890123456789012345678901234567890123456789", false},
		{"param", "power_save", true},
		{"param", "power-save", false},
		{"param", "x=1", false},
		{"param", "", false},
		{"value", "-1", true},
		{"value", "Y", true},
		{"value", "1,2,3", true},
		{"value", "VID:DID:quirk_names", true},
		{"value", "a b", false},
		{"value", "1\noptions evil x=1", false},
		{"value", "a=b", false},
		{"value", "'1'", false},
		{"value", "#1", false},
		{"value", "", false},
	}
	for _, tt := range tests {
		var err error
		switch tt.kind {
		case "module":
			err = ValidModuleName(tt.in)
		case "param":
			err = ValidParamName(tt.in)
		case "value":
			err = ValidParamValue(tt.in)
		}
		if (err == nil) != tt.valid {
			t.Errorf("%s %q: err = %v, want valid=%v", tt.kind, tt.in, err, tt.valid)
		}
	}
}

func TestEditBlacklist(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		module  string
		on      bool
		want    string
		changed bool
	}{
		{"add to missing file", "", "pcspkr", true, hdr + "blacklist pcspkr\n", true},
		{"add keeps other lines", hdr + "options snd_hda_intel power_save=1\n", "pcspkr", true,
			hdr + "options snd_hda_intel power_save=1\nblacklist pcspkr\n", true},
		{"already present", hdr + "blacklist pcspkr\n", "pcspkr", true, hdr + "blacklist pcspkr\n", false},
		{"dash and underscore are the same module", hdr + "blacklist snd_pcsp\n", "snd-pcsp", true, hdr + "blacklist snd_pcsp\n", false},
		{"remove", hdr + "blacklist pcspkr\nblacklist uvcvideo\n", "pcspkr", false, hdr + "blacklist uvcvideo\n", true},
		{"remove duplicates", hdr + "blacklist pcspkr\n# note\nblacklist pcspkr\n", "pcspkr", false, hdr + "# note\n", true},
		{"remove absent", hdr + "blacklist uvcvideo\n", "pcspkr", false, hdr + "blacklist uvcvideo\n", false},
		{"other commands untouched", hdr + "install pcspkr /bin/false\n", "pcspkr", false, hdr + "install pcspkr /bin/false\n", false},
		{"continuation lines untouched", hdr + "options x \\\nblacklist pcspkr\n", "pcspkr", false, hdr + "options x \\\nblacklist pcspkr\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := EditBlacklist(tt.in, tt.module, tt.on)
			assertEqual(t, "content", got, tt.want)
			assertEqual(t, "changed", changed, tt.changed)
		})
	}
}

func TestEditBootLoad(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		module  string
		on      bool
		want    string
		changed bool
	}{
		{"add to missing file", "", "vboxdrv", true, hdr + "vboxdrv\n", true},
		{"add", hdr + "vboxdrv\n", "tcp_bbr", true, hdr + "vboxdrv\ntcp_bbr\n", true},
		{"already present", hdr + "vboxdrv\n", "vboxdrv", true, hdr + "vboxdrv\n", false},
		{"remove", hdr + "vboxdrv\ntcp_bbr\n", "vboxdrv", false, hdr + "tcp_bbr\n", true},
		{"remove keeps comments", hdr + "; comment\nvboxdrv\n", "vboxdrv", false, hdr + "; comment\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := EditBootLoad(tt.in, tt.module, tt.on)
			assertEqual(t, "content", got, tt.want)
			assertEqual(t, "changed", changed, tt.changed)
		})
	}
}

func TestEditOption(t *testing.T) {
	tests := []struct {
		name         string
		in           string
		param, value string
		want         string
		changed      bool
	}{
		{"new line", "", "power_save", "1", hdr + "options snd_hda_intel power_save=1\n", true},
		{"replace value", hdr + "options snd_hda_intel power_save=1\n", "power_save", "0",
			hdr + "options snd_hda_intel power_save=0\n", true},
		{"same value", hdr + "options snd_hda_intel power_save=1\n", "power_save", "1",
			hdr + "options snd_hda_intel power_save=1\n", false},
		{"append to existing line", hdr + "options snd_hda_intel power_save=1\n", "probe_mask", "8",
			hdr + "options snd_hda_intel power_save=1 probe_mask=8\n", true},
		{"other modules untouched", hdr + "options iwlwifi power_save=1\n", "power_save", "1",
			hdr + "options iwlwifi power_save=1\noptions snd_hda_intel power_save=1\n", true},
		{"duplicates across lines collapse", hdr + "options snd_hda_intel power_save=1\noptions snd-hda-intel power_save=2 probe_mask=8\n", "power_save", "0",
			hdr + "options snd_hda_intel power_save=0\noptions snd-hda-intel probe_mask=8\n", true},
		{"remove keeps the line", hdr + "options snd_hda_intel power_save=1 probe_mask=8\n", "power_save", "",
			hdr + "options snd_hda_intel probe_mask=8\n", true},
		{"remove last param drops the line", hdr + "blacklist pcspkr\noptions snd_hda_intel power_save=1\n", "power_save", "",
			hdr + "blacklist pcspkr\n", true},
		{"remove absent", hdr + "options snd_hda_intel probe_mask=8\n", "power_save", "",
			hdr + "options snd_hda_intel probe_mask=8\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := EditOption(tt.in, "snd_hda_intel", tt.param, tt.value)
			assertEqual(t, "content", got, tt.want)
			assertEqual(t, "changed", changed, tt.changed)
		})
	}
}

// What the editors write, the parsers read back.
func TestEditRoundTrip(t *testing.T) {
	c, _ := EditBlacklist("", "pcspkr", true)
	c, _ = EditOption(c, "snd_hda_intel", "power_save", "1")
	entries, err := ParseModprobeConf(stringsReader(c), ModprobeConf)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "entries", entries, []BootEntry{
		{Kind: BootBlacklist, Module: "pcspkr", File: ModprobeConf, Line: 2},
		{Kind: BootOptions, Module: "snd_hda_intel", Args: "power_save=1", File: ModprobeConf, Line: 3},
	})
	assertEqual(t, "options", Options(entries, "snd-hda-intel"), []string{"power_save=1"})
}
