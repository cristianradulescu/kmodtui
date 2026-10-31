# CLAUDE.md

Guidance for working on kmodtui: a lazygit-style TUI for Linux kernel modules
(Ubuntu LTS and newer, single static Go binary).

## Status

- Phase 1 (done): read-only browsing.
- Phase 2 (current): writes through `kmodtui-helper`, run via `pkexec`. See
  "Write rules" below; they are not negotiable.

## Commands

```sh
make check   # gofmt check + go vet + go test ./...
make build   # bin/kmodtui, bin/kmodtui-helper, bin/<policy> (CGO_ENABLED=0)
make run     # go run ./cmd/kmodtui
go test ./internal/kmod -run TestParseModinfo -v
```

Run `make check` after every change. Never run anything with sudo/pkexec
(including `make install`), and never modify files outside this repo. The
end-to-end check of writes is the user's: they run `sudo make install`.

## Layout

- `cmd/kmodtui` - flags and wiring only.
- `cmd/kmodtui-helper` - root side, wiring only. **No flags**: a `-root`
  style flag on a pkexec-run binary would let it write anywhere.
- `internal/helper` - the helper's subcommands, validation and atomic file
  writes; `Env{Root, Exec, Out}` makes it testable against a temp root.
- `internal/kmod` - data sources, parsers, validators (`validate.go`) and the
  pure config editors (`bootwrite.go`, `Edit*`). `allconf.go` reads every
  modprobe.d / modules-load.d file with modprobe's override rules;
  `why.go` explains why a module is loaded; `klog.go` reads the kernel
  journal (`-D <root>/var/log/journal` for a fixture root). Must not import
  gocui or anything UI-related.
- `packaging/` - polkit policy template; `@HELPER@` is filled in by `make
  build` and must match `ui.HelperPath` (also set by the Makefile).
- `internal/ui` - gocui views, keybindings, list state, rendering.
  - `app.go`: state, layout, focus, handlers. `render.go`: panel and
    Details text. `keys.go`: the binding table (registration, bottom bar and
    `?` popup all come from it; add keys there only). `list.go`: filterable
    list state with no gocui dependency.
  - `popups.go`: a stack of popups (filter, help, confirm, params, value,
    message). Open/close only through `openPopup`/`closePopup`; closing the
    last one refocuses the view that was focused before the first.
  - `insight.go`: the Details sections that explain a module or device
    (Config from all files, Why loaded, Kernel log). The kernel log loads
    through `App.background` like modinfo and is cached per module/device
    until `R`.
  - Filters are kept per list (`listKey`: modules, devices, boot, allconf);
    the filter popup applies to the panel it was opened from.
  - `actions.go`: one builder per write returning an `action` (helper argv,
    effects as plain commands, dry run, warnings, notes). Every write goes
    through `confirm(action)`; with warnings only `y` runs it. The
    `executor` interface runs pkexec (suspending the TUI); tests use
    `fakeExecutor`.
  - Slow per-module reads (modinfo, params) run through `App.background` and
    set `detailsDirty`; `layout` re-renders. Tests make `background`
    synchronous.
  - A binding whose `when` is false returns `ErrKeybindingNotHandled` so a
    global binding can take the key; `Gui.ErrorHandler` swallows it when
    none does (otherwise MainLoop exits). Keep that handler.
  - gocui wraps lines without indentation, so text that may be long goes
    through `details.hang` / `indentWrap` (ANSI-aware) instead.

## UI tests

- `newTestApp` renders headlessly (`NewGuiOpts{Headless: true}`); `press`
  dispatches keys through the binding table like gocui does, including
  passing leftover errors through `Gui.ErrorHandler` (a non-nil result means
  the real app would exit). Assertions use `flat()` so they survive
  wrapping. Check log content via `logText()`, helper calls via
  `assertCalls`.
- gocui keeps the screen in a package global: one Gui per test, never
  `t.Parallel()`.
- For a visual check, print `a.screen()` from a throwaway test, or run
  `bin/kmodtui` in a dedicated tmux server (`tmux -L kmodtui-test ...`) and
  `capture-pane`.

## Rules

- **Every** filesystem read goes through `kmod.Source` (injectable root), and
  every external command goes through `kmod.Runner`. No direct `os.ReadFile`
  or `exec.Command` outside those.
- Every read or action returns a `kmod.Op` with a short description and the
  exact equivalent CLI command; the UI appends it to the command log. This is
  the core promise of the tool: the user can always fall back to the CLI.
- Missing or unreadable files are normal (containers, non-EFI machines,
  root-only params). Show a clear message in the panel / log; never panic.
- Text from sysfs, modinfo, ID databases and config files is untrusted (a USB
  device picks its own product string): parsers pass it through
  `kmod.Clean`, and the UI renders errors with `errText`. Never write raw
  external text into a view; ESC or a newline would restyle the UI or split
  a list row so it no longer matches the selected item.
- Module names are normalized with `kmod.Normalize` (`-` -> `_`) before
  comparing; /proc/modules uses `_`, modules.dep file names often use `-`.
- Prefer the standard library. Ask before adding any dependency (tests use
  plain `testing`, no testify).
- Parsers get table-driven tests against fixtures in
  `internal/kmod/testdata/`. Fixtures are trimmed copies of real output
  (Ubuntu 26.04, kernel 7.0.0-38-generic). Keep them byte-exact.

## gocui: use the jesseduffield fork, not jroimartin/gocui

- Pinned to a master pseudo-version in go.mod. Do **not** `go get @latest`:
  that resolves to the 2016 `v0.3.0` tag.
- The fork's `_examples/` are stale (they import awesome-gocui and use the old
  `NewGui` signature). Read the source in the module cache instead:
  `$(go env GOMODCACHE)/github.com/jesseduffield/gocui@<version>/{gui,view}.go`.
- Fork API facts we rely on:
  - `gocui.NewGui(gocui.NewGuiOpts{OutputMode: ..., Headless: ..., Width, Height})`.
  - `g.SetView(name, x0, y0, x1, y1, overlaps)` returns the new view **and**
    a wrapped `ErrUnknownView` on first creation. There is no
    `IsUnknownView`/`IsQuit`, and the go-errors wrapper has no `Unwrap`, so
    `errors.Is` does not work for it. Use the `ui.setView` helper.
  - `SetKeybinding(view, key any, mod, handler)`: key is a `gocui.Key` or a
    rune. Dispatch order: view binding, then the editor (editable views),
    then global (`""`) bindings. Rune bindings never fire in editable views.
  - Built-ins we use: `View.Tabs`/`TabIndex` (tab titles), `TitlePrefix`
    (`[2]`), `Footer` + `Gui.ShowListFooter` ("x of y"), `Gui.SelFrameColor` +
    `Gui.Highlight` (active border), `View.SelBgColor` + `View.Highlight`
    (selected row), `FrameRunes` (rounded corners), `View.FocusPoint`.
  - `NewGuiOpts{Headless: true}` + `g.Snapshot()` render without a terminal;
    use it for UI smoke tests.

## Write rules

- The TUI never runs as root. Writes go through `kmodtui-helper` via `pkexec`
  with the polkit policy. It uses `auth_admin` (a password for every
  change); do not switch to `auth_admin_keep`, which would let the
  authorized kmodtui process run any subcommand for a few minutes.
- Fixed subcommands only: load, unload, set-param, blacklist, unblacklist,
  boot-load, boot-unload, update-initramfs. No arbitrary commands or paths.
- Validate module names (`kmod.ValidModuleName`: `^[A-Za-z0-9_][A-Za-z0-9_-]*$`,
  so never a leading `-` that modprobe would read as an option) and require
  them in the running kernel's modules.dep. Parameter names and values have
  their own validators; values never contain spaces, quotes, `=` or newlines.
- Run only `/usr/sbin/modprobe` and `/usr/sbin/update-initramfs`, by
  absolute path, without a shell. Load/unload with `modprobe` /
  `modprobe -r`, never insmod/rmmod.
- Write only `/etc/modprobe.d/kmodtui.conf` and
  `/etc/modules-load.d/kmodtui.conf`, atomically (temp file + rename, 0644),
  through the `kmod.Edit*` functions, which keep lines they do not own.
  Never edit other config files.
- Confirmation dialog before every write, showing the exact command(s) and a
  dry run where one exists. Warnings (`kmod.Impact`) for modules in use,
  driving storage/network/display/wireless/input devices (also through
  dependent modules), or providing a mounted filesystem.
- After modprobe.d changes, the Changes panel shows "initramfs update
  needed" (`kmod.InitramfsPending`) and offers `update-initramfs -u`.
