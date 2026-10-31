package kmod

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const fixtureRoot = "testdata/root"

// fakeRunner answers "modinfo <name>" from testdata/modinfo/<name>.txt and
// any other command from outputs (keyed by the full command line). Unknown
// commands fail as if the binary were not installed.
type fakeRunner struct {
	outputs map[string]string
	calls   []string
}

func (f *fakeRunner) Run(name string, args ...string) ([]byte, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, line)
	if out, ok := f.outputs[line]; ok {
		return []byte(out), nil
	}
	if name == "modinfo" && len(args) > 0 {
		mod := args[len(args)-1]
		b, err := os.ReadFile(filepath.Join("testdata", "modinfo", mod+".txt"))
		if err != nil {
			return nil, fmt.Errorf("modinfo: exit status 1: modinfo: ERROR: Module %s not found.", mod)
		}
		return b, nil
	}
	return nil, &exec.Error{Name: name, Err: exec.ErrNotFound}
}

func fixtureSource(t *testing.T) (*Source, *fakeRunner) {
	t.Helper()
	r := &fakeRunner{}
	return &Source{Root: fixtureRoot, Runner: r}, r
}

func emptySource(t *testing.T) *Source {
	t.Helper()
	return &Source{Root: t.TempDir(), Runner: &fakeRunner{}}
}

func assertEqual[T any](t *testing.T, name string, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s:\n got: %#v\nwant: %#v", name, got, want)
	}
}

func stringsReader(s string) *strings.Reader { return strings.NewReader(s) }
