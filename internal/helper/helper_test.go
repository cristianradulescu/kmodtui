package helper

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cristianradulescu/kmodtui/internal/kmod"
)

const release = "7.0.0-38-generic"

type recorder struct {
	calls []string
	fail  error
}

func (r *recorder) exec(path string, args ...string) error {
	r.calls = append(r.calls, strings.Join(append([]string{path}, args...), " "))
	return r.fail
}

// newRoot builds a minimal root: kernel release, modules.dep from the kmod
// fixtures, and nvme parameters (one writable, one read-only).
func newRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dep, err := os.ReadFile(filepath.Join("..", "kmod", "testdata", "root", "lib", "modules", release, "modules.dep"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]struct {
		content string
		mode    os.FileMode
	}{
		"proc/sys/kernel/osrelease":                        {release + "\n", 0o444},
		"lib/modules/" + release + "/modules.dep":          {string(dep), 0o644},
		"sys/module/nvme/parameters/io_queue_depth":        {"1024\n", 0o644},
		"sys/module/nvme/parameters/max_host_mem_size_mb":  {"128\n", 0o444},
		"sys/module/nvme/parameters/use_threaded_interrup": {"0\n", 0o644},
	}
	for rel, f := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f.content), f.mode); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func newEnv(t *testing.T) (Env, *recorder, *bytes.Buffer) {
	t.Helper()
	rec := &recorder{}
	out := &bytes.Buffer{}
	return Env{Root: newRoot(t), Exec: rec.exec, Out: out}, rec, out
}

func readConf(t *testing.T, env Env, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(env.Root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCommands(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"load", "e1000e"}, "/usr/sbin/modprobe e1000e"},
		{[]string{"unload", "nvme"}, "/usr/sbin/modprobe -r nvme"},
		{[]string{"load", "snd-hda-intel"}, "/usr/sbin/modprobe snd-hda-intel"},
		{[]string{"update-initramfs"}, "/usr/sbin/update-initramfs -u"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			env, rec, out := newEnv(t)
			if err := Run(env, tt.args); err != nil {
				t.Fatal(err)
			}
			if len(rec.calls) != 1 || rec.calls[0] != tt.want {
				t.Errorf("calls = %q, want [%q]", rec.calls, tt.want)
			}
			if !strings.Contains(out.String(), "+ "+tt.want) {
				t.Errorf("output %q does not echo the command", out.String())
			}
		})
	}
}

func TestRejects(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		usage bool
		msg   string
	}{
		{"no subcommand", nil, true, "no subcommand"},
		{"unknown subcommand", []string{"rmmod", "nvme"}, true, "unknown subcommand"},
		{"insmod is not offered", []string{"insmod", "/tmp/x.ko"}, true, "unknown subcommand"},
		{"missing module", []string{"load"}, true, "exactly one module"},
		{"extra argument", []string{"unload", "nvme", "--force"}, true, "exactly one module"},
		{"option as module", []string{"unload", "-r"}, false, "invalid module name"},
		{"path as module", []string{"load", "../../tmp/evil"}, false, "invalid module name"},
		{"unknown module", []string{"load", "nosuchmod"}, false, "not in /lib/modules/7.0.0-38-generic/modules.dep"},
		{"built-in module", []string{"unload", "usbcore"}, false, "built into the kernel"},
		{"bad param name", []string{"set-param", "nvme", "io-queue", "1", "--now"}, false, "invalid parameter name"},
		{"bad value", []string{"set-param", "nvme", "io_queue_depth", "1 2", "--boot"}, false, "invalid parameter value"},
		{"newline in value", []string{"set-param", "nvme", "io_queue_depth", "1\nblacklist nvme", "--boot"}, false, "invalid parameter value"},
		{"no mode", []string{"set-param", "nvme", "io_queue_depth", "512"}, true, "needs --now, --boot"},
		{"unknown flag", []string{"set-param", "nvme", "io_queue_depth", "512", "--persist"}, true, "unknown set-param flag"},
		{"update-initramfs args", []string{"update-initramfs", "-k", "all"}, true, "takes no arguments"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, rec, _ := newEnv(t)
			err := Run(env, tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.msg) {
				t.Fatalf("err = %v, want containing %q", err, tt.msg)
			}
			if errors.Is(err, ErrUsage) != tt.usage {
				t.Errorf("usage error = %v, want %v", errors.Is(err, ErrUsage), tt.usage)
			}
			if len(rec.calls) != 0 {
				t.Errorf("ran %q despite the error", rec.calls)
			}
		})
	}
}

func TestBlacklistAndBootLoad(t *testing.T) {
	env, rec, out := newEnv(t)
	steps := [][]string{
		{"blacklist", "e1000e"},
		{"blacklist", "e1000e"}, // idempotent
		{"boot-load", "bridge"},
		{"set-param", "nvme", "io_queue_depth", "512", "--boot"},
	}
	for _, s := range steps {
		if err := Run(env, s); err != nil {
			t.Fatalf("%v: %v", s, err)
		}
	}
	if len(rec.calls) != 0 {
		t.Errorf("config edits ran commands: %q", rec.calls)
	}
	modprobe := readConf(t, env, kmod.ModprobeConf)
	want := kmod.ConfHeader + "\nblacklist e1000e\noptions nvme io_queue_depth=512\n"
	if modprobe != want {
		t.Errorf("modprobe.d:\n%s\nwant:\n%s", modprobe, want)
	}
	if got := readConf(t, env, kmod.ModulesLoadConf); got != kmod.ConfHeader+"\nbridge\n" {
		t.Errorf("modules-load.d: %q", got)
	}
	if !strings.Contains(out.String(), "already up to date") {
		t.Errorf("second blacklist should report no change: %q", out.String())
	}
	fi, err := os.Stat(filepath.Join(env.Root, kmod.ModprobeConf))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", fi.Mode().Perm())
	}
	// No temporary files left behind.
	entries, _ := os.ReadDir(filepath.Dir(filepath.Join(env.Root, kmod.ModprobeConf)))
	if len(entries) != 1 {
		t.Errorf("modprobe.d has %d entries, want only kmodtui.conf", len(entries))
	}

	// And back.
	for _, s := range [][]string{{"unblacklist", "e1000e"}, {"boot-unload", "bridge"}, {"set-param", "nvme", "io_queue_depth", "--clear-boot"}} {
		if err := Run(env, s); err != nil {
			t.Fatalf("%v: %v", s, err)
		}
	}
	if got := readConf(t, env, kmod.ModprobeConf); got != kmod.ConfHeader+"\n" {
		t.Errorf("after undo modprobe.d = %q", got)
	}
	if got := readConf(t, env, kmod.ModulesLoadConf); got != kmod.ConfHeader+"\n" {
		t.Errorf("after undo modules-load.d = %q", got)
	}
}

// Hand-written lines in kmodtui's file survive an edit.
func TestEditKeepsOtherLines(t *testing.T) {
	env, _, _ := newEnv(t)
	p := filepath.Join(env.Root, kmod.ModprobeConf)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("# mine\ninstall floppy /bin/false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Run(env, []string{"blacklist", "e1000e"}); err != nil {
		t.Fatal(err)
	}
	if got := readConf(t, env, kmod.ModprobeConf); got != "# mine\ninstall floppy /bin/false\nblacklist e1000e\n" {
		t.Errorf("content = %q", got)
	}
}

func TestClearAllOptions(t *testing.T) {
	env, _, _ := newEnv(t)
	for _, s := range [][]string{
		{"set-param", "nvme", "io_queue_depth", "512", "--boot"},
		{"set-param", "nvme", "write_queues", "2", "--boot"},
		{"blacklist", "e1000e"},
		{"set-param", "nvme", "--clear-boot-all"},
	} {
		if err := Run(env, s); err != nil {
			t.Fatalf("%v: %v", s, err)
		}
	}
	if got := readConf(t, env, kmod.ModprobeConf); got != kmod.ConfHeader+"\nblacklist e1000e\n" {
		t.Errorf("content = %q", got)
	}
	if err := Run(env, []string{"set-param", "-x", "--clear-boot-all"}); err == nil {
		t.Error("invalid module name accepted")
	}
}

func TestSetParamNow(t *testing.T) {
	env, _, out := newEnv(t)
	if err := Run(env, []string{"set-param", "nvme", "io_queue_depth", "512", "--now"}); err != nil {
		t.Fatal(err)
	}
	if got := readConf(t, env, "/sys/module/nvme/parameters/io_queue_depth"); got != "512" {
		t.Errorf("sysfs value = %q, want 512", got)
	}
	if !strings.Contains(out.String(), "+ echo 512 > /sys/module/nvme/parameters/io_queue_depth") {
		t.Errorf("output = %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(env.Root, kmod.ModprobeConf)); err == nil {
		t.Error("--now alone must not touch modprobe.d")
	}

	err := Run(env, []string{"set-param", "nvme", "max_host_mem_size_mb", "64", "--now"})
	if err == nil || !strings.Contains(err.Error(), "use --boot") {
		t.Errorf("read-only param: err = %v", err)
	}
	err = Run(env, []string{"set-param", "e1000e", "IntMode", "1", "--now"})
	if err == nil || !strings.Contains(err.Error(), "is the module loaded") {
		t.Errorf("module not loaded: err = %v", err)
	}
}

func TestCommandFailure(t *testing.T) {
	env, rec, _ := newEnv(t)
	rec.fail = errors.New("exit status 1")
	err := Run(env, []string{"unload", "nvme"})
	if err == nil || !strings.Contains(err.Error(), "modprobe failed: exit status 1") {
		t.Errorf("err = %v", err)
	}
}
