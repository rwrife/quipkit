package store

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fixedNow(ts string) func() time.Time {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return t }
}

func TestAdd_WritesFileWithFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path, err := Add(dir, "Hello there!", AddOptions{
		Title: "Greeting",
		Tags:  []string{"hello", "casual"},
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Errorf("wrote %s, expected under %s", path, dir)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	got := string(data)
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("expected frontmatter, got:\n%s", got)
	}
	if !strings.Contains(got, `title: "Greeting"`) {
		t.Errorf("missing title in frontmatter:\n%s", got)
	}
	if !strings.Contains(got, "tags: [hello, casual]") {
		t.Errorf("missing tags in frontmatter:\n%s", got)
	}
	if !strings.HasSuffix(got, "Hello there!\n") {
		t.Errorf("body missing trailing newline:\n%s", got)
	}

	// Round-trip through Load to make sure the loader accepts what we wrote.
	snips, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(snips) != 1 {
		t.Fatalf("Load got %d snippets, want 1", len(snips))
	}
	s := snips[0]
	if s.Title != "Greeting" {
		t.Errorf("Title = %q, want Greeting", s.Title)
	}
	if !reflect.DeepEqual(s.Tags, []string{"hello", "casual"}) {
		t.Errorf("Tags = %v", s.Tags)
	}
	if s.Body != "Hello there!" {
		t.Errorf("Body = %q", s.Body)
	}
}

func TestAdd_DerivesFilenameFromTitle(t *testing.T) {
	dir := t.TempDir()
	path, err := Add(dir, "Body!", AddOptions{Title: "My Cool Reply"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got, want := filepath.Base(path), "my-cool-reply.md"; got != want {
		t.Errorf("filename = %q, want %q", got, want)
	}
}

func TestAdd_DerivesFilenameFromBodyWhenNoTitle(t *testing.T) {
	dir := t.TempDir()
	path, err := Add(dir, "Thanks a lot!\nSecond line", AddOptions{})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	// First line slugified.
	if got, want := filepath.Base(path), "thanks-a-lot.md"; got != want {
		t.Errorf("filename = %q, want %q", got, want)
	}
}

func TestAdd_FallsBackToTimestampWhenSlugEmpty(t *testing.T) {
	dir := t.TempDir()
	path, err := Add(dir, "!!!", AddOptions{
		Now: fixedNow("2026-07-04T22:30:00Z"),
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got, want := filepath.Base(path), "snippet-20260704-223000.md"; got != want {
		t.Errorf("filename = %q, want %q", got, want)
	}
}

func TestAdd_RefusesToOverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	if _, err := Add(dir, "first", AddOptions{Filename: "note"}); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	_, err := Add(dir, "second", AddOptions{Filename: "note"})
	if err == nil {
		t.Fatal("expected error on duplicate filename, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %v, want to mention 'already exists'", err)
	}
}

func TestAdd_RejectsEmptyBody(t *testing.T) {
	dir := t.TempDir()
	if _, err := Add(dir, "   \n\t\n", AddOptions{Title: "x"}); err == nil {
		t.Error("expected error on empty body, got nil")
	}
}

func TestAdd_CleansTagsAndDedupes(t *testing.T) {
	dir := t.TempDir()
	path, err := Add(dir, "b", AddOptions{
		Title: "T",
		Tags:  []string{"work", " work ", "", "reply", "reply"},
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "tags: [work, reply]") {
		t.Errorf("tags not cleaned/deduped:\n%s", string(data))
	}
}

func TestAdd_NoFrontmatterWhenTitleAndTagsEmpty(t *testing.T) {
	dir := t.TempDir()
	path, err := Add(dir, "just a body", AddOptions{})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	data, _ := os.ReadFile(path)
	if strings.HasPrefix(string(data), "---") {
		t.Errorf("expected no frontmatter, got:\n%s", string(data))
	}
	if string(data) != "just a body\n" {
		t.Errorf("content = %q, want %q", string(data), "just a body\n")
	}
}

func TestAdd_AppendsMdSuffixIfMissing(t *testing.T) {
	dir := t.TempDir()
	path, err := Add(dir, "b", AddOptions{Filename: "no-suffix"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !strings.HasSuffix(path, ".md") {
		t.Errorf("path = %s, want .md suffix", path)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"":              "",
		"Hello World":   "hello-world",
		"  spaced  out": "spaced-out",
		"UPPER_CASE!!":  "upper-case",
		"a---b":         "a-b",
		"emoji 🎉 party": "emoji-party",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
