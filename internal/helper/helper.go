// Package helper implements kmodtui-helper, the only part of kmodtui that
// runs as root (through pkexec). It accepts a fixed set of subcommands,
// validates every argument, runs only modprobe and update-initramfs by
// absolute path, and writes only kmodtui's two configuration files.
package helper

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/cristianradulescu/kmodtui/internal/kmod"
)

// Absolute paths: the helper never looks anything up in $PATH.
const (
	ModprobePath        = "/usr/sbin/modprobe"
	UpdateInitramfsPath = "/usr/sbin/update-initramfs"
)

// ErrUsage marks errors caused by a malformed command line.
var ErrUsage = errors.New("usage")

// Env is everything the helper touches. Production uses Root "/" and runs
// real commands; tests point Root at a temporary tree and record commands.
type Env struct {
	Root string
	// Exec runs an absolute path with arguments, output going to Out.
	Exec func(path string, args ...string) error
	Out  io.Writer
}

// Usage lists the subcommands.
const Usage = `usage: kmodtui-helper <subcommand> [arguments]

  load MODULE                     modprobe MODULE
  unload MODULE                   modprobe -r MODULE
  set-param MODULE PARAM VALUE [--now] [--boot]
                                  --now:  write /sys/module/MODULE/parameters/PARAM
                                  --boot: set PARAM=VALUE in ` + kmod.ModprobeConf + `
  set-param MODULE PARAM --clear-boot
                                  remove PARAM from ` + kmod.ModprobeConf + `
  blacklist MODULE                add "blacklist MODULE" to ` + kmod.ModprobeConf + `
  unblacklist MODULE              remove it again
  boot-load MODULE                add MODULE to ` + kmod.ModulesLoadConf + `
  boot-unload MODULE              remove it again
  update-initramfs                update-initramfs -u
`

// Run executes one subcommand.
func Run(env Env, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: no subcommand", ErrUsage)
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "load", "unload", "blacklist", "unblacklist", "boot-load", "boot-unload":
		if len(rest) != 1 {
			return fmt.Errorf("%w: %s takes exactly one module name", ErrUsage, cmd)
		}
		module := rest[0]
		if err := env.checkModule(module); err != nil {
			return err
		}
		switch cmd {
		case "load":
			return env.run(ModprobePath, module)
		case "unload":
			return env.run(ModprobePath, "-r", module)
		case "blacklist", "unblacklist":
			return env.editFile(kmod.ModprobeConf, func(c string) (string, bool) {
				return kmod.EditBlacklist(c, module, cmd == "blacklist")
			}, describe(cmd == "blacklist", "blacklist "+module))
		default:
			return env.editFile(kmod.ModulesLoadConf, func(c string) (string, bool) {
				return kmod.EditBootLoad(c, module, cmd == "boot-load")
			}, describe(cmd == "boot-load", module))
		}
	case "set-param":
		return env.setParam(rest)
	case "update-initramfs":
		if len(rest) != 0 {
			return fmt.Errorf("%w: update-initramfs takes no arguments", ErrUsage)
		}
		return env.run(UpdateInitramfsPath, "-u")
	}
	return fmt.Errorf("%w: unknown subcommand %q", ErrUsage, cmd)
}

func describe(add bool, entry string) string {
	if add {
		return fmt.Sprintf("add %q", entry)
	}
	return fmt.Sprintf("remove %q", entry)
}

// checkModule validates the name and requires the module to be in the
// running kernel's modules.dep (so built-in or unknown names are refused).
func (env Env) checkModule(module string) error {
	if err := kmod.ValidModuleName(module); err != nil {
		return err
	}
	src := &kmod.Source{Root: env.Root}
	release, err := src.KernelRelease()
	if err != nil {
		return fmt.Errorf("reading the kernel release: %w", err)
	}
	idx, ops := src.LoadDepIndex(release)
	if ops[0].Err != nil {
		return fmt.Errorf("reading modules.dep: %w", ops[0].Err)
	}
	if !idx.Has(module) {
		return fmt.Errorf("module %q is not in %s/modules.dep (unknown or built into the kernel)", module, kmod.ModuleDir(release))
	}
	return nil
}

func (env Env) run(p string, args ...string) error {
	fmt.Fprintf(env.Out, "+ %s %s\n", p, strings.Join(args, " "))
	if err := env.Exec(p, args...); err != nil {
		return fmt.Errorf("%s failed: %w", path.Base(p), err)
	}
	return nil
}

func (env Env) setParam(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("%w: set-param MODULE PARAM VALUE [--now] [--boot] | set-param MODULE PARAM --clear-boot", ErrUsage)
	}
	module, param := args[0], args[1]
	if err := env.checkModule(module); err != nil {
		return err
	}
	if err := kmod.ValidParamName(param); err != nil {
		return err
	}
	if len(args) == 3 && args[2] == "--clear-boot" {
		return env.editFile(kmod.ModprobeConf, func(c string) (string, bool) {
			return kmod.EditOption(c, module, param, "")
		}, fmt.Sprintf("remove %s from the options for %s", param, module))
	}
	value := args[2]
	if err := kmod.ValidParamValue(value); err != nil {
		return err
	}
	var now, boot bool
	for _, f := range args[3:] {
		switch f {
		case "--now":
			now = true
		case "--boot":
			boot = true
		default:
			return fmt.Errorf("%w: unknown set-param flag %q", ErrUsage, f)
		}
	}
	if !now && !boot {
		return fmt.Errorf("%w: set-param needs --now, --boot or both", ErrUsage)
	}
	if now {
		if err := env.writeParam(module, param, value); err != nil {
			return err
		}
	}
	if boot {
		return env.editFile(kmod.ModprobeConf, func(c string) (string, bool) {
			return kmod.EditOption(c, module, param, value)
		}, fmt.Sprintf("set %s=%s in the options for %s", param, value, module))
	}
	return nil
}

// writeParam changes a parameter of the loaded module through sysfs.
func (env Env) writeParam(module, param, value string) error {
	rel := path.Join("/sys/module", kmod.Normalize(module), "parameters", param)
	p := filepath.Join(env.Root, rel)
	fi, err := os.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%s has no parameter %s right now (is the module loaded?)", module, param)
	case err != nil:
		return err
	case !fi.Mode().IsRegular():
		return fmt.Errorf("%s is not a regular file", rel)
	case fi.Mode().Perm()&0o200 == 0:
		return fmt.Errorf("%s.%s can only be set when the module is loaded (use --boot)", module, param)
	}
	fmt.Fprintf(env.Out, "+ echo %s > %s\n", value, rel)
	// Same flags as the shell's "echo VALUE > FILE"; sysfs accepts O_TRUNC.
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(value); err != nil {
		f.Close()
		return fmt.Errorf("the kernel rejected %s=%s: %w", param, value, err)
	}
	return f.Close()
}

// editFile applies edit to one of kmodtui's files and writes the result
// atomically: a temporary file in the same directory, then a rename, so a
// crash never leaves a half-written file. Unchanged content is not written.
func (env Env) editFile(rel string, edit func(string) (string, bool), what string) error {
	p := filepath.Join(env.Root, rel)
	old, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	content, changed := edit(string(old))
	if !changed {
		fmt.Fprintf(env.Out, "= %s already up to date (%s)\n", rel, what)
		return nil
	}
	fmt.Fprintf(env.Out, "+ edit %s: %s\n", rel, what)
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".kmodtui-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}
