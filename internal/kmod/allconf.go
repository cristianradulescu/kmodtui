package kmod

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// The directories modprobe and systemd-modules-load read, highest
// precedence first: a file hides any file with the same name in a later
// directory (modprobe.d(5), modules-load.d(5)). Files are then read in
// file-name order, whatever their directory.
var (
	ModprobeDirs    = []string{"/etc/modprobe.d", "/run/modprobe.d", "/usr/local/lib/modprobe.d", "/usr/lib/modprobe.d", "/lib/modprobe.d"}
	ModulesLoadDirs = []string{"/etc/modules-load.d", "/run/modules-load.d", "/usr/local/lib/modules-load.d", "/usr/lib/modules-load.d", "/lib/modules-load.d"}
)

// confFiles returns the effective *.conf files in dirs, in reading order.
func (s *Source) confFiles(dirs []string) ([]string, error) {
	seen := map[string]bool{}
	var files []string
	var firstErr error
	for _, d := range dirs {
		entries, err := s.readDir(d)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, e := range entries {
			n := e.Name()
			if !strings.HasSuffix(n, ".conf") || e.IsDir() || seen[n] {
				continue
			}
			seen[n] = true
			files = append(files, path.Join(d, n))
		}
	}
	slices.SortFunc(files, func(a, b string) int { return strings.Compare(path.Base(a), path.Base(b)) })
	return files, firstErr
}

// AllConfig reads every effective modprobe.d and modules-load.d file, so
// the UI can show which file blacklists, configures or loads a module.
// A file that cannot be read or parsed is reported and skipped.
func (s *Source) AllConfig() ([]BootEntry, Op) {
	op := Op{
		Desc: "Read all modprobe.d and modules-load.d files",
		Cmd:  "systemd-analyze cat-config modprobe.d; systemd-analyze cat-config modules-load.d",
	}
	var all []BootEntry
	var errs []error
	for _, set := range []struct {
		dirs  []string
		parse func(r *bytes.Reader, file string) ([]BootEntry, error)
	}{
		{ModprobeDirs, func(r *bytes.Reader, f string) ([]BootEntry, error) { return ParseModprobeConf(r, f) }},
		{ModulesLoadDirs, func(r *bytes.Reader, f string) ([]BootEntry, error) { return ParseModulesLoad(r, f) }},
	} {
		files, err := s.confFiles(set.dirs)
		if err != nil {
			errs = append(errs, err)
		}
		for _, f := range files {
			b, err := s.readFile(f)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			entries, err := set.parse(bytes.NewReader(b), f)
			if err != nil {
				errs = append(errs, err)
			}
			all = append(all, entries...)
		}
	}
	if len(errs) > 0 {
		op.Err = fmt.Errorf("%d config file problem(s), first: %w", len(errs), errs[0])
	}
	return all, op
}

// ConfigFor returns the entries that concern module: its blacklist,
// options, install/remove commands, softdeps, load-at-boot lines and the
// aliases that point to it.
func ConfigFor(entries []BootEntry, module string) []BootEntry {
	name := Normalize(module)
	var out []BootEntry
	for _, e := range entries {
		if Normalize(e.Module) == name {
			out = append(out, e)
		}
	}
	return out
}

// SoftdepsOn returns the softdep entries that make other modules pull in
// module ("softdep X pre: module" or "post: module").
func SoftdepsOn(entries []BootEntry, module string) []BootEntry {
	name := Normalize(module)
	var out []BootEntry
	for _, e := range entries {
		if e.Kind != BootSoftdep {
			continue
		}
		for _, f := range strings.Fields(e.Args) {
			if !strings.HasSuffix(f, ":") && Normalize(f) == name {
				out = append(out, e)
				break
			}
		}
	}
	return out
}
