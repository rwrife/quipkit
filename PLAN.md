# quipkit

> A local-only TUI for your own canned replies. Hit a hotkey, fuzzy-pick the right quip, paste it. No cloud, no inbox snooping, no "AI that replies like you" reading your whole life.

## 1. Pitch

`quipkit` is a tiny terminal tool that turns a folder of plain-markdown snippets into a lightning-fast, fuzzy-searchable reply picker. You type a few letters, see your canned responses ranked by relevance, hit Enter, and the text lands on your clipboard (or types itself). It's the boring, private, you-own-the-files answer to the 2026 wave of cloud agents that promise to "reply like you" — except quipkit actually *is* you, because the words are the ones you wrote.

## 2. Trend inspiration

The June 2026 Product Hunt / HN cycle is dominated by **autonomous agents that act on your behalf**:

- **Goldfish** — "Press Option. It knows your work and replies like you." (Mac, cloud, reads your context) — https://www.producthunt.com/products/goldfish-early-access
- **Slashy** — "AI assistant that does email for you." — https://www.producthunt.com/products/slashy-3
- **Bond** — "AI to-do list that completes itself." — https://www.producthunt.com/products/bond-12
- Product Hunt Weekly 2026-06-18, "AI Agents Shift to Autonomous Execution" — https://www.shareuhack.com/en/posts/product-hunt-weekly-2026-06-18

Simultaneously there's a loud **terminal/TUI renaissance** (Slumber, Posting, yazi, lazygit) where developers want fast, keyboard-driven, local tools instead of heavyweight GUIs:

- "The Terminal TUI Renaissance" — https://www.youngju.dev/blog/devops/2026-06-12-terminal-tui-tools-renaissance.en
- awesome-tuis — https://github.com/rothgar/awesome-tuis

quipkit sits at the intersection: take the *useful* part of "reply like you" (fast, on-brand canned text) and deliver it the way TUI people actually want it — local, fast, file-owned.

## 3. Why it's different

| Thing | What it does | Why quipkit is different |
|-------|-------------|--------------------------|
| Goldfish / Slashy | Cloud LLM reads your work context and generates replies | quipkit reads **nothing**. Snippets are files you wrote. Zero network, zero data egress. |
| Espanso / aText / TextExpander | Trigger-string → expansion text expanders | quipkit is **pick-then-paste**, not memorize-a-trigger. A fuzzy TUI means you don't need to remember `;addr`; you just search "address". |
| Raycast snippets | GUI snippet manager (Mac, partly cloud) | quipkit is terminal-native, cross-platform, snippets are plain `.md` you can git-version. |
| Alfred clipboard | Clipboard history | quipkit is *curated outbound* text, not a firehose of everything you copied. |

The fresh angle: **a fuzzy picker over a snippet library, with lightweight tone/category tagging, that never phones home.** It's the anti-Goldfish.

## 4. MVP scope (v0.1)

The smallest useful thing:

- Read snippets from `~/.quipkit/` — each snippet is a markdown file with optional frontmatter (`tags`, `title`).
- Launch a TUI: a search box + a results list filtered by fuzzy match on title/tags/body.
- Arrow keys / typing to narrow; `Enter` to select.
- On select: copy the snippet body to the system clipboard and exit (print a confirmation).
- `quipkit add "<text>"` to quickly stash a new snippet from the CLI.
- `quipkit list` to dump all snippets (non-interactive, pipe-friendly).
- Ships with 5 example snippets so it's useful in 30 seconds.

## 5. Tech stack

- **Language: Go.** Single static binary, trivial cross-platform distribution (macOS/Linux/Windows), fast startup — critical for a "hotkey → instant" feel.
- **TUI: Bubble Tea + Bubbles (list/textinput) + Lip Gloss.** The de-facto, well-maintained Go TUI stack; great fuzzy-list components already exist.
- **Fuzzy match: `sahilm/fuzzy`.** Tiny, fast, no deps.
- **Clipboard: `golang.design/x/clipboard` (or `atotto/clipboard`).** Cross-platform copy without spawning `xclip`/`pbcopy` shells where avoidable.
- **Config/snippets: plain files + `adrg/frontmatter` (or hand-rolled YAML).** No DB. The filesystem *is* the database.

Boring on purpose: no server, no SQLite, no LLM, no network stack.

## 6. Architecture

```
cmd/quipkit/main.go        # CLI entry: parse subcommand (tui default, add, list, edit)
internal/store/            # load/save snippets from ~/.quipkit, parse frontmatter
internal/match/            # fuzzy ranking over title+tags+body
internal/tui/              # Bubble Tea model: search box + list + preview pane
internal/clip/             # cross-platform clipboard copy
internal/config/           # resolve snippet dir, defaults, seed examples
```

Key modules:
- **store** — owns the snippet model (`Snippet{ID, Title, Tags, Body, Path}`) and disk I/O.
- **match** — pure function: `(query, []Snippet) -> ranked []Snippet`. Easily unit-tested.
- **tui** — thin; delegates ranking to `match`, copy to `clip`.

## 7. Milestones

1. **M1 — Scaffold + hello-world.** Go module, `cmd/quipkit`, prints version + reads a hardcoded snippet dir, lists filenames. CI builds a binary. ✅ shippable.
2. **M2 — Snippet store.** Load `.md` snippets with frontmatter from `~/.quipkit`, seed 5 examples on first run, `quipkit list` prints them.
3. **M3 — Fuzzy match core.** `internal/match` with ranking over title/tags/body + unit tests. `quipkit find <q>` prints ranked results non-interactively.
4. **M4 — TUI picker.** Bubble Tea search box + results list + preview pane; arrow/type to filter; `Enter` selects (prints to stdout for now).
5. **M5 — Clipboard + add.** On select, copy body to clipboard cross-platform; `quipkit add "<text>"` and `--tags`. Confirmation messaging.
6. **M6 — Polish + release.** Config file, `quipkit edit` opens `$EDITOR`, theming via Lip Gloss, README with GIF, GoReleaser cross-platform binaries + install instructions.

## 8. Backlog / future features (v0.2+)

1. **Placeholders** — `{{name}}` / `{{date}}` tokens prompted (or auto-filled) before paste.
2. **Auto-type mode** — simulate keystrokes instead of clipboard (for apps that block paste).
3. **Tone filter** — quick toggle to show only `formal` / `casual` / `apology` tagged snippets.
4. **Recent/frecency ranking** — surface the quips you actually use most.
5. **Multi-clip stacking** — pick several snippets in one session, assemble a longer message.
6. **Global hotkey daemon** — optional background mode that pops the TUI from anywhere.
7. **Snippet sets / namespaces** — `work`, `support`, `personal` switchable libraries.
8. **Import/export** — pull from Espanso YAML, Raycast snippets, or a CSV.
9. **Git auto-sync helper** — `quipkit sync` to commit/push your snippet repo.
10. **Variables file** — shared `vars.yaml` (your name, signature, links) usable in any snippet.
11. **Preview rendering** — render markdown in the preview pane (glamour).
12. **Stats** — `quipkit stats` showing most-used quips and time saved.

## 9. Out of scope

- ❌ Any LLM, generation, or "smart rewrite" — quipkit pastes *your* words verbatim. Generation is a non-goal for v1.
- ❌ Reading your email, calendar, browser, or app context. quipkit never touches anything but its own snippet folder.
- ❌ Cloud sync service / accounts / telemetry. (Git sync is a thin local helper, not a backend.)
- ❌ A GUI app. Terminal-first by design.
- ❌ Team collaboration / sharing servers.
