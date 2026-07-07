// Package main is the quipkit CLI entrypoint.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/mattn/go-isatty"

	"github.com/rwrife/quipkit/internal/clip"
	"github.com/rwrife/quipkit/internal/config"
	"github.com/rwrife/quipkit/internal/match"
	"github.com/rwrife/quipkit/internal/placeholders"
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

// copyToClipboard is the clipboard entrypoint the CLI actually uses.
// It's a package var so tests can swap it (and so the fallback path in
// cmdTUI doesn't have to know about the clip package internals).
var copyToClipboard = clip.Copy

// clipboardAvailable reports whether the system exposes a clipboard.
// Also swappable in tests.
var clipboardAvailable = clip.Available

// runEditor spawns the configured editor against the given file path.
// It's a package var so tests can stub it out without invoking a real
// editor binary.
var runEditor = func(editor, path string, stdin io.Reader, stdout, stderr io.Writer) error {
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		return fmt.Errorf("editor command is empty")
	}
	args := append(fields[1:], path)
	cmd := exec.Command(fields[0], args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
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
		fmt.Fprintln(stderr, "  (default)       launch interactive fuzzy picker (falls back to `list` when stdout is not a TTY)")
		fmt.Fprintln(stderr, "  list            list snippets (title\\ttags), pipe-friendly")
		fmt.Fprintln(stderr, "  find <query>    fuzzy-rank snippets by query (title\\ttags)")
		fmt.Fprintln(stderr, "  add <text>      write a new snippet (flags: --title, --tags a,b)")
		fmt.Fprintln(stderr, "  edit [query]    open a snippet in $EDITOR (fuzzy picks the top match, or opens picker on a TTY)")
		fmt.Fprintln(stderr, "snippet dir: $QUIPKIT_DIR, config `snippet_dir`, or ~/.quipkit")
		fmt.Fprintln(stderr, "config file: $XDG_CONFIG_HOME/quipkit/config or ~/.config/quipkit/config")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *versionFlag {
		fmt.Fprintf(stdout, "quipkit %s\n", Version)
		return 0
	}

	cfgFile, err := config.LoadFile()
	if err != nil {
		fmt.Fprintf(stderr, "quipkit: %v\n", err)
		return 1
	}
	dir, err := config.ResolveSnippetDir(cfgFile)
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
	case "add":
		return cmdAdd(dir, fs.Args()[1:], stdout, stderr)
	case "edit":
		return cmdEdit(dir, cfgFile, fs.Args()[1:], stdout, stderr)
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

// cmdAdd writes a new snippet. Positional args are joined with spaces to
// form the body (so both `quipkit add hello world` and
// `quipkit add "hello world"` work); when there are no positional args
// and stdin is not a TTY, the body is read from stdin instead so
// pipelines can pipe text in.
func cmdAdd(dir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	title := fs.String("title", "", "snippet title (frontmatter)")
	tags := fs.String("tags", "", "comma-separated tags, e.g. work,reply")
	filename := fs.String("file", "", "explicit filename (default: derived from title/body)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: quipkit add [--title T] [--tags a,b] [--file NAME] <text>")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	body := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if body == "" {
		// Fall back to stdin so `echo hi | quipkit add --title x` works.
		if !isatty.IsTerminal(os.Stdin.Fd()) {
			b, err := io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Fprintf(stderr, "quipkit: read stdin: %v\n", err)
				return 1
			}
			body = strings.TrimSpace(string(b))
		}
	}
	if body == "" {
		fmt.Fprintln(stderr, "quipkit: add needs snippet text (as args or on stdin)")
		fs.Usage()
		return 2
	}

	path, err := store.Add(dir, body, store.AddOptions{
		Title:    *title,
		Tags:     splitCSV(*tags),
		Filename: *filename,
	})
	if err != nil {
		fmt.Fprintf(stderr, "quipkit: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "added %s\n", path)
	return 0
}

// splitCSV parses a comma-separated flag value into trimmed, non-empty
// tokens. Kept here (not in store) because it's a CLI concern.
func splitCSV(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// cmdEdit opens a snippet in the user's configured editor.
//
// Resolution order for which snippet to open:
//  1. --id <id> (exact match by snippet id, i.e. file base name without .md)
//  2. Positional query → top fuzzy match by title/tags/body
//  3. No args + TTY → launch the interactive picker; the picked snippet
//     is the one opened
//  4. No args + non-TTY → error (nothing to pick from silently)
//
// The editor command is resolved via [config.Editor], honoring VISUAL,
// EDITOR, and the config-file `editor` value in that order.
func cmdEdit(dir string, cfg config.File, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "open snippet by exact id (file base name)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: quipkit edit [--id ID] [<query>]")
	}
	if err := fs.Parse(args); err != nil {
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

	target, code := resolveEditTarget(snips, *id, fs.Args(), stderr)
	if code != 0 {
		return code
	}

	editor := config.Editor(cfg)
	if err := runEditor(editor, target.Path, os.Stdin, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "quipkit: editor %q failed: %v\n", editor, err)
		return 1
	}
	fmt.Fprintf(stderr, "edited %s (%s)\n", target.Title, target.Path)
	return 0
}

// resolveEditTarget picks the snippet to edit based on flags/args and
// TTY state. It writes any user-facing errors directly to stderr and
// returns (nil, non-zero) in that case so the caller can propagate the
// exit code without re-formatting.
func resolveEditTarget(snips []store.Snippet, id string, args []string, stderr io.Writer) (*store.Snippet, int) {
	id = strings.TrimSpace(id)
	if id != "" {
		for i := range snips {
			if snips[i].ID == id {
				return &snips[i], 0
			}
		}
		fmt.Fprintf(stderr, "quipkit: no snippet with id %q\n", id)
		return nil, 1
	}

	query := strings.TrimSpace(strings.Join(args, " "))
	if query != "" {
		ranked := match.Rank(query, snips)
		if len(ranked) == 0 {
			fmt.Fprintf(stderr, "quipkit: no matches for %q\n", query)
			return nil, 1
		}
		s := ranked[0]
		return &s, 0
	}

	if !stdoutIsTTY() {
		fmt.Fprintln(stderr, "quipkit: edit needs --id or a query when stdin/stdout aren't a TTY")
		return nil, 2
	}

	res, err := tui.Run(snips)
	if err != nil {
		fmt.Fprintf(stderr, "quipkit: %v\n", err)
		return nil, 1
	}
	if res.Selected == nil {
		// User cancelled — silent 0 exit, same as cmdTUI.
		return nil, 0
	}
	// The picker returns a value-typed snippet, but its ID uniquely
	// identifies the on-disk file so we re-resolve from the loaded list
	// to get a stable pointer with the definite Path attached.
	for i := range snips {
		if snips[i].ID == res.Selected.ID {
			return &snips[i], 0
		}
	}
	s := *res.Selected
	return &s, 0
}

// cmdTUI launches the interactive picker. On successful selection, it
// renders any {{placeholders}} in the snippet body (using autofill
// values, an optional vars.yaml in the snippet dir, and any prompts
// the picker collected) and copies the result to the clipboard. When
// no clipboard backend is available (bare Linux server, etc.) it falls
// back to printing the rendered body to stdout with a hint on stderr
// so the user can still pipe or paste manually. On cancel or empty
// state, it exits 0 with no output.
func cmdTUI(dir string, stdout, stderr io.Writer) int {
	snips, err := store.Load(dir)
	if err != nil {
		fmt.Fprintf(stderr, "quipkit: %v\n", err)
		return 1
	}

	// Build the substitution map: built-in autofills first, then
	// vars.yaml overrides / additions. Prompt values collected by the
	// picker (in the placeholder phase) always win because they’re
	// applied last, via [tui.Model.Update].
	vals := placeholders.NewValues()
	vals.Autofill()
	if err := vals.LoadVars(dir); err != nil {
		// Surface a broken vars file but keep running — without vars,
		// the picker will just prompt for the tokens instead.
		fmt.Fprintf(stderr, "quipkit: %v\n", err)
	}

	res, err := tui.RunWithOptions(snips, tui.Options{Values: vals})
	if err != nil {
		fmt.Fprintf(stderr, "quipkit: %v\n", err)
		return 1
	}
	if res.Selected == nil {
		// User cancelled (Esc / Ctrl-C) or empty snippet set. Silent 0.
		return 0
	}

	body := res.Rendered
	if body == "" {
		body = res.Selected.Body
	}
	title := res.Selected.Title

	if !clipboardAvailable() {
		// No backend — dump the body so the user still gets value out of
		// their pick, and explain how to get real clipboard support.
		if _, werr := tui.WriteSelected(stdout, res); werr != nil {
			fmt.Fprintf(stderr, "quipkit: %v\n", werr)
			return 1
		}
		// Route via clip.Copy purely to get a consistent hinted error
		// message across platforms.
		if err := copyToClipboard(body); err != nil {
			fmt.Fprintf(stderr, "quipkit: %v\n", err)
		}
		return 0
	}

	if err := copyToClipboard(body); err != nil {
		// Backend claimed available but failed at write time. Still
		// print the body so the user isn't stuck.
		fmt.Fprintf(stderr, "quipkit: clipboard copy failed: %v\n", err)
		if _, werr := tui.WriteSelected(stdout, res); werr != nil {
			fmt.Fprintf(stderr, "quipkit: %v\n", werr)
			return 1
		}
		return 1
	}
	fmt.Fprintf(stderr, "copied %q to clipboard\n", title)
	return 0
}
