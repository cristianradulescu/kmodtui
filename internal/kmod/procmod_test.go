package kmod

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
)

func TestParseProcModules(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []Module
		wantErr string
	}{
		{
			name: "unused module",
			in:   "nvme_keyring 20480 1 nvme_core, Live 0x0000000000000000\n",
			want: []Module{{Name: "nvme_keyring", Size: 20480, RefCount: 1, UsedBy: []string{"nvme_core"}, State: "Live"}},
		},
		{
			name: "no holders",
			in:   "amdgpu 21585920 31 - Live 0x0000000000000000\n",
			want: []Module{{Name: "amdgpu", Size: 21585920, RefCount: 31, State: "Live"}},
		},
		{
			name: "several holders with trailing comma",
			in:   "llc 16384 2 bridge,stp, Live 0x0000000000000000\n",
			want: []Module{{Name: "llc", Size: 16384, RefCount: 2, UsedBy: []string{"bridge", "stp"}, State: "Live"}},
		},
		{
			name: "tainted out-of-tree module",
			in:   "vboxdrv 696320 2 vboxnetadp,vboxnetflt, Live 0x0000000000000000 (OE)\n",
			want: []Module{{Name: "vboxdrv", Size: 696320, RefCount: 2, UsedBy: []string{"vboxnetadp", "vboxnetflt"}, State: "Live", Taint: "OE"}},
		},
		{
			name: "unloading, refcount not tracked",
			in:   "pcspkr 12288 - - Unloading 0xffffffffc0a1c000\n",
			want: []Module{{Name: "pcspkr", Size: 12288, RefCount: -1, State: "Unloading"}},
		},
		{
			name: "blank lines ignored",
			in:   "\nstp 12288 1 bridge, Live 0x0000000000000000\n\n",
			want: []Module{{Name: "stp", Size: 12288, RefCount: 1, UsedBy: []string{"bridge"}, State: "Live"}},
		},
		{
			name:    "malformed line is skipped and reported",
			in:      "stp 12288 1 bridge, Live 0x0\ngarbage\nllc 16384 2 bridge,stp, Live 0x0\n",
			want:    []Module{{Name: "stp", Size: 12288, RefCount: 1, UsedBy: []string{"bridge"}, State: "Live"}, {Name: "llc", Size: 16384, RefCount: 2, UsedBy: []string{"bridge", "stp"}, State: "Live"}},
			wantErr: "line 2: expected at least 5 fields",
		},
		{
			name:    "bad size",
			in:      "stp big 1 bridge, Live 0x0\n",
			wantErr: `line 1: bad size "big" for stp`,
		},
		{
			name: "empty",
			in:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseProcModules(strings.NewReader(tt.in))
			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
			assertEqual(t, "modules", got, tt.want)
		})
	}
}

func TestLoadedModules(t *testing.T) {
	src, _ := fixtureSource(t)
	mods, op := src.LoadedModules()
	if op.Err != nil {
		t.Fatalf("LoadedModules: %v", op.Err)
	}
	assertEqual(t, "cmd", op.Cmd, "lsmod")
	if len(mods) != 28 {
		t.Errorf("got %d modules, want 28", len(mods))
	}
	byName := map[string]Module{}
	for _, m := range mods {
		byName[m.Name] = m
	}
	assertEqual(t, "nvme_core.UsedBy", byName["nvme_core"].UsedBy, []string{"nvme"})
	assertEqual(t, "vboxdrv.Taint", byName["vboxdrv"].Taint, "OE")
}

func TestLoadedModulesMissingFile(t *testing.T) {
	mods, op := emptySource(t).LoadedModules()
	if !errors.Is(op.Err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want not-exist", op.Err)
	}
	if mods != nil {
		t.Errorf("mods = %v, want nil", mods)
	}
}
