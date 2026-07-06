package tui

import (
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rwrife/quipkit/internal/store"
)

func sampleSnippets() []store.Snippet {
	return []store.Snippet{
		{ID: "addr-home", Title: "Home address", Tags: []string{"personal", "info"}, Body: "123 Main St\nAnytown, USA"},
		{ID: "addr-work", Title: "Work address", Tags: []string{"work", "info"}, Body: "456 Corp Blvd"},
		{ID: "greet", Title: "Friendly hello", Tags: []string{"casual"}, Body: "Hey there!"},
	}
}

// typeString feeds each rune to the model as a KeyRunes message, the way
// Bubble Tea's textinput actually receives typed characters.
func typeString(m Model, s string) Model {
	for _, r := range s {
		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = m2.(Model)
	}
	return m
}

func key(m Model, k tea.KeyType) Model {
	m2, _ := m.Update(tea.KeyMsg{Type: k})
	return m2.(Model)
}

func TestNewModel_DefaultsToAllSnippetsInOrder(t *testing.T) {
	snips := sampleSnippets()
	m := NewModel(snips)
	if got, want := len(m.filtered), len(snips); got != want {
		t.Fatalf("filtered length = %d, want %d", got, want)
	}
	for i, s := range m.filtered {
		if s.ID != snips[i].ID {
			t.Errorf("filtered[%d].ID = %q, want %q", i, s.ID, snips[i].ID)
		}
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}
}

func TestUpdate_TypingFiltersResults(t *testing.T) {
	m := NewModel(sampleSnippets())
	m = typeString(m, "addr")
	if len(m.filtered) == 0 {
		t.Fatal("expected at least one match for 'addr'")
	}
	for _, s := range m.filtered {
		if !strings.Contains(strings.ToLower(s.Title), "addr") &&
			!strings.Contains(strings.ToLower(strings.Join(s.Tags, " ")), "addr") &&
			!strings.Contains(strings.ToLower(s.Body), "addr") {
			t.Errorf("filtered snippet %q doesn't match query 'addr'", s.Title)
		}
	}
}

func TestUpdate_NoMatchesShowsEmptyFilter(t *testing.T) {
	m := NewModel(sampleSnippets())
	m = typeString(m, "zzznotathing")
	if got := len(m.filtered); got != 0 {
		t.Errorf("filtered length = %d, want 0", got)
	}
	// Enter with no matches must not panic and must not select anything.
	m = key(m, tea.KeyEnter)
	if m.picked != nil {
		t.Errorf("picked = %+v, want nil (Enter on empty filter should be no-op)", m.picked)
	}
	if m.quitting {
		t.Errorf("quitting = true, want false (Enter on empty filter should not quit)")
	}
	// View shouldn't panic and should mention "no matches".
	out := m.View()
	if !strings.Contains(out, "no matches") {
		t.Errorf("View() missing 'no matches' hint; got:\n%s", out)
	}
}

func TestUpdate_ArrowKeysMoveCursorWithinBounds(t *testing.T) {
	m := NewModel(sampleSnippets())
	// Down moves to 1, another Down to 2, one more should clamp.
	m = key(m, tea.KeyDown)
	if m.cursor != 1 {
		t.Fatalf("cursor after 1x down = %d, want 1", m.cursor)
	}
	m = key(m, tea.KeyDown)
	if m.cursor != 2 {
		t.Fatalf("cursor after 2x down = %d, want 2", m.cursor)
	}
	m = key(m, tea.KeyDown)
	if m.cursor != 2 {
		t.Errorf("cursor after 3x down = %d, want clamped 2", m.cursor)
	}
	// Up walks back and clamps at 0.
	m = key(m, tea.KeyUp)
	m = key(m, tea.KeyUp)
	m = key(m, tea.KeyUp)
	if m.cursor != 0 {
		t.Errorf("cursor after excess up = %d, want clamped 0", m.cursor)
	}
}

func TestUpdate_CursorClampsWhenFilterShrinks(t *testing.T) {
	m := NewModel(sampleSnippets())
	m = key(m, tea.KeyDown) // cursor -> 1
	m = key(m, tea.KeyDown) // cursor -> 2
	if m.cursor != 2 {
		t.Fatalf("pre-filter cursor = %d, want 2", m.cursor)
	}
	// Type something that only matches one snippet.
	m = typeString(m, "hello")
	if len(m.filtered) == 0 {
		t.Fatal("expected at least one match for 'hello'")
	}
	if m.cursor >= len(m.filtered) {
		t.Errorf("cursor = %d, want < %d (should clamp when filter shrinks)", m.cursor, len(m.filtered))
	}
}

func TestUpdate_EnterSelectsHighlightedRow(t *testing.T) {
	m := NewModel(sampleSnippets())
	m = key(m, tea.KeyDown) // highlight row 1 = addr-work
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if m.picked == nil {
		t.Fatal("picked = nil, want a snippet")
	}
	if got, want := m.picked.ID, "addr-work"; got != want {
		t.Errorf("picked.ID = %q, want %q", got, want)
	}
	if !m.quitting {
		t.Errorf("quitting = false, want true after Enter")
	}
	if cmd == nil {
		t.Errorf("Enter should return tea.Quit (non-nil cmd), got nil")
	}
}

func TestUpdate_EscQuitsWithoutSelection(t *testing.T) {
	m := NewModel(sampleSnippets())
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(Model)
	if m.picked != nil {
		t.Errorf("picked = %+v, want nil after Esc", m.picked)
	}
	if !m.quitting {
		t.Errorf("quitting = false, want true after Esc")
	}
	if cmd == nil {
		t.Errorf("Esc should dispatch tea.Quit (non-nil cmd)")
	}
}

func TestUpdate_CtrlCQuitsWithoutSelection(t *testing.T) {
	m := NewModel(sampleSnippets())
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = m2.(Model)
	if m.picked != nil {
		t.Errorf("picked = %+v, want nil after Ctrl-C", m.picked)
	}
	if !m.quitting {
		t.Errorf("quitting = false, want true after Ctrl-C")
	}
}

func TestUpdate_EmptySnippetListHandledGracefully(t *testing.T) {
	m := NewModel(nil)
	// Rendering must not panic.
	out := m.View()
	if !strings.Contains(out, "no snippets") {
		t.Errorf("View() missing 'no snippets' empty-state; got:\n%s", out)
	}
	// Enter is a safe no-op.
	m = key(m, tea.KeyEnter)
	if m.picked != nil {
		t.Errorf("picked = %+v, want nil on empty list", m.picked)
	}
	if m.quitting {
		t.Errorf("quitting = true, want false on empty-list Enter")
	}
}

func TestView_ShowsPreviewOfHighlightedSnippet(t *testing.T) {
	m := NewModel(sampleSnippets())
	// Feed a size event so the layout picks a real width.
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = m2.(Model)
	// Cursor starts on addr-home, whose body contains "Main St".
	out := m.View()
	if !strings.Contains(out, "Main St") {
		t.Errorf("View() should preview highlighted snippet body; got:\n%s", out)
	}
	// Move down and re-render; preview should update to work address.
	m = key(m, tea.KeyDown)
	out = m.View()
	if !strings.Contains(out, "Corp Blvd") {
		t.Errorf("View() should update preview after ↓; got:\n%s", out)
	}
}

func TestView_RendersSearchInputAndHelpLine(t *testing.T) {
	m := NewModel(sampleSnippets())
	out := m.View()
	if !strings.Contains(out, "search") {
		t.Errorf("View() missing 'search' label; got:\n%s", out)
	}
	if !strings.Contains(out, "enter: select") {
		t.Errorf("View() missing help line; got:\n%s", out)
	}
}

func TestWriteSelected_AppendsNewlineWhenMissing(t *testing.T) {
	s := &store.Snippet{Body: "hello"}
	var buf bytes.Buffer
	n, err := WriteSelected(&buf, Result{Selected: s})
	if err != nil {
		t.Fatalf("WriteSelected err = %v", err)
	}
	if got, want := buf.String(), "hello\n"; got != want {
		t.Errorf("WriteSelected wrote %q, want %q", got, want)
	}
	if n != len(buf.String()) {
		t.Errorf("WriteSelected returned n=%d, want %d", n, len(buf.String()))
	}
}

func TestWriteSelected_PreservesExistingTrailingNewline(t *testing.T) {
	s := &store.Snippet{Body: "hello\n"}
	var buf bytes.Buffer
	if _, err := WriteSelected(&buf, Result{Selected: s}); err != nil {
		t.Fatalf("WriteSelected err = %v", err)
	}
	if got, want := buf.String(), "hello\n"; got != want {
		t.Errorf("WriteSelected wrote %q, want %q", got, want)
	}
}

func TestWriteSelected_NilSelectedIsNoop(t *testing.T) {
	var buf bytes.Buffer
	n, err := WriteSelected(&buf, Result{})
	if err != nil {
		t.Fatalf("WriteSelected err = %v", err)
	}
	if n != 0 || buf.Len() != 0 {
		t.Errorf("WriteSelected(nil) wrote %q (n=%d), want empty", buf.String(), n)
	}
}

func TestWriteSelected_PrefersRenderedOverBody(t *testing.T) {
	s := &store.Snippet{Body: "Hi {{name}}"}
	var buf bytes.Buffer
	if _, err := WriteSelected(&buf, Result{Selected: s, Rendered: "Hi Ryan"}); err != nil {
		t.Fatalf("WriteSelected err = %v", err)
	}
	if got := buf.String(); got != "Hi Ryan\n" {
		t.Errorf("WriteSelected wrote %q, want %q", got, "Hi Ryan\n")
	}
}

// --- placeholder-phase tests ---

func placeholderSnippet() store.Snippet {
	return store.Snippet{
		ID:    "hello",
		Title: "Placeholder greeting",
		Body:  "Hey {{name}}, welcome to {{team:the team}}!",
	}
}

func TestUpdate_EnterOnPlaceholderSnippetEntersPromptPhase(t *testing.T) {
	m := NewModel([]store.Snippet{placeholderSnippet()})
	m = key(m, tea.KeyEnter)
	if m.phase != phasePrompt {
		t.Fatalf("phase = %d, want phasePrompt (%d)", m.phase, phasePrompt)
	}
	if m.picked == nil {
		t.Fatal("picked = nil, want the placeholder snippet")
	}
	if m.confirmed {
		t.Error("confirmed = true too early; prompt phase must not confirm")
	}
	if m.quitting {
		t.Error("quitting = true too early; prompt phase must not quit")
	}
	if got, want := len(m.prompts), 2; got != want {
		t.Errorf("len(prompts) = %d, want %d", got, want)
	}
}

func TestUpdate_EnterOnPlainSnippetSkipsPromptPhase(t *testing.T) {
	m := NewModel([]store.Snippet{{ID: "plain", Title: "Plain", Body: "just text"}})
	m = key(m, tea.KeyEnter)
	if m.phase == phasePrompt {
		t.Error("phase = phasePrompt, want phasePick (no placeholders, no prompt)")
	}
	if !m.confirmed {
		t.Error("confirmed = false, want true (plain snippet must confirm on Enter)")
	}
	if !m.quitting {
		t.Error("quitting = false, want true")
	}
}

func TestUpdate_PromptPhase_TabAdvances(t *testing.T) {
	m := NewModel([]store.Snippet{placeholderSnippet()})
	m = key(m, tea.KeyEnter) // enter prompt phase
	if m.promptIx != 0 {
		t.Fatalf("promptIx = %d, want 0", m.promptIx)
	}
	m = typeString(m, "Ryan")
	m = key(m, tea.KeyTab)
	if m.promptIx != 1 {
		t.Errorf("promptIx after Tab = %d, want 1", m.promptIx)
	}
	if m.prompts[0].Focused() {
		t.Error("prompts[0] should be blurred after Tab")
	}
	if !m.prompts[1].Focused() {
		t.Error("prompts[1] should be focused after Tab")
	}
}

func TestUpdate_PromptPhase_EnterOnLastConfirms(t *testing.T) {
	m := NewModel([]store.Snippet{placeholderSnippet()})
	m = key(m, tea.KeyEnter) // enter prompt phase
	m = typeString(m, "Ryan")
	m = key(m, tea.KeyEnter) // next prompt (team)
	if m.confirmed {
		t.Fatal("confirmed after first Enter; want false (still on team prompt)")
	}
	m = typeString(m, "eng")
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if !m.confirmed {
		t.Error("confirmed = false, want true after final Enter")
	}
	if !m.quitting {
		t.Error("quitting = false, want true after final Enter")
	}
	if cmd == nil {
		t.Error("final Enter should dispatch tea.Quit (non-nil cmd)")
	}
	if got, want := m.renderedBody(), "Hey Ryan, welcome to eng!"; got != want {
		t.Errorf("renderedBody = %q, want %q", got, want)
	}
}

func TestUpdate_PromptPhase_InlineDefaultUsedOnEmpty(t *testing.T) {
	m := NewModel([]store.Snippet{placeholderSnippet()})
	m = key(m, tea.KeyEnter) // enter prompt phase
	m = typeString(m, "Ryan")
	m = key(m, tea.KeyEnter) // move to team prompt (empty)
	// Don't type anything — pressing Enter with empty input must accept
	// the inline default ("the team").
	if got := m.prompts[1].Value(); got != "" {
		t.Errorf("prompts[1] should start empty (default shown as ghost text); got %q", got)
	}
	m = key(m, tea.KeyEnter)
	if !m.confirmed {
		t.Fatal("confirmed = false")
	}
	if got, want := m.renderedBody(), "Hey Ryan, welcome to the team!"; got != want {
		t.Errorf("renderedBody = %q, want %q", got, want)
	}
}

func TestUpdate_PromptPhase_EscCancelsBackToPicker(t *testing.T) {
	m := NewModel([]store.Snippet{placeholderSnippet()})
	m = key(m, tea.KeyEnter) // enter prompt phase
	m = typeString(m, "Ryan")
	m = key(m, tea.KeyEsc)
	if m.phase == phasePrompt {
		t.Error("phase still phasePrompt after Esc")
	}
	if m.picked != nil {
		t.Error("picked should be cleared after Esc back to picker")
	}
	if m.confirmed {
		t.Error("confirmed = true after Esc; want false")
	}
	if m.quitting {
		t.Error("quitting = true after Esc; want false (Esc in prompt phase only returns to picker)")
	}
}

func TestUpdate_PromptPhase_CtrlCQuits(t *testing.T) {
	m := NewModel([]store.Snippet{placeholderSnippet()})
	m = key(m, tea.KeyEnter) // enter prompt phase
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = m2.(Model)
	if !m.quitting {
		t.Error("quitting = false after Ctrl-C in prompt phase")
	}
	if m.confirmed {
		t.Error("Ctrl-C must not confirm")
	}
}

func TestUpdate_PromptPhase_ShiftTabGoesBack(t *testing.T) {
	m := NewModel([]store.Snippet{placeholderSnippet()})
	m = key(m, tea.KeyEnter) // enter prompt phase
	m = typeString(m, "Ryan")
	m = key(m, tea.KeyTab) // move to team prompt
	if m.promptIx != 1 {
		t.Fatalf("promptIx = %d, want 1", m.promptIx)
	}
	m = key(m, tea.KeyShiftTab)
	if m.promptIx != 0 {
		t.Errorf("promptIx after ShiftTab = %d, want 0", m.promptIx)
	}
}

func TestView_PromptPhase_ShowsPromptsAndPreview(t *testing.T) {
	m := NewModel([]store.Snippet{placeholderSnippet()})
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = m2.(Model)
	m = key(m, tea.KeyEnter) // enter prompt phase
	out := m.View()
	if !strings.Contains(out, "fill placeholders") {
		t.Errorf("View() missing header; got:\n%s", out)
	}
	if !strings.Contains(out, "name") || !strings.Contains(out, "team") {
		t.Errorf("View() missing prompt names; got:\n%s", out)
	}
	// Type into the first prompt and confirm the live preview updates.
	m = typeString(m, "Ryan")
	out = m.View()
	if !strings.Contains(out, "Hey Ryan") {
		t.Errorf("View() live preview should show typed value; got:\n%s", out)
	}
}
