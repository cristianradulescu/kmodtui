# CLAUDE.md

Guidance for working on kmodtui: a lazygit-style TUI for Linux kernel modules
(Ubuntu LTS and newer, single static Go binary).

## Status

- **Phase 1 (current): read-only.** No writes, no sudo, no pkexec anywhere.
- Phase 2 (not started; needs explicit go-ahead): writes through a separate
  `kmodtui-helper` binary run via `pkexec`. See "Phase 2 rules" below.

## Commands

```sh
make check   # gofmt check + go vet + go test ./...
make build   # static binary in bin/kmodtui (CGO_ENABLED=0)
make run     # go run ./cmd/kmodtui
go test ./internal/kmod -run TestParseModinfo -v
```

Run `make check` after every change. Never run anything with sudo/pkexec, and
never modify files outside this repo.

## Layout

- `cmd/kmodtui` - flags and wiring only.
- `internal/kmod` - data sources and parsers. Must not import gocui or
  anything UI-related.
- `internal/ui` - gocui views, keybindings, list state, rendering.
  - `app.go`: state, layout, focus, handlers. `render.go`: panel and
    Details text. `keys.go`: the binding table (registration, bottom bar and
    `?` popup all come from it; add keys there only). `list.go`: filterable
    list state with no gocui dependency.
  - Slow per-module reads (modinfo, params) run through `App.background` and
    set `detailsDirty`; `layout` re-renders. Tests make `background`
    synchronous.
  - gocui wraps lines without indentation, so text that may be long goes
    through `details.hang` / `indentWrap` (ANSI-aware) instead.

## UI tests

- `newTestApp` renders headlessly (`NewGuiOpts{Headless: true}`); `press`
  dispatches keys through the binding table like gocui does; assertions use
  `flat()` so they survive wrapping. Check log content via `logText()`.
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

## Phase 2 rules (for later, do not implement yet)

- The TUI never runs as root. Writes go through `kmodtui-helper` via `pkexec`
  with a polkit policy.
- Fixed subcommands only: load, unload, set-param, blacklist, unblacklist,
  boot-load, boot-unload, update-initramfs. No arbitrary commands or paths.
- Validate module names: `^[A-Za-z0-9_-]+$` and present in modules.dep.
- Load/unload with `modprobe` / `modprobe -r`, never insmod/rmmod.
- Write only `/etc/modprobe.d/kmodtui.conf` and
  `/etc/modules-load.d/kmodtui.conf`. Never edit other config files.
- Confirmation dialog before every write, showing the exact command(s). Extra
  warning when unloading a module in use, or one driving storage, network or
  display.
- Flag when a blacklist needs `update-initramfs -u` and offer to run it.
