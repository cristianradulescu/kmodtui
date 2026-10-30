package kmod

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Integrated Camera", "Integrated Camera"},
		{"Advanced Micro Devices, Inc. [AMD/ATI]", "Advanced Micro Devices, Inc. [AMD/ATI]"},
		{"Café ✓", "Café ✓"},
		{"Evil\x1b[2JCam", "Evil�[2JCam"},
		{"two\nlines", "two�lines"},
		{"tab\there", "tab�here"},
		{"del\x7f", "del�"},
		{"c1\u009b31m", "c1�31m"},
		{"bad\xffutf8", "bad�utf8"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := Clean(tt.in); got != tt.want {
			t.Errorf("Clean(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// A USB device chooses its own descriptor strings; the kernel passes them
// through to sysfs. They must come out free of control characters.
func TestHostileUSBStrings(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dev := "sys/bus/usb/devices/2-1/"
	write(dev+"idVendor", "dead\n")
	write(dev+"idProduct", "beef\n")
	write(dev+"manufacturer", "\x1b]8;;https://example.com\x07ACME\x1b]8;;\x07\n")
	write(dev+"product", "Evil\x1b[2J\nFake row\n")
	write("sys/bus/usb/devices/2-1:1.0/modalias", "usb:vDEADpBEEF\x1b[0m\n")

	devs, ops := (&Source{Root: root, Runner: &fakeRunner{}}).Devices(nil)
	if ops[1].Err != nil {
		t.Fatal(ops[1].Err)
	}
	if len(devs) != 1 {
		t.Fatalf("got %d devices, want 1", len(devs))
	}
	d := devs[0]
	for name, s := range map[string]string{"vendor": d.Vendor, "product": d.Product, "modalias": d.Bindings[0].Modalias} {
		if strings.ContainsAny(s, "\x1b\x07\n") {
			t.Errorf("%s still has control characters: %q", name, s)
		}
	}
	assertEqual(t, "product", d.Product, "Evil�[2J�Fake row")
}

func TestModinfoControlCharacters(t *testing.T) {
	mi := ParseModinfo("description:    Driver\x1b[8m hidden\nauthor:         A\x07B\nsignature:      AA:BB:\n\t\tCC:\x1bDD\n")
	assertEqual(t, "description", mi.Get("description"), "Driver�[8m hidden")
	assertEqual(t, "author", mi.Get("author"), "A�B")
	// The tab that marks a continuation line is still recognised.
	assertEqual(t, "signature", mi.Get("signature"), "AA:BB:\nCC:�DD")
}
