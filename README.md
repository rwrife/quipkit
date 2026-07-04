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

🚧 Early. M1 scaffold + M2 snippet store + M3 fuzzy match core + M4 TUI picker landed — on first run, `quipkit` seeds 5 example snippets into `~/.quipkit` (override with `QUIPKIT_DIR`), running `quipkit` in a terminal opens the interactive fuzzy picker, and pipe-friendly `quipkit list` / `quipkit find <query>` still work non-interactively. Selecting in the picker prints the snippet body to stdout (clipboard integration is M5). See [`PLAN.md`](./PLAN.md) for the roadmap and [issues](https://github.com/rwrife/quipkit/issues) for milestones.

## Build & try

```bash
make build                        # produces ./quipkit
./quipkit --version               # prints version
./quipkit                         # interactive picker (TTY) → prints selected body
./quipkit list                    # seeds ~/.quipkit on first run, then lists snippets
./quipkit find addr               # ranked fuzzy search (title > tags > body)
QUIPKIT_DIR=/tmp/qk ./quipkit list  # use a custom snippet dir
```

Picker keys: type to filter, ↑/↓ to move, `Enter` to select, `Esc`/`Ctrl-C` to quit. When stdout isn't a TTY (e.g. `quipkit | grep foo`), the default falls back to `list`.

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
