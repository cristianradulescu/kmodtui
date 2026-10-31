// Package ui is the gocui front-end: views, layout, focus and keybindings.
// All data comes from internal/kmod; every read is echoed to the command
// log with its CLI equivalent.
package ui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jesseduffield/gocui"

	"github.com/cristianradulescu/kmodtui/internal/kmod"
)

// View names.
const (
	viewStatus  = "status"
	viewModules = "modules"
	viewBoot    = "boot"
	viewChanges = "changes"
	viewDetails = "details"
	viewLog     = "log"
	viewOptions = "options"
	viewFilter  = "filter"
	viewHelp    = "help"
	viewConfirm = "confirm"
	viewParams  = "params"
	viewValue   = "value"
	viewMessage = "message"
)

// panelOrder is the Tab cycle.
var panelOrder = []string{viewStatus, viewModules, viewBoot, viewChanges, viewDetails, viewLog}

// Tabs of the Modules panel.
const (
	tabModules = iota
	tabDevices
)

const maxLogLines = 2000

// moduleInfo caches the slow per-module reads (modinfo and parameters).
type moduleInfo struct {
	info      kmod.ModInfo
	infoErr   error
	params    []kmod.Param
	paramsErr error
}

// App is the TUI state. All fields are only touched on the gocui goroutine.
type App struct {
	g   *gocui.Gui
	src *kmod.Source
	// background runs work off the UI goroutine and then runs the function
	// it returns on the UI goroutine. Tests make it synchronous.
	background func(work func() func())

	// Data, replaced by reload.
	status     kmod.Status
	statusOps  []kmod.Op
	loaded     []kmod.Module
	loadedErr  error
	idx        *kmod.DepIndex
	idxErr     error
	devices    []kmod.Device
	devicesErr error
	devByMod   map[string][]kmod.Device
	boot       []kmod.BootEntry
	bootErr    error
	aliases    *kmod.AliasIndex
	allConf    []kmod.BootEntry // every modprobe.d / modules-load.d entry
	allConfErr error
	mounts     map[string][]string
	initramfs  bool // /etc/modprobe.d changed after the initramfs was built
	info       map[string]*moduleInfo
	pending    map[string]bool
	klog       map[string]*kernelLog // kernel messages per module or device

	// exec runs kmodtui-helper through pkexec; tests use a fake.
	exec executor

	// UI state.
	showAll      bool
	tab          int
	mods         *list[kmod.Entry]
	devs         *list[kmod.Device]
	bootList     *list[kmod.BootEntry]
	allList      *list[kmod.BootEntry] // Boot config panel, "All files" tab
	bootTab      int
	changes      *list[change]
	filters      map[string]string // per list, see listKey
	context      string            // left panel whose selection Details shows
	popups       []string          // open popups, topmost last
	beforePopup  string            // view to refocus when the last popup closes
	act          *action           // the action the confirm popup offers
	params       *list[paramRow]
	paramModule  string
	valueErr     string // why the last value typed in the value prompt was refused
	message      string // text of the message popup
	logLines     []string
	bindings     []*binding
	ready        bool // views exist and have been rendered once
	detailsDirty bool
	size         [2]int
}

// New returns an App reading from src.
func New(src *kmod.Source) *App {
	a := &App{
		src:     src,
		context: viewModules,
		info:    map[string]*moduleInfo{},
		pending: map[string]bool{},
		klog:    map[string]*kernelLog{},
		filters: map[string]string{},
	}
	a.mods = newList(
		func(e kmod.Entry) string { return e.Name },
		func(e kmod.Entry, q string) bool { return strings.Contains(strings.ToLower(e.Name), kmod.Normalize(q)) },
	)
	a.devs = newList(func(d kmod.Device) string { return d.Bus + d.Addr }, matchDevice)
	a.bootList = newList(
		func(e kmod.BootEntry) string { return fmt.Sprintf("%s:%d", e.File, e.Line) },
		func(e kmod.BootEntry, q string) bool { return strings.Contains(e.Module, q) },
	)
	a.allList = newList(
		func(e kmod.BootEntry) string { return fmt.Sprintf("%s:%d", e.File, e.Line) },
		func(e kmod.BootEntry, q string) bool {
			return strings.Contains(strings.ToLower(e.Kind.String()+" "+e.Module+" "+e.Args+" "+e.File), q) ||
				strings.Contains(kmod.Normalize(strings.ToLower(e.Module)), kmod.Normalize(q))
		},
	)
	a.changes = newList(func(c change) string { return c.key }, func(change, string) bool { return true })
	a.params = newList(func(r paramRow) string { return r.name }, func(paramRow, string) bool { return true })
	return a
}

func matchDevice(d kmod.Device, q string) bool {
	fields := []string{d.Addr, d.Vendor, d.Product, d.ClassName, d.VendorID + ":" + d.ProductID}
	fields = append(fields, d.Drivers()...)
	fields = append(fields, d.Candidates()...)
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}

// Run starts the terminal UI and blocks until the user quits.
func (a *App) Run() error {
	g, err := gocui.NewGui(gocui.NewGuiOpts{OutputMode: gocui.OutputTrue})
	if err != nil {
		return fmt.Errorf("starting the terminal UI: %w", err)
	}
	defer g.Close()
	a.background = func(work func() func()) {
		go func() {
			apply := work()
			g.Update(func(*gocui.Gui) error { apply(); return nil })
		}()
	}
	a.exec = pkexecExecutor{g: g}
	if err := a.init(g); err != nil {
		return err
	}
	if err := g.MainLoop(); err != nil && !errors.Is(err, gocui.ErrQuit) {
		return err
	}
	return nil
}

func (a *App) init(g *gocui.Gui) error {
	a.g = g
	g.Highlight = true
	g.SelFrameColor = gocui.ColorGreen
	g.SelFgColor = gocui.ColorGreen | gocui.AttrBold
	g.ShowListFooter = true
	// A view binding that does not apply right now returns
	// ErrKeybindingNotHandled so a global binding can take the key. When none
	// does, gocui hands that error to MainLoop, which would exit; a key that
	// does nothing is not an error.
	g.ErrorHandler = func(err error) error {
		if errors.Is(err, gocui.ErrKeybindingNotHandled) {
			return nil
		}
		return err
	}
	g.SetManagerFunc(a.layout)
	a.reload()
	return a.registerBindings()
}

// reload reads everything except per-module details, which load lazily.
func (a *App) reload() {
	a.status, a.statusOps = a.src.Status()
	a.logOps(a.statusOps...)

	var op kmod.Op
	a.loaded, op = a.src.LoadedModules()
	a.loadedErr = op.Err
	a.logOps(op)

	var ops []kmod.Op
	a.idx, ops = a.src.LoadDepIndex(a.status.Release)
	a.idxErr = firstErr(ops)
	a.logOps(ops...)

	a.aliases, op = a.src.LoadAliasIndex(a.status.Release)
	a.logOps(op)
	a.devices, ops = a.src.Devices(a.aliases)
	a.devicesErr = firstErr(ops)
	a.devByMod = kmod.DevicesByModule(a.devices)
	a.logOps(ops...)

	a.boot, op = a.src.BootConfig()
	a.bootErr = op.Err
	a.logOps(op)
	a.allConf, op = a.src.AllConfig()
	a.allConfErr = op.Err
	a.logOps(op)

	a.mounts, op = a.src.Mounts()
	a.logOps(op)
	a.initramfs, op = a.src.InitramfsPending(a.status.Release)
	a.logOps(op)

	a.info = map[string]*moduleInfo{}
	a.klog = map[string]*kernelLog{}
	a.mods.set(kmod.Entries(a.loaded, a.idx, a.showAll))
	a.devs.set(a.devices)
	a.bootList.set(a.boot)
	a.allList.set(a.allConf)
	a.changes.set(a.computeChanges())
}

func firstErr(ops []kmod.Op) error {
	for _, op := range ops {
		if op.Err != nil {
			return op.Err
		}
	}
	return nil
}

// setView wraps gocui's SetView, which returns the new view together with a
// wrapped ErrUnknownView when it creates one (the fork has no IsUnknownView,
// and its go-errors wrapper does not support errors.Is).
func setView(g *gocui.Gui, name string, x0, y0, x1, y1 int) (v *gocui.View, created bool, err error) {
	v, err = g.SetView(name, x0, y0, x1, y1, 0)
	if err != nil {
		if v == nil {
			return nil, false, err
		}
		return v, true, nil
	}
	return v, false, nil
}

// layout positions every view; gocui calls it before each redraw.
func (a *App) layout(g *gocui.Gui) error {
	maxX, maxY := g.Size()
	resized := a.size != [2]int{maxX, maxY}
	a.size = [2]int{maxX, maxY}

	h := maxY - 1 // the last row is the key-hint bar
	lw := leftWidth(maxX)
	statusH, changesH := 5, 3
	bootH := min(max(a.bootList.len(), 1)+2, 6)
	if a.context == viewBoot {
		bootH = max(bootH, (h-statusH-changesH)/2)
	}
	modulesH := max(h-statusH-bootH-changesH, 3)
	detailsH := max(h*2/3, 3)

	rects := []struct {
		name           string
		x0, y0, x1, y1 int
	}{
		{viewStatus, 0, 0, lw - 1, statusH - 1},
		{viewModules, 0, statusH, lw - 1, statusH + modulesH - 1},
		{viewBoot, 0, statusH + modulesH, lw - 1, statusH + modulesH + bootH - 1},
		{viewChanges, 0, statusH + modulesH + bootH, lw - 1, h - 1},
		{viewDetails, lw, 0, maxX - 1, detailsH - 1},
		{viewLog, lw, detailsH, maxX - 1, h - 1},
		// Frameless one-line views: content starts at x0+1, y0+1.
		{viewOptions, -1, maxY - 2, maxX, maxY},
	}
	for _, r := range rects {
		v, created, err := setView(g, r.name, r.x0, r.y0, r.x1, r.y1)
		if err != nil {
			return err
		}
		if created {
			a.configure(v)
		}
	}

	if err := a.layoutPopups(g, maxX, maxY); err != nil {
		return err
	}

	if !a.ready {
		a.ready = true
		if _, err := g.SetCurrentView(viewModules); err != nil {
			return err
		}
		a.renderAll()
	} else if resized {
		// List columns and wrapped text depend on the panel widths.
		a.renderAll()
	}
	if a.detailsDirty {
		a.renderDetails()
	}
	return nil
}

// leftWidth is the width of the left column: 40% of the screen (device names
// are long), at least 40 columns on narrow terminals, at most 70.
func leftWidth(maxX int) int {
	return min(max(maxX*2/5, min(40, maxX/2)), 70)
}

var roundedFrame = []rune{'─', '│', '╭', '╮', '╰', '╯'}

func (a *App) configure(v *gocui.View) {
	v.FrameRunes = roundedFrame
	switch v.Name() {
	case viewStatus:
		v.TitlePrefix, v.Title = "[1]", "Status"
	case viewModules:
		v.TitlePrefix = "[2]"
		v.Tabs = []string{"Modules", "Devices"}
		v.SelFgColor = gocui.ColorGreen | gocui.AttrBold // active tab
		v.SelBgColor = gocui.ColorBlue
		v.InactiveViewSelBgColor = gocui.ColorDefault // bold only when unfocused
	case viewBoot:
		v.TitlePrefix = "[3]"
		v.Tabs = []string{"Boot config", "All files"}
		v.SelFgColor = gocui.ColorGreen | gocui.AttrBold // active tab
		v.SelBgColor = gocui.ColorBlue
		v.InactiveViewSelBgColor = gocui.ColorDefault
	case viewChanges:
		v.TitlePrefix, v.Title = "[4]", "Changes"
		v.SelBgColor = gocui.ColorBlue
		v.InactiveViewSelBgColor = gocui.ColorDefault
	case viewDetails:
		v.TitlePrefix, v.Title = "[0]", "Details"
		v.Wrap = true
	case viewLog:
		v.Title = "Command log"
		v.Wrap = true
		v.Autoscroll = true
	case viewOptions:
		v.Frame = false
	case viewHelp:
		v.Title = "Keybindings"
		v.Subtitle = "esc to close"
	case viewConfirm, viewMessage:
		v.Wrap = true
	case viewParams:
		v.SelBgColor = gocui.ColorBlue
		v.Highlight = true
	case viewValue:
		v.Editable = true
		v.Editor = gocui.EditorFunc(gocui.SimpleEditor)
	}
}

// currentView returns the focused view's name, or "".
func (a *App) currentView() string {
	if v := a.g.CurrentView(); v != nil {
		return v.Name()
	}
	return ""
}

func (a *App) focusHandler(name string) func() error {
	return func() error { return a.focus(name) }
}

func (a *App) focus(name string) error {
	if _, err := a.g.SetCurrentView(name); err != nil {
		return err
	}
	switch name {
	case viewStatus, viewModules, viewBoot, viewChanges:
		if a.context != name {
			a.context = name
			a.renderDetails()
		}
	}
	// The log follows new entries unless the user is reading it.
	if v, err := a.g.View(viewLog); err == nil {
		v.Autoscroll = name != viewLog
		if name == viewLog {
			v.FocusPoint(0, max(v.ViewLinesHeight()-1, 0), true)
		}
	}
	for _, n := range []string{viewModules, viewBoot, viewChanges} {
		if v, err := a.g.View(n); err == nil {
			v.HighlightInactive = n != name
		}
	}
	a.renderBar()
	return nil
}

func (a *App) cycleFocus(delta int) error {
	i := 0
	for j, n := range panelOrder {
		if n == a.currentView() {
			i = j
		}
	}
	return a.focus(panelOrder[(i+delta+len(panelOrder))%len(panelOrder)])
}

// move moves the selection in list panels and scrolls the other views.
func (a *App) move(delta int) error {
	switch cur := a.currentView(); cur {
	case viewModules, viewBoot, viewChanges, viewParams:
		return a.moveList(delta)
	default:
		v, err := a.g.View(cur)
		if err != nil {
			return nil
		}
		if delta > 0 {
			v.ScrollDown(delta)
		} else {
			v.ScrollUp(-delta)
		}
	}
	return nil
}

// moveList moves the selection of the focused list: Modules/Devices (also
// while the filter prompt is focused), Boot config, Changes or Parameters.
func (a *App) moveList(delta int) error {
	switch a.currentView() {
	case viewBoot:
		a.bootPanelList().move(delta)
		a.syncBootSelection()
	case viewChanges:
		a.changes.move(delta)
		a.syncChangesSelection()
	case viewParams:
		a.params.move(delta)
		a.renderParams() // the description below the table follows the selection
		return nil
	default:
		if a.tab == tabModules {
			a.mods.move(delta)
		} else {
			a.devs.move(delta)
		}
		a.syncModulesSelection()
	}
	a.renderDetails()
	return nil
}

func (a *App) pageSize() int {
	if v := a.g.CurrentView(); v != nil {
		return max(v.InnerHeight()-1, 1)
	}
	return 10
}

func (a *App) switchTab(delta int) error {
	if a.currentView() == viewBoot {
		a.bootTab = (a.bootTab + delta + 2) % 2
		a.renderBoot()
	} else {
		a.tab = (a.tab + delta + 2) % 2
		a.renderModules()
	}
	a.renderDetails()
	a.renderBar()
	return nil
}

// listKey names the list a panel shows right now; filters are kept per list.
func (a *App) listKey(view string) string {
	switch {
	case view == viewBoot && a.bootTab == 1:
		return "allconf"
	case view == viewBoot:
		return "boot"
	case a.tab == tabDevices:
		return "devices"
	}
	return "modules"
}

func (a *App) toggleAll() error {
	a.showAll = !a.showAll
	a.mods.set(kmod.Entries(a.loaded, a.idx, a.showAll))
	if a.showAll {
		a.logOps(kmod.Op{
			Desc: "List all available modules",
			Cmd:  "find " + kmod.ModuleDir(a.status.Release) + " -name '*.ko*'; cat " + kmod.ModuleDir(a.status.Release) + "/modules.builtin",
		})
	}
	a.renderModules()
	a.renderDetails()
	a.renderBar()
	return nil
}

func (a *App) refresh() error {
	a.reload()
	a.renderAll()
	return nil
}

// gotoDeviceModule jumps from a device to the module driving it (or the
// first candidate module for an unbound device).
func (a *App) gotoDeviceModule() error {
	d, ok := a.devs.selected()
	if !ok {
		return nil
	}
	targets := d.Drivers()
	if len(targets) == 0 {
		targets = d.Candidates()
	}
	for _, t := range targets {
		t = kmod.Normalize(t)
		a.tab = tabModules
		a.filters["modules"] = ""
		a.mods.setFilter("")
		if !a.mods.selectKey(t) && !a.showAll {
			a.showAll = true
			a.mods.set(kmod.Entries(a.loaded, a.idx, true))
		}
		if a.mods.selectKey(t) {
			break
		}
	}
	a.renderModules()
	a.renderDetails()
	a.renderBar()
	return nil
}
