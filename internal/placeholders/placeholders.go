// Package placeholders parses and renders {{token}} substitutions in
// snippet bodies.
//
// A placeholder is a `{{name}}` or `{{name:default}}` token. `name` is
// a lightweight identifier (letters, digits, underscore, dash, dot);
// anything else inside `{{ ... }}` is left literal so real markdown like
// `{{ not a placeholder }}` doesn't get eaten.
//
// The package is deliberately I/O free: the two entry points are
//
//   - Extract(body)                 → discover the tokens a snippet uses
//   - Render(body, values)          → substitute known tokens
//
// A small set of "known" tokens is auto-filled by [Values.Autofill]:
// date, time, datetime, year, month, day, now, and user. Unknown tokens
// come back from [Extract] with AutoFilled = false so the TUI / CLI can
// prompt for them before rendering. Defaults from a shared vars file
// are layered in via [Values.LoadVars].
//
// Escapes: a `{{name}}` token can be rendered literally by prefixing it
// with a backslash, e.g. `\{{name}}`. The backslash is consumed and the
// rest is emitted verbatim.
package placeholders

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Token is one placeholder discovered in a snippet body.
type Token struct {
	// Name is the identifier inside `{{ ... }}` (without the braces).
	Name string
	// Default is the inline default from `{{name:default}}`, or the
	// value pulled from a shared vars file if one was loaded. Empty
	// when nothing is known yet.
	Default string
	// AutoFilled is true when [Values.Autofill] can produce a value for
	// this token without prompting (date/time/user/etc).
	AutoFilled bool
	// AutoValue is the value AutoFilled tokens would render to, computed
	// against the Values at Extract time. Callers can display it in a
	// preview without re-rendering.
	AutoValue string
}

// tokenRE matches `{{name}}` or `{{name:default}}` non-greedily.
//   - group 1: name (letters/digits/underscore/dash/dot)
//   - group 2: optional `:default` payload (everything up to the closing `}}`)
//
// Whitespace around `name` and around the `:` is tolerated so
// `{{ name : Ryan }}` still parses.
var tokenRE = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.\-]+)\s*(?::([^}]*))?\}\}`)

// nameRE validates a bare placeholder name (used by parseName in tests
// and by future setters).
var nameRE = regexp.MustCompile(`^[A-Za-z0-9_.\-]+$`)

// Values is the resolved substitution map plus the timestamp autofill
// tokens key off. Split from a raw map so callers can layer sources:
// autofill first, then vars.yaml, then TUI prompts (each layer wins).
type Values struct {
	// Now is the reference time for date/time/datetime autofills. Zero
	// value means "resolve at render time via time.Now()".
	Now time.Time
	// User is the value the {{user}} token should render to. Empty
	// falls back to os/user lookup, then $USER, then "".
	User string
	// values is the merged map: token name → value. Populated by the
	// Set / LoadVars / Autofill helpers.
	values map[string]string
}

// NewValues returns an empty Values ready for Set / LoadVars / Autofill.
func NewValues() *Values {
	return &Values{values: map[string]string{}}
}

// Set assigns a raw value for name, overwriting any previous entry.
// Callers should have already validated `name` via [ValidName]; Set
// itself is permissive so tests can push arbitrary keys.
func (v *Values) Set(name, value string) {
	if v.values == nil {
		v.values = map[string]string{}
	}
	v.values[name] = value
}

// Get returns the value for name and whether it was present.
func (v *Values) Get(name string) (string, bool) {
	if v == nil || v.values == nil {
		return "", false
	}
	s, ok := v.values[name]
	return s, ok
}

// Has reports whether name has a value set (including empty string).
func (v *Values) Has(name string) bool {
	if v == nil || v.values == nil {
		return false
	}
	_, ok := v.values[name]
	return ok
}

// Autofill assigns the built-in tokens (date/time/user/etc) into v.
// Any pre-existing entry for a given key is left alone so caller-set
// overrides win (e.g. a user-typed `{{date}}` from the TUI prompt).
func (v *Values) Autofill() {
	if v.values == nil {
		v.values = map[string]string{}
	}
	now := v.Now
	if now.IsZero() {
		now = time.Now()
	}

	setIfAbsent := func(k, val string) {
		if _, ok := v.values[k]; !ok {
			v.values[k] = val
		}
	}

	setIfAbsent("date", now.Format("2006-01-02"))
	setIfAbsent("time", now.Format("15:04"))
	setIfAbsent("datetime", now.Format("2006-01-02 15:04"))
	setIfAbsent("year", now.Format("2006"))
	setIfAbsent("month", now.Format("01"))
	setIfAbsent("day", now.Format("02"))
	setIfAbsent("now", now.Format(time.RFC3339))
	setIfAbsent("user", v.resolveUser())
}

// resolveUser picks the best-effort current user name.
func (v *Values) resolveUser() string {
	if strings.TrimSpace(v.User) != "" {
		return v.User
	}
	if u, err := user.Current(); err == nil {
		if strings.TrimSpace(u.Username) != "" {
			return u.Username
		}
	}
	if u := strings.TrimSpace(os.Getenv("USER")); u != "" {
		return u
	}
	if u := strings.TrimSpace(os.Getenv("USERNAME")); u != "" {
		return u
	}
	return ""
}

// AutoKeys returns the set of built-in token names Autofill will
// populate. Exported so callers (Extract, tests) can classify tokens
// without magic strings sprinkled around.
func AutoKeys() []string {
	return []string{"date", "time", "datetime", "year", "month", "day", "now", "user"}
}

// IsAutoKey reports whether name is one of the built-in autofill tokens.
func IsAutoKey(name string) bool {
	switch strings.ToLower(name) {
	case "date", "time", "datetime", "year", "month", "day", "now", "user":
		return true
	}
	return false
}

// ValidName reports whether s is a syntactically valid token name.
func ValidName(s string) bool {
	return nameRE.MatchString(s)
}

// Extract returns the ordered, de-duplicated list of placeholders in
// body. Duplicate names are collapsed to the first occurrence; if
// multiple occurrences carry different inline defaults, the first
// non-empty default wins so later `{{name}}` bare uses inherit it.
//
// The returned tokens are decorated with AutoFilled / AutoValue against
// vals, so callers can decide which ones still need prompting. vals may
// be nil.
func Extract(body string, vals *Values) []Token {
	matches := tokenRE.FindAllStringSubmatchIndex(body, -1)
	if len(matches) == 0 {
		return nil
	}
	if vals == nil {
		vals = NewValues()
	}
	byName := map[string]*Token{}
	var order []string
	for _, m := range matches {
		// Skip escaped placeholders — a leading `\` means "render
		// literal". We can't unescape here (that's Render's job) but we
		// also shouldn't surface those to the caller as tokens to fill.
		if m[0] > 0 && body[m[0]-1] == '\\' {
			continue
		}
		name := strings.TrimSpace(body[m[2]:m[3]])
		if name == "" {
			continue
		}
		def := ""
		if m[4] >= 0 {
			def = strings.TrimSpace(body[m[4]:m[5]])
		}
		key := strings.ToLower(name)
		if existing, ok := byName[key]; ok {
			if existing.Default == "" && def != "" {
				existing.Default = def
			}
			continue
		}
		t := &Token{Name: name, Default: def}
		byName[key] = t
		order = append(order, key)
	}
	out := make([]Token, 0, len(order))
	for _, k := range order {
		t := byName[k]
		// Autofill classification: known auto keys are always autofilled;
		// otherwise a token counts as "autofilled" only if the caller has
		// already Set a value for it (e.g. via vars.yaml).
		switch {
		case vals.Has(t.Name):
			t.AutoFilled = true
			t.AutoValue = resolve(*t, vals)
		case IsAutoKey(t.Name):
			t.AutoFilled = true
			// Compute the auto value on the fly without mutating vals,
			// so a caller passing NewValues() sees a populated AutoValue
			// but their Values isn't silently side-effected.
			tmp := &Values{Now: vals.Now, User: vals.User, values: map[string]string{}}
			tmp.Autofill()
			t.AutoValue, _ = tmp.Get(strings.ToLower(t.Name))
		}
		out = append(out, *t)
	}
	return out
}

// MissingNames returns the names of tokens that Extract classified as
// not-yet-autofilled. Order matches Extract's insertion order.
func MissingNames(tokens []Token) []string {
	var out []string
	for _, t := range tokens {
		if !t.AutoFilled {
			out = append(out, t.Name)
		}
	}
	return out
}

// Render substitutes every `{{name}}` (or `{{name:default}}`) in body
// with the value from vals, falling back to the inline default. An
// escaped `\{{name}}` is emitted as `{{name}}` verbatim (backslash
// consumed, braces preserved).
//
// If a token has no value AND no default, the placeholder is left in
// the output untouched — this keeps `quipkit list` / `quipkit find`
// non-interactive output stable when the caller hasn't resolved
// prompts yet.
func Render(body string, vals *Values) string {
	if vals == nil {
		vals = NewValues()
	}
	// Fast path: no `{{` anywhere.
	if !strings.Contains(body, "{{") {
		return body
	}

	var b strings.Builder
	b.Grow(len(body))
	last := 0
	matches := tokenRE.FindAllStringSubmatchIndex(body, -1)
	for _, m := range matches {
		start, end := m[0], m[1]
		// Escape: preceding backslash strips itself, leaves braces literal.
		if start > 0 && body[start-1] == '\\' {
			b.WriteString(body[last : start-1]) // everything up to (but not) the backslash
			b.WriteString(body[start:end])      // literal `{{name}}`
			last = end
			continue
		}
		b.WriteString(body[last:start])

		name := strings.TrimSpace(body[m[2]:m[3]])
		def := ""
		if m[4] >= 0 {
			def = body[m[4]:m[5]]
		}
		tok := Token{Name: name, Default: def}
		if resolved, ok := resolveOK(tok, vals); ok {
			b.WriteString(resolved)
		} else {
			// Leave the original placeholder in place.
			b.WriteString(body[start:end])
		}
		last = end
	}
	b.WriteString(body[last:])
	return b.String()
}

// resolve returns the value tok should render to given vals, falling
// back to the inline default and finally to the empty string.
func resolve(tok Token, vals *Values) string {
	if v, ok := resolveOK(tok, vals); ok {
		return v
	}
	return ""
}

// resolveOK returns (value, true) if tok can be resolved (either from
// vals or from an inline default). Returns ("", false) when the token
// is entirely unknown and has no default.
func resolveOK(tok Token, vals *Values) (string, bool) {
	if vals != nil {
		if v, ok := vals.Get(tok.Name); ok {
			return v, true
		}
		// Case-insensitive fallback so `{{Name}}` matches a `name:` var.
		if v, ok := vals.Get(strings.ToLower(tok.Name)); ok {
			return v, true
		}
	}
	if strings.TrimSpace(tok.Default) != "" {
		return tok.Default, true
	}
	return "", false
}

// LoadVars merges values from an optional `vars.yaml` (or `.yml`) file
// living in dir into v. The parser understands the same tiny
// `key: value` grammar quipkit uses elsewhere; anything richer (nested
// objects, flow lists, multi-line scalars) is intentionally ignored.
//
// A missing file is not an error — LoadVars returns nil in that case so
// callers can call it unconditionally. Parse errors are surfaced so a
// broken vars file is visible.
//
// Existing entries in v are NOT overwritten: this lets Autofill or
// direct Set calls take precedence over shared defaults when both are
// present.
func (v *Values) LoadVars(dir string) error {
	if v.values == nil {
		v.values = map[string]string{}
	}
	path, ok := findVarsFile(dir)
	if !ok {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open vars file %s: %w", path, err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := sc.Text()
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, val, ok := splitVarKV(line)
		if !ok {
			return fmt.Errorf("%s:%d: expected `key: value`, got %q", path, lineNo, raw)
		}
		if !ValidName(k) {
			return fmt.Errorf("%s:%d: invalid var name %q", path, lineNo, k)
		}
		if _, exists := v.values[k]; exists {
			continue
		}
		v.values[k] = val
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read vars file %s: %w", path, err)
	}
	return nil
}

// findVarsFile picks the first existing vars file in dir. Both
// `vars.yaml` and `vars.yml` are honored (yaml preferred).
func findVarsFile(dir string) (string, bool) {
	candidates := []string{"vars.yaml", "vars.yml"}
	sort.Strings(candidates)
	for _, name := range candidates {
		p := dir + string(os.PathSeparator) + name
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

// splitVarKV parses `key: value` (or `key = value`), with optional
// surrounding quotes on the value.
func splitVarKV(line string) (string, string, bool) {
	sep := strings.IndexAny(line, ":=")
	if sep < 0 {
		return "", "", false
	}
	k := strings.TrimSpace(line[:sep])
	if k == "" {
		return "", "", false
	}
	v := strings.TrimSpace(line[sep+1:])
	// Strip trailing `# comment` when the value isn't quoted.
	if len(v) < 2 || (v[0] != '"' && v[0] != '\'') {
		if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
	}
	// Unquote.
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
	}
	return k, v, true
}
