package kmod

import (
	"errors"
	"strings"
	"testing"
)

func TestLogPattern(t *testing.T) {
	tests := []struct {
		terms []string
		want  string
	}{
		{[]string{"nvme"}, `\b(?:nvme)\b`},
		{[]string{"xhci_pci", "xhci_hcd", "xhci_hcd"}, `\b(?:xhci_pci|xhci_hcd)\b`},
		{[]string{"0000:c2:00.0"}, `\b(?:0000:c2:00\.0)\b`},
	}
	for _, tt := range tests {
		assertEqual(t, strings.Join(tt.terms, ","), logPattern(tt.terms), tt.want)
	}
}

func TestParseJournal(t *testing.T) {
	out := `2026-10-24T18:24:10+03:00 crimson-labs-01 kernel: mt7925e 0000:c1:00.0: WM Firmware Version: ____000000, Build Time: 20260605184805
2026-10-24T18:24:09+03:00 crimson-labs-01 kernel: mt7925e 0000:c1:00.0: enabling device (0000 -> 0002)
-- Boot 3f2a… --
not a journal line` + "\x1b[2J"
	assertEqual(t, "lines", ParseJournal(out), []LogLine{
		{Time: "Oct 24 18:24:10", Msg: "mt7925e 0000:c1:00.0: WM Firmware Version: ____000000, Build Time: 20260605184805"},
		{Time: "Oct 24 18:24:09", Msg: "mt7925e 0000:c1:00.0: enabling device (0000 -> 0002)"},
		{Msg: "not a journal line�[2J"},
	})
}

func TestKernelLog(t *testing.T) {
	const live = "journalctl -k -b -r --no-pager -o short-iso -n 20 --grep \\b(?:mt7925e)\\b"
	r := &fakeRunner{outputs: map[string]string{
		live: "2026-10-24T18:24:09+03:00 crimson-labs-01 kernel: mt7925e 0000:c1:00.0: enabling device (0000 -> 0002)\n",
	}}
	src := &Source{Root: "/", Runner: r}
	lines, op := src.KernelLog([]string{"mt7925e"}, 20)
	if op.Err != nil {
		t.Fatal(op.Err)
	}
	assertEqual(t, "cmd", op.Cmd, `journalctl -k -b -r -n 20 --grep '\b(?:mt7925e)\b'`)
	assertEqual(t, "lines", len(lines), 1)

	// A fixture root reads the fixture's journal directory.
	fix, fr := fixtureSource(t)
	fix.KernelLog([]string{"nvme"}, 5)
	if !strings.HasPrefix(fr.calls[0], "journalctl -D testdata/root/var/log/journal -k") {
		t.Errorf("call = %q", fr.calls[0])
	}
}

type scriptedRunner struct {
	out string
	err error
}

func (s scriptedRunner) Run(string, ...string) ([]byte, error) { return []byte(s.out), s.err }

func TestKernelLogNoMatchesIsNotAnError(t *testing.T) {
	src := &Source{Root: "/", Runner: scriptedRunner{out: "-- No entries --\n", err: errors.New("journalctl: exit status 1")}}
	lines, op := src.KernelLog([]string{"e1000e"}, 20)
	if op.Err != nil || len(lines) != 0 {
		t.Errorf("lines = %v, err = %v; want none and no error", lines, op.Err)
	}
	src = &Source{Root: "/", Runner: scriptedRunner{err: errors.New("journalctl: exit status 1: Failed to open journal")}}
	if _, op := src.KernelLog([]string{"e1000e"}, 20); op.Err == nil {
		t.Error("real failure not reported")
	}
}
