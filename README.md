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

🚧 Early. M1–M5 landed — on first run, `quipkit` seeds 5 example snippets into `~/.quipkit` (override with `QUIPKIT_DIR`), running `quipkit` in a terminal opens the interactive fuzzy picker, selecting **copies the snippet body to your system clipboard** and prints a `copied "…"` confirmation to stderr. `quipkit add` writes new snippets from the CLI. Pipe-friendly `quipkit list` / `quipkit find <query>` still work non-interactively. See [`PLAN.md`](./PLAN.md) for the roadmap and [issues](https://github.com/rwrife/quipkit/issues) for milestones.

## Build & try

```bash
make build                        # produces ./quipkit
./quipkit --version               # prints version
./quipkit                         # interactive picker (TTY) → copies pick to clipboard
./quipkit list                    # seeds ~/.quipkit on first run, then lists snippets
./quipkit find addr               # ranked fuzzy search (title > tags > body)
./quipkit add "Hey, thanks!" --title "Quick thanks" --tags casual,reply
QUIPKIT_DIR=/tmp/qk ./quipkit list  # use a custom snippet dir
```

Picker keys: type to filter, ↑/↓ to move, `Enter` to select (copies body to clipboard, exits), `Esc`/`Ctrl-C` to quit. When stdout isn't a TTY (e.g. `quipkit | grep foo`), the default falls back to `list`.

If no clipboard backend is installed (bare Linux server, etc.), `quipkit` prints the snippet body to stdout and a hint to stderr suggesting `xclip` / `xsel` / `wl-clipboard`.

Adding snippets from the CLI:

```bash
quipkit add "See you tomorrow." --title "Signoff" --tags casual
echo "multi-line\nbody here" | quipkit add --title "Piped" --tags demo
```

Or without make: `go build ./cmd/quipkit` / `go run ./cmd/quipkit --version`.

## Planned usage

```bash
quipkit              # open the fuzzy TUI picker → copies selected snippet to clipboard
quipkit add "text"   # stash a new snippet (optionally --tags work,reply)
quipkit list         # print all snippets (pipe-friendly)
quipkit find <query> # non-interactive ranked search
```

Snippets live in `~/.quipkit/*.md`, optionally with frontmatter:

```markdown
---
title: Friendly decline
tags: [casual, reply]
---
Thanks so much for thinking of me! I can't make this one, but keep me in the loop.
```

## Tech

Go · Bubble Tea TUI · fuzzy match · cross-platform clipboard. One static binary. No server.

## License

MIT
