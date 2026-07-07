package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withConfigDir points XDG_CONFIG_HOME at a temp dir and returns the
// resolved config path so tests can drop a file at it.
func withConfigDir(t *testing.T) (dir, cfgPath string) {
	t.Helper()
	dir = t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	p, err := ConfigFilePath()
	if err != nil {
		t.Fatalf("ConfigFilePath err: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return dir, p
}

func TestConfigFilePathXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-home")
	got, err := ConfigFilePath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/tmp/xdg-home", "quipkit", "config")
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestConfigFilePathDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	got, err := ConfigFilePath()
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "quipkit", "config")
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestLoadFileMissingIsZero(t *testing.T) {
	_, _ = withConfigDir(t)
	got, err := LoadFile()
	if err != nil {
		t.Fatalf("LoadFile err: %v", err)
	}
	if got.SnippetDir != "" || got.Editor != "" || got.Path != "" {
		t.Fatalf("want zero File, got %+v", got)
	}
}

func TestLoadFileParsesKV(t *testing.T) {
	_, path := withConfigDir(t)
	body := "# quipkit config\n" +
		"snippet_dir = /tmp/snips  # inline comment\n" +
		"editor = \"code --wait\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile()
	if err != nil {
		t.Fatalf("LoadFile err: %v", err)
	}
	if got.SnippetDir != "/tmp/snips" {
		t.Fatalf("SnippetDir want /tmp/snips, got %q", got.SnippetDir)
	}
	if got.Editor != "code --wait" {
		t.Fatalf("Editor want %q, got %q", "code --wait", got.Editor)
	}
	if got.Path != path {
		t.Fatalf("Path want %q, got %q", path, got.Path)
	}
}

func TestLoadFileAcceptsColonAndUnknownKeys(t *testing.T) {
	_, path := withConfigDir(t)
	body := "snippet-dir: /tmp/colon-snips\nfuture_option: something\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile()
	if err != nil {
		t.Fatalf("LoadFile err: %v", err)
	}
	if got.SnippetDir != "/tmp/colon-snips" {
		t.Fatalf("SnippetDir want %q, got %q", "/tmp/colon-snips", got.SnippetDir)
	}
	if got.Editor != "" {
		t.Fatalf("Editor should be empty, got %q", got.Editor)
	}
}

func TestLoadFileExpandsTilde(t *testing.T) {
	_, path := withConfigDir(t)
	body := "snippet_dir = ~/quipkit-tilde\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile()
	if err != nil {
		t.Fatalf("LoadFile err: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, "quipkit-tilde")
	if got.SnippetDir != want {
		t.Fatalf("SnippetDir want %q, got %q", want, got.SnippetDir)
	}
}

func TestLoadFileRejectsMalformedLine(t *testing.T) {
	_, path := withConfigDir(t)
	body := "snippet_dir = /tmp/x\nthis has no separator\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFile()
	if err == nil {
		t.Fatal("want parse error, got nil")
	}
	if !strings.Contains(err.Error(), "expected `key = value`") {
		t.Fatalf("want key/value complaint, got %v", err)
	}
}

func TestResolveSnippetDirPrecedence(t *testing.T) {
	// env wins over config
	t.Setenv(EnvVar, "/tmp/from-env")
	dir, err := ResolveSnippetDir(File{SnippetDir: "/tmp/from-cfg"})
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/tmp/from-env" {
		t.Fatalf("env should win: got %q", dir)
	}

	// config wins over default
	t.Setenv(EnvVar, "")
	dir, err = ResolveSnippetDir(File{SnippetDir: "/tmp/from-cfg"})
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/tmp/from-cfg" {
		t.Fatalf("cfg should win: got %q", dir)
	}

	// default falls through
	dir, err = ResolveSnippetDir(File{})
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, DefaultDirName)
	if dir != want {
		t.Fatalf("want default %q, got %q", want, dir)
	}
}

func TestEditorPrecedence(t *testing.T) {
	t.Setenv(VisualEnvVar, "vim-visual")
	t.Setenv(EditorEnvVar, "vim-editor")
	if got := Editor(File{Editor: "cfg-editor"}); got != "vim-visual" {
		t.Fatalf("VISUAL should win, got %q", got)
	}

	t.Setenv(VisualEnvVar, "")
	if got := Editor(File{Editor: "cfg-editor"}); got != "vim-editor" {
		t.Fatalf("EDITOR should win over cfg, got %q", got)
	}

	t.Setenv(EditorEnvVar, "")
	if got := Editor(File{Editor: "cfg-editor"}); got != "cfg-editor" {
		t.Fatalf("cfg should win over default, got %q", got)
	}

	if got := Editor(File{}); got != "vi" {
		t.Fatalf("default want vi, got %q", got)
	}
}

func TestLoadFileParsesAutoType(t *testing.T) {
	_, path := withConfigDir(t)
	body := "auto_type = yes\ntype_delay_ms = 15\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile()
	if err != nil {
		t.Fatalf("LoadFile err: %v", err)
	}
	if !got.AutoType || !got.AutoTypeSet {
		t.Errorf("AutoType=%v AutoTypeSet=%v, want both true", got.AutoType, got.AutoTypeSet)
	}
	if got.TypeDelayMs != 15 {
		t.Errorf("TypeDelayMs = %d, want 15", got.TypeDelayMs)
	}
}

func TestLoadFileAutoTypeAcceptsAliases(t *testing.T) {
	// The `type` alias is what users are likely to reach for first. Also
	// covers the false path ("off" as an alias for no).
	_, path := withConfigDir(t)
	if err := os.WriteFile(path, []byte("type: off\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile()
	if err != nil {
		t.Fatalf("LoadFile err: %v", err)
	}
	if got.AutoType || !got.AutoTypeSet {
		t.Errorf("AutoType=%v AutoTypeSet=%v, want false/true", got.AutoType, got.AutoTypeSet)
	}
}

func TestLoadFileRejectsBadAutoType(t *testing.T) {
	_, path := withConfigDir(t)
	if err := os.WriteFile(path, []byte("auto_type = maybe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFile()
	if err == nil {
		t.Fatal("want error for non-boolean auto_type, got nil")
	}
	if !strings.Contains(err.Error(), "boolean") {
		t.Errorf("err = %v, want boolean complaint", err)
	}
}

func TestLoadFileRejectsBadTypeDelay(t *testing.T) {
	_, path := withConfigDir(t)
	if err := os.WriteFile(path, []byte("type_delay_ms = fast\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFile()
	if err == nil {
		t.Fatal("want error for non-numeric type_delay_ms, got nil")
	}
	if !strings.Contains(err.Error(), "non-negative integer") {
		t.Errorf("err = %v, want integer complaint", err)
	}
}

func TestLoadFileAutoTypeAbsentLeavesSetFalse(t *testing.T) {
	// Absent auto_type must leave AutoTypeSet=false so the CLI knows
	// there's no config-level opinion.
	_, path := withConfigDir(t)
	if err := os.WriteFile(path, []byte("editor = vim\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile()
	if err != nil {
		t.Fatalf("LoadFile err: %v", err)
	}
	if got.AutoTypeSet {
		t.Errorf("AutoTypeSet = true, want false when key absent")
	}
}
