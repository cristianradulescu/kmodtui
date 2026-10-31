package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jesseduffield/gocui"

	"github.com/cristianradulescu/kmodtui/internal/kmod"
)

// binding is the single source of truth for a key: it is registered with
// gocui, shown in the bottom bar, and listed in the keybindings popup.
type binding struct {
	views   []string // nil means global
	keys    []any    // gocui.Key or rune
	label   string   // how the key is displayed, e.g. "j/↓"
	desc    string
	bar     bool          // show in the bottom bar when one of views is focused
	hint    func() string // bar text override, e.g. a toggle's current action
	when    func() bool   // binding only applies when this returns true
	popups  bool          // global binding that also works while a popup is open
	handler func() error
}

func (b *binding) active() bool { return b.when == nil || b.when() }

func (b *binding) barText() string {
	if b.hint != nil {
		return b.hint()
	}
	return b.desc + ": " + b.label
}

func (a *App) defineBindings() []*binding {
	nav := []string{viewModules, viewBoot, viewStatus, viewChanges, viewDetails, viewLog, viewHelp, viewConfirm, viewParams, viewMessage}
	onModulesTab := func() bool { return a.tab == tabModules }
	onDevicesTab := func() bool { return a.tab == tabDevices }
	loadedModule := a.modulePred(func(e kmod.Entry) bool { return e.State == kmod.Loaded })
	availableModule := a.modulePred(func(e kmod.Entry) bool { return e.State == kmod.Available })
	knownModule := a.modulePred(func(kmod.Entry) bool { return true })
	toggleHint := func(kind kmod.BootKind, on, off string) func() string {
		return func() string {
			if e, ok := a.selectedModule(); ok && a.inBoot(kind, e.Name) {
				return off
			}
			return on
		}
	}
	act := func() *action { return a.act }
	canRun := func() bool { return a.act != nil && a.exec.available() == nil && a.act.ready() }

	// Globals come first: when a view binding declines a key, gocui only
	// falls back to global bindings registered before it.
	bs := []*binding{
		{keys: []any{'1'}, label: "1", desc: "Focus Status", handler: a.focusHandler(viewStatus)},
		{keys: []any{'2'}, label: "2", desc: "Focus Modules / Devices", handler: a.focusHandler(viewModules)},
		{keys: []any{'3'}, label: "3", desc: "Focus Boot config", handler: a.focusHandler(viewBoot)},
		{keys: []any{'4'}, label: "4", desc: "Focus Changes", handler: a.focusHandler(viewChanges)},
		{keys: []any{'0'}, label: "0", desc: "Focus Details", handler: a.focusHandler(viewDetails)},
		{keys: []any{gocui.KeyTab}, label: "tab", desc: "Next panel", handler: func() error { return a.cycleFocus(1) }},
		{keys: []any{gocui.KeyBacktab}, label: "shift+tab", desc: "Previous panel", handler: func() error { return a.cycleFocus(-1) }},
		{keys: []any{'R'}, label: "R", desc: "Refresh", bar: true, handler: a.refresh},
		{keys: []any{'?'}, label: "?", desc: "Keybindings", bar: true, handler: a.openHelp},
		{keys: []any{'q'}, label: "q", desc: "Quit", bar: true, handler: quit},
		{keys: []any{gocui.KeyCtrlC}, label: "ctrl+c", desc: "Quit", popups: true, handler: quit},

		{views: nav, keys: []any{'j', gocui.KeyArrowDown}, label: "j/↓", desc: "Down", handler: func() error { return a.move(1) }},
		{views: nav, keys: []any{'k', gocui.KeyArrowUp}, label: "k/↑", desc: "Up", handler: func() error { return a.move(-1) }},
		{views: nav, keys: []any{gocui.KeyPgdn}, label: "pgdn", desc: "Page down", handler: func() error { return a.move(a.pageSize()) }},
		{views: nav, keys: []any{gocui.KeyPgup}, label: "pgup", desc: "Page up", handler: func() error { return a.move(-a.pageSize()) }},
		{views: nav, keys: []any{'g', gocui.KeyHome}, label: "g/home", desc: "Top", handler: func() error { return a.move(-bigMove) }},
		{views: nav, keys: []any{'G', gocui.KeyEnd}, label: "G/end", desc: "Bottom", handler: func() error { return a.move(bigMove) }},

		{views: []string{viewModules}, keys: []any{gocui.KeyEnter}, label: "enter", desc: "Go to the device's module", bar: true,
			hint: func() string { return "Go to module: enter" }, when: onDevicesTab, handler: a.gotoDeviceModule},
		{views: []string{viewModules}, keys: []any{'/'}, label: "/", desc: "Filter", bar: true, handler: a.openFilter},
		{views: []string{viewModules}, keys: []any{gocui.KeyEsc}, label: "esc", desc: "Clear filter", bar: true,
			when: func() bool { return a.filters[a.tab] != "" }, handler: a.clearFilter},
		{views: []string{viewModules}, keys: []any{'A'}, label: "A", desc: "Toggle loaded / all modules", bar: true,
			hint: func() string {
				if a.showAll {
					return "Loaded only: A"
				}
				return "All modules: A"
			}, when: onModulesTab, handler: a.toggleAll},
		{views: []string{viewModules}, keys: []any{'['}, label: "[", desc: "Previous tab", handler: func() error { return a.switchTab(-1) }},
		{views: []string{viewModules}, keys: []any{']'}, label: "]", desc: "Next tab", bar: true,
			hint: func() string { return "Tabs: [ ]" }, handler: func() error { return a.switchTab(1) }},

		// Writes: each opens the confirmation dialog first.
		{views: []string{viewModules}, keys: []any{'u'}, label: "u", desc: "Unload the module", bar: true,
			hint: func() string { return "Unload: u" }, when: loadedModule, handler: a.unloadSelected},
		{views: []string{viewModules}, keys: []any{'l'}, label: "l", desc: "Load the module", bar: true,
			hint: func() string { return "Load: l" }, when: availableModule, handler: a.loadSelected},
		{views: []string{viewModules}, keys: []any{'b'}, label: "b", desc: "Blacklist / remove the blacklist entry", bar: true,
			hint: toggleHint(kmod.BootBlacklist, "Blacklist: b", "Unblacklist: b"), when: knownModule, handler: a.toggleBlacklist},
		{views: []string{viewModules}, keys: []any{'p'}, label: "p", desc: "Parameters", bar: true,
			hint: func() string { return "Params: p" }, when: knownModule, handler: a.openParams},
		{views: []string{viewModules}, keys: []any{'a'}, label: "a", desc: "Load at boot / stop loading at boot", bar: true,
			hint: toggleHint(kmod.BootLoad, "Boot load: a", "No boot load: a"), when: knownModule, handler: a.toggleBootLoad},

		{views: []string{viewBoot}, keys: []any{'d'}, label: "d", desc: "Remove the selected entry", bar: true,
			hint: func() string { return "Remove: d" },
			when: func() bool { _, ok := a.bootList.selected(); return ok }, handler: a.removeSelectedBootEntry},
		{views: []string{viewChanges}, keys: []any{gocui.KeyEnter}, label: "enter", desc: "Fix the selected item", bar: true,
			hint: func() string { return "Fix: enter" },
			when: func() bool { c, ok := a.changes.selected(); return ok && c.fix != nil }, handler: a.fixSelectedChange},

		{views: []string{viewConfirm}, keys: []any{gocui.KeyEnter}, label: "enter", desc: "Run (when there is no warning)", bar: true,
			hint: func() string { return "Run: enter" },
			when: func() bool { return canRun() && len(act().warnings) == 0 }, handler: func() error { return a.runConfirmed(false) }},
		{views: []string{viewConfirm}, keys: []any{'y'}, label: "y", desc: "Run, also despite warnings", bar: true,
			hint: func() string { return "Run anyway: y" },
			when: func() bool { return canRun() && len(act().warnings) > 0 }, handler: func() error { return a.runConfirmed(true) }},
		{views: []string{viewConfirm}, keys: []any{'1'}, label: "1", desc: "Toggle: apply now", bar: true,
			hint: func() string { return "Now: 1" },
			when: func() bool { return act() != nil && act().param != nil && act().param.canNow }, handler: func() error { return a.toggleParamMode(false) }},
		{views: []string{viewConfirm}, keys: []any{'2'}, label: "2", desc: "Toggle: apply at next load", bar: true,
			hint: func() string { return "At next load: 2" },
			when: func() bool { return act() != nil && act().param != nil }, handler: func() error { return a.toggleParamMode(true) }},
		{views: []string{viewConfirm}, keys: []any{gocui.KeyEsc, 'n'}, label: "esc/n", desc: "Cancel", bar: true,
			hint: func() string { return "Cancel: esc" }, handler: a.cancelConfirm},

		{views: []string{viewParams}, keys: []any{gocui.KeyEnter}, label: "enter", desc: "Change the selected parameter", bar: true,
			hint: func() string { return "Change: enter" }, handler: a.editParam},
		{views: []string{viewParams}, keys: []any{'x'}, label: "x", desc: "Remove its at-next-load value", bar: true,
			hint: func() string { return "Remove at-next-load value: x" },
			when: func() bool { r, ok := a.params.selected(); return ok && r.boot != "" }, handler: a.clearParamBoot},
		{views: []string{viewParams}, keys: []any{gocui.KeyEsc, 'q'}, label: "esc", desc: "Close", bar: true, handler: a.closePopup},

		{views: []string{viewValue}, keys: []any{gocui.KeyEnter}, label: "enter", desc: "Use this value", bar: true,
			hint: func() string { return "Set: enter" }, handler: a.submitValue},
		{views: []string{viewValue}, keys: []any{gocui.KeyEsc}, label: "esc", desc: "Cancel", bar: true,
			hint: func() string { return "Cancel: esc" }, handler: a.closePopup},

		{views: []string{viewMessage}, keys: []any{gocui.KeyEsc, gocui.KeyEnter, 'q'}, label: "esc", desc: "Close", bar: true, handler: a.closePopup},

		{views: []string{viewFilter}, keys: []any{gocui.KeyEnter, gocui.KeyTab}, label: "enter", desc: "Keep filter", handler: a.closeFilter},
		{views: []string{viewFilter}, keys: []any{gocui.KeyEsc}, label: "esc", desc: "Clear filter", handler: a.cancelFilter},
		{views: []string{viewFilter}, keys: []any{gocui.KeyArrowDown}, label: "↓", desc: "Down (while filtering)", handler: func() error { return a.moveList(1) }},
		{views: []string{viewFilter}, keys: []any{gocui.KeyArrowUp}, label: "↑", desc: "Up (while filtering)", handler: func() error { return a.moveList(-1) }},

		{views: []string{viewHelp}, keys: []any{gocui.KeyEsc, 'q', '?'}, label: "esc", desc: "Close", bar: true, handler: a.closeHelp},
	}
	return bs
}

const bigMove = 1 << 30

func quit() error { return gocui.ErrQuit }

func (a *App) registerBindings() error {
	a.bindings = a.defineBindings()
	for _, b := range a.bindings {
		views := b.views
		if views == nil {
			views = []string{""}
		}
		for _, view := range views {
			for _, key := range b.keys {
				if err := a.g.SetKeybinding(view, key, gocui.ModNone, a.wrap(b)); err != nil {
					return fmt.Errorf("binding %s on %q: %w", b.label, view, err)
				}
			}
		}
	}
	return nil
}

func (a *App) wrap(b *binding) func(*gocui.Gui, *gocui.View) error {
	return func(*gocui.Gui, *gocui.View) error {
		if b.views == nil && a.topPopup() != "" && !b.popups {
			return nil
		}
		if !b.active() {
			return gocui.ErrKeybindingNotHandled
		}
		return b.handler()
	}
}

// hints renders the bottom bar: the focused view's bindings, then globals.
func (a *App) hints() string {
	cur := a.currentView()
	var parts []string
	for _, global := range []bool{false, true} {
		for _, b := range a.bindings {
			if !b.bar || !b.active() || (b.views == nil) != global {
				continue
			}
			if !global && !slices.Contains(b.views, cur) {
				continue
			}
			if global && a.topPopup() != "" {
				continue
			}
			parts = append(parts, b.barText())
		}
	}
	return strings.Join(parts, dim(" | "))
}

// helpGroup is the keybindings-popup section a binding is listed under.
func helpGroup(b *binding) string {
	switch {
	case b.views == nil:
		return "Global"
	case len(b.views) > 1:
		return "Lists, details and log"
	case b.views[0] == viewModules:
		return "Modules / Devices panel"
	case b.views[0] == viewBoot:
		return "Boot config panel"
	case b.views[0] == viewChanges:
		return "Changes panel"
	case b.views[0] == viewFilter:
		return "Filter prompt"
	case b.views[0] == viewConfirm:
		return "Confirmation dialog"
	case b.views[0] == viewParams || b.views[0] == viewValue:
		return "Parameters"
	}
	return "" // the help and message popups' own keys
}

// helpText lists every binding, grouped by where it applies.
func (a *App) helpText() string {
	var sb strings.Builder
	for i, group := range []string{"Global", "Lists, details and log", "Modules / Devices panel", "Boot config panel", "Changes panel", "Parameters", "Confirmation dialog", "Filter prompt"} {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(bold(group) + "\n")
		for _, b := range a.bindings {
			if helpGroup(b) == group {
				sb.WriteString("  " + cyan(fit(b.label, 10)) + " " + b.desc + "\n")
			}
		}
	}
	sb.WriteString("\n" + dim("Every change is confirmed first and runs through pkexec "+HelperPath+"."))
	return sb.String()
}
