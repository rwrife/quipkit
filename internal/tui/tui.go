// Package tui implements the quipkit interactive picker.
//
// It's a small Bubble Tea program with three visible parts:
//
//	┌ search › <query>                       ┐
//	│ ▸ Result title              [tags]     │
//	│   Another result            [tags]     │
//	├─────────────────────────────────────────┤
//	│ preview of highlighted snippet body     │
//	└─────────────────────────────────────────┘
//	  enter: select   ↑↓: move   esc: quit
//
// When the picked snippet contains {{placeholders}} that aren't
// autofilled, the picker transitions to a second "prompt" phase with
// one textinput per unresolved token. Tab/Enter advances to the next
// prompt; Enter on the last prompt confirms and quits with the
// rendered snippet ready for the clipboard. Esc backs out of the prompt
// phase and returns to the picker.
//
// The model is deliberately thin: filtering is delegated to
// [github.com/rwrife/quipkit/internal/match], substitution to
// [github.com/rwrife/quipkit/internal/placeholders], and I/O of the
// selected snippet is left to the caller.
package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rwrife/quipkit/internal/match"
	"github.com/rwrife/quipkit/internal/placeholders"
	"github.com/rwrife/quipkit/internal/store"
)

// Result is what [Run] returns after the user picks (or doesn't).
type Result struct {
	// Selected is the snippet the user picked, or nil if they cancelled
	// (Esc / Ctrl-C) or there were no snippets to pick from.
	Selected *store.Snippet
	// Rendered is the snippet body with {{placeholders}} substituted
	// using the autofill/vars/prompt values. When there are no
	// placeholders (or the snippet doesn't need any prompts), Rendered
	// equals Selected.Body. When the caller cancels, Rendered is empty.
	Rendered string
}

// Options controls picker behavior. The zero value is fine; use
// [Options.Values] to plumb autofill + vars.yaml into the placeholder
// prompts.
type Options struct {
	// Values is the pre-populated substitution map. May be nil, in
	// which case the picker builds an empty one and only inline
	// defaults / built-in autofills apply. Callers typically build
	// this with placeholders.NewValues(), then call Autofill() and
	// LoadVars() before handing it in.
	Values *placeholders.Values
}

// Run launches the interactive picker over the given snippets with
// default options. It's kept as-is so older callers don't have to
// change.
//
// It blocks until the user selects, cancels, or the underlying Bubble
// Tea program exits with an error.
func Run(snippets []store.Snippet) (Result, error) {
	return RunWithOptions(snippets, Options{})
}

// RunWithOptions is like [Run] but accepts an [Options] struct so
// callers can plumb autofill/vars into the placeholder prompts.
func RunWithOptions(snippets []store.Snippet, opts Options) (Result, error) {
	m := NewModelWithOptions(snippets, opts)
	prog := tea.NewProgram(m)
	final, err := prog.Run()
	if err != nil {
		return Result{}, err
	}
	fm, ok := final.(Model)
	if !ok {
		return Result{}, fmt.Errorf("tui: unexpected final model type %T", final)
	}
	if fm.picked != nil && fm.confirmed {
		return Result{Selected: fm.picked, Rendered: fm.renderedBody()}, nil
	}
	return Result{}, nil
}

// phase enumerates the picker's high-level states.
type phase int

const (
	phasePick   phase = iota // typing / choosing a snippet
	phasePrompt              // filling in {{placeholders}} for the picked snippet
)

// Model is the Bubble Tea model powering the picker.
//
// It is intentionally exported so tests can drive it through
// [Model.Update] directly without spinning up a real terminal.
type Model struct {
	// all is the immutable full snippet list (in whatever order the
	// caller provided).
	all []store.Snippet
	// filtered is `all` after applying the current query. Its order is
	// what the user sees in the results list.
	filtered []store.Snippet
	// cursor is the index into filtered of the currently highlighted row.
	// Always 0 when filtered is empty.
	cursor int
	// input is the fuzzy-query textinput bubble.
	input textinput.Model
	// width/height come from tea.WindowSizeMsg. Zero until the first
	// resize event; View copes with that by using compact fallbacks.
	width, height int
	// picked, when non-nil, means the user hit Enter on a valid row.
	// It transitions the model to the prompt phase (if placeholders
	// need values) or straight to confirmed = true.
	picked *store.Snippet
	// confirmed reports whether the picker should exit with a valid
	// result. In the placeholder-prompt phase it becomes true only
	// after the user finishes (or skips) every prompt.
	confirmed bool
	// quitting is set once we've dispatched tea.Quit, so View can render
	// a final line instead of the full picker if we ever peek at it.
	quitting bool

	// Placeholder-phase state.
	phase phase
	// values is the running substitution map (autofill + vars + prompts).
	values *placeholders.Values
	// prompts are one textinput per unresolved token, in extract order.
	prompts []textinput.Model
	// tokens is the ordered set of placeholders the picked snippet
	// exposes; only the entries without AutoFilled populate a prompt.
	tokens []placeholders.Token
	// promptIx is the index into prompts of the currently focused input.
	promptIx int
}

// NewModel builds a Model for the given snippets with default options.
func NewModel(snippets []store.Snippet) Model {
	return NewModelWithOptions(snippets, Options{})
}

// NewModelWithOptions builds a Model for the given snippets using opts.
// The initial filtered view is the input order.
func NewModelWithOptions(snippets []store.Snippet, opts Options) Model {
	ti := textinput.New()
	ti.Placeholder = "type to filter…"
	ti.Prompt = "› "
	ti.CharLimit = 200
	ti.Focus()

	vals := opts.Values
	if vals == nil {
		vals = placeholders.NewValues()
	}
	return Model{
		all:      snippets,
		filtered: snippets,
		input:    ti,
		values:   vals,
	}
}

// Init satisfies tea.Model. We start with the text input's blink.
func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

// Update satisfies tea.Model. Key handling is kept in one place so the
// tests can drive it via [tea.KeyMsg] values without any renderer.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if sz, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = sz.Width
		m.height = sz.Height
		return m, nil
	}
	if m.phase == phasePrompt {
		return m.updatePrompt(msg)
	}
	return m.updatePick(msg)
}

// updatePick handles input in the fuzzy-picker phase. On Enter, if the
// picked snippet exposes placeholders that still need values, the model
// transitions to phasePrompt instead of quitting.
func (m Model) updatePick(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitting = true
			return m, tea.Quit

		case tea.KeyEnter:
			if len(m.filtered) == 0 {
				// Nothing to pick; ignore Enter rather than quitting so
				// the user can keep refining the query.
				return m, nil
			}
			s := m.filtered[m.cursor]
			m.picked = &s

			// Extract placeholders against the current values so
			// autofilled tokens don't produce prompts.
			m.tokens = placeholders.Extract(s.Body, m.values)
			missing := placeholders.MissingNames(m.tokens)
			if len(missing) == 0 {
				m.confirmed = true
				m.quitting = true
				return m, tea.Quit
			}
			m.enterPromptPhase(missing)
			return m, textinput.Blink

		case tea.KeyUp, tea.KeyCtrlK:
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil

		case tea.KeyDown, tea.KeyCtrlJ:
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			return m, nil
		}
	}

	// Anything not handled above (printable keys, backspace, etc.) goes
	// to the text input; then we re-run the filter.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.refilter()
	return m, cmd
}

// enterPromptPhase builds one textinput per missing token and focuses
// the first one. missing must be in the same order as
// placeholders.MissingNames(m.tokens).
func (m *Model) enterPromptPhase(missing []string) {
	m.phase = phasePrompt
	m.promptIx = 0
	m.prompts = make([]textinput.Model, 0, len(missing))
	// Index tokens by name for quick lookup of defaults.
	byName := map[string]placeholders.Token{}
	for _, t := range m.tokens {
		byName[strings.ToLower(t.Name)] = t
	}
	for _, name := range missing {
		ti := textinput.New()
		ti.Prompt = "› "
		ti.CharLimit = 200
		ti.Placeholder = name
		if t, ok := byName[strings.ToLower(name)]; ok && t.Default != "" {
			// Show the default as ghost text; an empty submission (Tab /
			// Enter without typing) accepts it, so the user doesn't have
			// to hit Backspace-x-N before overriding.
			ti.Placeholder = t.Default
		}
		m.prompts = append(m.prompts, ti)
	}
	if len(m.prompts) > 0 {
		m.prompts[0].Focus()
	}
}

// updatePrompt handles input in the placeholder-fill phase.
//
// Key bindings:
//   - Esc:        cancel back to the picker (values collected so far are dropped)
//   - Ctrl-C:     quit the whole picker without confirming
//   - Enter/Tab:  save current value, advance to next prompt; Enter on
//     the last prompt confirms
//   - Shift-Tab:  step back to the previous prompt
func (m Model) updatePrompt(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.Type {
		case tea.KeyCtrlC:
			m.quitting = true
			return m, tea.Quit

		case tea.KeyEsc:
			// Bail back to the picker without confirming; keep the
			// query/results state intact so the user can pick again.
			m.exitPromptPhase()
			return m, textinput.Blink

		case tea.KeyEnter, tea.KeyTab:
			m.capturePrompt()
			if m.promptIx >= len(m.prompts)-1 {
				m.confirmed = true
				m.quitting = true
				return m, tea.Quit
			}
			m.focusPrompt(m.promptIx + 1)
			return m, textinput.Blink

		case tea.KeyShiftTab:
			m.capturePrompt()
			if m.promptIx > 0 {
				m.focusPrompt(m.promptIx - 1)
			}
			return m, textinput.Blink
		}
	}

	// Route the message to the focused input.
	if len(m.prompts) == 0 {
		return m, nil
	}
	var cmd tea.Cmd
	m.prompts[m.promptIx], cmd = m.prompts[m.promptIx].Update(msg)
	return m, cmd
}

// capturePrompt copies the current prompt's value into m.values so
// subsequent prompts (and the final Render) see it. An empty value
// falls back to the inline default (if any) so "tab past" accepts it.
func (m *Model) capturePrompt() {
	if m.promptIx < 0 || m.promptIx >= len(m.prompts) {
		return
	}
	name := placeholderNameFor(m.tokens, m.promptIx)
	if name == "" {
		return
	}
	if m.values == nil {
		m.values = placeholders.NewValues()
	}
	val := m.prompts[m.promptIx].Value()
	if val == "" {
		val = defaultForToken(m.tokens, name)
	}
	m.values.Set(name, val)
}

// defaultForToken returns the inline default for name (empty when there
// isn't one). Matching is case-insensitive so `{{Name:pal}}` and
// `{{name}}` share defaults.
func defaultForToken(tokens []placeholders.Token, name string) string {
	key := strings.ToLower(name)
	for _, t := range tokens {
		if strings.ToLower(t.Name) == key {
			return t.Default
		}
	}
	return ""
}

// focusPrompt switches focus to the prompt at ix, blurring the current one.
func (m *Model) focusPrompt(ix int) {
	if ix < 0 || ix >= len(m.prompts) {
		return
	}
	for i := range m.prompts {
		if i == ix {
			m.prompts[i].Focus()
		} else {
			m.prompts[i].Blur()
		}
	}
	m.promptIx = ix
}

// exitPromptPhase drops the collected prompts and returns to the picker.
func (m *Model) exitPromptPhase() {
	m.phase = phasePick
	m.picked = nil
	m.prompts = nil
	m.tokens = nil
	m.promptIx = 0
	m.input.Focus()
}

// placeholderNameFor returns the name of the ix-th unresolved token in
// tokens (i.e. the ix-th name in placeholders.MissingNames(tokens)).
func placeholderNameFor(tokens []placeholders.Token, ix int) string {
	seen := 0
	for _, t := range tokens {
		if t.AutoFilled {
			continue
		}
		if seen == ix {
			return t.Name
		}
		seen++
	}
	return ""
}

// renderedBody returns the picked snippet body with every {{placeholder}}
// substituted using m.values. Safe to call once picked != nil.
func (m Model) renderedBody() string {
	if m.picked == nil {
		return ""
	}
	return placeholders.Render(m.picked.Body, m.values)
}

// refilter recomputes filtered from the current query and clamps the
// cursor to a valid row.
func (m *Model) refilter() {
	q := strings.TrimSpace(m.input.Value())
	if q == "" {
		m.filtered = m.all
	} else {
		m.filtered = match.Rank(q, m.all)
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// Styles. Kept private and deliberately understated — this is a picker,
// not a design showcase. Colors are ANSI so they degrade well.
var (
	styleBorder    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleCursor    = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	styleTitle     = lipgloss.NewStyle().Bold(true)
	styleTitleSel  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	styleTags      = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styleDim       = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	stylePreview   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styleHelp      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleEmpty     = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true)
	stylePromptLbl = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
)

// View satisfies tea.Model. It renders the full picker frame.
func (m Model) View() string {
	if m.quitting {
		// Once we've asked to quit, Bubble Tea may still call View a few
		// times; return an empty string so nothing garbles the terminal
		// after the picker exits.
		return ""
	}
	if m.phase == phasePrompt {
		return m.viewPrompt()
	}

	var b strings.Builder

	// Search line: `search › <query>`
	b.WriteString(styleDim.Render("search "))
	b.WriteString(m.input.View())
	b.WriteString("\n")
	b.WriteString(styleBorder.Render(strings.Repeat("─", clamp(m.width, 20, 80))))
	b.WriteString("\n")

	// Results block.
	if len(m.all) == 0 {
		b.WriteString(styleEmpty.Render("  no snippets yet — try `quipkit add` to create one"))
		b.WriteString("\n")
	} else if len(m.filtered) == 0 {
		b.WriteString(styleEmpty.Render(fmt.Sprintf("  no matches for %q", m.input.Value())))
		b.WriteString("\n")
	} else {
		maxRows := 8
		if m.height > 0 {
			// Reserve room for the search line + separator + preview
			// (roughly 6 lines) + help line. Anything left over is
			// available for results, capped at 15 for sanity.
			avail := m.height - 10
			if avail > 15 {
				avail = 15
			}
			if avail > 0 {
				maxRows = avail
			}
		}
		rows := m.filtered
		if len(rows) > maxRows {
			rows = rows[:maxRows]
		}
		for i, s := range rows {
			b.WriteString(renderRow(s, i == m.cursor))
			b.WriteString("\n")
		}
		if len(m.filtered) > maxRows {
			more := fmt.Sprintf("  … %d more", len(m.filtered)-maxRows)
			b.WriteString(styleDim.Render(more))
			b.WriteString("\n")
		}
	}

	// Preview block.
	b.WriteString(styleBorder.Render(strings.Repeat("─", clamp(m.width, 20, 80))))
	b.WriteString("\n")
	b.WriteString(previewBlock(m.currentSnippet()))
	b.WriteString("\n")

	// Help line.
	b.WriteString(styleHelp.Render("  enter: select   ↑↓: move   esc: quit"))
	return b.String()
}

// viewPrompt renders the placeholder-fill phase.
func (m Model) viewPrompt() string {
	var b strings.Builder
	title := "(snippet)"
	if m.picked != nil {
		title = m.picked.Title
	}
	b.WriteString(stylePromptLbl.Render("fill placeholders for "))
	b.WriteString(styleTitle.Render(title))
	b.WriteString("\n")
	b.WriteString(styleBorder.Render(strings.Repeat("─", clamp(m.width, 20, 80))))
	b.WriteString("\n")

	// Missing prompts (with focus indicator).
	names := placeholders.MissingNames(m.tokens)
	for i, name := range names {
		cursor := "  "
		label := name
		if i == m.promptIx {
			cursor = styleCursor.Render("▸ ")
			label = stylePromptLbl.Render(name)
		}
		b.WriteString(cursor)
		b.WriteString(label)
		b.WriteString("  ")
		b.WriteString(m.prompts[i].View())
		b.WriteString("\n")
	}

	// Live preview of the rendered snippet with the current values.
	b.WriteString(styleBorder.Render(strings.Repeat("─", clamp(m.width, 20, 80))))
	b.WriteString("\n")
	b.WriteString(livePreview(m.picked, m.previewValues()))
	b.WriteString("\n")

	b.WriteString(styleHelp.Render("  enter/tab: next   shift-tab: back   esc: back to picker"))
	return b.String()
}

// previewValues returns a Values snapshot that layers the currently
// typed prompt inputs on top of m.values, without mutating m.values.
// This lets the live preview reflect what the user is typing right now.
func (m Model) previewValues() *placeholders.Values {
	snap := placeholders.NewValues()
	// Base: copy known keys the render path uses (autofill + vars + captured prompts).
	for _, k := range append(placeholders.AutoKeys(), tokenNames(m.tokens)...) {
		if v, ok := m.values.Get(k); ok {
			snap.Set(k, v)
		}
	}
	// Overlay: any prompt buffer that hasn't been captured yet.
	names := placeholders.MissingNames(m.tokens)
	for i, name := range names {
		if i >= len(m.prompts) {
			break
		}
		val := m.prompts[i].Value()
		if val == "" {
			// No typed input yet — fall back to the token's inline default
			// so the live preview shows what "tab past" would produce.
			val = defaultForToken(m.tokens, name)
			if val == "" {
				continue
			}
		}
		snap.Set(name, val)
	}
	return snap
}

// tokenNames extracts the Name field of every token in the input.
func tokenNames(tokens []placeholders.Token) []string {
	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, t.Name)
	}
	return out
}

// livePreview renders up to 6 lines of the picked snippet with tokens
// substituted using vals. It's OK for vals to leave some tokens
// unresolved — they'll appear in their `{{name}}` form.
func livePreview(s *store.Snippet, vals *placeholders.Values) string {
	if s == nil {
		return styleEmpty.Render("  (no preview)")
	}
	rendered := placeholders.Render(s.Body, vals)
	body := strings.TrimSpace(rendered)
	if body == "" {
		return styleEmpty.Render("  (empty snippet)")
	}
	lines := strings.Split(body, "\n")
	const maxPreview = 6
	trimmed := false
	if len(lines) > maxPreview {
		lines = lines[:maxPreview]
		trimmed = true
	}
	var b strings.Builder
	for _, ln := range lines {
		b.WriteString("  ")
		b.WriteString(stylePreview.Render(ln))
		b.WriteString("\n")
	}
	if trimmed {
		b.WriteString(styleDim.Render("  …"))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderRow returns a single result line.
func renderRow(s store.Snippet, selected bool) string {
	cursor := "  "
	title := styleTitle.Render(s.Title)
	if selected {
		cursor = styleCursor.Render("▸ ")
		title = styleTitleSel.Render(s.Title)
	}
	tags := ""
	if len(s.Tags) > 0 {
		tags = styleTags.Render("  [" + strings.Join(s.Tags, ", ") + "]")
	}
	return cursor + title + tags
}

// previewBlock renders up to 5 body lines of the highlighted snippet.
func previewBlock(s *store.Snippet) string {
	if s == nil {
		return styleEmpty.Render("  (no preview)")
	}
	body := strings.TrimSpace(s.Body)
	if body == "" {
		return styleEmpty.Render("  (empty snippet)")
	}
	lines := strings.Split(body, "\n")
	const maxPreview = 5
	trimmed := false
	if len(lines) > maxPreview {
		lines = lines[:maxPreview]
		trimmed = true
	}
	var b strings.Builder
	for _, ln := range lines {
		b.WriteString("  ")
		b.WriteString(stylePreview.Render(ln))
		b.WriteString("\n")
	}
	if trimmed {
		b.WriteString(styleDim.Render("  …"))
		b.WriteString("\n")
	}
	// Trim the trailing newline so callers control spacing.
	return strings.TrimRight(b.String(), "\n")
}

// currentSnippet returns a pointer to the highlighted snippet, or nil if
// there's nothing to highlight.
func (m Model) currentSnippet() *store.Snippet {
	if len(m.filtered) == 0 {
		return nil
	}
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return nil
	}
	s := m.filtered[m.cursor]
	return &s
}

// clamp keeps x within [lo, hi]. Used to size the horizontal rules
// sensibly when the terminal width is unknown or absurd.
func clamp(x, lo, hi int) int {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// WriteSelected writes the rendered body of the picker Result to w. It
// exists so the CLI (and tests) share one code path: pick → print body
// → newline if the body didn't already end in one. If r.Selected is
// nil, nothing is written and the return is (0, nil).
//
// Historical note: earlier versions of this helper wrote r.Selected.Body
// verbatim. It now prefers r.Rendered so {{placeholders}} come out
// substituted; when Rendered is empty (older callers, or a pre-render
// failure), it falls back to r.Selected.Body so behavior degrades
// gracefully.
func WriteSelected(w io.Writer, r Result) (int, error) {
	if r.Selected == nil {
		return 0, nil
	}
	body := r.Rendered
	if body == "" {
		body = r.Selected.Body
	}
	if body == "" || !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return io.WriteString(w, body)
}
