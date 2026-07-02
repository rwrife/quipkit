package store

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseSnippet_WithFrontmatter(t *testing.T) {
	data := []byte("---\ntitle: Hello\ntags: [a, b, c]\n---\nBody line 1\nBody line 2\n")
	s := ParseSnippet("hello.md", data)
	if s.ID != "hello" {
		t.Errorf("ID = %q, want hello", s.ID)
	}
	if s.Title != "Hello" {
		t.Errorf("Title = %q, want Hello", s.Title)
	}
	if !reflect.DeepEqual(s.Tags, []string{"a", "b", "c"}) {
		t.Errorf("Tags = %v", s.Tags)
	}
	if s.Body != "Body line 1\nBody line 2" {
		t.Errorf("Body = %q", s.Body)
	}
}

func TestParseSnippet_QuotedAndCSVTags(t *testing.T) {
	data := []byte("---\ntitle: \"With, comma\"\ntags: one, two , three\n---\nhi\n")
	s := ParseSnippet("x.md", data)
	if s.Title != "With, comma" {
		t.Errorf("Title = %q", s.Title)
	}
	if !reflect.DeepEqual(s.Tags, []string{"one", "two", "three"}) {
		t.Errorf("Tags = %v", s.Tags)
	}
}

func TestParseSnippet_NoFrontmatter_FirstLineTitle(t *testing.T) {
	data := []byte("# The Title\nrest of body\n")
	s := ParseSnippet("note.md", data)
	if s.Title != "The Title" {
		t.Errorf("Title = %q", s.Title)
	}
	if s.Body != "# The Title\nrest of body" {
		t.Errorf("Body = %q", s.Body)
	}
}

func TestParseSnippet_NoFrontmatter_EmptyFallsBackToID(t *testing.T) {
	s := ParseSnippet("plain.md", []byte(""))
	if s.Title != "plain" {
		t.Errorf("Title = %q, want plain", s.Title)
	}
}

func TestParseSnippet_UnterminatedFrontmatter_TreatedAsBody(t *testing.T) {
	data := []byte("---\ntitle: oops\nno closing marker\n")
	s := ParseSnippet("x.md", data)
	if s.Title == "oops" {
		t.Errorf("should not parse unterminated frontmatter as title")
	}
}

func TestSeedAndLoad(t *testing.T) {
	dir := t.TempDir()
	written, err := Seed(dir)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if len(written) != len(Examples()) {
		t.Fatalf("wrote %d files, want %d", len(written), len(Examples()))
	}
	// Second call is a no-op.
	written2, err := Seed(dir)
	if err != nil {
		t.Fatalf("Seed second: %v", err)
	}
	if len(written2) != 0 {
		t.Errorf("second seed wrote %d, want 0", len(written2))
	}

	snips, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(snips) != len(Examples()) {
		t.Fatalf("Load got %d, want %d", len(snips), len(Examples()))
	}
	for _, s := range snips {
		if s.Title == "" {
			t.Errorf("snippet %s has empty title", s.ID)
		}
		if s.Path == "" || filepath.Dir(s.Path) != dir {
			t.Errorf("snippet %s has odd path %q", s.ID, s.Path)
		}
	}
}

func TestLoad_MissingDirIsEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	snips, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(snips) != 0 {
		t.Errorf("got %d, want 0", len(snips))
	}
}

func TestLoad_IgnoresNonMarkdown(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keep.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skip.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	snips, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(snips) != 1 || snips[0].ID != "keep" {
		t.Errorf("got %+v", snips)
	}
}
