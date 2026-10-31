package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jesseduffield/gocui"

	"github.com/cristianradulescu/kmodtui/internal/kmod"
)

func (a *App) view(name string) *gocui.View {
	v, err := a.g.View(name)
	if err != nil {
		return nil
	}
	return v
}

func (a *App) renderAll() {
	a.renderStatus()
	a.renderModules()
	a.renderBoot()
	a.renderChanges()
	a.renderDetails()
	a.renderLog()
	a.renderBar()
}

// syncSelection points a list view's cursor (the highlighted row) at sel.
func syncSelection(v *gocui.View, n, sel int) {
	v.Highlight = n > 0
	if n > 0 {
		v.FocusPoint(0, sel, true)
	} else {
		v.SetOrigin(0, 0)
		v.SetCursor(0, 0)
	}
}

// Status panel.

func (a *App) statusErr(i int) error {
	if i < len(a.statusOps) {
		return a.statusOps[i].Err
	}
	return nil
}

func (a *App) renderStatus() {
	v := a.view(viewStatus)
	if v == nil {
		return
	}
	v.Clear()
	rel := a.status.Release
	if a.statusErr(0) != nil {
		rel = red("unknown")
	}
	fmt.Fprintln(v, dim(fit("Kernel", 12))+rel)
	fmt.Fprintln(v, dim(fit("Secure Boot", 12))+secureBootText(a.status.SecureBoot, a.statusErr(1)))
	fmt.Fprint(v, dim(fit("Taint", 12))+taintText(a.status, a.statusErr(2)))
}

func secureBootText(sb kmod.SecureBoot, err error) string {
	if err != nil {
		return red("unknown")
	}
	var s string
	switch sb.State {
	case kmod.SBEnabled:
		s = green("enabled")
	case kmod.SBDisabled:
		s = yellow("disabled")
	default:
		s = dim(sb.State.String())
	}
	for _, n := range sb.Notes {
		if strings.Contains(n, "Setup Mode") {
			s += dim(" (setup mode)")
		}
	}
	return s
}

func taintText(st kmod.Status, err error) string {
	switch {
	case err != nil:
		return red("unknown")
	case st.Taint == 0:
		return green("none")
	}
	return yellow(kmod.TaintLetters(st.TaintFlags)) + dim(fmt.Sprintf(" (%d)", st.Taint))
}

// Modules / Devices panel.

func (a *App) renderModules() {
	v := a.view(viewModules)
	if v == nil {
		return
	}
	v.TabIndex = a.tab
	v.Clear()
	if a.tab == tabModules {
		a.writeModuleRows(v, v.InnerWidth())
	} else {
		a.writeDeviceRows(v, v.InnerWidth())
	}
	a.syncModulesSelection()
}

func (a *App) syncModulesSelection() {
	v := a.view(viewModules)
	if v == nil {
		return
	}
	// Filter and "all" go in the bottom border next to "x of y": the top
	// border is taken by the tabs on narrow panels.
	var footer []string
	if f := a.filters[a.tab]; f != "" {
		footer = append(footer, "filter: "+f)
	}
	counter := ""
	if a.tab == tabModules {
		if a.showAll {
			footer = append(footer, "all")
		}
		counter = a.mods.footer()
		syncSelection(v, a.mods.len(), a.mods.sel)
	} else {
		counter = a.devs.footer()
		syncSelection(v, a.devs.len(), a.devs.sel)
	}
	if counter != "" {
		footer = append(footer, counter)
	}
	v.Footer = strings.Join(footer, " · ")
}

func (a *App) writeModuleRows(v *gocui.View, width int) {
	if a.mods.len() == 0 {
		switch {
		case a.loadedErr != nil && len(a.loaded) == 0:
			fmt.Fprint(v, red("Cannot list modules: "+errText(a.loadedErr)))
		case a.filters[tabModules] != "":
			fmt.Fprint(v, dim("No modules match the filter"))
		default:
			fmt.Fprint(v, dim("No modules"))
		}
		return
	}
	const sizeW, usedW = 6, 3
	markerW := 0
	if a.showAll {
		markerW = 2
	}
	longest := 0
	for i := 0; i < a.mods.len(); i++ {
		longest = max(longest, len(moduleLabel(a.mods.at(i))))
	}
	nameW := max(min(longest, width-markerW-sizeW-usedW-2), 8)

	var sb strings.Builder
	for i := 0; i < a.mods.len(); i++ {
		e := a.mods.at(i)
		if a.showAll {
			sb.WriteString(stateMarker(e.State) + " ")
		}
		label := moduleLabel(e)
		cell := fit(label, nameW)
		if label != e.Name && strings.HasPrefix(cell, label) {
			// Colour the taint suffix, e.g. "vboxdrv OE".
			cell = e.Name + yellow(label[len(e.Name):]) + cell[len(label):]
		}
		sb.WriteString(cell + " ")
		switch e.State {
		case kmod.Loaded:
			used := "-"
			if e.Module.RefCount >= 0 {
				used = strconv.Itoa(e.Module.RefCount)
			}
			sb.WriteString(padLeft(shortSize(e.Module.Size), sizeW) + " " + padLeft(used, usedW))
		case kmod.Available:
			sb.WriteString(dim(padLeft("-", sizeW) + " " + padLeft("-", usedW)))
		case kmod.Builtin:
			sb.WriteString(dim(padLeft("built-in", sizeW+1+usedW)))
		}
		sb.WriteString("\n")
	}
	fmt.Fprint(v, sb.String())
}

// moduleLabel is the module name plus its taint letters, if any.
func moduleLabel(e kmod.Entry) string {
	if e.Module != nil && e.Module.Taint != "" {
		return e.Name + " " + e.Module.Taint
	}
	return e.Name
}

func stateMarker(s kmod.State) string {
	switch s {
	case kmod.Loaded:
		return green("●")
	case kmod.Builtin:
		return cyan("■")
	}
	return dim("○")
}

func (a *App) writeDeviceRows(v *gocui.View, width int) {
	if a.devs.len() == 0 {
		switch {
		case a.devicesErr != nil && len(a.devices) == 0:
			fmt.Fprint(v, red("Cannot list devices: "+errText(a.devicesErr)))
		case a.filters[tabDevices] != "":
			fmt.Fprint(v, dim("No devices match the filter"))
		default:
			fmt.Fprint(v, dim("No devices"))
		}
		return
	}
	addrW, modW := 7, 1
	for i := 0; i < a.devs.len(); i++ {
		d := a.devs.at(i)
		addrW = max(addrW, len(shortAddr(d)))
		modW = max(modW, len(driverText(d)))
	}
	modW = min(modW, 18)
	nameW := max(width-3-1-addrW-1-modW-1, 8)

	var sb strings.Builder
	for i := 0; i < a.devs.len(); i++ {
		d := a.devs.at(i)
		sb.WriteString(dim(d.Bus) + " " + fit(shortAddr(d), addrW) + " " + fit(deviceName(d), nameW) + " ")
		mod := fit(driverText(d), modW)
		switch {
		case len(d.Drivers()) > 0:
			sb.WriteString(green(mod))
		case len(d.Candidates()) > 0:
			sb.WriteString(yellow(mod))
		default:
			sb.WriteString(dim(mod))
		}
		sb.WriteString("\n")
	}
	fmt.Fprint(v, sb.String())
}

// shortAddr drops the PCI domain when it is the usual 0000.
func shortAddr(d kmod.Device) string {
	return strings.TrimPrefix(d.Addr, "0000:")
}

func deviceName(d kmod.Device) string {
	for _, s := range []string{d.Product, d.ClassName, d.Vendor} {
		if s != "" {
			return s
		}
	}
	return d.VendorID + ":" + d.ProductID
}

// driverText is the module column: bound modules, candidate modules for an
// unbound device (with a "?"), or a dash.
func driverText(d kmod.Device) string {
	if drv := d.Drivers(); len(drv) > 0 {
		return strings.Join(drv, ",")
	}
	if c := d.Candidates(); len(c) > 0 {
		return strings.Join(c, ",") + "?"
	}
	return "—"
}

// Boot config and Changes panels.

func (a *App) renderBoot() {
	v := a.view(viewBoot)
	if v == nil {
		return
	}
	v.Clear()
	switch {
	case a.bootList.len() > 0:
		var sb strings.Builder
		for i := 0; i < a.bootList.len(); i++ {
			e := a.bootList.at(i)
			sb.WriteString(bootKindText(e.Kind) + " " + e.Module)
			if e.Kind == kmod.BootOptions {
				sb.WriteString(" " + dim(e.Args))
			}
			sb.WriteString("\n")
		}
		fmt.Fprint(v, sb.String())
	case a.bootErr != nil:
		fmt.Fprint(v, red("Cannot read boot config: "+errText(a.bootErr)))
	default:
		fmt.Fprint(v, dim("No entries managed by kmodtui"))
	}
	a.syncBootSelection()
}

func (a *App) syncBootSelection() {
	if v := a.view(viewBoot); v != nil {
		v.Footer = a.bootList.footer()
		syncSelection(v, a.bootList.len(), a.bootList.sel)
	}
}

func bootKindText(k kmod.BootKind) string {
	s := fit(k.String(), 12)
	switch k {
	case kmod.BootBlacklist:
		return red(s)
	case kmod.BootLoad:
		return green(s)
	case kmod.BootOptions:
		return cyan(s)
	}
	return dim(s)
}

func (a *App) renderChanges() {
	v := a.view(viewChanges)
	if v == nil {
		return
	}
	v.Clear()
	if a.changes.len() == 0 {
		fmt.Fprint(v, dim("No pending changes"))
	} else {
		var sb strings.Builder
		for i := 0; i < a.changes.len(); i++ {
			sb.WriteString(a.changes.at(i).text + "\n")
		}
		fmt.Fprint(v, sb.String())
	}
	a.syncChangesSelection()
}

func (a *App) syncChangesSelection() {
	if v := a.view(viewChanges); v != nil {
		v.Footer = a.changes.footer()
		syncSelection(v, a.changes.len(), a.changes.sel)
	}
}

// Bottom bar.

func (a *App) renderBar() {
	v := a.view(viewOptions)
	if v == nil {
		return
	}
	v.Clear()
	switch {
	case a.topPopup() == viewFilter:
		// The editable filter view starts right after this label.
		fmt.Fprint(v, green("Filter:"))
	case a.topPopup() == viewValue && a.valueErr != "":
		fmt.Fprint(v, red(a.valueErr)+dim(" | ")+a.hints())
	default:
		fmt.Fprint(v, a.hints())
	}
}

// Command log.

func (a *App) logOps(ops ...kmod.Op) {
	for _, op := range ops {
		a.appendLog(cyan("$ "+op.Cmd) + "  " + dim("# "+op.Desc))
		if op.Err != nil {
			a.appendLog("  " + red(errText(op.Err)))
		}
	}
}

func (a *App) appendLog(line string) {
	a.logLines = append(a.logLines, line)
	if len(a.logLines) > maxLogLines {
		a.logLines = slices.Clone(a.logLines[len(a.logLines)-maxLogLines:])
		a.renderLog()
		return
	}
	if !a.ready {
		return // written by the first renderLog
	}
	if v := a.view(viewLog); v != nil {
		fmt.Fprintln(v, indentWrap(line, v.InnerWidth(), 2))
	}
}

func (a *App) renderLog() {
	v := a.view(viewLog)
	if v == nil {
		return
	}
	v.Clear()
	var sb strings.Builder
	for _, l := range a.logLines {
		sb.WriteString(indentWrap(l, v.InnerWidth(), 2) + "\n")
	}
	fmt.Fprint(v, sb.String())
}

// Details view.

// details builds the Details text: aligned "label  value" fields, sections
// and the equivalent CLI commands. Long lines are wrapped to width with
// continuation lines indented, which gocui's own wrapping cannot do.
type details struct {
	sb    strings.Builder
	width int
}

const labelW = 13

func (a *App) newDetails() *details {
	d := &details{}
	if v := a.view(viewDetails); v != nil {
		d.width = v.InnerWidth()
	}
	return d
}

func (d *details) line(s string) { d.sb.WriteString(indentWrap(s, d.width, 2) + "\n") }

func (d *details) field(label, value string) { d.hang(dim(fit(label, labelW)), value) }

// hang writes prefix followed by text, wrapping text so continuation lines
// line up under its first character.
func (d *details) hang(prefix, text string) {
	pw := visibleLen(prefix)
	avail := d.width - pw
	if d.width <= 0 || visibleLen(text) <= avail {
		d.sb.WriteString(prefix + text + "\n")
		return
	}
	if avail < 16 && pw > 6 {
		// The prefix leaves too little room: give the text its own lines.
		d.sb.WriteString(prefix + "\n")
		prefix, pw, avail = "      ", 6, d.width-6
	}
	lines := wrapText(text, avail, avail)
	d.sb.WriteString(prefix + lines[0] + "\n")
	for _, l := range lines[1:] {
		d.sb.WriteString(strings.Repeat(" ", pw) + l + "\n")
	}
}

// more continues the previous field on an indented line.
func (d *details) more(value string) { d.field("", value) }

func (d *details) section(title string) { d.sb.WriteString("\n" + bold(title) + "\n") }

func (d *details) commands(cmds ...string) {
	d.section("Equivalent commands")
	for _, c := range cmds {
		d.line("  " + cyan("$ "+c))
	}
}

func (d *details) String() string { return d.sb.String() }

func (a *App) renderDetails() {
	a.detailsDirty = false
	v := a.view(viewDetails)
	if v == nil {
		return
	}
	var text string
	switch a.context {
	case viewStatus:
		text = a.statusDetails()
	case viewBoot:
		text = a.bootDetails()
	case viewChanges:
		text = a.changesDetails()
	default:
		if a.tab == tabDevices {
			text = a.deviceDetails()
		} else {
			text = a.moduleDetails()
		}
	}
	v.Clear()
	v.SetOrigin(0, 0)
	fmt.Fprint(v, text)
}

// moduleInfo returns the cached modinfo and parameter values for name, or
// nil while they are loading in the background.
func (a *App) moduleInfo(name string) *moduleInfo {
	key := kmod.Normalize(name)
	if mi, ok := a.info[key]; ok || a.pending[key] {
		return mi
	}
	a.pending[key] = true
	release := a.status.Release
	a.background(func() func() {
		info, infoOp := a.src.Modinfo(name, release)
		params, paramsOp := a.src.Params(name)
		return func() {
			delete(a.pending, key)
			a.info[key] = &moduleInfo{info: info, infoErr: infoOp.Err, params: params, paramsErr: paramsOp.Err}
			a.logOps(infoOp)
			if len(params) > 0 || paramsOp.Err != nil {
				a.logOps(paramsOp)
			}
			a.detailsDirty = true
		}
	})
	return a.info[key] // already set when background ran synchronously
}

func (a *App) moduleDetails() string {
	e, ok := a.mods.selected()
	if !ok {
		return dim("No module selected.")
	}
	name := kmod.Normalize(e.Name)
	d := a.newDetails()
	d.line(bold(e.Name))
	d.line("")

	switch e.State {
	case kmod.Loaded:
		m := e.Module
		state := green("loaded") + " · " + longSize(m.Size)
		if m.State != "Live" {
			state += " · " + yellow(strings.ToLower(m.State))
		}
		d.field("State", state)
	case kmod.Available:
		d.field("State", yellow("not loaded")+dim(" (available in "+kmod.ModuleDir(a.status.Release)+")"))
	case kmod.Builtin:
		d.field("State", cyan("built into the kernel"))
	}

	mi := a.moduleInfo(e.Name)
	switch {
	case mi == nil:
		d.field("modinfo", dim("loading…"))
	case mi.infoErr != nil:
		d.field("modinfo", red(errText(mi.infoErr)))
	default:
		a.modinfoFields(d, e, mi.info)
	}

	if e.Module != nil && e.Module.Taint != "" {
		d.field("Taint", yellow(e.Module.Taint))
	}
	if order := a.idx.LoadOrder(name); len(order) > 1 {
		d.field("Load order", strings.Join(order, dim(" → ")))
	}
	// modules.dep lists transitive dependencies, so this is "at any depth".
	if req := a.idx.RequiredBy(name); len(req) > 0 {
		d.field("Needed by", dim(fmt.Sprintf("%d, any depth: ", len(req)))+joinLimit(req, 12))
	} else {
		d.field("Needed by", dim("—"))
	}
	if e.Module != nil {
		used := joinLimit(e.Module.UsedBy, 12)
		if e.Module.RefCount > len(e.Module.UsedBy) {
			used += dim(fmt.Sprintf(" (use count %d)", e.Module.RefCount))
		}
		d.field("Used by", used)
	}
	var conf []string
	if a.inBoot(kmod.BootBlacklist, name) {
		conf = append(conf, red("blacklisted"))
	}
	if a.inBoot(kmod.BootLoad, name) {
		conf = append(conf, green("loads at boot"))
	}
	if opts := kmod.Options(a.boot, name); len(opts) > 0 {
		conf = append(conf, "options "+cyan(strings.Join(opts, " ")))
	}
	if len(conf) > 0 {
		d.field("kmodtui.conf", strings.Join(conf, ", "))
	}
	devs := a.devByMod[name]
	for i, dev := range devs {
		text := dev.Bus + " " + shortAddr(dev) + "  " + deviceName(dev)
		if i == 0 {
			d.field("Devices", text)
		} else {
			d.more(text)
		}
	}

	if mi != nil {
		a.paramSection(d, e, mi)
	}

	cmds := []string{"modinfo " + e.Name}
	if e.State != kmod.Available {
		cmds = append(cmds, "grep -H . /sys/module/"+name+"/parameters/*")
	}
	if e.State == kmod.Loaded {
		cmds = append(cmds, "ls /sys/module/"+name+"/holders")
	}
	if e.State != kmod.Builtin {
		cmds = append(cmds, "modprobe --show-depends "+e.Name)
	}
	d.commands(cmds...)
	return d.String()
}

func (a *App) modinfoFields(d *details, e kmod.Entry, mi kmod.ModInfo) {
	if v := mi.Get("description"); v != "" {
		d.field("Description", v)
	}
	if authors := mi.All("author"); len(authors) > 0 {
		d.field("Author", strings.Join(authors, "; "))
	}
	if v := mi.Get("license"); v != "" {
		d.field("License", v)
	}
	if v := mi.Get("version"); v != "" {
		d.field("Version", v)
	}
	d.field("File", mi.Get("filename"))
	switch {
	case mi.Builtin():
	case mi.Signed():
		sig := green("signed")
		if s := mi.Get("signer"); s != "" {
			sig += " by " + s
		}
		if h := mi.Get("sig_hashalgo"); h != "" {
			sig += dim(" (" + h + ")")
		}
		d.field("Signature", sig)
	default:
		d.field("Signature", yellow("unsigned"))
	}
	if !mi.Builtin() && mi.Get("intree") != "Y" {
		d.field("In-tree", yellow("no (out-of-tree module)"))
	}
	d.field("Depends", joinLimit(mi.Depends(), 12))
	if n := len(mi.All("alias")); n > 0 {
		d.field("Aliases", fmt.Sprintf("%d device patterns", n))
	}
	if n := len(mi.All("firmware")); n > 0 {
		d.field("Firmware", fmt.Sprintf("%d files", n))
	}
}

// paramSection merges the parameters modinfo declares with the current
// values under /sys/module/<name>/parameters.
func (a *App) paramSection(d *details, e kmod.Entry, mi *moduleInfo) {
	type row struct {
		name, value, desc string
		hasValue          bool
	}
	rows := map[string]*row{}
	for _, p := range mi.info.Params {
		desc := p.Desc
		if p.Type != "" {
			desc = strings.TrimSpace(desc + " [" + p.Type + "]")
		}
		rows[p.Name] = &row{name: p.Name, desc: desc}
	}
	for _, p := range mi.params {
		r := rows[p.Name]
		if r == nil {
			r = &row{name: p.Name}
			rows[p.Name] = r
		}
		r.hasValue = true
		r.value = p.Value
		if p.Unreadable != "" {
			r.value = yellow("(" + p.Unreadable + ")")
		} else if r.value == "" {
			r.value = dim(`""`)
		}
	}
	if len(rows) == 0 && mi.paramsErr == nil {
		return
	}
	sorted := make([]*row, 0, len(rows))
	nameW := 0
	for _, r := range rows {
		sorted = append(sorted, r)
		nameW = max(nameW, len(r.name))
	}
	slices.SortFunc(sorted, func(x, y *row) int { return cmp.Compare(x.name, y.name) })
	nameW = min(nameW, 24)

	title := fmt.Sprintf("Parameters (%d)", len(rows))
	live := e.State != kmod.Available
	if !live {
		title += dim(" — not loaded, no current values")
	}
	d.section(title)
	if mi.paramsErr != nil {
		d.line("  " + red(errText(mi.paramsErr)))
	}
	valueW := 0
	for _, r := range sorted {
		if !r.hasValue {
			r.value = dim("(not in sysfs)")
		}
		valueW = max(valueW, visibleLen(r.value))
	}
	valueW = min(valueW, 11)
	for _, r := range sorted {
		// Names and values are never truncated (they are what you type on
		// the command line); long ones just push the description right.
		prefix := "  " + padRight(r.name, nameW) + "  "
		if live {
			prefix = "  " + padRight(r.name, nameW) + " = " + padRight(r.value, valueW) + "  "
		}
		d.hang(prefix, dim(r.desc))
	}
}

func (a *App) deviceDetails() string {
	dev, ok := a.devs.selected()
	if !ok {
		return dim("No device selected.")
	}
	d := a.newDetails()
	d.line(bold(deviceName(dev)))
	d.line("")
	d.field("Address", dev.Bus+" "+dev.Addr)
	d.field("Vendor", idText(dev.Vendor, dev.VendorID))
	d.field("Product", idText(dev.Product, dev.ProductID))
	if dev.Bus == "pci" {
		d.field("Class", idText(dev.ClassName, dev.Class))
	}

	var cmds []string
	if dev.Bus == "pci" {
		cmds = append(cmds, "lspci -nnk -s "+dev.Addr)
		a.bindingFields(d, dev.Bindings[0])
	} else {
		cmds = append(cmds, "lsusb -t", "lsusb -d "+dev.VendorID+":"+dev.ProductID)
		a.interfaceRows(d, dev.Bindings)
	}
	for _, b := range dev.Bindings {
		if b.Driver == "" && b.Modalias != "" {
			cmds = append(cmds, "modprobe --resolve-alias "+shellQuote(b.Modalias))
		}
	}
	if mods := dev.Drivers(); len(mods) > 0 {
		d.line("")
		d.line(dim("Press enter to open " + mods[0] + " in the Modules tab."))
	} else if c := dev.Candidates(); len(c) > 0 {
		d.line("")
		d.line(dim("Press enter to open " + c[0] + " in the Modules tab."))
	}
	d.commands(slices.Compact(cmds)...)
	return d.String()
}

func idText(name, id string) string {
	if name == "" {
		return dim(id)
	}
	return name + dim(" ["+id+"]")
}

func (a *App) bindingFields(d *details, b kmod.Binding) {
	switch {
	case b.Driver == "" && len(b.Candidates) > 0:
		d.field("Driver", yellow("none bound"))
		d.field("Candidates", strings.Join(b.Candidates, ", ")+dim(" (from modules.alias)"))
	case b.Driver == "":
		d.field("Driver", dim("none bound, no matching module"))
	default:
		d.field("Driver", b.Driver)
		d.field("Module", a.moduleState(b))
	}
	if b.Modalias != "" {
		d.field("Modalias", dim(b.Modalias))
	}
}

// interfaceRows lists USB interfaces one per line: path, class, and the
// bound module, or the modalias and candidates when nothing is bound.
func (a *App) interfaceRows(d *details, bs []kmod.Binding) {
	d.section("Interfaces")
	pathW, classW := 0, 0
	for _, b := range bs {
		pathW = max(pathW, len(b.Path))
		classW = max(classW, len(ifaceClass(b)))
	}
	classW = min(classW, 24)
	for _, b := range bs {
		row := "  " + fit(b.Path, pathW) + "  " + fit(ifaceClass(b), classW) + "  "
		switch {
		case b.Driver != "":
			row += a.moduleState(b)
		case len(b.Candidates) > 0:
			row += yellow("none bound") + " · candidates: " + strings.Join(b.Candidates, ", ")
		default:
			row += dim("none bound")
		}
		d.line(row)
		if b.Driver == "" && b.Modalias != "" {
			d.line(strings.Repeat(" ", 4+pathW) + dim(b.Modalias))
		}
	}
}

func ifaceClass(b kmod.Binding) string {
	if b.ClassName != "" {
		return b.ClassName
	}
	return "class " + b.Class
}

func (a *App) moduleState(b kmod.Binding) string {
	if b.Module == "" {
		return dim("none (part of the kernel core)")
	}
	if b.Builtin {
		return b.Module + " " + cyan("(built-in)")
	}
	for _, m := range a.loaded {
		if kmod.Normalize(m.Name) == kmod.Normalize(b.Module) {
			return b.Module + " " + green("(loaded)")
		}
	}
	return b.Module
}

func (a *App) statusDetails() string {
	d := a.newDetails()
	d.line(bold("System"))
	d.line("")
	st := a.status
	if err := a.statusErr(0); err != nil {
		d.field("Kernel", red(errText(err)))
	} else {
		d.field("Kernel", st.Release)
	}

	if err := a.statusErr(1); err != nil {
		d.field("Secure Boot", red(errText(err)))
	} else {
		d.field("Secure Boot", secureBootText(st.SecureBoot, nil))
		for _, n := range st.SecureBoot.Notes {
			d.more(dim(n))
		}
	}

	if err := a.statusErr(2); err != nil {
		d.field("Taint", red(errText(err)))
	} else {
		d.field("Taint", taintText(st, nil))
		for _, f := range st.TaintFlags {
			d.more(yellow(f.Letter) + "  " + f.Desc)
		}
	}
	var tainted []string
	for _, m := range a.loaded {
		if m.Taint != "" {
			tainted = append(tainted, m.Name+" "+yellow(m.Taint))
		}
	}
	if len(tainted) > 0 {
		slices.Sort(tainted)
		d.field("Tainted by", strings.Join(tainted, ", "))
	}
	cmds := make([]string, 0, len(a.statusOps))
	for _, op := range a.statusOps {
		cmds = append(cmds, op.Cmd)
	}
	d.commands(cmds...)
	return d.String()
}

func (a *App) bootDetails() string {
	d := a.newDetails()
	e, ok := a.bootList.selected()
	if !ok {
		d.line(bold("Boot config"))
		d.line("")
		d.line("kmodtui only manages these two files:")
		d.line("")
		d.field("modprobe.d", kmod.ModprobeConf+dim("  (blacklist, options)"))
		d.field("modules-load", kmod.ModulesLoadConf+dim("  (load at boot)"))
		d.line("")
		if a.bootErr != nil {
			d.line(red(errText(a.bootErr)))
		} else {
			d.line(dim("Neither has entries yet. Editing them is planned for phase 2."))
		}
		d.commands("cat "+kmod.ModprobeConf+" "+kmod.ModulesLoadConf, "modprobe --showconfig | less")
		return d.String()
	}

	d.line(bold(e.Kind.String() + " " + e.Module))
	d.line("")
	d.field("File", fmt.Sprintf("%s (line %d)", e.File, e.Line))
	switch e.Kind {
	case kmod.BootBlacklist:
		d.field("Effect", e.Module+" is not loaded automatically for matching hardware; an explicit `modprobe "+e.Module+"` still loads it.")
	case kmod.BootLoad:
		d.field("Effect", "systemd-modules-load loads "+e.Module+" at every boot.")
	case kmod.BootOptions:
		d.field("Options", e.Args)
		d.field("Effect", "modprobe passes these parameters whenever it loads "+e.Module+".")
	default:
		d.field("Line", e.Args)
	}
	state := dim("not loaded")
	for _, m := range a.loaded {
		if kmod.Normalize(m.Name) == kmod.Normalize(e.Module) {
			state = green("loaded")
		}
	}
	d.field("Now", state)
	cmds := []string{"cat " + e.File}
	if e.File == kmod.ModprobeConf {
		cmds = append(cmds, "modprobe --showconfig | grep -w "+e.Module)
	} else {
		cmds = append(cmds, "systemctl status systemd-modules-load")
	}
	d.commands(cmds...)
	return d.String()
}

func (a *App) changesDetails() string {
	d := a.newDetails()
	c, ok := a.changes.selected()
	if !ok {
		d.line(bold("Changes"))
		d.line("")
		d.line("No pending changes.")
		d.line("")
		d.line(dim("Follow-up work after an edit shows up here, for example \"initramfs update needed\" after a blacklist, or a blacklisted module that is still loaded."))
		d.commands("stat -c '%y %n' /etc/modprobe.d " + kmod.ModprobeConf + " /boot/initrd.img")
		return d.String()
	}
	d.line(bold(stripSGR(c.text)))
	d.line("")
	for _, l := range c.detail {
		d.line(l)
	}
	if c.fix != nil {
		d.line("")
		d.line(dim("Press enter in the Changes panel to fix it (you confirm first)."))
		d.commands(c.fixCmd)
	}
	return d.String()
}
