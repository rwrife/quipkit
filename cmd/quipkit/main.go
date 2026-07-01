// Package main is the quipkit CLI entrypoint.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rwrife/quipkit/internal/config"
	"github.com/rwrife/quipkit/internal/store"
)

// Version is the quipkit version string. Overridable via -ldflags "-X main.Version=...".
var Version = "0.1.0-dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("quipkit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	versionFlag := fs.Bool("version", false, "print version and exit")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "quipkit %s\n", Version)
		fmt.Fprintln(stderr, "usage: quipkit [--version] [command]")
		fmt.Fprintln(stderr, "commands:")
		fmt.Fprintln(stderr, "  list    list snippets (title\\ttags), pipe-friendly")
		fmt.Fprintln(stderr, "snippet dir: $QUIPKIT_DIR or ~/.quipkit")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *versionFlag {
		fmt.Fprintf(stdout, "quipkit %s\n", Version)
		return 0
	}

	dir, err := config.SnippetDir()
	if err != nil {
		fmt.Fprintf(stderr, "quipkit: %v\n", err)
		return 1
	}

	// Seed examples on first run (no-op if any snippets exist).
	if _, err := store.Seed(dir); err != nil {
		fmt.Fprintf(stderr, "quipkit: %v\n", err)
		return 1
	}

	cmd := "list"
	if fs.NArg() > 0 {
		cmd = fs.Arg(0)
	}

	switch cmd {
	case "list":
		return cmdList(dir, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "quipkit: unknown command %q\n", cmd)
		return 2
	}
}

func cmdList(dir string, stdout, stderr io.Writer) int {
	snips, err := store.Load(dir)
	if err != nil {
		fmt.Fprintf(stderr, "quipkit: %v\n", err)
		return 1
	}
	if len(snips) == 0 {
		fmt.Fprintf(stderr, "quipkit: no snippets found in %s\n", dir)
		return 0
	}
	for _, s := range snips {
		fmt.Fprintf(stdout, "%s\t%s\n", s.Title, strings.Join(s.Tags, ","))
	}
	return 0
}
