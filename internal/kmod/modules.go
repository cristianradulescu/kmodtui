package kmod

import (
	"cmp"
	"slices"
)

// State says whether a module is loaded, available on disk, or built in.
type State int

const (
	Loaded State = iota
	Available
	Builtin
)

func (s State) String() string {
	switch s {
	case Loaded:
		return "loaded"
	case Available:
		return "not loaded"
	case Builtin:
		return "built-in"
	}
	return "unknown"
}

// Entry is one row of the module list.
type Entry struct {
	Name  string
	State State
	// Module holds /proc/modules data; nil unless State is Loaded.
	Module *Module
}

// Entries merges loaded modules with the index, sorted by name. With all
// false only loaded modules are returned; with all true, also every
// available (not loaded) and built-in module.
func Entries(loaded []Module, idx *DepIndex, all bool) []Entry {
	seen := make(map[string]bool, len(loaded))
	var entries []Entry
	for i := range loaded {
		m := &loaded[i]
		seen[Normalize(m.Name)] = true
		entries = append(entries, Entry{Name: m.Name, State: Loaded, Module: m})
	}
	if all && idx != nil {
		for _, n := range idx.Names() {
			if !seen[n] {
				entries = append(entries, Entry{Name: n, State: Available})
			}
		}
		for _, n := range idx.BuiltinNames() {
			if !seen[n] {
				entries = append(entries, Entry{Name: n, State: Builtin})
			}
		}
	}
	slices.SortFunc(entries, func(a, b Entry) int { return cmp.Compare(a.Name, b.Name) })
	return entries
}
