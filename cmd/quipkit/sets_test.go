package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// End-to-end coverage for `--set`: creating a set, writing into it, and
// making sure the base library stays untouched.
func TestRun_SetIsolatesLibraries(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, "QUIPKIT_DIR", dir)
	// Make sure no ambient set leaks in from the test environment.
	withEnv(t, "QUIPKIT_SET", "")

	// Create a set.
	var out, errBuf bytes.Buffer
	if code := run([]string{"sets", "create", "work"}, &out, &errBuf); code != 0 {
		t.Fatalf("sets create exit = %d, stderr=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "created set \"work\"") {
		t.Errorf("sets create stdout = %q", out.String())
	}

	// Add a snippet into `work`.
	out.Reset()
	errBuf.Reset()
	if code := run([]string{"--set", "work", "add", "--title", "Standup opener", "morning team"}, &out, &errBuf); code != 0 {
		t.Fatalf("--set add exit = %d, stderr=%q", code, errBuf.String())
	}

	// Verify the file landed under sets/work, not the base dir.
	if _, err := os.Stat(filepath.Join(dir, "sets", "work")); err != nil {
		t.Fatalf("expected sets/work dir: %v", err)
	}
	baseEntries, _ := os.ReadDir(dir)
	for _, e := range baseEntries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			// The base library was seeded on first invocation (which is fine),
			// but the Standup opener must not appear here.
			data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			if strings.Contains(string(data), "morning team") {
				t.Fatalf("snippet leaked into base dir: %s", e.Name())
			}
		}
	}

	// list without --set only shows base library.
	out.Reset()
	errBuf.Reset()
	if code := run([]string{"list"}, &out, &errBuf); code != 0 {
		t.Fatalf("list exit = %d, stderr=%q", code, errBuf.String())
	}
	if strings.Contains(out.String(), "Standup opener") {
		t.Fatalf("base list leaked set snippet: %q", out.String())
	}

	// list --set work must show it.
	out.Reset()
	errBuf.Reset()
	if code := run([]string{"--set", "work", "list"}, &out, &errBuf); code != 0 {
		t.Fatalf("--set list exit = %d, stderr=%q", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "Standup opener") {
		t.Fatalf("--set list missing snippet: %q", out.String())
	}

	// `sets` lists the set with a count >= 1.
	out.Reset()
	errBuf.Reset()
	if code := run([]string{"sets"}, &out, &errBuf); code != 0 {
		t.Fatalf("sets exit = %d, stderr=%q", code, errBuf.String())
	}
	if !strings.HasPrefix(out.String(), "work\t") {
		t.Fatalf("sets list = %q, want 'work\\t...'", out.String())
	}
}

func TestRun_SetInvalidNameRejected(t *testing.T) {
	dir := t.TempDir()
	withEnv(t, "QUIPKIT_DIR", dir)
	withEnv(t, "QUIPKIT_SET", "")

	var out, errBuf bytes.Buffer
	code := run([]string{"--set", "../evil", "list"}, &out, &errBuf)
	if code == 0 {
		t.Fatalf("expected non-zero exit for bad set, got 0 (stderr=%q)", errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "invalid character") {
		t.Errorf("stderr should explain the rejection, got %q", errBuf.String())
	}
}
