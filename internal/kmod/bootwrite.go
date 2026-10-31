package kmod

import (
	"slices"
	"strings"
)

// ConfHeader starts every file kmodtui creates.
const ConfHeader = "# Managed by kmodtui. Edit with kmodtui or remove entries by hand."

// The Edit* functions change kmodtui's own configuration files as text.
// They touch only the lines they own, keep every other line (comments,
// hand-written entries) as is, and report whether anything changed, so
// running the same edit twice is a no-op. Lines continued with '\' are
// left alone.

func splitConf(content string) []string {
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return []string{ConfHeader}
	}
	return strings.Split(content, "\n")
}

func joinConf(lines []string) string { return strings.Join(lines, "\n") + "\n" }

// continued reports whether line i is, or continues, a '\' continuation.
func continued(lines []string, i int) bool {
	if strings.HasSuffix(lines[i], `\`) {
		return true
	}
	return i > 0 && strings.HasSuffix(lines[i-1], `\`)
}

// EditBlacklist adds ("blacklist <module>") or removes the blacklist entry
// for module in modprobe.d content.
func EditBlacklist(content, module string, on bool) (string, bool) {
	lines := splitConf(content)
	match := func(i int) bool {
		f := strings.Fields(lines[i])
		return !continued(lines, i) && len(f) == 2 && f[0] == "blacklist" && Normalize(f[1]) == Normalize(module)
	}
	return editLines(lines, match, "blacklist "+module, on)
}

// EditBootLoad adds or removes module in modules-load.d content.
func EditBootLoad(content, module string, on bool) (string, bool) {
	lines := splitConf(content)
	match := func(i int) bool {
		return Normalize(strings.TrimSpace(lines[i])) == Normalize(module)
	}
	return editLines(lines, match, module, on)
}

// editLines makes sure exactly the lines selected by match are present (on)
// or absent (off). Adding appends entry when no line matches.
func editLines(lines []string, match func(int) bool, entry string, on bool) (string, bool) {
	var kept []string
	found := false
	for i, l := range lines {
		if match(i) {
			found = true
			if !on {
				continue
			}
		}
		kept = append(kept, l)
	}
	switch {
	case on && found:
		return joinConf(lines), false
	case on:
		return joinConf(append(kept, entry)), true
	default:
		return joinConf(kept), found
	}
}

// EditOption sets param=value on module's "options" line in modprobe.d
// content, adding the line if needed. An empty value removes the parameter,
// and the line goes when it has no parameters left.
func EditOption(content, module, param, value string) (string, bool) {
	lines := splitConf(content)
	changed, done := false, false
	var out []string
	for i, l := range lines {
		f := strings.Fields(l)
		if continued(lines, i) || len(f) < 2 || f[0] != "options" || Normalize(f[1]) != Normalize(module) {
			out = append(out, l)
			continue
		}
		params := f[2:]
		idx := slices.IndexFunc(params, func(p string) bool { return strings.SplitN(p, "=", 2)[0] == param })
		switch {
		case idx >= 0 && (value == "" || done):
			params = slices.Delete(params, idx, idx+1)
			changed = true
		case idx >= 0:
			if params[idx] != param+"="+value {
				params[idx] = param + "=" + value
				changed = true
			}
			done = true
		case value != "" && !done:
			params = append(params, param+"="+value)
			changed, done = true, true
		}
		if len(params) > 0 {
			out = append(out, strings.Join(append([]string{"options", f[1]}, params...), " "))
		} else {
			changed = true
		}
	}
	if value != "" && !done {
		out = append(out, "options "+module+" "+param+"="+value)
		changed = true
	}
	if !changed {
		return joinConf(lines), false
	}
	return joinConf(out), true
}

// ClearOptions removes every "options <module> ..." line from modprobe.d
// content.
func ClearOptions(content, module string) (string, bool) {
	lines := splitConf(content)
	match := func(i int) bool {
		f := strings.Fields(lines[i])
		return !continued(lines, i) && len(f) >= 2 && f[0] == "options" && Normalize(f[1]) == Normalize(module)
	}
	return editLines(lines, match, "", false)
}

// Options returns the parameters kmodtui's modprobe.d content sets for
// module, as written there ("power_save=1").
func Options(entries []BootEntry, module string) []string {
	var out []string
	for _, e := range entries {
		if e.Kind == BootOptions && Normalize(e.Module) == Normalize(module) {
			out = append(out, strings.Fields(e.Args)...)
		}
	}
	return out
}
