package kmod

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeTaint(t *testing.T) {
	tests := []struct {
		in      uint64
		letters string
	}{
		{0, ""},
		{1, "P"},
		{12288, "OE"},  // out-of-tree + unsigned, e.g. VirtualBox or NVIDIA DKMS
		{4097, "PO"},   // proprietary + out-of-tree
		{512, "W"},     // warning
		{1 << 19, "J"}, // newest documented flag
		{1 << 30, "?"}, // unknown future bit
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.in), func(t *testing.T) {
			assertEqual(t, "letters", TaintLetters(DecodeTaint(tt.in)), tt.letters)
		})
	}
	assertEqual(t, "desc", DecodeTaint(8192)[0].Desc, "unsigned module was loaded")
}

func TestParseMokutil(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want SecureBoot
	}{
		{"enabled", "SecureBoot enabled\n", SecureBoot{State: SBEnabled}},
		{"disabled", "SecureBoot disabled\n", SecureBoot{State: SBDisabled}},
		{"setup mode", "SecureBoot disabled\nPlatform is in Setup Mode\n", SecureBoot{State: SBDisabled, Notes: []string{"Platform is in Setup Mode"}}},
		{"shim validation off", "SecureBoot enabled\nSecureBoot validation is disabled in shim\n", SecureBoot{State: SBEnabled, Notes: []string{"SecureBoot validation is disabled in shim"}}},
		{"no support", "This system doesn't support Secure Boot\n", SecureBoot{State: SBUnsupported}},
		{"legacy bios (stderr in error)", "mokutil: exit status 255: EFI variables are not supported on this system", SecureBoot{State: SBUnsupported}},
		{"empty", "", SecureBoot{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertEqual(t, "state", ParseMokutil(tt.in), tt.want)
		})
	}
}

func TestParseEFIBoolVar(t *testing.T) {
	tests := []struct {
		name    string
		in      []byte
		want    bool
		wantErr bool
	}{
		{"off", []byte{0x06, 0, 0, 0, 0}, false, false},
		{"on", []byte{0x06, 0, 0, 0, 1}, true, false},
		{"short", []byte{0x06, 0, 0}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEFIBoolVar(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			assertEqual(t, "value", got, tt.want)
		})
	}
}

func TestStatusFixture(t *testing.T) {
	src, runner := fixtureSource(t)
	st, ops := src.Status()
	for _, op := range ops {
		if op.Err != nil {
			t.Errorf("%s: %v", op.Desc, op.Err)
		}
	}
	assertEqual(t, "release", st.Release, "7.0.0-38-generic")
	assertEqual(t, "secure boot", st.SecureBoot, SecureBoot{State: SBDisabled, Notes: []string{"Platform is in Setup Mode"}})
	assertEqual(t, "taint", TaintLetters(st.TaintFlags), "OE")
	assertEqual(t, "commands", []string{ops[0].Cmd, ops[1].Cmd, ops[2].Cmd}, []string{
		"uname -r",
		"od -An -tu1 /sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c",
		"cat /proc/sys/kernel/tainted",
	})
	// Fixture roots never run mokutil: it would describe the live machine.
	if len(runner.calls) != 0 {
		t.Errorf("unexpected commands: %v", runner.calls)
	}
}

func TestStatusEmptyRoot(t *testing.T) {
	st, ops := emptySource(t).Status()
	if ops[0].Err == nil || ops[2].Err == nil {
		t.Errorf("want errors for missing osrelease and tainted, got %v / %v", ops[0].Err, ops[2].Err)
	}
	assertEqual(t, "secure boot", st.SecureBoot.State, SBUnsupported)
}

func TestStatusSecureBootEnabled(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sys/firmware/efi/efivars")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SecureBoot-"+efiGlobalGUID), []byte{0x16, 0, 0, 0, 1}, 0o644); err != nil {
		t.Fatal(err)
	}
	src := &Source{Root: root, Runner: &fakeRunner{}}
	st, _ := src.Status()
	assertEqual(t, "secure boot", st.SecureBoot, SecureBoot{State: SBEnabled})
}

func TestStatusLiveUsesMokutil(t *testing.T) {
	runner := &fakeRunner{outputs: map[string]string{"mokutil --sb-state": "SecureBoot enabled\n"}}
	src := &Source{Root: "/", Runner: runner}
	sb, op := src.secureBoot()
	assertEqual(t, "state", sb.State, SBEnabled)
	assertEqual(t, "cmd", op.Cmd, "mokutil --sb-state")
}
