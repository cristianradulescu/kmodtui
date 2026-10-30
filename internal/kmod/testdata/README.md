# Test fixtures

`root/` is a miniature filesystem root used through `kmod.Source{Root: ...}`.
`modinfo/` holds `modinfo <name>` output keyed by module name.

Captured verbatim (then trimmed) from Ubuntu 26.04.1, kernel
`7.0.0-38-generic`, on an AMD laptop:

- `root/proc/modules`: real lines for the modules the fixture devices use.
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
