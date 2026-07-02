// Package store loads quipkit snippets from disk.
//
// A snippet is a plain markdown file. It may optionally start with a YAML
// frontmatter block delimited by "---" lines, e.g.:
//
//	---
//	title: My greeting
//	tags: [hello, casual]
//	---
//	Hey there!
//
// If frontmatter is absent, the title falls back to the first non-empty line
// of the body (with any leading "#" stripped) or, failing that, the file's
// base name without extension.
package store

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Snippet is a single loaded snippet.
type Snippet struct {
	ID    string   // stable id: file base name without extension
	Title string   // human title
	Tags  []string // optional tags
	Body  string   // snippet body (frontmatter stripped)
	Path  string   // absolute path on disk
}

// Load reads every `.md` snippet in dir. Missing dir returns an empty slice.
// Results are sorted by ID for stable output.
func Load(dir string) ([]Snippet, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read snippet dir %s: %w", dir, err)
	}
	var out []Snippet
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.EqualFold(filepath.Ext(name), ".md") {
			continue
		}
		full := filepath.Join(dir, name)
		data, err := os.ReadFile(full)
		if err != nil {
			return nil, fmt.Errorf("read snippet %s: %w", full, err)
		}
		s := ParseSnippet(name, data)
		s.Path = full
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ParseSnippet parses a snippet from its file name and raw content.
// It is exported for testing.
func ParseSnippet(filename string, data []byte) Snippet {
	id := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	title, tags, body := splitFrontmatter(string(data))
	title = strings.TrimSpace(title)
	if title == "" {
		title = firstMeaningfulLine(body)
	}
	if title == "" {
		title = id
	}
	return Snippet{
		ID:    id,
		Title: title,
		Tags:  tags,
		Body:  strings.TrimRight(body, "\n"),
	}
}

// splitFrontmatter returns (title, tags, body). It tolerates missing
// frontmatter and only understands the two fields quipkit cares about
// today: `title` and `tags`. Tags may be a YAML flow list ("[a, b]") or a
// comma-separated string.
func splitFrontmatter(text string) (title string, tags []string, body string) {
	// Normalize BOM.
	text = strings.TrimPrefix(text, "\ufeff")
	if !strings.HasPrefix(text, "---") {
		return "", nil, text
	}
	// Must be followed by newline.
	rest := text[3:]
	if !strings.HasPrefix(rest, "\n") && !strings.HasPrefix(rest, "\r\n") {
		return "", nil, text
	}
	// Find closing "---" on its own line.
	scanner := bufio.NewScanner(strings.NewReader(rest))
	// Preserve raw for body reconstruction.
	var fmLines []string
	closed := false
	consumed := 0
	for scanner.Scan() {
		line := scanner.Text()
		consumed += len(line) + 1 // +1 for the \n we stripped
		if strings.TrimRight(line, "\r") == "---" {
			closed = true
			break
		}
		fmLines = append(fmLines, line)
	}
	if err := scanner.Err(); err != nil || !closed {
		return "", nil, text
	}
	body = rest[consumed:]
	body = strings.TrimLeft(body, "\n")

	for _, ln := range fmLines {
		k, v, ok := splitKV(ln)
		if !ok {
			continue
		}
		switch strings.ToLower(k) {
		case "title":
			title = unquote(v)
		case "tags":
			tags = parseTags(v)
		}
	}
	return title, tags, body
}

func splitKV(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	i := strings.IndexByte(line, ':')
	if i < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
}

func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

func parseTags(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
		v = v[1 : len(v)-1]
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(unquote(strings.TrimSpace(p)))
		if t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstMeaningfulLine(body string) string {
	for _, ln := range strings.Split(body, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" {
			continue
		}
		t = strings.TrimLeft(t, "#")
		return strings.TrimSpace(t)
	}
	return ""
}
