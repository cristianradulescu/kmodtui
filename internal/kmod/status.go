package kmod

import (
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"strconv"
	"strings"
)

// SecureBootState is the firmware Secure Boot state.
type SecureBootState int

const (
	SBUnknown SecureBootState = iota
	SBEnabled
	SBDisabled
	// SBUnsupported: no EFI (legacy BIOS boot) or no Secure Boot support.
	SBUnsupported
)

func (s SecureBootState) String() string {
	switch s {
	case SBEnabled:
		return "enabled"
	case SBDisabled:
		return "disabled"
	case SBUnsupported:
		return "not supported"
	}
	return "unknown"
}

// SecureBoot is the Secure Boot state plus extra notes, e.g. setup mode.
type SecureBoot struct {
	State SecureBootState
	Notes []string
}

// TaintFlag is one set bit of /proc/sys/kernel/tainted.
type TaintFlag struct {
	Bit    int
	Letter string
	Desc   string
}

// taintFlags follows Documentation/admin-guide/tainted-kernels.rst.
var taintFlags = []TaintFlag{
	{0, "P", "proprietary module was loaded"},
	{1, "F", "module was force loaded"},
	{2, "S", "kernel running on an out of specification system"},
	{3, "R", "module was force unloaded"},
	{4, "M", "processor reported a Machine Check Exception"},
	{5, "B", "bad page referenced or unexpected page flags"},
	{6, "U", "taint requested by userspace"},
	{7, "D", "kernel died recently (OOPS or BUG)"},
	{8, "A", "ACPI table overridden by user"},
	{9, "W", "kernel issued a warning"},
	{10, "C", "staging driver was loaded"},
	{11, "I", "workaround for platform firmware bug applied"},
	{12, "O", "externally-built (out-of-tree) module was loaded"},
	{13, "E", "unsigned module was loaded"},
	{14, "L", "soft lockup occurred"},
	{15, "K", "kernel has been live patched"},
	{16, "X", "auxiliary taint (distribution-defined)"},
	{17, "T", "kernel built with the struct randomization plugin"},
	{18, "N", "in-kernel test has been run"},
	{19, "J", "userspace used a mutating debug operation in fwctl"},
}

// DecodeTaint lists the flags set in a /proc/sys/kernel/tainted value.
// Unknown bits are reported with Letter "?".
func DecodeTaint(v uint64) []TaintFlag {
	var flags []TaintFlag
	for bit := 0; bit < 64; bit++ {
		if v&(1<<bit) == 0 {
			continue
		}
		if bit < len(taintFlags) {
			flags = append(flags, taintFlags[bit])
		} else {
			flags = append(flags, TaintFlag{bit, "?", fmt.Sprintf("unknown taint bit %d", bit)})
		}
	}
	return flags
}

// TaintLetters returns the compact letter form, e.g. "OE".
func TaintLetters(flags []TaintFlag) string {
	var b strings.Builder
	for _, f := range flags {
		b.WriteString(f.Letter)
	}
	return b.String()
}

// ParseMokutil parses `mokutil --sb-state` output:
//
//	SecureBoot enabled
//	SecureBoot disabled
//	Platform is in Setup Mode
//	This system doesn't support Secure Boot
func ParseMokutil(out string) SecureBoot {
	var sb SecureBoot
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case line == "SecureBoot enabled":
			sb.State = SBEnabled
		case line == "SecureBoot disabled":
			sb.State = SBDisabled
		case strings.Contains(line, "doesn't support Secure Boot"),
			strings.Contains(line, "EFI variables are not supported"):
			sb.State = SBUnsupported
		default:
			sb.Notes = append(sb.Notes, line)
		}
	}
	return sb
}

// EFI global variable GUID; SecureBoot and SetupMode live under it.
const efiGlobalGUID = "8be4df61-93ca-11d2-aa0d-00e098032b8c"

const (
	efivarsDir     = "/sys/firmware/efi/efivars"
	secureBootVar  = efivarsDir + "/SecureBoot-" + efiGlobalGUID
	setupModeVar   = efivarsDir + "/SetupMode-" + efiGlobalGUID
	secureBootFrom = "od -An -tu1 " + secureBootVar
)

// ParseEFIBoolVar decodes a one-byte EFI variable as exposed by efivarfs:
// 4 bytes of attributes followed by the value.
func ParseEFIBoolVar(b []byte) (bool, error) {
	if len(b) < 5 {
		return false, fmt.Errorf("EFI variable too short (%d bytes)", len(b))
	}
	return b[4] == 1, nil
}

// Status is the system state shown in the Status panel.
type Status struct {
	Release    string
	SecureBoot SecureBoot
	Taint      uint64
	TaintFlags []TaintFlag
}

// Status reads the kernel release, Secure Boot state and taint flags.
// Fields that cannot be read keep their zero value; see the returned ops.
func (s *Source) Status() (Status, []Op) {
	var st Status
	var ops []Op

	relOp := Op{Desc: "Kernel release", Cmd: "uname -r"}
	st.Release, relOp.Err = s.readString("/proc/sys/kernel/osrelease")
	ops = append(ops, relOp)

	var sbOp Op
	st.SecureBoot, sbOp = s.secureBoot()
	ops = append(ops, sbOp)

	taintOp := Op{Desc: "Kernel taint flags", Cmd: "cat /proc/sys/kernel/tainted"}
	if v, err := s.readString("/proc/sys/kernel/tainted"); err != nil {
		taintOp.Err = err
	} else if st.Taint, err = strconv.ParseUint(v, 10, 64); err != nil {
		taintOp.Err = fmt.Errorf("/proc/sys/kernel/tainted: %w", err)
	} else {
		st.TaintFlags = DecodeTaint(st.Taint)
	}
	ops = append(ops, taintOp)
	return st, ops
}

// secureBoot asks mokutil when it is installed (live system only) and
// otherwise reads the SecureBoot EFI variable directly; both work as a
// regular user.
func (s *Source) secureBoot() (SecureBoot, Op) {
	if s.Live() {
		op := Op{Desc: "Secure Boot state", Cmd: "mokutil --sb-state"}
		out, err := s.Runner.Run("mokutil", "--sb-state")
		if err == nil {
			return ParseMokutil(string(out)), op
		}
		if !errors.Is(err, exec.ErrNotFound) {
			// mokutil exits non-zero on non-EFI systems; its message says why.
			if sb := ParseMokutil(err.Error()); sb.State == SBUnsupported {
				return sb, op
			}
		}
	}

	op := Op{Desc: "Secure Boot state (EFI variable)", Cmd: secureBootFrom}
	if !s.exists("/sys/firmware/efi") {
		return SecureBoot{State: SBUnsupported, Notes: []string{"not booted with EFI (legacy BIOS)"}}, op
	}
	b, err := s.readFile(secureBootVar)
	if errors.Is(err, fs.ErrNotExist) {
		return SecureBoot{State: SBUnsupported, Notes: []string{"firmware has no SecureBoot variable"}}, op
	}
	if err != nil {
		op.Err = err
		return SecureBoot{}, op
	}
	on, err := ParseEFIBoolVar(b)
	if err != nil {
		op.Err = fmt.Errorf("%s: %w", secureBootVar, err)
		return SecureBoot{}, op
	}
	sb := SecureBoot{State: SBDisabled}
	if on {
		sb.State = SBEnabled
	}
	if b, err := s.readFile(setupModeVar); err == nil {
		if setup, err := ParseEFIBoolVar(b); err == nil && setup {
			sb.Notes = append(sb.Notes, "Platform is in Setup Mode")
		}
	}
	return sb, op
}
