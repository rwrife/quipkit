// Package main is the quipkit CLI entrypoint.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"

	"github.com/rwrife/quipkit/internal/config"
	"github.com/rwrife/quipkit/internal/match"
	"github.com/rwrife/quipkit/internal/store"
	"github.com/rwrife/quipkit/internal/tui"
)

// Version is the quipkit version string. Overridable via -ldflags "-X main.Version=...".
var Version = "0.1.0-dev"

// stdoutIsTTY tells the CLI whether the default command should launch
// the interactive picker or fall back to non-interactive `list` output.
// Overridable in tests.
var stdoutIsTTY = func() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) && isatty.IsTerminal(os.Stdin.Fd())
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("quipkit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	versionFlag := fs.Bool("version", false, "print version and exit")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "quipkit %s\n", Version)
		fmt.Fprintln(stderr, "usage: quipkit [--version] [command] [args]")
		fmt.Fprintln(stderr, "commands:")
		fmt.Fprintln(stderr, "  (default)      launch interactive fuzzy picker (falls back to `list` when stdout is not a TTY)")
		fmt.Fprintln(stderr, "  list           list snippets (title\\ttags), pipe-friendly")
		fmt.Fprintln(stderr, "  find <query>   fuzzy-rank snippets by query (title\\ttags)")
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

	// Default command depends on whether we have a real terminal. When
	// piped or redirected, fall back to `list` so `quipkit | grep foo`
	// keeps working; the TUI would just error on a non-TTY.
	defaultCmd := "list"
	if stdoutIsTTY() {
		defaultCmd = "tui"
	}
	cmd := defaultCmd
	if fs.NArg() > 0 {
		cmd = fs.Arg(0)
	}

	switch cmd {
	case "tui":
		return cmdTUI(dir, stdout, stderr)
	case "list":
		return cmdList(dir, stdout, stderr)
	case "find":
		return cmdFind(dir, fs.Args()[1:], stdout, stderr)
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

// cmdFind prints snippets ranked by fuzzy relevance to the query.
// Output format matches `list`: title\ttags (pipe-friendly, stable).
// An empty/whitespace query behaves like `list`.
func cmdFind(dir string, args []string, stdout, stderr io.Writer) int {
	query := strings.TrimSpace(strings.Join(args, " "))
	if query == "" {
		fmt.Fprintln(stderr, "quipkit: usage: quipkit find <query>")
		return 2
	}
	snips, err := store.Load(dir)
	if err != nil {
		fmt.Fprintf(stderr, "quipkit: %v\n", err)
		return 1
	}
	if len(snips) == 0 {
		fmt.Fprintf(stderr, "quipkit: no snippets found in %s\n", dir)
		return 0
	}
	ranked := match.Rank(query, snips)
	if len(ranked) == 0 {
		fmt.Fprintf(stderr, "quipkit: no matches for %q\n", query)
		return 0
	}
	for _, s := range ranked {
		fmt.Fprintf(stdout, "%s\t%s\n", s.Title, strings.Join(s.Tags, ","))
	}
	return 0
}

// cmdTUI launches the interactive picker. On successful selection, it
// prints the snippet body to stdout (clipboard integration lands in M5).
// On cancel or empty state, it exits with 0 and prints nothing.
func cmdTUI(dir string, stdout, stderr io.Writer) int {
	snips, err := store.Load(dir)
	if err != nil {
		fmt.Fprintf(stderr, "quipkit: %v\n", err)
		return 1
	}
	res, err := tui.Run(snips)
	if err != nil {
		fmt.Fprintf(stderr, "quipkit: %v\n", err)
		return 1
	}
	if _, err := tui.WriteSelected(stdout, res); err != nil {
		fmt.Fprintf(stderr, "quipkit: %v\n", err)
		return 1
	}
	return 0
}
