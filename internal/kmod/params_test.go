package kmod

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParams(t *testing.T) {
	// Checked-out fixture files are always owner-writable (git keeps no
	// read-only bit); TestParamsWritable covers read-only parameters.
	src, _ := fixtureSource(t)
	tests := []struct {
		module string
		want   []Param
	}{
		{"nvme", []Param{
			{Name: "io_queue_depth", Value: "1024", Writable: true},
			{Name: "max_host_mem_size_mb", Value: "128", Writable: true},
			{Name: "noacpi", Value: "N", Writable: true},
			{Name: "poll_queues", Value: "0", Writable: true},
			{Name: "quirks", Value: "", Writable: true},
			{Name: "sgl_threshold", Value: "32768", Writable: true},
			{Name: "use_cmb_sqes", Value: "Y", Writable: true},
			{Name: "use_threaded_interrupts", Value: "0", Writable: true},
			{Name: "write_queues", Value: "0", Writable: true},
		}},
		// Built-in modules expose parameters too.
		{"usbcore", []Param{{Name: "autosuspend", Value: "2", Writable: true}, {Name: "old_scheme_first", Value: "N", Writable: true}}},
		// Dashes are normalized: /sys/module always uses underscores.
		{"nvme-core", nil},
		// Loaded module without parameters, and a module that is not loaded.
		{"btusb", nil},
		{"e1000e", nil},
	}
	for _, tt := range tests {
		t.Run(tt.module, func(t *testing.T) {
			got, op := src.Params(tt.module)
			if op.Err != nil {
				t.Fatal(op.Err)
			}
			if len(tt.want) == 0 && len(got) == 0 {
				return
			}
			assertEqual(t, "params", got, tt.want)
		})
	}
}

func TestParamsUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read 0400 files")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "sys/module/amdgpu/parameters")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// We own these files, so a 0400 file would be readable. 0040 reproduces
	// the root-only case: readable by someone, but not by us.
	files := map[string]os.FileMode{"aspm": 0o444, "gartsize": 0o040, "new_array": 0o200}
	for name, mode := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("-1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
	}
	src := &Source{Root: root, Runner: &fakeRunner{}}
	got, op := src.Params("amdgpu")
	if op.Err != nil {
		t.Fatal(op.Err)
	}
	assertEqual(t, "params", got, []Param{
		{Name: "aspm", Value: "-1"},
		{Name: "gartsize", Unreadable: "root only"},
		{Name: "new_array", Unreadable: "write-only", Writable: true},
	})
	assertEqual(t, "cmd", op.Cmd, "grep -H . /sys/module/amdgpu/parameters/*")
}
