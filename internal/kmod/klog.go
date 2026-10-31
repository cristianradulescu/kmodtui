package kmod

import (
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// LogLine is one kernel message.
type LogLine struct {
	Time string // "Oct 24 18:24:09"; empty if the line had no timestamp
	Msg  string
}

// logPattern builds the journalctl --grep pattern: any of terms as a whole
// word, so "snd" does not match "snd_hda_intel" and "nvme" not "nvme0n1".
func logPattern(terms []string) string {
	var quoted []string
	for _, t := range terms {
		if q := regexp.QuoteMeta(t); t != "" && !slices.Contains(quoted, q) {
			quoted = append(quoted, q)
		}
	}
	return `\b(?:` + strings.Join(quoted, "|") + `)\b`
}

// quoteArg single-quotes s for a POSIX shell.
func quoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// KernelLog returns up to n kernel messages from the current boot that
// mention any of terms (module names, driver names, device addresses),
// newest first. It needs read access to the system journal (groups adm or
// systemd-journal); without it journalctl simply finds nothing.
func (s *Source) KernelLog(terms []string, n int) ([]LogLine, Op) {
	pattern := logPattern(terms)
	args := []string{"-k", "-b", "-r", "--no-pager", "-o", "short-iso", "-n", strconv.Itoa(n), "--grep", pattern}
	op := Op{
		Desc: "Kernel messages about " + strings.Join(terms, ", "),
		Cmd:  "journalctl -k -b -r -n " + strconv.Itoa(n) + " --grep " + quoteArg(pattern),
	}
	if !s.Live() {
		// Read the fixture's journal, never the live machine's.
		args = append([]string{"-D", path.Join(s.Root, "var/log/journal")}, args...)
	}
	out, err := s.Runner.Run("journalctl", args...)
	text := strings.TrimSpace(string(out))
	if err != nil && !strings.Contains(text, "-- No entries --") {
		// journalctl exits 1 when nothing matches; that is not an error.
		op.Err = err
		return nil, op
	}
	return ParseJournal(text), op
}

// ParseJournal parses `journalctl -o short-iso` kernel lines:
//
//	2026-10-24T18:24:09+03:00 crimson-labs-01 kernel: mt7925e 0000:c1:00.0: enabling device
func ParseJournal(out string) []LogLine {
	var lines []LogLine
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimRight(l, "\r")
		if l == "" || strings.HasPrefix(l, "-- ") {
			continue
		}
		ts, rest, ok := strings.Cut(l, " ")
		t, err := time.Parse(time.RFC3339, ts)
		if !ok || err != nil {
			lines = append(lines, LogLine{Msg: Clean(l)})
			continue
		}
		// Drop "<host> kernel: ".
		if _, msg, ok := strings.Cut(rest, " kernel: "); ok {
			rest = msg
		}
		lines = append(lines, LogLine{Time: t.Format("Jan 02 15:04:05"), Msg: Clean(rest)})
	}
	return lines
}
