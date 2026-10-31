# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-10-31

First release: a lazygit-style TUI for Linux kernel modules on Ubuntu LTS and
newer, shipped as a single static binary.

### Added

- Browsing of loaded and available modules, modinfo, parameter values, the
  depmod index (modules.dep, modules.builtin, modules.alias), PCI/USB devices
  with their drivers and modules, Secure Boot state and taint flags.
- Panels for Status, Modules/Devices (filter, all-modules toggle, jump from a
  device to its module), Boot config, Changes and Details, plus a command log,
  context-sensitive key hints and a keybindings popup (`?`).
- The equivalent CLI command for every read and action, shown in Details and
  in the command log.
- Write actions on the Modules tab: unload (`u`), load (`l`), blacklist on/off
  (`b`), load at boot on/off (`a`) and parameters (`p`: now via sysfs, at next
  load via an options line, or both). Boot config: remove an entry (`d`).
  Changes: fix an item with Enter ("initramfs update needed", a blacklisted
  module that is still loaded, a boot-load module that is not loaded).
- A confirmation dialog before every change with the exact pkexec command, the
  plain commands it amounts to and a modprobe dry run. Warnings for modules in
  use, driving storage/network/display/wireless/input devices (also through
  dependent modules) or providing a mounted filesystem; with warnings only `y`
  runs the change.
- `kmodtui-helper`, the only part that runs as root, started through `pkexec`
  with a polkit policy. It takes no flags, accepts a fixed set of subcommands
  (load, unload, set-param, blacklist, unblacklist, boot-load, boot-unload,
  update-initramfs), runs only `/usr/sbin/modprobe` and
  `/usr/sbin/update-initramfs`, and writes only `/etc/modprobe.d/kmodtui.conf`
  and `/etc/modules-load.d/kmodtui.conf`, atomically, keeping lines it does
  not own.
- "All files" tab in the Boot config panel: every modprobe.d and
  modules-load.d file, read with modprobe's override rules, including
  install, remove, softdep and alias lines. Read-only for files kmodtui does
  not manage; filterable with `/`.
- Module Details list every config entry about the module with file and line;
  blacklisting notes when another file already blacklists it.
- "Why loaded" in Details: the device a module drives, a load-at-boot entry, a
  softdep, a mounted filesystem, or the loaded modules that need it and what
  those are for.
- Kernel log in Details: the last 20 kernel messages of the current boot for
  the selected module or device (`journalctl -k --grep`).
- Makefile targets for static builds (`make build`), checks (`make check`)
  and installation (`make install`, which refuses a `bin/` built for another
  `PREFIX`).

### Security

- The polkit policy uses `auth_admin`: every change asks for a password, so an
  authorization cannot be reused for other helper subcommands.
- The helper validates module names (no leading `-`) and requires them in the
  running kernel's modules.dep; parameter names and values are validated too.
- Control characters and invalid UTF-8 in text from sysfs, modinfo, the depmod
  index, ID databases, config files and command output are replaced, so a
  device-supplied string cannot restyle the UI or split a list row.

[Unreleased]: https://github.com/cristianradulescu/kmodtui/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/cristianradulescu/kmodtui/releases/tag/v0.1.0
