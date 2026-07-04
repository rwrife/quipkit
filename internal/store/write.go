package store

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// AddOptions controls how [Add] writes a new snippet file.
//
// All fields are optional:
//   - Title becomes the file's frontmatter title. When empty, the file
//     is written without a `title:` line and the loader falls back to
//     the first non-empty body line (or the filename) at read time.
//   - Tags is a de-duplicated, whitespace-trimmed list. Empty tags are
//     dropped. Order is preserved from the caller's slice.
//   - Filename lets the caller pick the on-disk basename (with or
//     without the .md suffix). When empty, [Add] derives one from Title
//     (or the first body line) and falls back to a timestamp so two
//     rapid-fire adds never collide.
//   - Now is injectable for deterministic tests. Nil = time.Now.
type AddOptions struct {
	Title    string
	Tags     []string
	Filename string
	Now      func() time.Time
}

// Add writes body to a new snippet file inside dir and returns the full
// path. The directory is created if missing.
//
// It refuses to overwrite an existing file: if the resolved filename
// already exists, Add returns an error rather than clobbering the user's
// existing snippet. Pick a different Filename (or let Add derive one)
// and try again.
func Add(dir, body string, opts AddOptions) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", fmt.Errorf("add snippet: body is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}

	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}

	tags := cleanTags(opts.Tags)

	name := strings.TrimSpace(opts.Filename)
	if name == "" {
		name = deriveFilename(opts.Title, body, now)
	}
	if !strings.EqualFold(filepath.Ext(name), ".md") {
		name += ".md"
	}
	full := filepath.Join(dir, name)
	if _, err := os.Stat(full); err == nil {
		return "", fmt.Errorf("add snippet: %s already exists", full)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat %s: %w", full, err)
	}

	content := renderSnippet(opts.Title, tags, body)
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", full, err)
	}
	return full, nil
}

// cleanTags trims whitespace, drops empties, and de-duplicates while
// preserving the caller's order. Case is preserved (tags are labels).
func cleanTags(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// renderSnippet builds the on-disk representation. Frontmatter is
// omitted entirely when there's nothing to record, which keeps
// hand-authored snippets and Add-produced snippets visually similar.
func renderSnippet(title string, tags []string, body string) string {
	var b strings.Builder
	if title != "" || len(tags) > 0 {
		b.WriteString("---\n")
		if title != "" {
			// Always quote titles: colons and other YAML metachars in a
			// user-supplied title otherwise break the parser.
			b.WriteString("title: ")
			b.WriteString(quoteYAML(title))
			b.WriteString("\n")
		}
		if len(tags) > 0 {
			b.WriteString("tags: [")
			for i, t := range tags {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(t)
			}
			b.WriteString("]\n")
		}
		b.WriteString("---\n")
	}
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

// quoteYAML double-quotes a scalar, escaping embedded double-quotes and
// backslashes. Enough for user-typed titles; not a full YAML emitter.
func quoteYAML(s string) string {
	esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
	return `"` + esc + `"`
}

// slugRE strips anything that isn't a nice URL/file character.
var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

// deriveFilename builds a stable, filesystem-safe basename from the
// title (preferred) or first non-empty body line, with a timestamp
// fallback if slugification produced nothing usable.
func deriveFilename(title, body string, now func() time.Time) string {
	seed := strings.TrimSpace(title)
	if seed == "" {
		seed = firstMeaningfulLine(body)
	}
	slug := Slugify(seed)
	if slug == "" {
		return "snippet-" + now().UTC().Format("20060102-150405") + ".md"
	}
	// Cap length so we don't produce 200-char filenames from a long
	// title/body; leaves plenty of room for the .md suffix.
	if len(slug) > 60 {
		slug = strings.TrimRight(slug[:60], "-")
	}
	return slug + ".md"
}

// Slugify lower-cases, replaces non-alphanumerics with `-`, and trims
// leading/trailing dashes. Exported for tests.
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRE.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}
