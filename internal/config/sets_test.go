package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNormalizeSetNameAndResolveSet(t *testing.T) {
	// env wins over cfg
	t.Setenv(SetEnvVar, "work")
	got := ResolveSet(File{DefaultSet: "personal"})
	if got != "work" {
		t.Fatalf("env should win: got %q", got)
	}

	// "default" alias resolves to base ("")
	t.Setenv(SetEnvVar, "default")
	if got := ResolveSet(File{}); got != "" {
		t.Fatalf("default alias should be empty, got %q", got)
	}

	// cfg used when env empty
	t.Setenv(SetEnvVar, "")
	if got := ResolveSet(File{DefaultSet: "support"}); got != "support" {
		t.Fatalf("cfg fallback: got %q", got)
	}

	// override wins over env + cfg
	t.Setenv(SetEnvVar, "work")
	if got := ResolveSetWithOverride(File{DefaultSet: "personal"}, "adhoc"); got != "adhoc" {
		t.Fatalf("flag override: got %q", got)
	}
}

func TestValidateSetName(t *testing.T) {
	ok := []string{"work", "support", "team-1", "a_b", "Personal"}
	bad := []string{"", "..", "with space", "slash/inside", "dot.name", "sneaky/../oops"}
	for _, s := range ok {
		if err := ValidateSetName(s); err != nil {
			t.Errorf("expected %q ok, got %v", s, err)
		}
	}
	for _, s := range bad {
		if err := ValidateSetName(s); err == nil {
			t.Errorf("expected %q invalid, got nil", s)
		}
	}
}

func TestEffectiveDir(t *testing.T) {
	base := "/snip"
	if got, err := EffectiveDir(base, ""); err != nil || got != base {
		t.Fatalf("base: %q %v", got, err)
	}
	got, err := EffectiveDir(base, "work")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, SetsDirName, "work")
	if got != want {
		t.Fatalf("want %q got %q", want, got)
	}
	if _, err := EffectiveDir(base, "../evil"); err == nil {
		t.Fatal("expected traversal rejected")
	}
}

func TestListAndCreateSets(t *testing.T) {
	base := t.TempDir()

	// Empty state: no sets/ dir yet.
	got, err := ListSets(base)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty list: %v %v", got, err)
	}

	if _, err := CreateSet(base, "work"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateSet(base, "support"); err != nil {
		t.Fatal(err)
	}
	// Stray non-set folder should be ignored.
	if err := os.MkdirAll(filepath.Join(base, SetsDirName, "not a set"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err = ListSets(base)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"support", "work"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v got %v", want, got)
	}

	// Idempotent create.
	if _, err := CreateSet(base, "work"); err != nil {
		t.Fatalf("re-create should be idempotent: %v", err)
	}

	// Bad name.
	if _, err := CreateSet(base, "../oops"); err == nil {
		t.Fatal("expected invalid name error")
	}
}
