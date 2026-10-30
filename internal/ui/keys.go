package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jesseduffield/gocui"
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
	nav := []string{viewModules, viewBoot, viewStatus, viewChanges, viewDetails, viewLog, viewHelp}
	onModulesTab := func() bool { return a.tab == tabModules }
	onDevicesTab := func() bool { return a.tab == tabDevices }

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
		if b.views == nil && a.popup != "" && !b.popups {
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
			if global && a.popup != "" {
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
	case b.views[0] == viewFilter:
		return "Filter prompt"
	}
	return "" // the popup's own keys
}

// helpText lists every binding, grouped by where it applies.
func (a *App) helpText() string {
	var sb strings.Builder
	for i, group := range []string{"Global", "Lists, details and log", "Modules / Devices panel", "Filter prompt"} {
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
	sb.WriteString("\n" + dim("Read-only (phase 1): nothing here changes the system."))
	return sb.String()
}
