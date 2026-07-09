package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SetEnvVar is the environment variable that overrides the active
// snippet set for a single invocation. It wins over the config file's
// `default_set` but loses to the top-level `--set` CLI flag.
const SetEnvVar = "QUIPKIT_SET"

// SetsDirName is the subfolder under the snippet directory that holds
// named sets. Each set is a folder of markdown snippets — mirroring the
// layout of the base directory itself.
//
// Layout:
//
//	<snippetDir>/
//	  *.md                (the default / unset library)
//	  sets/
//	    work/*.md         (set "work")
//	    support/*.md      (set "support")
//	    personal/*.md
const SetsDirName = "sets"

// DefaultSetName is the reserved alias for "no set" — i.e. the base
// snippet directory, not a subfolder under sets/. Passing `--set default`
// or setting `QUIPKIT_SET=default` explicitly selects the base library.
const DefaultSetName = "default"

// ResolveSet picks the effective set name from env / config. An empty
// return means "use the base library" (the default). The CLI is
// responsible for layering its own `--set` flag on top before calling
// [EffectiveDir]: pass the flag through [ResolveSetWithOverride] to get
// the full precedence chain in one place.
func ResolveSet(cfg File) string {
	if v := strings.TrimSpace(os.Getenv(SetEnvVar)); v != "" {
		return normalizeSetName(v)
	}
	return normalizeSetName(cfg.DefaultSet)
}

// ResolveSetWithOverride folds the CLI flag into the precedence chain.
// Order: flag > env > config > "" (base).
func ResolveSetWithOverride(cfg File, cliOverride string) string {
	if v := strings.TrimSpace(cliOverride); v != "" {
		return normalizeSetName(v)
	}
	return ResolveSet(cfg)
}

// normalizeSetName trims and lowercases the "default" alias into "". All
// other names are returned trimmed but otherwise verbatim so validation
// errors report exactly what the user typed.
func normalizeSetName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.EqualFold(name, DefaultSetName) {
		return ""
	}
	return name
}

// ValidateSetName ensures a set name is safe as a folder name. We
// deliberately restrict the character class rather than lean on the
// filesystem, because a user typo like `--set ../whoops` would happily
// resolve to something surprising otherwise.
func ValidateSetName(name string) error {
	if name == "" {
		return fmt.Errorf("set name is empty")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return fmt.Errorf("set name %q contains invalid character %q (allowed: letters, digits, - and _)", name, r)
		}
	}
	return nil
}

// EffectiveDir returns the snippet directory to actually read/write for
// the given set. An empty set name yields the base directory. A
// non-empty set name yields `<base>/sets/<name>` after validation.
func EffectiveDir(baseDir, setName string) (string, error) {
	if setName == "" {
		return baseDir, nil
	}
	if err := ValidateSetName(setName); err != nil {
		return "", err
	}
	return filepath.Join(baseDir, SetsDirName, setName), nil
}

// ListSets returns the names of every set defined under baseDir, sorted
// for stable output. Missing `sets/` directory returns an empty slice
// with no error — the user simply hasn't created any sets yet.
func ListSets(baseDir string) ([]string, error) {
	root := filepath.Join(baseDir, SetsDirName)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sets dir %s: %w", root, err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if err := ValidateSetName(name); err != nil {
			// Silently skip folders that don't look like sets so a
			// stray `.git` or backup dir doesn't spam the listing.
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// CreateSet materializes an empty set folder under baseDir. It is safe
// to call on an existing set (returns nil, no error) so callers can use
// it as an idempotent "ensure this exists" primitive.
func CreateSet(baseDir, name string) (string, error) {
	if err := ValidateSetName(name); err != nil {
		return "", err
	}
	dir := filepath.Join(baseDir, SetsDirName, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return dir, nil
}
