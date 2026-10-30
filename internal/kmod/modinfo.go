package kmod

import (
	"strings"
)

// Field is one "key: value" pair from modinfo output, in output order.
// Continuation lines (e.g. the signature hex dump) are joined with "\n".
type Field struct {
	Key   string
	Value string
}

// ParamInfo is a "parm:" entry: a parameter the module accepts.
type ParamInfo struct {
	Name string
	Desc string
	Type string // int, uint, bool, charp, "array of int", ...; empty if not declared
}

// ModInfo is parsed modinfo output.
type ModInfo struct {
	Fields []Field
	Params []ParamInfo
}

// paramTypes are the parameter types modinfo prints in parentheses after the
// description (the kernel's param_ops_* names).
var paramTypes = map[string]bool{
	"byte": true, "short": true, "ushort": true, "int": true, "uint": true,
	"long": true, "ulong": true, "ullong": true, "hexint": true,
	"charp": true, "string": true, "bool": true, "invbool": true, "bint": true,
}

func isParamType(s string) bool {
	return paramTypes[strings.TrimPrefix(s, "array of ")]
}

// ParseModinfo parses the output of `modinfo <name>`. Lines look like
// "key:<spaces>value"; lines starting with a tab continue the previous value.
func ParseModinfo(out string) ModInfo {
	var mi ModInfo
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		if line[0] == '\t' || line[0] == ' ' {
			if n := len(mi.Fields); n > 0 {
				mi.Fields[n-1].Value += "\n" + Clean(strings.TrimSpace(line))
			}
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := Field{Key: Clean(key), Value: Clean(strings.TrimSpace(val))}
		mi.Fields = append(mi.Fields, f)
		if key == "parm" {
			mi.Params = append(mi.Params, parseParm(f.Value))
		}
	}
	return mi
}

// parseParm splits "name:description (type)". modinfo omits the description
// when there is none ("name:int") and the type when it is undeclared.
func parseParm(v string) ParamInfo {
	name, rest, _ := strings.Cut(v, ":")
	p := ParamInfo{Name: name}
	rest = strings.TrimSpace(rest)
	if isParamType(rest) {
		p.Type = rest
		return p
	}
	if strings.HasSuffix(rest, ")") {
		if i := strings.LastIndex(rest, " ("); i >= 0 && isParamType(rest[i+2:len(rest)-1]) {
			p.Desc = rest[:i]
			p.Type = rest[i+2 : len(rest)-1]
			return p
		}
	}
	p.Desc = rest
	return p
}

// Get returns the first value for key, or "".
func (m ModInfo) Get(key string) string {
	for _, f := range m.Fields {
		if f.Key == key {
			return f.Value
		}
	}
	return ""
}

// All returns every value for key (alias, firmware, author, ... repeat).
func (m ModInfo) All(key string) []string {
	var vs []string
	for _, f := range m.Fields {
		if f.Key == key {
			vs = append(vs, f.Value)
		}
	}
	return vs
}

// Builtin reports whether the module is compiled into the kernel.
func (m ModInfo) Builtin() bool {
	return m.Get("filename") == "(builtin)"
}

// Depends returns the direct dependencies, normalized.
func (m ModInfo) Depends() []string {
	var deps []string
	for _, d := range strings.Split(m.Get("depends"), ",") {
		if d = strings.TrimSpace(d); d != "" {
			deps = append(deps, Normalize(d))
		}
	}
	return deps
}

// Signed reports whether the module file carries a signature.
func (m ModInfo) Signed() bool {
	return m.Get("sig_id") != "" || m.Get("signer") != ""
}

// Modinfo runs `modinfo <name>`. For a fixture root it passes -b/-k so
// modinfo looks below Root instead of the live /lib/modules.
func (s *Source) Modinfo(name, release string) (ModInfo, Op) {
	op := Op{Desc: "Module info for " + name, Cmd: "modinfo " + name}
	args := []string{name}
	if !s.Live() {
		args = []string{"-b", s.Root, "-k", release, name}
	}
	out, err := s.Runner.Run("modinfo", args...)
	if err != nil {
		op.Err = err
		return ModInfo{}, op
	}
	return ParseModinfo(string(out)), op
}
