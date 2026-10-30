// Package kmod reads kernel-module state from procfs, sysfs, the module
// index files in /lib/modules and modinfo. It has no UI dependencies.
//
// Every filesystem read goes through a Source, whose Root can point at a
// fixture tree, and every external command goes through a Runner. Each read
// reports an Op: a short description plus the equivalent CLI command.
package kmod

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Op describes one read (or, later, one action): what it was, the exact CLI
// command that does the same thing, and whether it failed.
type Op struct {
	Desc string
	Cmd  string
	Err  error
}

// Runner runs an external command and returns its standard output.
type Runner interface {
	Run(name string, args ...string) ([]byte, error)
}

// ExecRunner runs commands with os/exec in the C locale. A missing binary
// yields an error that matches exec.ErrNotFound with errors.Is.
type ExecRunner struct{}

func (ExecRunner) Run(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return out, fmt.Errorf("%s: %w: %s", name, err, msg)
		}
		return out, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

// Source reads kernel-module state below Root ("/" on a live system).
type Source struct {
	Root   string
	Runner Runner
}

// NewSource returns a Source reading the live system through ExecRunner.
func NewSource(root string) *Source {
	if root == "" {
		root = "/"
	}
	return &Source{Root: root, Runner: ExecRunner{}}
}

// Live reports whether the Source reads the real system rather than a
// fixture tree. External commands only describe the live system, so they are
// skipped (or redirected) otherwise.
func (s *Source) Live() bool {
	return filepath.Clean(s.Root) == "/"
}

func (s *Source) path(p string) string {
	return filepath.Join(s.Root, p)
}

func (s *Source) readFile(p string) ([]byte, error) {
	return os.ReadFile(s.path(p))
}

// readString reads a small attribute file, trims surrounding whitespace and
// cleans control characters (see Clean).
func (s *Source) readString(p string) (string, error) {
	b, err := s.readFile(p)
	return Clean(strings.TrimSpace(string(b))), err
}

func (s *Source) readDir(p string) ([]fs.DirEntry, error) {
	return os.ReadDir(s.path(p))
}

func (s *Source) stat(p string) (fs.FileInfo, error) {
	return os.Stat(s.path(p))
}

// linkName returns the last element of a symlink's target, e.g. "amdgpu" for
// driver -> ../../../../bus/pci/drivers/amdgpu. The target need not exist.
func (s *Source) linkName(p string) (string, error) {
	target, err := os.Readlink(s.path(p))
	if err != nil {
		return "", err
	}
	return Clean(filepath.Base(target)), nil
}

func (s *Source) exists(p string) bool {
	_, err := os.Lstat(s.path(p))
	return err == nil
}

// Normalize returns the canonical form of a module name. modprobe treats '-'
// and '_' as equivalent; /proc/modules and /sys/module always use '_'.
func Normalize(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}
