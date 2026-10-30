package kmod

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Module is one line of /proc/modules: a loaded module.
type Module struct {
	Name string
	Size int64
	// RefCount is the use count, or -1 when the kernel reports "-"
	// (module unloading not supported).
	RefCount int
	// UsedBy lists the loaded modules that depend on this one.
	UsedBy []string
	// State is Live, Loading or Unloading.
	State string
	// Taint holds the module's taint letters (e.g. "OE"), empty if none.
	Taint string
}

// ParseProcModules parses the /proc/modules format:
//
//	name size refcount used_by state address [(taint)]
//
// Malformed lines are skipped; the first one is reported in the error, along
// with the modules parsed from the valid lines.
func ParseProcModules(r io.Reader) ([]Module, error) {
	var (
		mods     []Module
		firstErr error
	)
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		m, err := parseProcModulesLine(line)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("line %d: %w", n, err)
			}
			continue
		}
		mods = append(mods, m)
	}
	if err := sc.Err(); err != nil {
		return mods, err
	}
	return mods, firstErr
}

func parseProcModulesLine(line string) (Module, error) {
	f := strings.Fields(line)
	if len(f) < 5 {
		return Module{}, fmt.Errorf("expected at least 5 fields, got %d: %q", len(f), line)
	}
	size, err := strconv.ParseInt(f[1], 10, 64)
	if err != nil {
		return Module{}, fmt.Errorf("bad size %q for %s", f[1], f[0])
	}
	ref := -1
	if f[2] != "-" {
		if ref, err = strconv.Atoi(f[2]); err != nil {
			return Module{}, fmt.Errorf("bad use count %q for %s", f[2], f[0])
		}
	}
	m := Module{Name: f[0], Size: size, RefCount: ref, State: f[4]}
	if f[3] != "-" {
		for _, h := range strings.Split(f[3], ",") {
			if h != "" {
				m.UsedBy = append(m.UsedBy, h)
			}
		}
	}
	if last := f[len(f)-1]; len(f) > 6 && strings.HasPrefix(last, "(") && strings.HasSuffix(last, ")") {
		m.Taint = strings.Trim(last, "()")
	}
	return m, nil
}

// LoadedModules reads /proc/modules, in kernel order (most recently loaded
// first), like lsmod.
func (s *Source) LoadedModules() ([]Module, Op) {
	op := Op{Desc: "List loaded modules", Cmd: "lsmod"}
	b, err := s.readFile("/proc/modules")
	if err != nil {
		op.Err = err
		return nil, op
	}
	mods, err := ParseProcModules(bytes.NewReader(b))
	if err != nil {
		op.Err = fmt.Errorf("/proc/modules: %w", err)
	}
	return mods, op
}
