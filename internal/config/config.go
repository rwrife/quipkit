// Package config resolves the quipkit snippet directory and basic listing.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvVar is the environment variable that overrides the snippet directory.
const EnvVar = "QUIPKIT_DIR"

// DefaultDirName is the default folder name under the user's home directory.
const DefaultDirName = ".quipkit"

// SnippetDir resolves the snippet directory: $QUIPKIT_DIR if set, else ~/.quipkit.
func SnippetDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv(EnvVar)); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, DefaultDirName), nil
}

// ListSnippetFiles returns the base names of `.md` files in dir.
// If dir does not exist, returns an empty slice with no error.
func ListSnippetFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read snippet dir %s: %w", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(strings.ToLower(name), ".md") {
			out = append(out, name)
		}
	}
	return out, nil
}
