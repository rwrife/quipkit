// Package main is the quipkit CLI entrypoint.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/rwrife/quipkit/internal/clip"
	"github.com/rwrife/quipkit/internal/config"
	"github.com/rwrife/quipkit/internal/frecency"
	"github.com/rwrife/quipkit/internal/match"
	"github.com/rwrife/quipkit/internal/placeholders"
	"github.com/rwrife/quipkit/internal/store"
	"github.com/rwrife/quipkit/internal/tui"
	"github.com/rwrife/quipkit/internal/typeit"
)

// typeMode captures how the CLI-level --type / --no-type flags interact
// with the config file's `auto_type` default. It's tri-state because
// there's a real difference between "user explicitly said no" and "user
// didn't say anything, so honor the config".
type typeMode int

const (
	typeModeUnset  typeMode = iota // no flag passed — fall back to config
	typeModeForce                  // --type
	typeModeReject                 // --no-type
)

// resolveAutoType folds the CLI flag and config default into a single
// bool. Extracted from run() so both `cmdTUI` and the tests can call
// it without going through the flag parser.
func resolveAutoType(cli typeMode, cfg config.File) bool {
	switch cli {
	case typeModeForce:
		return true
	case typeModeReject:
		return false
	}
	if cfg.AutoTypeSet {
		return cfg.AutoType
	}
	return false
}

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

// typeText is the auto-type entrypoint. Kept as a package var so tests
// can swap it and cmdTUI doesn't have to import the typeit shell-out
// path directly.
var typeText = typeit.Type

// typeAvailable reports whether a keystroke-injection backend is
// installed. Package var so tests can swap it.
var typeAvailable = typeit.Available

// typeBackendName returns the resolved backend name (e.g. "xdotool"),
// or "" when none is available. Used for the copied/typed confirmation
// line so users can see which tool actually did the typing.
var typeBackendName = func() string { return typeit.Detect().Name }

// timeNow returns the current wall-clock time. It's a package var so
// tests can pin time-of-day when they exercise the frecency-aware
// commands.
var timeNow = time.Now

// loadFrecency reads the frecency state from dir. Errors are surfaced
// via stderr but non-fatal: quipkit should degrade to plain fuzzy
// ranking rather than refusing to run because of a corrupt state file.
func loadFrecency(dir string, stderr io.Writer) *frecency.Values {
	v, err := frecency.Load(dir)
	if err != nil {
		fmt.Fprintf(stderr, "quipkit: %v (frecency ranking disabled)\n", err)
		return frecency.NewValues()
	}
	return v
}

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
	typeFlag := fs.Bool("type", false, "type the picked snippet via OS keystroke injection instead of copying")
	noTypeFlag := fs.Bool("no-type", false, "force clipboard mode even if the config enables auto-type")
	typeDelayFlag := fs.Int("type-delay-ms", -1, "per-keystroke delay for --type mode in milliseconds (>=0)")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "quipkit %s\n", Version)
		fmt.Fprintln(stderr, "usage: quipkit [--version] [--type|--no-type] [--type-delay-ms N] [command] [args]")
		fmt.Fprintln(stderr, "commands:")
		fmt.Fprintln(stderr, "  (default)       launch interactive fuzzy picker (falls back to `list` when stdout is not a TTY)")
		fmt.Fprintln(stderr, "  list            list snippets (title\\ttags), pipe-friendly")
		fmt.Fprintln(stderr, "  find <query>    fuzzy-rank snippets by query (title\\ttags)")
		fmt.Fprintln(stderr, "  add <text>      write a new snippet (flags: --title, --tags a,b)")
		fmt.Fprintln(stderr, "  edit [query]    open a snippet in $EDITOR (fuzzy picks the top match, or opens picker on a TTY)")
		fmt.Fprintln(stderr, "  stats [--limit N] show most-used snippets by frecency")
		fmt.Fprintln(stderr, "snippet dir: $QUIPKIT_DIR, config `snippet_dir`, or ~/.quipkit")
		fmt.Fprintln(stderr, "config file: $XDG_CONFIG_HOME/quipkit/config or ~/.config/quipkit/config")
		fmt.Fprintln(stderr, "auto-type: --type / --no-type (config `auto_type`), --type-delay-ms (config `type_delay_ms`)")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *versionFlag {
		fmt.Fprintf(stdout, "quipkit %s\n", Version)
		return 0
	}

	if *typeFlag && *noTypeFlag {
		fmt.Fprintln(stderr, "quipkit: --type and --no-type are mutually exclusive")
		return 2
	}
	mode := typeModeUnset
	switch {
	case *typeFlag:
		mode = typeModeForce
	case *noTypeFlag:
		mode = typeModeReject
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
		return cmdTUI(dir, cfgFile, tuiOptions{typeMode: mode, typeDelayMs: *typeDelayFlag}, stdout, stderr)
	case "list":
		return cmdList(dir, stdout, stderr)
	case "find":
		return cmdFind(dir, fs.Args()[1:], stdout, stderr)
	case "add":
		return cmdAdd(dir, fs.Args()[1:], stdout, stderr)
	case "edit":
		return cmdEdit(dir, cfgFile, fs.Args()[1:], stdout, stderr)
	case "stats":
		return cmdStats(dir, fs.Args()[1:], stdout, stderr)
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
	ranked := match.RankWithFrecency(query, snips, loadFrecency(dir, stderr).Score)
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

	target, code := resolveEditTarget(snips, *id, fs.Args(), loadFrecency(dir, stderr).Score, stderr)
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
// exit code without re-formatting. The frec argument, when non-nil,
// blends frecency into the query-driven top-match lookup so `quipkit
// edit foo` picks the *foo* you edit most often rather than merely the
// first alphabetically-tied match.
func resolveEditTarget(snips []store.Snippet, id string, args []string, frec match.FrecencyFn, stderr io.Writer) (*store.Snippet, int) {
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
		ranked := match.RankWithFrecency(query, snips, frec)
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

// tuiOptions carries the CLI-level knobs that affect how the picker's
// result is delivered (clipboard vs. keystrokes, per-keystroke delay).
type tuiOptions struct {
	typeMode    typeMode
	typeDelayMs int // -1 when the flag wasn't provided
}

// cmdTUI launches the interactive picker. On successful selection, it
// renders any {{placeholders}} in the snippet body (using autofill
// values, an optional vars.yaml in the snippet dir, and any prompts
// the picker collected) and delivers the result: either typing it via
// OS keystroke injection (when --type / config `auto_type` is on) or
// copying to the system clipboard. When neither backend is available,
// it falls back to printing the rendered body to stdout with a hint on
// stderr so the user can still pipe or paste manually. On cancel or
// empty state, it exits 0 with no output.
func cmdTUI(dir string, cfg config.File, opts tuiOptions, stdout, stderr io.Writer) int {
	snips, err := store.Load(dir)
	if err != nil {
		fmt.Fprintf(stderr, "quipkit: %v\n", err)
		return 1
	}

	// Load frecency state (best-effort) so the picker can bubble the
	// user's most-used snippets to the top and blend recency into query
	// results. A corrupt file is surfaced but never fatal.
	frec := loadFrecency(dir, stderr)

	// Order the snippet list before handing it to the picker so both the
	// initial (empty-query) view and every intermediate filter benefit
	// from frecency. The TUI's own live filter runs match.Rank over the
	// same slice, so on later keystrokes the ordering is regenerated
	// from the input order — which is now frecency-ordered.
	snips = match.RankWithFrecency("", snips, frec.Score)

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

	// Record the selection so it counts toward frecency on the next
	// run. We do this before delivery: even if the clipboard write
	// fails, the user did pick this snippet, and skipping the record
	// would understate their usage.
	frec.Record(res.Selected.ID, timeNow())
	if err := frec.Save(dir); err != nil {
		// Non-fatal — warn but keep going; the pick is still delivered.
		fmt.Fprintf(stderr, "quipkit: could not update frecency state: %v\n", err)
	}

	body := res.Rendered
	if body == "" {
		body = res.Selected.Body
	}
	title := res.Selected.Title

	if resolveAutoType(opts.typeMode, cfg) {
		return deliverByTyping(body, title, cfg, opts, res, stdout, stderr)
	}
	return deliverByClipboard(body, title, res, stdout, stderr)
}

// deliverByTyping is the auto-type branch of cmdTUI's delivery step.
// Extracted so the clipboard path stays readable and so a future
// non-TUI command (say, `quipkit type <query>`) can reuse it.
func deliverByTyping(body, title string, cfg config.File, opts tuiOptions, res tui.Result, stdout, stderr io.Writer) int {
	if !typeAvailable() {
		fmt.Fprintf(stderr, "quipkit: auto-type requested but no backend available\n")
		// Still surface the body so the user isn't stuck; hint via
		// typeit.Type's wrapped error which includes an install command.
		if _, werr := tui.WriteSelected(stdout, res); werr != nil {
			fmt.Fprintf(stderr, "quipkit: %v\n", werr)
			return 1
		}
		if err := typeText(body, typeit.Options{}); err != nil {
			fmt.Fprintf(stderr, "quipkit: %v\n", err)
		}
		return 1
	}

	delay := resolveTypeDelay(opts.typeDelayMs, cfg.TypeDelayMs)
	if err := typeText(body, typeit.Options{Delay: delay}); err != nil {
		fmt.Fprintf(stderr, "quipkit: type failed: %v\n", err)
		if _, werr := tui.WriteSelected(stdout, res); werr != nil {
			fmt.Fprintf(stderr, "quipkit: %v\n", werr)
			return 1
		}
		return 1
	}
	backend := typeBackendName()
	if backend == "" {
		fmt.Fprintf(stderr, "typed %q\n", title)
	} else {
		fmt.Fprintf(stderr, "typed %q via %s\n", title, backend)
	}
	return 0
}

// deliverByClipboard is the original clipboard delivery path, extracted
// verbatim so cmdTUI's dispatch stays a two-liner.
func deliverByClipboard(body, title string, res tui.Result, stdout, stderr io.Writer) int {
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

// resolveTypeDelay picks the effective per-keystroke delay from the CLI
// flag and the config file. The CLI flag wins when set (>=0), otherwise
// the config value is used, otherwise zero (no explicit delay).
func resolveTypeDelay(cliMs, cfgMs int) time.Duration {
	switch {
	case cliMs >= 0:
		return time.Duration(cliMs) * time.Millisecond
	case cfgMs > 0:
		return time.Duration(cfgMs) * time.Millisecond
	}
	return 0
}

// cmdStats prints the most-frequently-used snippets from the local
// frecency state. It's read-only — running `quipkit stats` never
// touches the state file, so it's safe to run from a heartbeat or a
// scripted dashboard. When no snippets have been picked yet, it exits
// 0 with an explanatory line on stderr instead of an empty table so
// the user knows the feature exists.
//
// Output format is title\tcount\tage, chosen to match the pipe-friendly
// tab-separated style used by `list` and `find`. Age is a compact
// relative string ("3h ago", "2d ago", "never") so a `stats | sort`
// is still sensible.
func cmdStats(dir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	fs.SetOutput(stderr)
	limit := fs.Int("limit", 10, "maximum rows to print (0 = all)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: quipkit stats [--limit N]")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *limit < 0 {
		fmt.Fprintln(stderr, "quipkit: --limit must be >= 0")
		return 2
	}

	snips, err := store.Load(dir)
	if err != nil {
		fmt.Fprintf(stderr, "quipkit: %v\n", err)
		return 1
	}
	// Build a title lookup so we don't have to bake title into the
	// state file (which would be stale the moment the user renames a
	// snippet). Frecency keys on ID; we resolve to title here.
	titles := make(map[string]string, len(snips))
	for _, s := range snips {
		titles[s.ID] = s.Title
	}

	frec := loadFrecency(dir, stderr)
	top := frec.TopAt(timeNow(), *limit)
	if len(top) == 0 {
		fmt.Fprintln(stderr, "quipkit: no usage recorded yet — pick a snippet with the TUI to populate stats")
		return 0
	}
	for _, row := range top {
		title, ok := titles[row.ID]
		if !ok {
			// The snippet the state file remembers no longer exists on
			// disk. Flag it so the user can prune stale entries by hand
			// (rare enough that we don't auto-clean).
			title = row.ID + " (missing)"
		}
		fmt.Fprintf(stdout, "%s\t%d\t%s\n", title, row.Count, relTime(timeNow(), row.LastUsed))
	}
	return 0
}

// relTime formats how long ago last was, relative to now. It's a
// compact humanized string designed to fit into a tab-separated stats
// line ("3h ago", "2d ago", etc.). The zero value renders as "never".
func relTime(now, last time.Time) string {
	if last.IsZero() || last.Unix() == 0 {
		return "never"
	}
	delta := now.Sub(last)
	if delta < 0 {
		delta = 0
	}
	switch {
	case delta < time.Minute:
		return "just now"
	case delta < time.Hour:
		return fmt.Sprintf("%dm ago", int(delta.Minutes()))
	case delta < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(delta.Hours()))
	case delta < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(delta.Hours()/24))
	}
	return fmt.Sprintf("%dmo ago", int(delta.Hours()/(24*30)))
}
