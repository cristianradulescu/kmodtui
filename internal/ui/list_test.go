package ui

import (
	"strings"
	"testing"
)

func newStringList() *list[string] {
	return newList(func(s string) string { return s }, func(s, q string) bool { return strings.Contains(s, q) })
}

func shownItems(l *list[string]) []string {
	var out []string
	for i := 0; i < l.len(); i++ {
		out = append(out, l.at(i))
	}
	return out
}

func TestList(t *testing.T) {
	tests := []struct {
		name     string
		items    []string
		steps    func(l *list[string])
		shown    []string
		selected string
		footer   string
	}{
		{
			name:     "starts at the top",
			items:    []string{"amdgpu", "btusb", "nvme"},
			steps:    func(l *list[string]) {},
			shown:    []string{"amdgpu", "btusb", "nvme"},
			selected: "amdgpu",
			footer:   "1 of 3",
		},
		{
			name:     "move clamps at both ends",
			items:    []string{"amdgpu", "btusb", "nvme"},
			steps:    func(l *list[string]) { l.move(10); l.move(-1); l.move(-10); l.move(1) },
			shown:    []string{"amdgpu", "btusb", "nvme"},
			selected: "btusb",
			footer:   "2 of 3",
		},
		{
			name:     "filter keeps selection when it still matches",
			items:    []string{"nvme", "nvme_core", "snd"},
			steps:    func(l *list[string]) { l.move(1); l.setFilter("NVME") },
			shown:    []string{"nvme", "nvme_core"},
			selected: "nvme_core",
			footer:   "2 of 2",
		},
		{
			name:     "filter resets selection when it no longer matches",
			items:    []string{"nvme", "nvme_core", "snd"},
			steps:    func(l *list[string]) { l.move(2); l.setFilter("nvme") },
			shown:    []string{"nvme", "nvme_core"},
			selected: "nvme",
			footer:   "1 of 2",
		},
		{
			name:  "no matches",
			items: []string{"nvme"},
			steps: func(l *list[string]) { l.setFilter("zzz"); l.move(1) },
		},
		{
			name:     "set keeps the selected key across reloads",
			items:    []string{"a", "b", "c"},
			steps:    func(l *list[string]) { l.move(1); l.set([]string{"x", "a", "b"}) },
			shown:    []string{"x", "a", "b"},
			selected: "b",
			footer:   "3 of 3",
		},
		{
			name:     "clearing the filter keeps the selection",
			items:    []string{"a", "bb", "c", "bd"},
			steps:    func(l *list[string]) { l.setFilter("b"); l.move(1); l.setFilter("") },
			shown:    []string{"a", "bb", "c", "bd"},
			selected: "bd",
			footer:   "4 of 4",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newStringList()
			l.set(tt.items)
			tt.steps(l)
			if got := shownItems(l); strings.Join(got, ",") != strings.Join(tt.shown, ",") {
				t.Errorf("shown = %v, want %v", got, tt.shown)
			}
			got, _ := l.selected()
			if got != tt.selected {
				t.Errorf("selected = %q, want %q", got, tt.selected)
			}
			if l.footer() != tt.footer {
				t.Errorf("footer = %q, want %q", l.footer(), tt.footer)
			}
		})
	}
}

func TestFit(t *testing.T) {
	tests := []struct {
		in   string
		w    int
		want string
	}{
		{"nvme", 6, "nvme  "},
		{"nvme_core", 6, "nvme_…"},
		{"nvme", 4, "nvme"},
		{"nvme", 1, "…"},
		{"nvme", 0, ""},
	}
	for _, tt := range tests {
		if got := fit(tt.in, tt.w); got != tt.want {
			t.Errorf("fit(%q, %d) = %q, want %q", tt.in, tt.w, got, tt.want)
		}
	}
}

func TestSizes(t *testing.T) {
	tests := []struct {
		in          int64
		short, long string
	}{
		{512, "512B", "512 bytes"},
		{12288, "12K", "12.0 KiB (12288 bytes)"},
		{21585920, "20.6M", "20.6 MiB (21585920 bytes)"},
	}
	for _, tt := range tests {
		if got := shortSize(tt.in); got != tt.short {
			t.Errorf("shortSize(%d) = %q, want %q", tt.in, got, tt.short)
		}
		if got := longSize(tt.in); got != tt.long {
			t.Errorf("longSize(%d) = %q, want %q", tt.in, got, tt.long)
		}
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct{ in, want string }{
		{"nvme", "nvme"},
		{"pci:v00001022d000017F0sv000017AAsd00003820bc11sc80i00", "pci:v00001022d000017F0sv000017AAsd00003820bc11sc80i00"},
		{"usb:v*p*", "'usb:v*p*'"},
		{"it's", `'it'\''s'`},
	}
	for _, tt := range tests {
		if got := shellQuote(tt.in); got != tt.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWrapText(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		first, rest int
		want        []string
	}{
		{"fits", "AMD GPU", 20, 20, []string{"AMD GPU"}},
		{"word wrap", "drm_display_helper, amdxcp, gpu_sched, drm_buddy", 20, 18,
			[]string{"drm_display_helper,", "amdxcp, gpu_sched,", "drm_buddy"}},
		{"long word is split at path separators", "/lib/modules/7.0.0-38-generic/kernel", 12, 12,
			[]string{"/lib/modules", "/7.0.0-38-", "generic/", "kernel"}},
		{"long word without separators is split at the limit", "abcdefghijklmnop", 6, 6,
			[]string{"abcdef", "ghijkl", "mnop"}},
		{"styles survive wrapping", dim("one two") + " " + cyan("three"), 7, 7,
			[]string{dim("one") + " " + dim("two"), cyan("three")}},
		{"style split inside a long word", yellow("abcdefgh"), 5, 5,
			[]string{yellow("abcde"), yellow("fgh")}},
		{"empty", "", 10, 10, []string{""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrapText(tt.in, tt.first, tt.rest)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("wrapText = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIndentWrap(t *testing.T) {
	tests := []struct {
		in    string
		width int
		want  string
	}{
		{"  short line", 20, "  short line"},
		{"  $ cat /etc/modprobe.d/kmodtui.conf", 20, "  $ cat\n      /etc/modprobe.\n      d/kmodtui.conf"},
		{cyan("$ lsmod") + "  " + dim("# List loaded modules"), 16, cyan("$") + " " + cyan("lsmod") + " " + dim("#") + " " + dim("List") + "\n    " + dim("loaded") + "\n    " + dim("modules")},
	}
	for _, tt := range tests {
		if got := indentWrap(tt.in, tt.width, 4); got != tt.want {
			t.Errorf("indentWrap(%q, %d) =\n%q\nwant\n%q", tt.in, tt.width, got, tt.want)
		}
	}
	if n := visibleLen(cyan("$ lsmod") + "  " + dim("# x")); n != 12 {
		t.Errorf("visibleLen = %d, want 12", n)
	}
}
