// Command kmodtui is a lazygit-style TUI for Linux kernel modules.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/cristianradulescu/kmodtui/internal/kmod"
	"github.com/cristianradulescu/kmodtui/internal/ui"
)

func main() {
	root := flag.String("root", "/", "filesystem root to read from (a fixture tree for development)")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [-root DIR]\n\n", os.Args[0])
		fmt.Fprintln(flag.CommandLine.Output(), "A read-only TUI for Linux kernel modules. Press ? inside for keybindings.")
		fmt.Fprintln(flag.CommandLine.Output())
		flag.PrintDefaults()
	}
	flag.Parse()

	if fi, err := os.Stat(*root); err != nil || !fi.IsDir() {
		fmt.Fprintf(os.Stderr, "kmodtui: -root %s is not a directory\n", *root)
		os.Exit(2)
	}
	if err := ui.New(kmod.NewSource(*root)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "kmodtui:", err)
		os.Exit(1)
	}
}
