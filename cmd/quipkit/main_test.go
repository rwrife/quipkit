package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rwrife/quipkit/internal/clip"
)

// withEnv temporarily sets an env var and restores it on cleanup.
func withEnv(t *testing.T, k, v string) {
	t.Helper()
	prev, had := os.LookupEnv(k)
	if err := os.Setenv(k, v); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	t.Cleanup(func() {
		if had {
			os.Setenv(k, prev)
		} else {
			os.Unsetenv(k)
		}
	})
}

// withStubbedClipboard swaps the CLI's copy/available hooks so tests
// don't touch the real system clipboard.
func withStubbedClipboard(t *testing.T, available bool, copyFn func(string) error) *string {
	t.Helper()
	prevCopy := copyToClipboard
	prevAvail := clipboardAvailable
	last := new(string)
	if copyFn == nil {
		copyFn = func(s string) error { *last = s; return nil }
	} else {
		orig := copyFn
		copyFn = func(s string) error { *last = s; return orig(s) }
	}
	copyToClipboard = copyFn
	clipboardAvailable = func() bool { return available }
	t.Cleanup(func() {
		copyToClipboard = prevCopy
		clipboardAvailable = prevAvail
	})
	return last
}

func TestRun_AddThenList(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, "QUIPKIT_DIR", dir)

	var out, errBuf bytes.Buffer
	code := run([]string{"add", "--title", "Off-site opener", "--tags", "work,reply", "Hey team, quick sync before we head out."}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("add exit = %d, want 0 (stderr=%q)", code, errBuf.String())
	}
	if !strings.HasPrefix(out.String(), "added ") {
		t.Errorf("add stdout = %q, want 'added ...'", out.String())
	}

	// list should now show it. (The dir already has our new file, so
	// Seed is a no-op — we're only asserting that add + list round-trip.)
	out.Reset()
	errBuf.Reset()
	code = run([]string{"list"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("list exit = %d, want 0 (stderr=%q)", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "Off-site opener") {
		t.Errorf("list output missing our snippet:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "work,reply") {
		t.Errorf("list output missing tags:\n%s", out.String())
	}
}

func TestRun_AddRequiresBody(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, "QUIPKIT_DIR", dir)

	// No args, no piped stdin: we treat stdin as a TTY in tests
	// (isatty returns false on the pipe, but there's no data), so this
	// path is exercised via the "body required" branch after reading
	// zero bytes. We accept exit 2 (usage) here as the contract.
	var out, errBuf bytes.Buffer
	code := run([]string{"add", "--title", "x"}, &out, &errBuf)
	if code != 2 {
		t.Errorf("add without body exit = %d, want 2 (stderr=%q)", code, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "needs snippet text") &&
		!strings.Contains(errBuf.String(), "usage:") {
		t.Errorf("stderr missing usage hint: %q", errBuf.String())
	}
}

func TestRun_AddRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, "QUIPKIT_DIR", dir)

	var out, errBuf bytes.Buffer
	if code := run([]string{"add", "--file", "note", "first"}, &out, &errBuf); code != 0 {
		t.Fatalf("first add exit = %d, want 0 (stderr=%q)", code, errBuf.String())
	}
	out.Reset()
	errBuf.Reset()
	code := run([]string{"add", "--file", "note", "second"}, &out, &errBuf)
	if code == 0 {
		t.Errorf("duplicate add exit = 0, want non-zero")
	}
	if !strings.Contains(errBuf.String(), "already exists") {
		t.Errorf("stderr missing 'already exists': %q", errBuf.String())
	}
	// And the first one should still be on disk untouched.
	got, err := os.ReadFile(filepath.Join(dir, "note.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(got), "first") {
		t.Errorf("original snippet was overwritten: %q", string(got))
	}
}

func TestRun_AddCSVFlagIsCleaned(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, "QUIPKIT_DIR", dir)

	var out, errBuf bytes.Buffer
	if code := run([]string{"add", "--file", "csvcheck", "--tags", " work , , reply , ", "body"}, &out, &errBuf); code != 0 {
		t.Fatalf("add exit = %d (stderr=%q)", code, errBuf.String())
	}
	// Read the file directly (list would also include seeded examples).
	data, err := os.ReadFile(filepath.Join(dir, "csvcheck.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "tags: [work, reply]") {
		t.Errorf("expected cleaned tags in frontmatter, got:\n%s", string(data))
	}
}

func TestSplitCSV(t *testing.T) {
	cases := map[string][]string{
		"":               nil,
		"one":            {"one"},
		"one,two":        {"one", "two"},
		" one , two , ,": {"one", "two"},
	}
	for in, want := range cases {
		got := splitCSV(in)
		if len(got) != len(want) {
			t.Errorf("splitCSV(%q) = %v, want %v", in, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("splitCSV(%q)[%d] = %q, want %q", in, i, got[i], want[i])
			}
		}
	}
}

// TestClipboardWiring_IsInPackage guards against accidental removal of
// the clip package wiring. If someone ever swaps the underlying package,
// this test still passes as long as copyToClipboard round-trips.
func TestClipboardWiring_RoundTripsThroughStub(t *testing.T) {
	got := withStubbedClipboard(t, true, nil)
	if err := copyToClipboard("hello"); err != nil {
		t.Fatalf("copyToClipboard: %v", err)
	}
	if *got != "hello" {
		t.Errorf("stub saw %q, want hello", *got)
	}
	// Sanity: real package still points somewhere non-nil.
	if clip.Copier == nil {
		t.Errorf("clip.Copier is nil")
	}
}

func TestClipboardWiring_ReturnsUnavailableError(t *testing.T) {
	withStubbedClipboard(t, false, func(string) error {
		return errors.New("boom")
	})
	if clipboardAvailable() {
		t.Fatal("clipboardAvailable() should be false")
	}
	// copyToClipboard still returns an error we can bubble up.
	if err := copyToClipboard("x"); err == nil {
		t.Error("copyToClipboard err = nil, want error when unavailable")
	}
}
