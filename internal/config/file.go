package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigDirName is the folder name used under $XDG_CONFIG_HOME (or
// ~/.config when unset).
const ConfigDirName = "quipkit"

// ConfigFileName is the base filename of the optional config file. It's
// extension-less on purpose: the format is a tiny `key = value` grammar,
// not YAML/TOML — deliberately no new dependency.
const ConfigFileName = "config"

// EditorEnvVar is the environment variable that overrides the configured
// editor. Set to match POSIX convention.
const EditorEnvVar = "EDITOR"

// VisualEnvVar is the environment variable checked before EDITOR, also
// per POSIX convention.
const VisualEnvVar = "VISUAL"

// File is the parsed contents of the optional quipkit config file.
//
// Fields are zero-valued when not present; callers should treat empty
// strings as "not set" and fall back to env / defaults themselves.
type File struct {
	// SnippetDir overrides the snippet directory when set. Env
	// QUIPKIT_DIR still wins so ad-hoc invocations remain trivial.
	SnippetDir string
	// Editor is the command to spawn for `quipkit edit`. May be a bare
	// name ("vim") or include args ("code --wait").
	Editor string
	// Path is the absolute path we loaded from; empty when no file was
	// found (Load returns a zero File in that case).
	Path string
}

// ConfigFilePath returns the resolved absolute path of the optional
// config file (whether or not it exists on disk).
//
// Order:
//  1. $XDG_CONFIG_HOME/quipkit/config
//  2. ~/.config/quipkit/config
func ConfigFilePath() (string, error) {
	if v := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); v != "" {
		return filepath.Join(v, ConfigDirName, ConfigFileName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".config", ConfigDirName, ConfigFileName), nil
}

// LoadFile reads the optional config file. A missing file is not an
// error — LoadFile returns a zero File with Path unset in that case.
// Parse errors (malformed lines) are surfaced so a broken config is
// visible instead of silently ignored.
func LoadFile() (File, error) {
	path, err := ConfigFilePath()
	if err != nil {
		return File{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return File{}, nil
		}
		return File{}, fmt.Errorf("open config %s: %w", path, err)
	}
	defer f.Close()

	out := File{Path: path}
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := splitConfigKV(line)
		if !ok {
			return File{}, fmt.Errorf("%s:%d: expected `key = value`, got %q", path, lineNo, sc.Text())
		}
		switch strings.ToLower(k) {
		case "snippet_dir", "snippet-dir", "snippetdir":
			out.SnippetDir = expandTilde(v)
		case "editor":
			out.Editor = v
		default:
			// Unknown keys are ignored on purpose so old binaries don't
			// choke on new config keys.
		}
	}
	if err := sc.Err(); err != nil {
		return File{}, fmt.Errorf("read config %s: %w", path, err)
	}
	return out, nil
}

// Editor resolves which editor command to use for `quipkit edit`.
//
// Precedence:
//  1. $VISUAL
//  2. $EDITOR
//  3. cfg.Editor (from config file)
//  4. "vi" as a POSIX-safe fallback
func Editor(cfg File) string {
	if v := strings.TrimSpace(os.Getenv(VisualEnvVar)); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv(EditorEnvVar)); v != "" {
		return v
	}
	if v := strings.TrimSpace(cfg.Editor); v != "" {
		return v
	}
	return "vi"
}

// ResolveSnippetDir picks the effective snippet directory, applying the
// full precedence chain in one place:
//
//  1. $QUIPKIT_DIR (env)
//  2. cfg.SnippetDir (config file)
//  3. ~/.quipkit (default)
func ResolveSnippetDir(cfg File) (string, error) {
	if v := strings.TrimSpace(os.Getenv(EnvVar)); v != "" {
		return v, nil
	}
	if v := strings.TrimSpace(cfg.SnippetDir); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, DefaultDirName), nil
}

// splitConfigKV parses `key = value` (or `key: value`) with the value
// optionally quoted. Trailing `# comments` are stripped when the value
// isn't quoted.
func splitConfigKV(line string) (string, string, bool) {
	sep := strings.IndexAny(line, "=:")
	if sep < 0 {
		return "", "", false
	}
	k := strings.TrimSpace(line[:sep])
	v := strings.TrimSpace(line[sep+1:])
	if k == "" {
		return "", "", false
	}
	// Quoted value: consume up to the matching quote, ignore anything
	// after (so trailing comments after a quoted string are fine).
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') {
		q := v[0]
		end := strings.IndexByte(v[1:], q)
		if end >= 0 {
			return k, v[1 : 1+end], true
		}
		// Unterminated quote — treat literally, minus the leading quote.
		return k, v[1:], true
	}
	// Unquoted: strip trailing `# comment`.
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return k, v, true
}

// expandTilde replaces a leading `~/` with the user's home directory.
// Anything else (including a bare `~`) is returned unchanged so we don't
// silently mangle unusual paths.
func expandTilde(p string) string {
	if !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[2:])
}
