package kmod

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"
)

// DryRun asks modprobe what loading (or, with unload, removing) module
// would do, without doing it: `modprobe -n -v [-r] module`. It needs no
// root. The output lists the insmod/rmmod steps; refusals such as "Module
// nvme is in use" come back as the error.
func (s *Source) DryRun(module string, unload bool) (string, Op) {
	args := []string{"-n", "-v"}
	if unload {
		args = append(args, "-r")
	}
	op := Op{Desc: "Dry run for " + module, Cmd: "modprobe " + strings.Join(append(args, module), " ")}
	if !s.Live() {
		args = append(args, "-d", s.Root)
	}
	out, err := s.Runner.Run("modprobe", append(args, module)...)
	op.Err = err
	return CleanLines(strings.TrimSpace(string(out))), op
}

// Mounts maps each mounted filesystem type to its mount points, from
// /proc/mounts.
func (s *Source) Mounts() (map[string][]string, Op) {
	op := Op{Desc: "List mounted filesystems", Cmd: "findmnt -o TARGET,FSTYPE"}
	b, err := s.readFile("/proc/mounts")
	if err != nil {
		op.Err = err
		return nil, op
	}
	mounts := map[string][]string{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) >= 3 {
			// Mount points escape spaces as \040.
			target := strings.ReplaceAll(Clean(f[1]), `\040`, " ")
			mounts[Clean(f[2])] = append(mounts[Clean(f[2])], target)
		}
	}
	op.Err = sc.Err()
	return mounts, op
}

// InitramfsPending reports whether /etc/modprobe.d changed after the
// initramfs was built. initramfs-tools copies /etc/modprobe.d into the
// image, so blacklists and options there only reach early boot after
// `update-initramfs -u`, which rebuilds the newest kernel's image
// (/boot/initrd.img). The image itself is root-only; its timestamp is not.
func (s *Source) InitramfsPending(release string) (bool, Op) {
	const dir = "/etc/modprobe.d"
	initrd := "/boot/initrd.img"
	op := Op{Desc: "Compare modprobe.d with the initramfs", Cmd: "stat -c '%y %n' " + dir + " " + ModprobeConf + " " + initrd}
	img, err := s.stat(initrd)
	if errors.Is(err, fs.ErrNotExist) {
		initrd = "/boot/initrd.img-" + release
		img, err = s.stat(initrd)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, op // no initramfs on this system
	}
	if err != nil {
		op.Err = err
		return false, op
	}
	var newest time.Time
	for _, p := range []string{dir, ModprobeConf} {
		if fi, err := s.stat(p); err == nil && fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	return newest.After(img.ModTime()), op
}

// DeviceRole classifies a device as "storage", "network", "display",
// "wireless" or "input", or "" when losing its driver is no big deal.
func DeviceRole(d Device) string {
	if d.Bus == "pci" && len(d.Class) >= 2 {
		switch d.Class[:2] {
		case "01":
			return "storage"
		case "02":
			return "network"
		case "03":
			return "display"
		case "0d":
			return "wireless"
		}
		return ""
	}
	for _, b := range d.Bindings {
		switch b.Class {
		case "08":
			return "storage"
		case "02", "0a":
			return "network"
		case "e0":
			return "wireless"
		case "03":
			return "input"
		}
	}
	return ""
}

// Impact explains what goes away with module: whether it is in use, which
// important devices it (or a loaded module depending on it) drives, and
// which mounted filesystems it provides. An empty result means no warning.
func Impact(module string, loaded []Module, devByMod map[string][]Device, mounts map[string][]string) []string {
	name := Normalize(module)
	byName := map[string]Module{}
	for _, m := range loaded {
		byName[Normalize(m.Name)] = m
	}

	var out []string
	if m, ok := byName[name]; ok && (m.RefCount > 0 || len(m.UsedBy) > 0) {
		s := fmt.Sprintf("in use (use count %d)", m.RefCount)
		if len(m.UsedBy) > 0 {
			s = fmt.Sprintf("in use by %s", strings.Join(m.UsedBy, ", "))
		}
		out = append(out, s)
	}

	// The module and every loaded module that depends on it, at any depth.
	affected := []string{name}
	for i := 0; i < len(affected); i++ {
		for _, user := range byName[affected[i]].UsedBy {
			if u := Normalize(user); !slices.Contains(affected, u) {
				affected = append(affected, u)
			}
		}
	}
	for _, mod := range affected {
		for _, d := range devByMod[mod] {
			role := DeviceRole(d)
			if role == "" {
				continue
			}
			s := fmt.Sprintf("drives %s device %s %s", role, d.Bus, d.Addr)
			if n := deviceLabel(d); n != "" {
				s += " (" + n + ")"
			}
			if mod != name {
				s += " through " + mod
			}
			out = append(out, s)
		}
	}

	// FUSE mounts carry a subtype ("fuse.portal"); the module is "fuse".
	var types []string
	for fstype := range mounts {
		if Normalize(fstype) == name || strings.HasPrefix(fstype, name+".") {
			types = append(types, fstype)
		}
	}
	slices.Sort(types)
	for _, fstype := range types {
		out = append(out, fmt.Sprintf("provides the %s filesystem mounted at %s", fstype, strings.Join(mounts[fstype], ", ")))
	}
	return out
}

func deviceLabel(d Device) string {
	for _, s := range []string{d.Product, d.ClassName} {
		if s != "" {
			return s
		}
	}
	return ""
}
