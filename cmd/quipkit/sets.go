package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/rwrife/quipkit/internal/config"
	"github.com/rwrife/quipkit/internal/store"
)

// cmdSets implements the `sets` meta-command:
//
//	quipkit sets                # list every set under <snippetDir>/sets/
//	quipkit sets create <name>  # materialize a new (empty) set folder
//
// Sets are just subfolders of `<snippetDir>/sets/` holding the same
// kind of markdown snippets as the base library, so this command is a
// thin convenience layer over `mkdir` / `ls`: it validates names and
// keeps the on-disk layout self-documenting.
//
// It always operates on the *base* snippet dir, ignoring the active
// `--set` for the invocation \u2014 listing sets while a set is active
// should still show all of them, not just the current one.
func cmdSets(baseDir string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return listSets(baseDir, stdout, stderr)
	}
	switch args[0] {
	case "list":
		return listSets(baseDir, stdout, stderr)
	case "create":
		return createSet(baseDir, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "quipkit: unknown sets subcommand %q\n", args[0])
		fmt.Fprintln(stderr, "usage: quipkit sets [list|create <name>]")
		return 2
	}
}

func listSets(baseDir string, stdout, stderr io.Writer) int {
	sets, err := config.ListSets(baseDir)
	if err != nil {
		fmt.Fprintf(stderr, "quipkit: %v\n", err)
		return 1
	}
	if len(sets) == 0 {
		fmt.Fprintf(stderr, "quipkit: no sets defined (create one with `quipkit sets create <name>`)\n")
		return 0
	}
	for _, s := range sets {
		// title\tcount matches the tab-separated style of `list` and
		// `stats`, so a `sets | column -t` still lines up.
		dir, _ := config.EffectiveDir(baseDir, s)
		count := 0
		if snips, err := store.Load(dir); err == nil {
			count = len(snips)
		}
		fmt.Fprintf(stdout, "%s\t%d\n", s, count)
	}
	return 0
}

func createSet(baseDir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sets create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: quipkit sets create <name>")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	name := fs.Arg(0)
	dir, err := config.CreateSet(baseDir, name)
	if err != nil {
		fmt.Fprintf(stderr, "quipkit: %v\n", err)
		return 1
	}
	// Seed the new set so a freshly-created `work` has starter
	// snippets, matching the first-run behavior of the base library.
	if _, err := store.Seed(dir); err != nil {
		fmt.Fprintf(stderr, "quipkit: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "created set %q at %s\n", name, dir)
	return 0
}
