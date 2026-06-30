package config

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestSnippetDirEnvOverride(t *testing.T) {
	t.Setenv(EnvVar, "/tmp/quipkit-test-dir")
	got, err := SnippetDir()
	if err != nil {
		t.Fatalf("SnippetDir err: %v", err)
	}
	if got != "/tmp/quipkit-test-dir" {
		t.Fatalf("want override, got %q", got)
	}
}

func TestSnippetDirDefault(t *testing.T) {
	t.Setenv(EnvVar, "")
	got, err := SnippetDir()
	if err != nil {
		t.Fatalf("SnippetDir err: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, DefaultDirName)
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestListSnippetFiles(t *testing.T) {
	dir := t.TempDir()
	files := []string{"a.md", "b.MD", "ignored.txt", "c.md"}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ListSnippetFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{"a.md", "b.MD", "c.md"}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
}

func TestListSnippetFilesMissingDir(t *testing.T) {
	got, err := ListSnippetFiles(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("expected nil err for missing dir, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty, got %v", got)
	}
}
