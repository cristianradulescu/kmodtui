# Test fixtures

`root/` is a miniature filesystem root used through `kmod.Source{Root: ...}`.
`modinfo/` holds `modinfo <name>` output keyed by module name.

Captured verbatim (then trimmed) from Ubuntu 26.04.1, kernel
`7.0.0-38-generic`, on an AMD laptop:

- `root/proc/modules`: real lines for the modules the fixture devices use.
- `root/proc/mounts`: real lines (root, /boot, EFI, a FUSE and a snap mount).
- `root/etc/modprobe.d/{blacklist,iwlwifi,alsa-base}.conf`,
  `root/usr/lib/modprobe.d/{aliases,fbdev-blacklist,systemd}.conf`,
  `root/etc/modules`, `root/etc/modules-load.d/cups-filters.conf` (and
  `modules.conf -> ../modules`), `root/usr/lib/modules-load.d/*.conf`: real
  files, trimmed.
- `root/sys/...`: real attribute files; symlinks keep their original
  relative targets (they dangle here; the code only reads the link name).
- `root/lib/modules/7.0.0-38-generic/modules.{dep,alias,builtin}`: real
  lines, filtered to the fixture modules.
- `root/usr/share/misc/{pci,usb}.ids`: real entries for the fixture devices
  (some devices are intentionally absent, as they are upstream).
- `modinfo/*.txt`: real output; long `alias:`/`firmware:` lists cut short.

Crafted to cover cases this machine does not have:

- `vboxdrv`, `vboxnetflt`, `vboxnetadp` (out-of-tree, unsigned, taint `OE`),
  and `root/proc/sys/kernel/tainted` = 12288 (O + E) to match.
- `0000:c3:00.1` (NPU) has its `driver` link removed, so it is an unbound
  device whose module (`amdxdna`) is found through `modules.alias`.
- `root/etc/*/kmodtui.conf` (kmodtui-managed boot config).
- `root/etc/modprobe.d/fbdev-blacklist.conf`: same name as the packaged file
  in `/usr/lib`, so it must hide it.
- `root/lib/modprobe.d/aliases.conf`: same name as `/usr/lib`'s, so it must
  be ignored (merged /usr: `/lib` is `/usr/lib`).
- `root/etc/modprobe.d/bluetooth-local.conf`: a softdep (Ubuntu ships none).
- `root/etc/modprobe.d/README`: not a `.conf` file, so it must be ignored.
