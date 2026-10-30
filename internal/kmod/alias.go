package kmod

import (
	"bufio"
	"bytes"
	"io"
	"path"
	"slices"
	"strings"
)

// AliasIndex maps device modaliases to modules (modules.alias).
type AliasIndex struct {
	// byBus groups patterns by the text before the first ':' ("pci", "usb",
	// ...) so a lookup only scans patterns for the device's bus.
	byBus map[string][]aliasEntry
}

type aliasEntry struct {
	pattern string
	module  string
}

func aliasBus(s string) string {
	bus, _, _ := strings.Cut(s, ":")
	return bus
}

// ParseModulesAlias parses lines of the form "alias <pattern> <module>".
func ParseModulesAlias(r io.Reader) (*AliasIndex, error) {
	idx := &AliasIndex{byBus: map[string][]aliasEntry{}}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 3 || f[0] != "alias" {
			continue
		}
		bus := aliasBus(f[1])
		idx.byBus[bus] = append(idx.byBus[bus], aliasEntry{pattern: f[1], module: Normalize(f[2])})
	}
	return idx, sc.Err()
}

// Lookup returns the modules whose alias pattern matches modalias, sorted
// and de-duplicated: what `modprobe --resolve-alias <modalias>` prints.
func (a *AliasIndex) Lookup(modalias string) []string {
	if a == nil || modalias == "" {
		return nil
	}
	var mods []string
	for _, e := range a.byBus[aliasBus(modalias)] {
		// modprobe uses fnmatch(3); modaliases contain no '/', so path.Match
		// has the same semantics for '*', '?' and '[...]'.
		if ok, err := path.Match(e.pattern, modalias); err == nil && ok {
			mods = append(mods, e.module)
		}
	}
	slices.Sort(mods)
	return slices.Compact(mods)
}

// LoadAliasIndex reads modules.alias for release.
func (s *Source) LoadAliasIndex(release string) (*AliasIndex, Op) {
	file := path.Join(ModuleDir(release), "modules.alias")
	op := Op{Desc: "Read module alias index", Cmd: "cat " + file}
	b, err := s.readFile(file)
	if err != nil {
		op.Err = err
		return nil, op
	}
	idx, err := ParseModulesAlias(bytes.NewReader(b))
	op.Err = err
	return idx, op
}
