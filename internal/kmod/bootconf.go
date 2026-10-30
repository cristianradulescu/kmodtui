package kmod

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
)

// The only configuration files kmodtui manages. It never edits others.
const (
	ModprobeConf    = "/etc/modprobe.d/kmodtui.conf"
	ModulesLoadConf = "/etc/modules-load.d/kmodtui.conf"
)

// BootKind is the kind of a boot configuration entry.
type BootKind int

const (
	BootBlacklist BootKind = iota // "blacklist <mod>" in modprobe.d
	BootOptions                   // "options <mod> <args>" in modprobe.d
	BootLoad                      // "<mod>" in modules-load.d
	BootOther                     // any other modprobe.d command, kept verbatim
)

func (k BootKind) String() string {
	switch k {
	case BootBlacklist:
		return "blacklist"
	case BootOptions:
		return "options"
	case BootLoad:
		return "load at boot"
	}
	return "other"
}

// BootEntry is one entry of a kmodtui-managed configuration file.
type BootEntry struct {
	Kind   BootKind
	Module string
	Args   string // options arguments, or the full line for BootOther
	File   string
	Line   int
}

// ParseModprobeConf parses modprobe.d(5) syntax: '#' comments, '\' line
// continuation, and the commands blacklist / options (others are kept as
// BootOther).
func ParseModprobeConf(r io.Reader, file string) ([]BootEntry, error) {
	var entries []BootEntry
	sc := bufio.NewScanner(r)
	var pending string
	start := 0
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if pending == "" {
			start = n
		}
		if strings.HasSuffix(line, `\`) {
			pending += strings.TrimSuffix(line, `\`) + " "
			continue
		}
		line, pending = pending+line, ""
		f := strings.Fields(Clean(strings.ReplaceAll(line, "\t", " ")))
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		e := BootEntry{File: file, Line: start}
		switch {
		case f[0] == "blacklist" && len(f) == 2:
			e.Kind, e.Module = BootBlacklist, f[1]
		case f[0] == "options" && len(f) >= 2:
			e.Kind, e.Module, e.Args = BootOptions, f[1], strings.Join(f[2:], " ")
		default:
			e.Kind, e.Args = BootOther, strings.Join(f, " ")
			if len(f) >= 2 {
				e.Module = f[1]
			}
		}
		entries = append(entries, e)
	}
	return entries, sc.Err()
}

// ParseModulesLoad parses modules-load.d(5) syntax: one module per line,
// comments start with '#' or ';'.
func ParseModulesLoad(r io.Reader, file string) ([]BootEntry, error) {
	var entries []BootEntry
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := Clean(strings.TrimSpace(sc.Text()))
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if strings.ContainsAny(line, " \uFFFD") {
			return entries, fmt.Errorf("%s:%d: expected one module name, got %q", file, n, line)
		}
		entries = append(entries, BootEntry{Kind: BootLoad, Module: line, File: file, Line: n})
	}
	return entries, sc.Err()
}

// BootConfig reads kmodtui's own modprobe.d and modules-load.d files.
// Missing files simply mean no entries.
func (s *Source) BootConfig() ([]BootEntry, Op) {
	op := Op{Desc: "Read kmodtui boot config", Cmd: "cat " + ModprobeConf + " " + ModulesLoadConf}
	var all []BootEntry
	parsers := []struct {
		file  string
		parse func(io.Reader, string) ([]BootEntry, error)
	}{
		{ModprobeConf, ParseModprobeConf},
		{ModulesLoadConf, ParseModulesLoad},
	}
	for _, p := range parsers {
		b, err := s.readFile(p.file)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err == nil {
			var entries []BootEntry
			entries, err = p.parse(bytes.NewReader(b), p.file)
			all = append(all, entries...)
		}
		if err != nil && op.Err == nil {
			op.Err = err
		}
	}
	return all, op
}
