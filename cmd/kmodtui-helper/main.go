// Command kmodtui-helper performs kmodtui's privileged actions. It is run
// as root through pkexec (see packaging/) and takes no flags: everything it
// may do is a fixed subcommand, see helper.Usage.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/cristianradulescu/kmodtui/internal/helper"
)

func main() {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "kmodtui-helper: must run as root; kmodtui starts it with pkexec")
		os.Exit(1)
	}
	env := helper.Env{
		Root: "/",
		Out:  os.Stdout,
		Exec: func(path string, args ...string) error {
			cmd := exec.Command(path, args...)
			cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			return cmd.Run()
		},
	}
	if err := helper.Run(env, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "kmodtui-helper:", err)
		if errors.Is(err, helper.ErrUsage) {
			fmt.Fprint(os.Stderr, "\n"+helper.Usage)
			os.Exit(2)
		}
		os.Exit(1)
	}
}
