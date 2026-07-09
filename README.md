# quipkit 💬

**A local-only TUI for your own canned replies.** Hit a key, fuzzy-search your snippets, paste the right one. No cloud, no inbox access, no "AI that replies like you" reading your entire life — because the words are already yours.

```
$ quipkit
┌ search › addr ──────────────────────────────┐
│ ▸ Mailing address        [personal, info]    │
│   Office address         [work, info]        │
│   "Address the feedback"  [support, reply]   │
└──────────────────────────────────────────────┘
  enter: copy   ↑↓: move   esc: quit
```

## Why

2026 is full of cloud agents that promise to "reply like you" by ingesting your email, work context, and keystrokes. quipkit is the opposite bet: a tiny, fast, **offline** picker over a folder of plain-markdown snippets you wrote and own. The filesystem is the database. The network is never touched.

## Status

🚧 Early. M1–M5 landed and M6 polish is in flight — on first run, `quipkit` seeds 5 example snippets into `~/.quipkit` (override with `QUIPKIT_DIR` or a config file), running `quipkit` in a terminal opens the interactive fuzzy picker, selecting **copies the snippet body to your system clipboard** (or **types it as keystrokes** with `--type`) and prints a `copied "…"` / `typed "…"` confirmation to stderr. `quipkit add` writes new snippets from the CLI, `quipkit edit` opens a snippet in `$EDITOR`, `quipkit --set <name>` switches to a namespaced library under `sets/`, and pipe-friendly `quipkit list` / `quipkit find <query>` still work non-interactively. See [`PLAN.md`](./PLAN.md) for the roadmap and [issues](https://github.com/rwrife/quipkit/issues) for milestones.

## Install

```bash
# Latest tagged release (once v0.1.0 ships):
go install github.com/rwrife/quipkit/cmd/quipkit@latest

# Or grab a pre-built binary from the Releases page:
# https://github.com/rwrife/quipkit/releases

# Verify:
quipkit --version
```

Release archives (`quipkit_<version>_<os>_<arch>.tar.gz` / `.zip` on Windows) are produced by GoReleaser on every `v*` tag; a `checksums.txt` sits alongside them for integrity checks.

## Build & try

```bash
make build                        # produces ./quipkit
./quipkit --version               # prints version
./quipkit                         # interactive picker (TTY) → copies pick to clipboard
./quipkit --type                  # same, but type the pick as keystrokes instead
./quipkit --type --type-delay-ms 15  # add a per-keystroke delay for finicky targets
./quipkit list                    # seeds ~/.quipkit on first run, then lists snippets
./quipkit find addr               # ranked fuzzy search (title > tags > body)
./quipkit add "Hey, thanks!" --title "Quick thanks" --tags casual,reply
./quipkit edit greet              # opens the top match in $EDITOR
./quipkit edit --id my-snippet    # opens by exact snippet id (file base name)
QUIPKIT_DIR=/tmp/qk ./quipkit list  # use a custom snippet dir
```

Picker keys: type to filter, ↑/↓ to move, `Enter` to select (copies body to clipboard, exits), `Esc`/`Ctrl-C` to quit. When stdout isn't a TTY (e.g. `quipkit | grep foo`), the default falls back to `list`.

If no clipboard backend is installed (bare Linux server, etc.), `quipkit` prints the snippet body to stdout and a hint to stderr suggesting `xclip` / `xsel` / `wl-clipboard`.

Adding snippets from the CLI:

```bash
quipkit add "See you tomorrow." --title "Signoff" --tags casual
echo "multi-line\nbody here" | quipkit add --title "Piped" --tags demo
```

Editing snippets:

```bash
quipkit edit                  # TTY → open the picker, then $EDITOR on the pick
quipkit edit thanks           # non-interactive: open the top fuzzy match
quipkit edit --id signoff     # explicit: open the snippet with that file base name
```

`$VISUAL` wins over `$EDITOR`, both win over the config-file `editor` value, and `vi` is the last-resort fallback.

Or without make: `go build ./cmd/quipkit` / `go run ./cmd/quipkit --version`.

## Snippet sets

Sets are switchable snippet libraries — keep `work` macros out of your `personal` replies and vice versa. A set is just a subfolder under `<snippetDir>/sets/`:

```
~/.quipkit/
  greeting.md            # base library (used when no set is active)
  sets/
    work/*.md            # `--set work`
    support/*.md         # `--set support`
    personal/*.md        # `--set personal`
```

Manage sets:

```bash
quipkit sets                 # list sets and their snippet counts
quipkit sets create work     # create + seed a new set folder
quipkit --set work           # launch the picker against `work`
quipkit --set work add "…"   # add a snippet into `work`
QUIPKIT_SET=support quipkit  # persist the choice for a shell session
```

Set names must be `[a-zA-Z0-9_-]+` (so `--set ../oops` gets rejected instead of escaping the snippet dir). The reserved name `default` is an alias for the base library. Set the default globally with `default_set = work` in the config file; `--set` and `$QUIPKIT_SET` always override it.

## Configuration

Zero-config by default. Drop a file at `$XDG_CONFIG_HOME/quipkit/config` (or `~/.config/quipkit/config`) to override the snippet directory and/or editor:

```ini
# ~/.config/quipkit/config
snippet_dir   = ~/notes/quips
editor        = "code --wait"
auto_type     = yes   # default to keystroke injection instead of clipboard
type_delay_ms = 10    # per-keystroke pause for --type mode (0 = no delay)
default_set   = work  # snippet set to use when --set / $QUIPKIT_SET aren't set
```

Syntax: `key = value` or `key: value`, `#` starts a comment, values can be quoted, `~/` expands to your home dir. Unknown keys are ignored so newer options don't break older binaries. Booleans accept `true`/`false`, `yes`/`no`, `on`/`off`, `1`/`0`.

Precedence — first thing set wins:

- **Snippet dir:** `$QUIPKIT_DIR` → config `snippet_dir` → `~/.quipkit`
- **Snippet set:** `--set NAME` → `$QUIPKIT_SET` → config `default_set` → base library
- **Editor:** `$VISUAL` → `$EDITOR` → config `editor` → `vi`
- **Auto-type:** `--type` / `--no-type` → config `auto_type` → clipboard
- **Type delay:** `--type-delay-ms N` → config `type_delay_ms` → 0 (backend default)

## Auto-type mode

Some apps (looking at you, certain Electron chat clients) refuse to accept clipboard paste, or you might just not want to clobber the clipboard. `quipkit --type` picks a snippet the usual way, then types it into the currently focused window via OS-native keystroke injection:

- **macOS:** `osascript` (System Events → `keystroke`). Requires that your terminal has **Accessibility** permission — grant it in *System Settings → Privacy & Security → Accessibility*, then re-launch the terminal.
- **Linux:** first of `wtype` (Wayland), `ydotool`, or `xdotool` (X11) found on `$PATH`. Install one:
  ```bash
  sudo apt install xdotool          # X11
  sudo apt install wtype            # Wayland (Sway, Hyprland, GNOME/KDE Wayland)
  # ydotool needs uinput permission; see its README for setup.
  ```
- **Windows:** `powershell.exe` / `pwsh` (`System.Windows.Forms.SendKeys`). No install required.

**Timing:** if characters get dropped, add a small pause with `--type-delay-ms 10` (or set `type_delay_ms = 10` in config). Values below 1 ms are clamped to 1 ms because none of the backends honor sub-millisecond delays.

**Focus:** quipkit does not try to be clever about focus. Whatever window has keyboard focus when the picker exits gets the keystrokes — so pick your snippet, alt-tab (or click) to the target, then confirm. On most desktops the terminal will lose focus automatically when the picker closes.

**No backend? No problem.** If nothing suitable is on `$PATH`, `--type` prints the snippet to stdout and a hint on stderr explaining what to install — the pick isn't lost.

## Planned usage

```bash
quipkit              # open the fuzzy TUI picker → copies selected snippet to clipboard
quipkit add "text"   # stash a new snippet (optionally --tags work,reply)
quipkit edit [q]     # open a snippet in $EDITOR (fuzzy match, or picker on a TTY)
quipkit list         # print all snippets (pipe-friendly)
quipkit find <query> # non-interactive ranked search
quipkit stats        # show most-used snippets (frecency-ranked)
```

Snippets live in `~/.quipkit/*.md`, optionally with frontmatter:

```markdown
---
title: Friendly decline
tags: [casual, reply]
---
Thanks so much for thinking of me! I can't make this one, but keep me in the loop.
```

## Placeholders

Snippet bodies can contain `{{token}}` (or `{{token:default}}`) markers. When you pick a snippet, quipkit fills them in before copying:

```markdown
---
title: Personalized intro
---
Hi {{name}},

Thanks for reaching out on {{date}}. Happy to chat about {{topic:the project}}.

Cheers,
{{signature:me}}
```

- **Auto-filled tokens** (no prompt): `date`, `time`, `datetime`, `year`, `month`, `day`, `now`, `user`.
- **Inline defaults** (`{{topic:the project}}`) are shown as ghost text — Tab / Enter without typing accepts them.
- **Unknown tokens** get a one-input-per-token prompt after you pick, with a live preview of the rendered snippet. `Esc` bails back to the picker.
- **Literal `{{...}}`** — escape with a leading backslash: `\{{example}}` renders as `{{example}}`.

### Shared vars

Drop a `vars.yaml` (or `vars.yml`) in your snippet dir to pre-fill any token across every snippet:

```yaml
# ~/.quipkit/vars.yaml
name:      Ryan
signature: |
  -- Ryan (sent from quipkit)
team:      platform
```

Values in `vars.yaml` don't override anything you set at prompt time — they're just baseline defaults so common tokens (your name, signature, team) don't get asked about every time. Syntax matches the config file: `key: value` / `key = value`, `#` for comments, values may be quoted.

## Frecency ranking

quipkit keeps a tiny local record of which snippets you actually pick so the ones you use most — recently — float to the top. It's the classic **freq × recency** blend: usage count multiplied by an exponential decay (2-week half-life by default) on the last-used timestamp.

- **Empty query in the TUI**: snippets are ordered by frecency, most-used first. Never-picked snippets keep their alphabetical order and appear below the ranked ones — so on day one the library still looks predictable.
- **Non-empty query**: frecency *nudges* the fuzzy ranking. It'll break ties between similarly-scored matches and lift familiar snippets above random fuzzy hits, but it can never leapfrog a snippet whose title contains the query as an exact whole word.
- **`quipkit stats`**: prints the top snippets by frecency, one per line as `title\tcount\tage` (e.g. `Friendly decline\t7\t3h ago`). Read-only — safe to run from a heartbeat or a dashboard. Pass `--limit N` to change how many rows print (0 = all).

Usage state lives in `<snippet-dir>/.state.json`. It's plain JSON, human-editable, and safe to delete: the next `quipkit` invocation just starts fresh. Add it to `.gitignore` if you version-control your snippet folder and want per-machine rather than shared frecency.

## Tech

Go · Bubble Tea TUI · fuzzy match · cross-platform clipboard. One static binary. No server.

## License

MIT
