package ui

import (
	"fmt"
	"strings"

	"github.com/cristianradulescu/kmodtui/internal/kmod"
)

// Details sections that explain a module or device: every config entry
// about it, why it is loaded, and what the kernel said about it.

const logLines = 20

// kernelLog caches the kernel messages for one module or device.
type kernelLog struct {
	terms []string
	lines []kmod.LogLine
	cmd   string
	err   error
}

// kernelLogFor returns the cached messages for key, or nil while they load
// in the background (journalctl can take a moment).
func (a *App) kernelLogFor(key string, terms []string) *kernelLog {
	if kl, ok := a.klog[key]; ok || a.pending["log:"+key] {
		return kl
	}
	a.pending["log:"+key] = true
	a.background(func() func() {
		lines, op := a.src.KernelLog(terms, logLines)
		return func() {
			delete(a.pending, "log:"+key)
			a.klog[key] = &kernelLog{terms: terms, lines: lines, cmd: op.Cmd, err: op.Err}
			a.logOps(op)
			a.detailsDirty = true
		}
	})
	return a.klog[key]
}

// moduleLogTerms: the module name, plus driver names that differ from it
// (module xhci_pci logs as driver xhci_hcd).
func (a *App) moduleLogTerms(name string) []string {
	terms := []string{name}
	for _, d := range a.devByMod[name] {
		for _, b := range d.Bindings {
			if kmod.Normalize(b.Module) == name && b.Driver != "" && b.Driver != name {
				terms = append(terms, b.Driver)
			}
		}
	}
	return terms
}

// deviceLogTerms: the device address and its USB interfaces.
func deviceLogTerms(d kmod.Device) []string {
	if d.Bus == "pci" {
		return []string{d.Addr}
	}
	terms := []string{"usb " + d.Addr}
	for _, b := range d.Bindings {
		terms = append(terms, b.Path)
	}
	return terms
}

// logSection renders the kernel messages and returns the command for the
// "Equivalent commands" list ("" while loading).
func (a *App) logSection(d *details, key string, terms []string) string {
	kl := a.kernelLogFor(key, terms)
	d.section("Kernel log" + dim(" (this boot, newest first)"))
	switch {
	case kl == nil:
		d.line("  " + dim("loading…"))
		return ""
	case kl.err != nil:
		d.line("  " + red(errText(kl.err)))
	case len(kl.lines) == 0:
		d.line("  " + dim("No kernel messages mention "+strings.Join(terms, ", ")+" in this boot."))
		d.line("  " + dim("journalctl only shows kernel messages to members of the adm or systemd-journal groups."))
	default:
		for _, l := range kl.lines {
			d.hang("  "+dim(fit(l.Time, 15))+" ", l.Msg)
		}
	}
	return kl.cmd
}

// configSection lists every config entry about module, from all files,
// each with its file and line on the next line (paths are long).
func (a *App) configSection(d *details, module string) {
	entries := kmod.ConfigFor(a.allConf, module)
	softdeps := kmod.SoftdepsOn(a.allConf, module)
	if len(entries) == 0 && len(softdeps) == 0 {
		return
	}
	d.section("Config")
	indent := strings.Repeat(" ", 15) // under the text after the kind column
	for _, e := range entries {
		what := ""
		switch e.Kind {
		case kmod.BootOptions, kmod.BootInstall, kmod.BootRemove, kmod.BootSoftdep:
			what = e.Args
		case kmod.BootAlias:
			what = e.Args + dim(" → "+e.Module)
		}
		d.hang("  "+bootKindText(e.Kind)+" ", what)
		d.hang(indent, configSource(e))
	}
	for _, e := range softdeps {
		d.hang("  "+bootKindText(kmod.BootSoftdep)+" ", "pulled in by "+e.Module+dim(" ("+e.Args+")"))
		d.hang(indent, configSource(e))
	}
}

func configSource(e kmod.BootEntry) string {
	s := fmt.Sprintf("%s:%d", e.File, e.Line)
	if e.File == kmod.ModprobeConf || e.File == kmod.ModulesLoadConf {
		return cyan(s) + dim(" (kmodtui)")
	}
	return dim(s)
}

// whySection says why a loaded module is loaded.
func (a *App) whySection(d *details, module string) {
	reasons := kmod.WhyLoaded(module, a.loaded, a.devByMod, a.allConf, a.mounts)
	for i, r := range reasons {
		if i == 0 {
			d.field("Why loaded", r)
		} else {
			d.more(r)
		}
	}
}

// blacklistedElsewhere returns the blacklist entries for module in files
// other than kmodtui's own.
func (a *App) blacklistedElsewhere(module string) []string {
	var out []string
	for _, e := range kmod.ConfigFor(a.allConf, module) {
		if e.Kind == kmod.BootBlacklist && e.File != kmod.ModprobeConf {
			out = append(out, fmt.Sprintf("%s:%d", e.File, e.Line))
		}
	}
	return out
}
