package ui

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	"github.com/jesseduffield/gocui"

	"github.com/cristianradulescu/kmodtui/internal/kmod"
)

// HelperPath is where kmodtui-helper is installed. The Makefile sets it at
// build time to match the path in the polkit policy.
var HelperPath = "/usr/local/libexec/kmodtui-helper"

const pkexecPath = "/usr/bin/pkexec"

// action is one confirmed write: the helper invocations kmodtui will run
// and everything the confirmation dialog shows about them.
type action struct {
	title      string
	args       [][]string // helper invocations, in order
	does       []string   // what they do, as the commands you would type
	preview    string     // dry-run output
	previewCmd string
	previewErr error
	warnings   []string // shown in red; Enter no longer confirms, only 'y'
	notes      []string
	param      *paramChoice // set-param: the runs depend on the chosen modes
}

func (act *action) runs() [][]string {
	if act.param != nil {
		return act.param.runs()
	}
	return act.args
}

func (act *action) effects() []string {
	if act.param != nil {
		return act.param.effects()
	}
	return act.does
}

func (act *action) ready() bool { return len(act.runs()) > 0 }

// paramChoice is a set-param action: apply now (sysfs), at next load
// (options line), or both.
type paramChoice struct {
	module, name, value string
	canNow              bool
	whyNotNow           string
	now, boot           bool
}

func (p *paramChoice) runs() [][]string {
	if !p.now && !p.boot {
		return nil
	}
	args := []string{"set-param", p.module, p.name, p.value}
	if p.now {
		args = append(args, "--now")
	}
	if p.boot {
		args = append(args, "--boot")
	}
	return [][]string{args}
}

func (p *paramChoice) effects() []string {
	var out []string
	if p.now {
		out = append(out, cyan("$ echo "+p.value+" | sudo tee /sys/module/"+kmod.Normalize(p.module)+"/parameters/"+p.name))
	}
	if p.boot {
		out = append(out, "set "+cyan(p.name+"="+p.value)+" on the "+cyan("options "+p.module)+" line in "+kmod.ModprobeConf)
	}
	return out
}

// executor runs kmodtui-helper with root rights.
type executor interface {
	// available says why the helper cannot run, or nil.
	available() error
	run(args []string) (output string, err error)
}

// pkexecExecutor runs the helper through pkexec. The TUI is suspended
// meanwhile, so polkit's password prompt works both as a desktop dialog
// and as a text prompt in the terminal.
type pkexecExecutor struct{ g *gocui.Gui }

func (pkexecExecutor) available() error {
	if _, err := os.Stat(HelperPath); err != nil {
		return fmt.Errorf("kmodtui-helper is not installed at %s. Install it with: make build && sudo make install", HelperPath)
	}
	if _, err := os.Stat(pkexecPath); err != nil {
		return fmt.Errorf("pkexec is not installed at %s (Ubuntu package: pkexec)", pkexecPath)
	}
	return nil
}

func (e pkexecExecutor) run(args []string) (string, error) {
	if err := e.g.Suspend(); err != nil {
		return "", err
	}
	defer e.g.Resume()
	// The terminal is in normal mode now: Ctrl-C at the password prompt
	// must stop pkexec, not kill kmodtui.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	fmt.Printf("\nkmodtui: %s %s %s\n", pkexecPath, HelperPath, strings.Join(args, " "))
	var buf bytes.Buffer
	cmd := exec.Command(pkexecPath, append([]string{HelperPath}, args...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &buf)
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// pkexec's own exit codes; anything else is the helper's.
		switch exitErr.ExitCode() {
		case 126:
			err = errors.New("authentication was cancelled")
		case 127:
			err = errors.New("not authorized: pkexec could not get permission")
		}
	}
	return buf.String(), err
}

// run executes a confirmed action, logs what happened, and reloads.
func (a *App) run(act *action) error {
	var output []string
	var runErr error
	for _, args := range act.runs() {
		out, err := a.exec.run(args)
		a.logOps(kmod.Op{Desc: act.title, Cmd: "pkexec " + HelperPath + " " + strings.Join(args, " "), Err: err})
		for _, l := range strings.Split(strings.TrimSpace(kmod.CleanLines(out)), "\n") {
			if l != "" {
				a.appendLog("  " + dim(l))
				output = append(output, l)
			}
		}
		if err != nil {
			runErr = err
			break
		}
	}
	a.reload()
	a.renderAll()
	if runErr != nil {
		msg := red(act.title+" failed: ") + errText(runErr)
		if len(output) > 0 {
			msg += "\n\n" + strings.Join(output, "\n")
		}
		return a.showMessage(msg)
	}
	return nil
}

// Facts about modules used by the actions.

func (a *App) selectedModule() (kmod.Entry, bool) {
	if a.tab != tabModules {
		return kmod.Entry{}, false
	}
	return a.mods.selected()
}

func (a *App) isLoaded(name string) bool {
	for _, m := range a.loaded {
		if kmod.Normalize(m.Name) == kmod.Normalize(name) {
			return true
		}
	}
	return false
}

// inBoot reports whether kmodtui's config has an entry of kind for name.
func (a *App) inBoot(kind kmod.BootKind, name string) bool {
	for _, e := range a.boot {
		if e.Kind == kind && kmod.Normalize(e.Module) == kmod.Normalize(name) {
			return true
		}
	}
	return false
}

// modulePred builds a "when" for module keys: the Modules tab is showing
// and the selected module, known to modules.dep, satisfies f.
func (a *App) modulePred(f func(kmod.Entry) bool) func() bool {
	return func() bool {
		e, ok := a.selectedModule()
		return ok && a.idx.Has(e.Name) && f(e)
	}
}

// Action builders.

func (a *App) dryRun(act *action, name string, unload bool) {
	out, op := a.src.DryRun(name, unload)
	a.logOps(op)
	act.preview, act.previewCmd, act.previewErr = out, op.Cmd, op.Err
}

func (a *App) unloadAction(name string) *action {
	act := &action{
		title:    "Unload " + name,
		args:     [][]string{{"unload", name}},
		does:     []string{cyan("$ modprobe -r " + name)},
		warnings: kmod.Impact(name, a.loaded, a.devByMod, a.mounts),
		notes:    []string{"modprobe -r also removes the modules it pulled in that nothing else uses; the dry run lists them."},
	}
	a.dryRun(act, name, true)
	return act
}

func (a *App) loadAction(name string) *action {
	act := &action{
		title: "Load " + name,
		args:  [][]string{{"load", name}},
		does:  []string{cyan("$ modprobe " + name)},
	}
	if a.inBoot(kmod.BootBlacklist, name) {
		act.notes = append(act.notes, name+" is blacklisted in "+kmod.ModprobeConf+". That only stops automatic loading; this explicit load still works.")
	}
	if mi := a.info[kmod.Normalize(name)]; mi != nil && mi.infoErr == nil && !mi.info.Builtin() {
		if !mi.info.Signed() || mi.info.Get("intree") != "Y" {
			act.notes = append(act.notes, name+" is an unsigned or out-of-tree module: loading it taints the kernel, and Secure Boot may refuse it.")
		}
	}
	a.dryRun(act, name, false)
	return act
}

// impactWarnings keeps the device and filesystem warnings (not "in use"),
// for actions that act at the next boot rather than now.
func (a *App) impactWarnings(name, consequence string) []string {
	var out []string
	for _, w := range kmod.Impact(name, a.loaded, a.devByMod, a.mounts) {
		if strings.HasPrefix(w, "drives ") || strings.HasPrefix(w, "provides ") {
			out = append(out, "it "+w+"; "+consequence)
		}
	}
	return out
}

const initramfsNote = "Early boot uses the copy of /etc/modprobe.d in the initramfs; kmodtui lists \"initramfs update needed\" under Changes until update-initramfs runs."

func (a *App) blacklistAction(name string, on bool) *action {
	if !on {
		act := &action{
			title: "Remove the blacklist entry for " + name,
			args:  [][]string{{"unblacklist", name}},
			does:  []string{"remove " + cyan("blacklist "+name) + " from " + kmod.ModprobeConf},
			notes: []string{initramfsNote},
		}
		if other := a.blacklistedElsewhere(name); len(other) > 0 {
			act.notes = append(act.notes, name+" stays blacklisted by "+strings.Join(other, ", ")+"; kmodtui does not edit that file.")
		}
		return act
	}
	act := &action{
		title:    "Blacklist " + name,
		args:     [][]string{{"blacklist", name}},
		does:     []string{cyan("$ echo 'blacklist " + name + "' | sudo tee -a " + kmod.ModprobeConf)},
		warnings: a.impactWarnings(name, "without automatic loading that may not work after the next boot"),
		notes: []string{
			"Blacklisting stops automatic loading, such as for matching hardware at boot. An explicit `modprobe " + name + "` still loads it.",
			initramfsNote,
		},
	}
	if a.isLoaded(name) {
		act.notes = append(act.notes, name+" stays loaded until you unload it or reboot.")
	}
	if other := a.blacklistedElsewhere(name); len(other) > 0 {
		act.notes = append(act.notes, "Already blacklisted in "+strings.Join(other, ", ")+"; this adds kmodtui's own entry.")
	}
	return act
}

func (a *App) bootLoadAction(name string, on bool) *action {
	if !on {
		return &action{
			title: "Stop loading " + name + " at boot",
			args:  [][]string{{"boot-unload", name}},
			does:  []string{"remove " + cyan(name) + " from " + kmod.ModulesLoadConf},
			notes: []string{"It stays loaded until you unload it or reboot."},
		}
	}
	act := &action{
		title: "Load " + name + " at boot",
		args:  [][]string{{"boot-load", name}},
		does:  []string{cyan("$ echo " + name + " | sudo tee -a " + kmod.ModulesLoadConf)},
		notes: []string{"systemd-modules-load loads it at every boot."},
	}
	if !a.isLoaded(name) {
		act.notes = append(act.notes, "This does not load it now; press l for that.")
	}
	return act
}

func (a *App) setParamAction(r paramRow, value string) *action {
	p := &paramChoice{module: a.paramModule, name: r.name, value: value}
	switch {
	case !a.isLoaded(a.paramModule):
		p.whyNotNow = a.paramModule + " is not loaded"
	case !r.hasValue:
		p.whyNotNow = "this parameter is not exposed in /sys/module"
	case !r.writable:
		p.whyNotNow = "the kernel only accepts this parameter when the module loads"
	default:
		p.canNow = true
	}
	p.now, p.boot = p.canNow, !p.canNow
	return &action{
		title: "Set " + a.paramModule + "." + r.name + " = " + value,
		param: p,
		notes: []string{
			"Toggle with 1 and 2. At next load means the next time the module loads: after a reboot, or after unloading and loading it.",
			initramfsNote,
		},
	}
}

func (a *App) clearParamAction(r paramRow) *action {
	return &action{
		title: "Remove " + a.paramModule + "." + r.name + " from the options line",
		args:  [][]string{{"set-param", a.paramModule, r.name, "--clear-boot"}},
		does:  []string{"remove " + cyan(r.name+"="+r.boot) + " from the " + cyan("options "+a.paramModule) + " line in " + kmod.ModprobeConf},
		notes: []string{"The current value stays until the module loads again.", initramfsNote},
	}
}

func (a *App) initramfsAction() *action {
	return &action{
		title: "Update the initramfs",
		args:  [][]string{{"update-initramfs"}},
		does:  []string{cyan("$ update-initramfs -u")},
		notes: []string{"Rebuilds /boot/initrd.img for the newest installed kernel, so early boot sees the current /etc/modprobe.d. This takes a while."},
	}
}

// removeBootAction undoes one entry of kmodtui's config.
func (a *App) removeBootAction(e kmod.BootEntry) (*action, error) {
	switch e.Kind {
	case kmod.BootBlacklist:
		return a.blacklistAction(e.Module, false), nil
	case kmod.BootLoad:
		return a.bootLoadAction(e.Module, false), nil
	case kmod.BootOptions:
		return &action{
			title: "Remove the options for " + e.Module,
			args:  [][]string{{"set-param", e.Module, "--clear-boot-all"}},
			does:  []string{"remove every " + cyan("options "+e.Module) + " line from " + kmod.ModprobeConf},
			notes: []string{"Current values stay until the module loads again.", initramfsNote},
		}, nil
	}
	return nil, fmt.Errorf("kmodtui only removes blacklist, options and load entries. Edit %s line %d by hand", e.File, e.Line)
}

// Key handlers.

func (a *App) unloadSelected() error {
	if e, ok := a.selectedModule(); ok && e.State == kmod.Loaded {
		return a.confirm(a.unloadAction(e.Name))
	}
	return nil
}

func (a *App) loadSelected() error {
	if e, ok := a.selectedModule(); ok && e.State == kmod.Available {
		return a.confirm(a.loadAction(e.Name))
	}
	return nil
}

func (a *App) toggleBlacklist() error {
	if e, ok := a.selectedModule(); ok {
		return a.confirm(a.blacklistAction(e.Name, !a.inBoot(kmod.BootBlacklist, e.Name)))
	}
	return nil
}

func (a *App) toggleBootLoad() error {
	if e, ok := a.selectedModule(); ok {
		return a.confirm(a.bootLoadAction(e.Name, !a.inBoot(kmod.BootLoad, e.Name)))
	}
	return nil
}

// bootPanelList is the list the Boot config panel shows: kmodtui's own
// entries, or every config file.
func (a *App) bootPanelList() *list[kmod.BootEntry] {
	if a.bootTab == 1 {
		return a.allList
	}
	return a.bootList
}

// ownedByKmodtui reports whether e is in one of kmodtui's two files and of
// a kind kmodtui-helper can remove.
func (a *App) ownedByKmodtui(e kmod.BootEntry) bool {
	return (e.File == kmod.ModprobeConf || e.File == kmod.ModulesLoadConf) && kmod.Removable(e.Kind)
}

func (a *App) removeSelectedBootEntry() error {
	e, ok := a.bootPanelList().selected()
	if !ok {
		return nil
	}
	if !kmod.Removable(e.Kind) {
		return a.showMessage(fmt.Sprintf("kmodtui only removes blacklist, options and load entries. Edit %s line %d by hand.", e.File, e.Line))
	}
	if !a.idx.Has(e.Module) {
		// kmodtui-helper only touches modules listed in modules.dep.
		return a.showMessage(fmt.Sprintf("%s is not a module of this kernel (not in modules.dep), so kmodtui-helper will not touch it.\nRemove line %d of %s by hand, for example:\n\n  sudo sed -i '%dd' %s",
			e.Module, e.Line, e.File, e.Line, e.File))
	}
	act, err := a.removeBootAction(e)
	if err != nil {
		return a.showMessage(err.Error())
	}
	return a.confirm(act)
}

// Changes panel.

// change is a pending follow-up shown in the Changes panel.
type change struct {
	key    string
	text   string
	detail []string
	fix    func() *action // nil when there is nothing kmodtui can do
	fixCmd string         // what fixing runs, for Details
}

func (a *App) computeChanges() []change {
	var out []change
	if a.initramfs {
		out = append(out, change{
			key:  "initramfs",
			text: yellow("initramfs update needed"),
			detail: []string{
				"/etc/modprobe.d changed after the initramfs (/boot/initrd.img) was built.",
				"Early boot still uses the old blacklists and options until it is rebuilt.",
			},
			fix:    a.initramfsAction,
			fixCmd: "update-initramfs -u",
		})
	}
	for _, e := range a.boot {
		name := e.Module
		switch {
		case e.Kind == kmod.BootBlacklist && a.isLoaded(name):
			out = append(out, change{
				key:    "blacklisted:" + name,
				text:   name + " is blacklisted but loaded",
				detail: []string{"The blacklist applies from the next boot. Unload " + name + " to apply it now."},
				fix:    func() *action { return a.unloadAction(name) },
				fixCmd: "modprobe -r " + name,
			})
		case e.Kind == kmod.BootLoad && !a.isLoaded(name) && a.idx.Has(name):
			out = append(out, change{
				key:    "bootload:" + name,
				text:   name + " loads at boot but is not loaded",
				detail: []string{name + " is listed in " + kmod.ModulesLoadConf + " but not loaded right now."},
				fix:    func() *action { return a.loadAction(name) },
				fixCmd: "modprobe " + name,
			})
		}
	}
	return out
}

func (a *App) fixSelectedChange() error {
	c, ok := a.changes.selected()
	if !ok || c.fix == nil {
		return nil
	}
	return a.confirm(c.fix())
}
