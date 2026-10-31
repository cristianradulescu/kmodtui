package kmod

import (
	"errors"
	"io/fs"
	"path"
	"strings"
)

// Param is the current value of a module parameter from
// /sys/module/<name>/parameters/<param>.
type Param struct {
	Name  string
	Value string
	// Unreadable says why Value is empty ("root only", "write-only" or an
	// error message); empty when the value was read.
	Unreadable string
	// Writable means root can change it at runtime through sysfs.
	Writable bool
}

// Params reads the current parameter values of a loaded or built-in module.
// A module without a parameters directory (not loaded, or no parameters)
// yields no params and no error.
func (s *Source) Params(module string) ([]Param, Op) {
	module = Normalize(module)
	dir := path.Join("/sys/module", module, "parameters")
	op := Op{Desc: "Parameter values for " + module, Cmd: "grep -H . " + dir + "/*"}
	entries, err := s.readDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, op
	}
	if err != nil {
		op.Err = err
		return nil, op
	}
	params := make([]Param, 0, len(entries))
	for _, e := range entries {
		p := Param{Name: Clean(e.Name())}
		if fi, err := e.Info(); err == nil {
			p.Writable = fi.Mode().Perm()&0o200 != 0
		}
		file := path.Join(dir, e.Name())
		b, err := s.readFile(file)
		switch {
		case err == nil:
			p.Value = Clean(strings.TrimRight(string(b), "\n"))
		case errors.Is(err, fs.ErrPermission):
			p.Unreadable = "root only"
			if fi, serr := s.stat(file); serr == nil && fi.Mode().Perm()&0o444 == 0 {
				p.Unreadable = "write-only"
			}
		default:
			p.Unreadable = Clean(err.Error())
		}
		params = append(params, p)
	}
	return params, op
}
