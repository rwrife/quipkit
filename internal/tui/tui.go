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
// The model is deliberately thin: filtering is delegated to
// [github.com/rwrife/quipkit/internal/match], I/O of the selected snippet
// is left to the caller (M4 prints the body to stdout; clipboard lands in M5).
//
// This file exposes both the interactive [Run] entrypoint (used by the
// CLI) and the [Model] type itself so it can be exercised directly in
// unit tests without a real terminal.
package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rwrife/quipkit/internal/match"
	"github.com/rwrife/quipkit/internal/store"
)

// Result is what [Run] returns after the user picks (or doesn't).
type Result struct {
	// Selected is the snippet the user picked, or nil if they cancelled
	// (Esc / Ctrl-C) or there were no snippets to pick from.
	Selected *store.Snippet
}

// Run launches the interactive picker over the given snippets.
//
// It blocks until the user selects, cancels, or the underlying Bubble Tea
// program exits with an error.
//
// snippets may be nil / empty; the picker still runs and shows an empty
// state so the caller doesn't need to special-case that (Update handles
// zero-snippet Enter as a no-op).
func Run(snippets []store.Snippet) (Result, error) {
	m := NewModel(snippets)
	prog := tea.NewProgram(m)
	final, err := prog.Run()
	if err != nil {
		return Result{}, err
	}
	fm, ok := final.(Model)
	if !ok {
		return Result{}, fmt.Errorf("tui: unexpected final model type %T", final)
	}
	if fm.picked != nil {
		return Result{Selected: fm.picked}, nil
	}
	return Result{}, nil
}

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
	// picked, when non-nil, means the user hit Enter on a valid row and
	// the program should quit with that snippet as the result.
	picked *store.Snippet
	// quitting is set once we've dispatched tea.Quit, so View can render
	// a final line instead of the full picker if we ever peek at it.
	quitting bool
}

// NewModel builds a Model for the given snippets. The initial filtered
// view is the input order.
func NewModel(snippets []store.Snippet) Model {
	ti := textinput.New()
	ti.Placeholder = "type to filter…"
	ti.Prompt = "› "
	ti.CharLimit = 200
	ti.Focus()

	return Model{
		all:      snippets,
		filtered: snippets,
		input:    ti,
	}
}

// Init satisfies tea.Model. We start with the text input's blink.
func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

// Update satisfies tea.Model. Key handling is kept in one place so the
// tests can drive it via [tea.KeyMsg] values without any renderer.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
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
			m.quitting = true
			return m, tea.Quit

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
	styleBorder   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleCursor   = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	styleTitle    = lipgloss.NewStyle().Bold(true)
	styleTitleSel = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	styleTags     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	styleDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	stylePreview  = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styleHelp     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleEmpty    = lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Italic(true)
)

// View satisfies tea.Model. It renders the full picker frame.
func (m Model) View() string {
	if m.quitting {
		// Once we've asked to quit, Bubble Tea may still call View a few
		// times; return an empty string so nothing garbles the terminal
		// after the picker exits.
		return ""
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

// WriteSelected writes the body of the picker Result to w. It exists so
// the CLI (and tests) share one code path: pick → print body → newline
// if the body didn't already end in one. If r.Selected is nil, nothing
// is written and the return is (0, nil).
func WriteSelected(w io.Writer, r Result) (int, error) {
	if r.Selected == nil {
		return 0, nil
	}
	body := r.Selected.Body
	if body == "" || !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return io.WriteString(w, body)
}
