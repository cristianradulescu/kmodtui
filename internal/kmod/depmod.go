package kmod

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// DepIndex is the module index depmod writes to /lib/modules/<release>:
// loadable modules and their dependencies (modules.dep), plus the modules
// compiled into the kernel (modules.builtin).
type DepIndex struct {
	paths   map[string]string   // name -> path relative to the module dir
	deps    map[string][]string // name -> every dependency, in modules.dep order
	revdeps map[string][]string // name -> modules whose modules.dep line lists it
	builtin map[string]string   // name -> path in modules.builtin
}

// ModuleDir is where depmod's index files live for a kernel release.
func ModuleDir(release string) string {
	return path.Join("/lib/modules", release)
}

// ModuleNameFromPath turns "kernel/drivers/nvme/host/nvme-core.ko.zst" into
// "nvme_core".
func ModuleNameFromPath(p string) string {
	base := path.Base(p)
	if i := strings.Index(base, ".ko"); i >= 0 {
		base = base[:i]
	}
	return Normalize(base)
}

func newDepIndex() *DepIndex {
	return &DepIndex{
		paths:   map[string]string{},
		deps:    map[string][]string{},
		revdeps: map[string][]string{},
		builtin: map[string]string{},
	}
}

// ParseModulesDep parses modules.dep lines of the form
//
//	kernel/drivers/nvme/host/nvme.ko.zst: kernel/drivers/nvme/host/nvme-core.ko.zst ...
//
// depmod lists the full (transitive) dependency set, so that loading the
// entries right to left, then the module itself, satisfies every symbol.
func ParseModulesDep(r io.Reader) (*DepIndex, error) {
	idx := newDepIndex()
	if err := idx.addModulesDep(r); err != nil {
		return nil, err
	}
	return idx, nil
}

func (d *DepIndex) addModulesDep(r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		file, rest, ok := strings.Cut(line, ":")
		if !ok {
			return fmt.Errorf("modules.dep line %d: missing ':': %q", n, line)
		}
		name := ModuleNameFromPath(file)
		d.paths[name] = file
		for _, dep := range strings.Fields(rest) {
			dn := ModuleNameFromPath(dep)
			d.deps[name] = append(d.deps[name], dn)
			d.revdeps[dn] = append(d.revdeps[dn], name)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	for k := range d.revdeps {
		slices.Sort(d.revdeps[k])
	}
	return nil
}

// AddBuiltin adds modules.builtin entries (one path per line).
func (d *DepIndex) AddBuiltin(r io.Reader) error {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		d.builtin[ModuleNameFromPath(line)] = line
	}
	return sc.Err()
}

// Has reports whether name is a loadable module listed in modules.dep.
func (d *DepIndex) Has(name string) bool {
	_, ok := d.paths[Normalize(name)]
	return ok
}

// Path returns the module file relative to the module dir, or "".
func (d *DepIndex) Path(name string) string { return d.paths[Normalize(name)] }

// Deps returns every dependency of name as listed in modules.dep.
func (d *DepIndex) Deps(name string) []string { return d.deps[Normalize(name)] }

// LoadOrder returns the modules `modprobe name` would load, in order: its
// dependencies (modules.dep right to left), then name itself.
func (d *DepIndex) LoadOrder(name string) []string {
	name = Normalize(name)
	deps := d.deps[name]
	order := make([]string, 0, len(deps)+1)
	for i := len(deps) - 1; i >= 0; i-- {
		order = append(order, deps[i])
	}
	return append(order, name)
}

// RequiredBy returns the modules that need name (directly or indirectly),
// i.e. those whose modules.dep line lists it. Sorted.
func (d *DepIndex) RequiredBy(name string) []string { return d.revdeps[Normalize(name)] }

// IsBuiltin reports whether name is compiled into the kernel.
func (d *DepIndex) IsBuiltin(name string) bool {
	_, ok := d.builtin[Normalize(name)]
	return ok
}

// Names returns all loadable module names, sorted.
func (d *DepIndex) Names() []string {
	return sortedKeys(d.paths)
}

// BuiltinNames returns all built-in module names, sorted.
func (d *DepIndex) BuiltinNames() []string {
	return sortedKeys(d.builtin)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// LoadDepIndex reads modules.dep and modules.builtin for release. A missing
// modules.builtin is not an error (some kernels have none). The returned
// index is never nil, so callers can use it even when a read failed.
func (s *Source) LoadDepIndex(release string) (*DepIndex, []Op) {
	dir := ModuleDir(release)
	idx := newDepIndex()

	depFile := path.Join(dir, "modules.dep")
	depOp := Op{Desc: "Read module dependency index", Cmd: "cat " + depFile}
	if b, err := s.readFile(depFile); err != nil {
		depOp.Err = err
	} else if err := idx.addModulesDep(bytes.NewReader(b)); err != nil {
		depOp.Err = err
	}

	builtinFile := path.Join(dir, "modules.builtin")
	builtinOp := Op{Desc: "Read built-in module list", Cmd: "cat " + builtinFile}
	b, err := s.readFile(builtinFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		builtinOp.Err = err
	default:
		builtinOp.Err = idx.AddBuiltin(bytes.NewReader(b))
	}
	return idx, []Op{depOp, builtinOp}
}
