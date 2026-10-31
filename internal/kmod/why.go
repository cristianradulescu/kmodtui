package kmod

import (
	"fmt"
	"slices"
	"strings"
)

// directReasons are the things that load a module by themselves: hardware
// it drives (udev loads it by modalias), a modules-load.d entry, another
// loaded module's softdep, or a mounted filesystem.
func directReasons(name string, loaded map[string]Module, devByMod map[string][]Device, conf []BootEntry, mounts map[string][]string) []string {
	var out []string
	for _, d := range devByMod[name] {
		s := "drives "
		kind := DeviceRole(d)
		if kind == "" {
			kind = strings.ToLower(d.ClassName)
		}
		if kind != "" {
			s += kind + " "
		}
		s += "device " + d.Bus + " " + d.Addr
		if n := deviceLabel(d); n != "" && !strings.EqualFold(n, kind) {
			s += " (" + n + ")"
		}
		out = append(out, s)
	}
	for _, e := range ConfigFor(conf, name) {
		if e.Kind == BootLoad {
			out = append(out, "loaded at boot: listed in "+e.File)
		}
	}
	for _, e := range SoftdepsOn(conf, name) {
		if _, ok := loaded[Normalize(e.Module)]; ok {
			out = append(out, fmt.Sprintf("soft dependency of %s (%s)", e.Module, e.File))
		}
	}
	for _, fstype := range mountedTypes(name, mounts) {
		out = append(out, fmt.Sprintf("provides the %s filesystem mounted at %s", fstype, strings.Join(mounts[fstype], ", ")))
	}
	return out
}

// mountedTypes returns the mounted filesystem types module provides; FUSE
// mounts carry a subtype ("fuse.portal").
func mountedTypes(name string, mounts map[string][]string) []string {
	var types []string
	for fstype := range mounts {
		if Normalize(fstype) == name || strings.HasPrefix(fstype, name+".") {
			types = append(types, fstype)
		}
	}
	slices.Sort(types)
	return types
}

// WhyLoaded explains why a loaded module is loaded, one reason per line:
// its own direct reasons, then the loaded modules that need it and what
// those are ultimately for.
func WhyLoaded(module string, loaded []Module, devByMod map[string][]Device, conf []BootEntry, mounts map[string][]string) []string {
	byName := make(map[string]Module, len(loaded))
	for _, m := range loaded {
		byName[Normalize(m.Name)] = m
	}
	name := Normalize(module)
	m, ok := byName[name]
	if !ok {
		return nil
	}
	reasons := directReasons(name, byName, devByMod, conf, mounts)

	if len(m.UsedBy) > 0 {
		reasons = append(reasons, "needed by "+strings.Join(m.UsedBy, ", "))
		// Walk up the users until modules with a reason of their own.
		seen := map[string]bool{name: true}
		queue := slices.Clone(m.UsedBy)
		var roots []string
		for len(queue) > 0 && len(roots) < 4 {
			u := Normalize(queue[0])
			queue = queue[1:]
			if seen[u] {
				continue
			}
			seen[u] = true
			if r := directReasons(u, byName, devByMod, conf, mounts); len(r) > 0 {
				roots = append(roots, "  for "+u+", which "+r[0])
				continue
			}
			queue = append(queue, byName[u].UsedBy...)
		}
		reasons = append(reasons, roots...)
	}

	if len(reasons) == 0 {
		if m.RefCount > 0 {
			reasons = append(reasons, fmt.Sprintf("in use (use count %d) by the kernel or by programs, not by other modules", m.RefCount))
		}
		reasons = append(reasons, "no device, dependent module or config entry explains it: it was probably loaded by hand, by a service, or on demand (for a network protocol, crypto algorithm or similar)")
	}
	return reasons
}
