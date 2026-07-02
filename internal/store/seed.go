package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// Seed writes the example snippets into dir, creating dir if needed.
// It only writes when dir has no `.md` files, so re-running is safe.
// Returns the list of filenames written (may be empty).
func Seed(dir string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	existing, err := Load(dir)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return nil, nil
	}
	var written []string
	for _, ex := range Examples() {
		full := filepath.Join(dir, ex.Filename)
		if err := os.WriteFile(full, []byte(ex.Content), 0o644); err != nil {
			return written, fmt.Errorf("write seed %s: %w", full, err)
		}
		written = append(written, ex.Filename)
	}
	return written, nil
}
