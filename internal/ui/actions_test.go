package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/jesseduffield/gocui"
)

func (a *App) fake() *fakeExecutor { return a.exec.(*fakeExecutor) }

// selectModule shows the module in the Modules tab (switching to "all" if
// needed) and focuses the panel.
func (a *App) selectModule(t *testing.T, name string) {
	t.Helper()
	if err := a.focus(viewModules); err != nil {
		t.Fatal(err)
	}
	if !a.mods.selectKey(name) {
		if err := a.toggleAll(); err != nil {
			t.Fatal(err)
		}
		if !a.mods.selectKey(name) {
			t.Fatalf("module %s not in the list", name)
		}
	}
	a.syncModulesSelection()
	a.renderDetails()
	a.renderBar()
	a.redraw(t)
}

func (a *App) mustPress(t *testing.T, keys ...any) {
	t.Helper()
	for _, k := range keys {
		if err := a.press(t, k); err != nil {
			t.Fatalf("key %v: %v", k, err)
		}
	}
}

// title is a popup's frame title (not part of its text).
func (a *App) title(t *testing.T, name string) string {
	t.Helper()
	v, err := a.g.View(name)
	if err != nil {
		t.Fatalf("view %s: %v", name, err)
	}
	return v.Title
}

func assertCalls(t *testing.T, a *App, want ...string) {
	t.Helper()
	var got []string
	for _, c := range a.fake().calls {
		got = append(got, strings.Join(c, " "))
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("helper calls = %q, want %q", got, want)
	}
}

func TestUnloadNeedsExplicitYesWhenRisky(t *testing.T) {
	a := newTestApp(t, fixtureRoot, 140, 45)
	a.selectModule(t, "nvme")
	assertContains(t, "bar", a.viewText(t, viewOptions), "Unload: u", "Blacklist: b", "Params: p", "Boot load: a")

	a.mustPress(t, 'u')
	if a.currentView() != viewConfirm {
		t.Fatalf("focus = %q, want the confirm dialog", a.currentView())
	}
	text := a.viewText(t, viewConfirm)
	assertContains(t, "confirm", text,
		"$ pkexec /usr/local/libexec/kmodtui-helper unload nvme",
		"$ modprobe -r nvme",
		"Dry run", "rmmod nvme",
		"Careful", "in use (use count 3)", "drives storage device pci 0000:bf:00.0",
		"y: run anyway")
	assertContains(t, "log", a.logText(), "$ modprobe -n -v -r nvme  # Dry run for nvme")

	// Enter is refused while there are warnings; only y runs it.
	a.mustPress(t, gocui.KeyEnter)
	assertCalls(t, a)
	if a.currentView() != viewConfirm {
		t.Fatalf("Enter closed a dialog with warnings")
	}
	a.mustPress(t, 'y')
	assertCalls(t, a, "unload nvme")
	if a.currentView() != viewModules || a.topPopup() != "" {
		t.Errorf("after running: focus %q, popups %v", a.currentView(), a.popups)
	}
	assertContains(t, "log", a.logText(), "$ pkexec /usr/local/libexec/kmodtui-helper unload nvme  # Unload nvme")
	// The state is re-read afterwards.
	if n := strings.Count(a.logText(), "$ lsmod"); n != 2 {
		t.Errorf("lsmod logged %d times, want 2 (startup and after the action)", n)
	}
}

func TestCancelRunsNothing(t *testing.T) {
	a := newTestApp(t, fixtureRoot, 140, 45)
	a.selectModule(t, "nvme")
	a.mustPress(t, 'u', gocui.KeyEsc)
	assertCalls(t, a)
	if a.currentView() != viewModules {
		t.Errorf("focus = %q, want modules", a.currentView())
	}
	a.mustPress(t, 'u', 'n')
	assertCalls(t, a)
}

func TestLoadAvailableModule(t *testing.T) {
	a := newTestApp(t, fixtureRoot, 140, 45)
	a.selectModule(t, "e1000e")
	bar := a.viewText(t, viewOptions)
	assertContains(t, "bar", bar, "Load: l")
	assertNotContains(t, "bar", bar, "Unload: u")

	a.mustPress(t, 'u') // not loaded: nothing to unload
	if a.topPopup() != "" {
		t.Fatalf("u opened %q for a module that is not loaded", a.topPopup())
	}
	a.mustPress(t, 'l')
	assertContains(t, "confirm", a.viewText(t, viewConfirm),
		"kmodtui-helper load e1000e", "$ modprobe e1000e", "insmod /lib/modules/7.0.0-38-generic/kernel/e1000e.ko.zst", "enter: run")
	a.mustPress(t, gocui.KeyEnter)
	assertCalls(t, a, "load e1000e")
}

func TestBlacklistToggle(t *testing.T) {
	a := newTestApp(t, fixtureRoot, 140, 45)

	// uvcvideo is blacklisted in the fixture's kmodtui.conf.
	a.selectModule(t, "uvcvideo")
	assertContains(t, "bar", a.viewText(t, viewOptions), "Unblacklist: b")
	assertContains(t, "details", a.viewText(t, viewDetails), "kmodtui.conf blacklisted")
	a.mustPress(t, 'b')
	assertEqual(t, "title", a.title(t, viewConfirm), "Remove the blacklist entry for uvcvideo")
	assertContains(t, "confirm", a.viewText(t, viewConfirm), "kmodtui-helper unblacklist uvcvideo", "initramfs")
	a.mustPress(t, gocui.KeyEnter)

	// btusb drives the Bluetooth adapter: blacklisting it warns.
	a.selectModule(t, "btusb")
	assertContains(t, "bar", a.viewText(t, viewOptions), "Blacklist: b")
	a.mustPress(t, 'b')
	assertContains(t, "confirm", a.viewText(t, viewConfirm),
		"kmodtui-helper blacklist btusb", "echo 'blacklist btusb' | sudo tee -a /etc/modprobe.d/kmodtui.conf",
		"it drives wireless device usb 3-5", "btusb stays loaded until you unload it or reboot")
	a.mustPress(t, 'y')
	assertCalls(t, a, "unblacklist uvcvideo", "blacklist btusb")
}

func TestBootLoadToggle(t *testing.T) {
	a := newTestApp(t, fixtureRoot, 140, 45)
	a.selectModule(t, "vboxdrv") // listed in the fixture's modules-load.d
	assertContains(t, "bar", a.viewText(t, viewOptions), "No boot load: a")
	a.mustPress(t, 'a', gocui.KeyEnter)
	a.selectModule(t, "bridge")
	a.mustPress(t, 'a')
	assertEqual(t, "title", a.title(t, viewConfirm), "Load bridge at boot")
	assertContains(t, "confirm", a.viewText(t, viewConfirm), "echo bridge | sudo tee -a /etc/modules-load.d/kmodtui.conf")
	a.mustPress(t, gocui.KeyEnter)
	assertCalls(t, a, "boot-unload vboxdrv", "boot-load bridge")
}

func TestBuiltinModuleOffersNoWrites(t *testing.T) {
	a := newTestApp(t, fixtureRoot, 140, 45)
	a.selectModule(t, "usbcore")
	bar := a.viewText(t, viewOptions)
	assertNotContains(t, "bar", bar, "Unload", "Blacklist", "Params", "Boot load")
	a.mustPress(t, 'u', 'b', 'a', 'p')
	if a.topPopup() != "" {
		t.Errorf("a key opened %q for a built-in module", a.topPopup())
	}
	assertCalls(t, a)
}

func TestSetParam(t *testing.T) {
	a := newTestApp(t, fixtureRoot, 140, 45)
	a.selectModule(t, "nvme")
	a.mustPress(t, 'p')
	if a.currentView() != viewParams {
		t.Fatalf("focus = %q, want parameters", a.currentView())
	}
	assertContains(t, "params", a.viewText(t, viewParams), "parameter", "io_queue_depth", "1024", "write_queues",
		"set io queue depth, should >= 2 and < 4096")
	a.mustPress(t, 'j')
	assertContains(t, "params", a.viewText(t, viewParams), "Maximum Host Memory Buffer (HMB) size per controller")
	a.mustPress(t, 'k')

	// First row is io_queue_depth; open the value prompt prefilled with 1024.
	a.mustPress(t, gocui.KeyEnter)
	v := a.g.CurrentView()
	if v.Name() != viewValue || strings.TrimSpace(v.TextArea.GetContent()) != "1024" {
		t.Fatalf("value prompt %q holds %q", v.Name(), v.TextArea.GetContent())
	}

	// A value with a space is refused before anything runs.
	v.TextArea.Clear()
	a.typeText(t, "5 12")
	a.mustPress(t, gocui.KeyEnter)
	if a.currentView() != viewValue {
		t.Fatalf("invalid value left the prompt")
	}
	assertContains(t, "bar", a.viewText(t, viewOptions), "invalid parameter value")

	a.g.CurrentView().TextArea.Clear()
	a.typeText(t, "512")
	a.mustPress(t, gocui.KeyEnter)
	confirm := a.viewText(t, viewConfirm)
	assertEqual(t, "title", a.title(t, viewConfirm), "Set nvme.io_queue_depth = 512")
	assertContains(t, "confirm", confirm,
		"kmodtui-helper set-param nvme io_queue_depth 512 --now",
		"echo 512 | sudo tee /sys/module/nvme/parameters/io_queue_depth",
		"[x] 1 now", "[ ] 2 at next load")

	// Also persist it, then run.
	a.mustPress(t, '2')
	assertContains(t, "confirm", a.viewText(t, viewConfirm), "set-param nvme io_queue_depth 512 --now --boot", "[x] 2 at next load")
	a.mustPress(t, '1', '2') // neither: nothing to run
	assertContains(t, "confirm", a.viewText(t, viewConfirm), "Choose at least one of 1 / 2")
	a.mustPress(t, gocui.KeyEnter)
	assertCalls(t, a)
	a.mustPress(t, '1', '2', gocui.KeyEnter)
	assertCalls(t, a, "set-param nvme io_queue_depth 512 --now --boot")
	if a.topPopup() != "" {
		t.Errorf("popups still open: %v", a.popups)
	}
}

func TestSetParamOfUnloadedModuleIsAtNextLoad(t *testing.T) {
	a := newTestApp(t, fixtureRoot, 140, 45)
	a.selectModule(t, "vboxdrv")
	a.mustPress(t, 'p', gocui.KeyEnter) // cpu_check
	a.typeText(t, "N")
	a.mustPress(t, gocui.KeyEnter)
	assertContains(t, "confirm", a.viewText(t, viewConfirm), "[-] 1 now", "[x] 2 at next load", "--boot")
	a.mustPress(t, '1') // cannot apply now
	assertContains(t, "confirm", a.viewText(t, viewConfirm), "set-param vboxdrv cpu_check N --boot")
}

func TestHelperNotInstalled(t *testing.T) {
	a := newTestApp(t, fixtureRoot, 140, 45)
	a.fake().unavailable = errors.New("kmodtui-helper is not installed at /usr/local/libexec/kmodtui-helper. Install it with: make build && sudo make install")
	a.selectModule(t, "e1000e")
	a.mustPress(t, 'l')
	assertContains(t, "confirm", a.viewText(t, viewConfirm), "not installed", "sudo make install", "esc: close")
	a.mustPress(t, gocui.KeyEnter, 'y')
	assertCalls(t, a)
}

func TestActionFailureShowsMessage(t *testing.T) {
	a := newTestApp(t, fixtureRoot, 140, 45)
	a.fake().err = errors.New("exit status 1")
	a.fake().output = "+ /usr/sbin/modprobe -r bridge\nmodprobe: FATAL: Module bridge is in use.\n"
	a.selectModule(t, "bridge")
	a.mustPress(t, 'u', gocui.KeyEnter)
	if a.currentView() != viewMessage {
		t.Fatalf("focus = %q, want the message popup", a.currentView())
	}
	assertContains(t, "message", a.viewText(t, viewMessage), "Unload bridge failed: exit status 1", "Module bridge is in use")
	assertContains(t, "log", a.logText(), "modprobe: FATAL: Module bridge is in use.")
	a.mustPress(t, gocui.KeyEsc)
	if a.currentView() != viewModules {
		t.Errorf("focus = %q, want modules", a.currentView())
	}
}

func TestBootPanelRemove(t *testing.T) {
	a := newTestApp(t, fixtureRoot, 140, 45)
	a.mustPress(t, '3')
	assertContains(t, "bar", a.viewText(t, viewOptions), "Remove: d")

	// pcspkr is not a module of the fixture kernel: explain instead.
	a.mustPress(t, 'd')
	assertContains(t, "message", a.viewText(t, viewMessage), "not in modules.dep", "sudo sed -i '2d' /etc/modprobe.d/kmodtui.conf")
	a.mustPress(t, gocui.KeyEsc, 'j', 'd')
	assertContains(t, "confirm", a.viewText(t, viewConfirm), "unblacklist uvcvideo")
	a.mustPress(t, gocui.KeyEnter)
	assertCalls(t, a, "unblacklist uvcvideo")
}

func TestChangesPanel(t *testing.T) {
	a := newTestApp(t, fixtureRoot, 140, 45)
	assertContains(t, "changes", a.viewText(t, viewChanges), "uvcvideo is blacklisted but loaded")

	a.initramfs = true
	a.changes.set(a.computeChanges())
	a.renderChanges()
	// The selection stays on the item that was selected; go to the top.
	a.mustPress(t, '4', 'g')
	assertContains(t, "changes", a.viewText(t, viewChanges), "initramfs update needed")
	assertContains(t, "details", a.viewText(t, viewDetails), "/etc/modprobe.d changed after the initramfs", "$ update-initramfs -u")
	a.mustPress(t, gocui.KeyEnter)
	assertEqual(t, "title", a.title(t, viewConfirm), "Update the initramfs")
	assertContains(t, "confirm", a.viewText(t, viewConfirm), "kmodtui-helper update-initramfs")
	a.mustPress(t, gocui.KeyEnter)
	assertCalls(t, a, "update-initramfs")
}

func TestHelpListsWriteKeys(t *testing.T) {
	a := newTestApp(t, fixtureRoot, 140, 50)
	a.mustPress(t, '?')
	assertContains(t, "help", a.viewText(t, viewHelp),
		"Unload the module", "Blacklist / remove the blacklist entry", "Parameters", "Remove the selected entry",
		"Confirmation dialog", "Run, also despite warnings", "Every change is confirmed first")
}

func assertEqual[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// Keys whose binding does not apply right now must do nothing, not end the
// program (gocui returns ErrKeybindingNotHandled to MainLoop otherwise).
func TestInactiveKeysDoNotQuit(t *testing.T) {
	a := newTestApp(t, fixtureRoot, 140, 45)
	a.mustPress(t, gocui.KeyEsc)   // no filter to clear
	a.mustPress(t, gocui.KeyEnter) // Modules tab: Enter is for devices
	a.selectModule(t, "usbcore")
	a.mustPress(t, 'u', 'b', 'p')
	a.fake().unavailable = errors.New("not installed")
	a.selectModule(t, "e1000e")
	a.mustPress(t, 'l', 'y', gocui.KeyEnter, '1', '2')
	if a.currentView() != viewConfirm {
		t.Errorf("focus = %q, want the confirm dialog to stay open", a.currentView())
	}
	assertCalls(t, a)
}
