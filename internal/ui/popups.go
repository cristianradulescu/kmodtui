package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jesseduffield/gocui"

	"github.com/cristianradulescu/kmodtui/internal/kmod"
)

// Popups stack: the filter prompt, the keybindings help, the confirmation
// dialog, the parameters list, the value prompt and messages. The topmost
// popup has the focus; closing the last one returns to the view that was
// focused before the first one opened.

func (a *App) topPopup() string {
	if len(a.popups) == 0 {
		return ""
	}
	return a.popups[len(a.popups)-1]
}

func (a *App) openPopup(name string) error {
	if len(a.popups) == 0 {
		a.beforePopup = a.currentView()
	}
	a.popups = append(a.popups, name)
	a.g.Cursor = name == viewFilter || name == viewValue
	if err := a.g.ForceLayoutAndRedraw(); err != nil {
		return err
	}
	if _, err := a.g.SetCurrentView(name); err != nil {
		return err
	}
	a.renderBar()
	return nil
}

func (a *App) closePopup() error {
	if len(a.popups) == 0 {
		return nil
	}
	name := a.topPopup()
	a.popups = a.popups[:len(a.popups)-1]
	if err := a.g.DeleteView(name); err != nil {
		return err
	}
	top := a.topPopup()
	a.g.Cursor = top == viewFilter || top == viewValue
	if top == "" {
		return a.focus(a.beforePopup)
	}
	if _, err := a.g.SetCurrentView(top); err != nil {
		return err
	}
	a.renderBar()
	return nil
}

func (a *App) closeAllPopups() error {
	for len(a.popups) > 0 {
		if err := a.closePopup(); err != nil {
			return err
		}
	}
	return nil
}

// layoutPopups positions the open popups, creating and filling them on
// first use, and stacks them in opening order.
func (a *App) layoutPopups(g *gocui.Gui, maxX, maxY int) error {
	for _, name := range a.popups {
		var x0, y0, x1, y1 int
		switch name {
		case viewFilter:
			// One line in the key-hint bar, right after the "Filter:" label.
			x0, y0, x1, y1 = len("Filter: ")-1, maxY-2, maxX, maxY
		case viewValue:
			w := min(maxX-4, 60)
			x0, y0 = (maxX-w)/2, maxY/2-1
			x1, y1 = x0+w, y0+2
		default:
			w, h := a.popupSize(name, maxX, maxY)
			x0, y0 = (maxX-w)/2, (maxY-h)/2
			x1, y1 = x0+w, y0+h
		}
		v, created, err := setView(g, name, x0, y0, x1, y1)
		if err != nil {
			return err
		}
		if created {
			a.configure(v)
			a.fillPopup(v)
		}
		if _, err := g.SetViewOnTop(name); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) popupSize(name string, maxX, maxY int) (int, int) {
	w := min(maxX-4, 88)
	lines := 10
	switch name {
	case viewHelp:
		w = min(maxX-4, 72)
		lines = strings.Count(a.helpText(), "\n") + 1
	case viewConfirm:
		lines = strings.Count(a.confirmText(w-1), "\n") + 1
	case viewParams:
		lines = a.params.len() + 4 // header, rows, blank, two description lines
	case viewMessage:
		lines = strings.Count(a.message, "\n") + 2
	}
	return w, min(maxY-4, lines+1)
}

func (a *App) fillPopup(v *gocui.View) {
	switch v.Name() {
	case viewFilter:
		v.Frame = false
		v.Editable = true
		v.Editor = gocui.EditorFunc(a.filterEditor)
		v.TextArea.TypeString(a.filters[a.listKey(a.beforePopup)])
		v.RenderTextArea()
	case viewHelp:
		fmt.Fprint(v, a.helpText())
	case viewConfirm:
		a.renderConfirm()
	case viewParams:
		v.Title = "Parameters of " + a.paramModule
		a.renderParams()
	case viewValue:
		if r, ok := a.params.selected(); ok {
			v.Title = a.paramModule + "." + r.name + " ="
			v.TextArea.TypeString(r.initial())
			v.RenderTextArea()
		}
	case viewMessage:
		v.Title = "kmodtui"
		fmt.Fprint(v, a.message)
	}
}

// Filter prompt.

func (a *App) openFilter() error { return a.openPopup(viewFilter) }

func (a *App) filterEditor(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) bool {
	handled := gocui.SimpleEditor(v, key, ch, mod)
	if handled {
		a.applyFilter(a.beforePopup, strings.TrimSpace(v.TextArea.GetContent()))
	}
	return handled
}

// applyFilter filters the list the given panel shows right now.
func (a *App) applyFilter(view, f string) {
	key := a.listKey(view)
	a.filters[key] = f
	switch key {
	case "modules":
		a.mods.setFilter(f)
	case "devices":
		a.devs.setFilter(f)
	case "boot":
		a.bootList.setFilter(f)
	case "allconf":
		a.allList.setFilter(f)
	}
	if view == viewBoot {
		a.renderBoot()
	} else {
		a.renderModules()
	}
	a.renderDetails()
}

func (a *App) closeFilter() error { return a.closePopup() }

func (a *App) cancelFilter() error {
	a.applyFilter(a.beforePopup, "")
	return a.closePopup()
}

func (a *App) clearFilter() error {
	a.applyFilter(a.currentView(), "")
	a.renderBar()
	return nil
}

// Help and messages.

func (a *App) openHelp() error  { return a.openPopup(viewHelp) }
func (a *App) closeHelp() error { return a.closePopup() }

func (a *App) showMessage(text string) error {
	a.message = text
	if a.topPopup() == viewMessage {
		if err := a.closePopup(); err != nil {
			return err
		}
	}
	return a.openPopup(viewMessage)
}

// Confirmation dialog.

func (a *App) confirm(act *action) error {
	a.act = act
	return a.openPopup(viewConfirm)
}

func (a *App) renderConfirm() {
	v := a.view(viewConfirm)
	if v == nil || a.act == nil {
		return
	}
	v.Title = a.act.title
	v.Clear()
	fmt.Fprint(v, a.confirmText(v.InnerWidth()))
}

// confirmText shows exactly what will run, what it does, the dry run and
// any warnings.
func (a *App) confirmText(width int) string {
	act := a.act
	if act == nil {
		return ""
	}
	d := &details{width: width}
	d.line(bold("kmodtui will run"))
	for _, args := range act.runs() {
		d.line("  " + cyan("$ pkexec "+HelperPath+" "+strings.Join(args, " ")))
	}
	d.line(bold("which does"))
	for _, c := range act.effects() {
		d.line("  " + c)
	}
	if act.param != nil {
		d.line("")
		d.line(bold("Apply"))
		d.line("  " + checkbox(act.param.now, act.param.canNow) + " 1  now: write /sys/module/" + kmod.Normalize(act.param.module) + "/parameters/" + act.param.name)
		if !act.param.canNow {
			d.line("        " + dim(act.param.whyNotNow))
		}
		d.line("  " + checkbox(act.param.boot, true) + " 2  at next load: options line in " + kmod.ModprobeConf)
	}
	if act.preview != "" || act.previewErr != nil {
		d.line("")
		d.line(bold("Dry run") + dim("  "+act.previewCmd))
		for _, l := range strings.Split(act.preview, "\n") {
			if l != "" {
				d.line("  " + l)
			}
		}
		if act.previewErr != nil {
			d.line("  " + yellow(errText(act.previewErr)))
		}
	}
	if len(act.warnings) > 0 {
		d.line("")
		d.line(red(bold("Careful")))
		for _, w := range act.warnings {
			d.line("  " + red("! ") + w)
		}
	}
	for i, n := range act.notes {
		if i == 0 {
			d.line("")
		}
		d.line(dim(n))
	}
	d.line("")
	switch err := a.exec.available(); {
	case err != nil:
		d.line(red(errText(err)))
		d.line(dim("esc: close"))
	case !act.ready():
		d.line(dim("Choose at least one of 1 / 2.  esc: cancel"))
	case len(act.warnings) > 0:
		d.line(bold("y") + ": run anyway   " + bold("esc") + ": cancel")
	default:
		d.line(bold("enter") + ": run   " + bold("esc") + ": cancel")
	}
	return strings.TrimRight(d.String(), "\n")
}

func checkbox(on, enabled bool) string {
	switch {
	case !enabled:
		return dim("[-]")
	case on:
		return green("[x]")
	}
	return "[ ]"
}

func (a *App) cancelConfirm() error {
	a.act = nil
	return a.closePopup()
}

// runConfirmed runs the confirmed action. With warnings, Enter is refused
// and only 'y' runs it, so a reflexive Enter cannot unload the disk driver.
func (a *App) runConfirmed(explicit bool) error {
	act := a.act
	if act == nil || a.exec.available() != nil || !act.ready() || (len(act.warnings) > 0 && !explicit) {
		return nil
	}
	if err := a.closeAllPopups(); err != nil {
		return err
	}
	a.act = nil
	return a.run(act)
}

func (a *App) toggleParamMode(boot bool) error {
	if a.act == nil || a.act.param == nil {
		return nil
	}
	p := a.act.param
	if boot {
		p.boot = !p.boot
	} else if p.canNow {
		p.now = !p.now
	}
	a.renderConfirm()
	a.renderBar()
	return nil
}

// Parameters popup and value prompt.

// paramRow is one parameter in the parameters popup.
type paramRow struct {
	name, desc string
	value      string // current value, if the module is loaded
	hasValue   bool
	writable   bool   // can change at runtime
	boot       string // value set in kmodtui's options line, if any
}

// initial is the value the prompt starts with.
func (r paramRow) initial() string {
	switch {
	case r.boot != "":
		return r.boot
	case r.hasValue:
		return r.value
	}
	return ""
}

func (a *App) paramRows(e kmod.Entry, mi *moduleInfo) []paramRow {
	rows := map[string]*paramRow{}
	for _, p := range mi.info.Params {
		desc := p.Desc
		if p.Type != "" {
			desc = strings.TrimSpace(desc + " [" + p.Type + "]")
		}
		rows[p.Name] = &paramRow{name: p.Name, desc: desc}
	}
	for _, p := range mi.params {
		r := rows[p.Name]
		if r == nil {
			r = &paramRow{name: p.Name}
			rows[p.Name] = r
		}
		r.hasValue = p.Unreadable == ""
		r.value = p.Value
		r.writable = p.Writable
	}
	for _, opt := range kmod.Options(a.boot, e.Name) {
		name, value, _ := strings.Cut(opt, "=")
		if r := rows[name]; r != nil {
			r.boot = value
		} else {
			rows[name] = &paramRow{name: name, boot: value}
		}
	}
	out := make([]paramRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	slices.SortFunc(out, func(x, y paramRow) int { return strings.Compare(x.name, y.name) })
	return out
}

func (a *App) openParams() error {
	e, ok := a.selectedModule()
	if !ok {
		return nil
	}
	mi := a.moduleInfo(e.Name)
	switch {
	case mi == nil:
		return a.showMessage("modinfo for " + e.Name + " is still loading; try again in a moment.")
	case mi.infoErr != nil:
		return a.showMessage("Cannot list the parameters of " + e.Name + ":\n" + errText(mi.infoErr))
	}
	rows := a.paramRows(e, mi)
	if len(rows) == 0 {
		return a.showMessage(e.Name + " has no parameters.")
	}
	a.paramModule = e.Name
	a.params.set(rows)
	return a.openPopup(viewParams)
}

func (a *App) renderParams() {
	v := a.view(viewParams)
	if v == nil {
		return
	}
	v.Clear()
	nameW, valueW := 0, 0
	for i := 0; i < a.params.len(); i++ {
		r := a.params.at(i)
		nameW = max(nameW, len(r.name))
		valueW = max(valueW, len(r.value))
	}
	nameW, valueW = min(nameW, 28), min(valueW, 12)
	var sb strings.Builder
	sb.WriteString(dim(padRight("parameter", nameW)+"  "+padRight("now", valueW)+"  at next load") + "\n")
	for i := 0; i < a.params.len(); i++ {
		r := a.params.at(i)
		now := dim("—")
		if r.hasValue {
			now = r.value
			if now == "" {
				now = dim(`""`)
			}
		}
		boot := dim("—")
		if r.boot != "" {
			boot = cyan(r.boot)
		}
		ro := ""
		if r.hasValue && !r.writable {
			ro = dim("  read-only now")
		}
		sb.WriteString(padRight(r.name, nameW) + "  " + padRight(now, valueW) + "  " + padRight(boot, 12) + ro + "\n")
	}
	// The selected parameter's description, below the table.
	if r, ok := a.params.selected(); ok && r.desc != "" {
		sb.WriteString("\n" + indentWrap(dim(r.desc), v.InnerWidth(), 0) + "\n")
	}
	fmt.Fprint(v, sb.String())
	a.syncParamsSelection()
}

// syncParamsSelection highlights the selected parameter; row 0 is the header.
func (a *App) syncParamsSelection() {
	if v := a.view(viewParams); v != nil {
		v.FocusPoint(0, a.params.sel+1, true)
		v.Footer = a.params.footer()
	}
	a.renderBar()
}

func (a *App) editParam() error {
	a.valueErr = ""
	return a.openPopup(viewValue)
}

func (a *App) submitValue() error {
	v := a.view(viewValue)
	r, ok := a.params.selected()
	if v == nil || !ok {
		return nil
	}
	value := strings.TrimSpace(v.TextArea.GetContent())
	if err := kmod.ValidParamValue(value); err != nil {
		a.valueErr = err.Error()
		a.renderBar()
		return nil
	}
	return a.confirm(a.setParamAction(r, value))
}

func (a *App) clearParamBoot() error {
	r, ok := a.params.selected()
	if !ok || r.boot == "" {
		return nil
	}
	return a.confirm(a.clearParamAction(r))
}
