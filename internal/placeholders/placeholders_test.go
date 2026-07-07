package placeholders

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestExtract_NoPlaceholders(t *testing.T) {
	got := Extract("just some text with no tokens", nil)
	if got != nil {
		t.Errorf("Extract() = %v, want nil", got)
	}
}

func TestExtract_SimpleToken(t *testing.T) {
	got := Extract("Hi {{name}}!", NewValues())
	if len(got) != 1 {
		t.Fatalf("got %d tokens, want 1", len(got))
	}
	if got[0].Name != "name" {
		t.Errorf("Name = %q", got[0].Name)
	}
	if got[0].AutoFilled {
		t.Errorf("AutoFilled = true, want false for unknown token")
	}
}

func TestExtract_AutoFilledKnownTokens(t *testing.T) {
	got := Extract("Today is {{date}} at {{time}}, hello {{user}}", NewValues())
	if len(got) != 3 {
		t.Fatalf("got %d tokens, want 3", len(got))
	}
	for _, tk := range got {
		if !tk.AutoFilled {
			t.Errorf("token %q should be AutoFilled", tk.Name)
		}
		if tk.AutoValue == "" {
			t.Errorf("token %q AutoValue is empty", tk.Name)
		}
	}
}

func TestExtract_InlineDefault(t *testing.T) {
	got := Extract("Hi {{name:friend}}!", NewValues())
	if len(got) != 1 {
		t.Fatalf("got %d tokens, want 1", len(got))
	}
	if got[0].Default != "friend" {
		t.Errorf("Default = %q, want %q", got[0].Default, "friend")
	}
	// Inline default alone doesn't make a token AutoFilled (still user-facing).
	if got[0].AutoFilled {
		t.Errorf("AutoFilled = true, want false (inline default is a fallback, not an autofill)")
	}
}

func TestExtract_DeduplicatesPreservingFirstDefault(t *testing.T) {
	body := "Hi {{name}}, welcome {{name:pal}}. {{name}} again."
	got := Extract(body, NewValues())
	if len(got) != 1 {
		t.Fatalf("got %d unique tokens, want 1", len(got))
	}
	if got[0].Default != "pal" {
		t.Errorf("Default = %q, want %q (first non-empty default wins)", got[0].Default, "pal")
	}
}

func TestExtract_IgnoresEscapedTokens(t *testing.T) {
	body := `Literal \{{name}} and real {{name}}.`
	got := Extract(body, NewValues())
	if len(got) != 1 {
		t.Fatalf("got %d tokens, want 1", len(got))
	}
	if got[0].Name != "name" {
		t.Errorf("Name = %q", got[0].Name)
	}
}

func TestExtract_LeavesRealMarkdownAlone(t *testing.T) {
	// `{{ not a placeholder }}` contains a space and would not match
	// our identifier regex; make sure we don't grab it.
	body := "before {{ nope this has spaces }} after"
	got := Extract(body, NewValues())
	if got != nil {
		t.Errorf("Extract() = %v, want nil", got)
	}
}

func TestExtract_MarksVarsValueAsAutoFilled(t *testing.T) {
	vals := NewValues()
	vals.Set("signature", "-- Ryan")
	got := Extract("Cheers,\n{{signature}}", vals)
	if len(got) != 1 {
		t.Fatalf("got %d tokens", len(got))
	}
	if !got[0].AutoFilled {
		t.Error("signature should be autofilled from vals")
	}
	if got[0].AutoValue != "-- Ryan" {
		t.Errorf("AutoValue = %q", got[0].AutoValue)
	}
}

func TestRender_SubstitutesAndKeepsUnknown(t *testing.T) {
	vals := NewValues()
	vals.Set("name", "Ryan")
	body := "Hi {{name}}, your token {{missing}} stays."
	out := Render(body, vals)
	want := "Hi Ryan, your token {{missing}} stays."
	if out != want {
		t.Errorf("Render() = %q\nwant %q", out, want)
	}
}

func TestRender_UsesInlineDefaultWhenAbsent(t *testing.T) {
	body := "Hi {{name:friend}}!"
	out := Render(body, NewValues())
	if out != "Hi friend!" {
		t.Errorf("Render() = %q, want %q", out, "Hi friend!")
	}
}

func TestRender_ValuesBeatInlineDefault(t *testing.T) {
	vals := NewValues()
	vals.Set("name", "Ryan")
	body := "Hi {{name:friend}}!"
	if out := Render(body, vals); out != "Hi Ryan!" {
		t.Errorf("Render() = %q, want %q", out, "Hi Ryan!")
	}
}

func TestRender_EscapedTokenBecomesLiteral(t *testing.T) {
	vals := NewValues()
	vals.Set("name", "Ryan")
	body := `Literal \{{name}} vs live {{name}}.`
	want := "Literal {{name}} vs live Ryan."
	if out := Render(body, vals); out != want {
		t.Errorf("Render() = %q\nwant %q", out, want)
	}
}

func TestRender_AutofillDate(t *testing.T) {
	vals := NewValues()
	vals.Now = time.Date(2026, 7, 6, 10, 30, 0, 0, time.UTC)
	vals.Autofill()
	body := "Signed on {{date}} at {{time}}."
	want := "Signed on 2026-07-06 at 10:30."
	if out := Render(body, vals); out != want {
		t.Errorf("Render() = %q\nwant %q", out, want)
	}
}

func TestRender_UserOverridesEmpty(t *testing.T) {
	vals := NewValues()
	vals.User = "ryan"
	vals.Autofill()
	body := "hi from {{user}}"
	if out := Render(body, vals); out != "hi from ryan" {
		t.Errorf("Render() = %q", out)
	}
}

func TestRender_FastPathNoBraces(t *testing.T) {
	body := "no placeholders here at all"
	if out := Render(body, NewValues()); out != body {
		t.Errorf("Render() = %q, want unchanged", out)
	}
}

func TestRender_NilValues(t *testing.T) {
	body := "Hi {{name:friend}}, {{unknown}}!"
	// nil vals must not panic; inline default applies, unknown stays literal.
	out := Render(body, nil)
	want := "Hi friend, {{unknown}}!"
	if out != want {
		t.Errorf("Render() = %q\nwant %q", out, want)
	}
}

func TestMissingNames(t *testing.T) {
	vals := NewValues()
	vals.Autofill()
	tokens := Extract("A {{date}} B {{name}} C {{team:eng}} D", vals)
	missing := MissingNames(tokens)
	// date is autofilled; name and team are user-facing (inline default counts as "missing" — needs a prompt or explicit accept).
	want := []string{"name", "team"}
	if !reflect.DeepEqual(missing, want) {
		t.Errorf("MissingNames = %v, want %v", missing, want)
	}
}

func TestIsAutoKey(t *testing.T) {
	for _, k := range AutoKeys() {
		if !IsAutoKey(k) {
			t.Errorf("IsAutoKey(%q) = false, want true", k)
		}
	}
	if IsAutoKey("something") {
		t.Error("IsAutoKey(\"something\") = true, want false")
	}
	// Case-insensitive.
	if !IsAutoKey("DATE") {
		t.Error("IsAutoKey(\"DATE\") should be true")
	}
}

func TestValidName(t *testing.T) {
	valid := []string{"name", "user_id", "team-lead", "v2", "obj.name"}
	invalid := []string{"", "has space", "bang!", "hi/there"}
	for _, s := range valid {
		if !ValidName(s) {
			t.Errorf("ValidName(%q) = false, want true", s)
		}
	}
	for _, s := range invalid {
		if ValidName(s) {
			t.Errorf("ValidName(%q) = true, want false", s)
		}
	}
}

func TestLoadVars_MissingFileIsNoError(t *testing.T) {
	dir := t.TempDir()
	v := NewValues()
	if err := v.LoadVars(dir); err != nil {
		t.Errorf("LoadVars missing file: %v", err)
	}
}

func TestLoadVars_ReadsAndMerges(t *testing.T) {
	dir := t.TempDir()
	body := "name: Ryan\nsignature: '-- Ryan'\nteam: eng # comment\n# a comment line\n"
	if err := os.WriteFile(filepath.Join(dir, "vars.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	v := NewValues()
	if err := v.LoadVars(dir); err != nil {
		t.Fatalf("LoadVars: %v", err)
	}
	tests := map[string]string{
		"name":      "Ryan",
		"signature": "-- Ryan",
		"team":      "eng",
	}
	for k, want := range tests {
		got, ok := v.Get(k)
		if !ok {
			t.Errorf("missing key %q", k)
			continue
		}
		if got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestLoadVars_DoesNotOverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	body := "name: FromFile\n"
	if err := os.WriteFile(filepath.Join(dir, "vars.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	v := NewValues()
	v.Set("name", "FromCode")
	if err := v.LoadVars(dir); err != nil {
		t.Fatal(err)
	}
	if got, _ := v.Get("name"); got != "FromCode" {
		t.Errorf("name = %q, want %q (existing must win)", got, "FromCode")
	}
}

func TestLoadVars_BadLineErrors(t *testing.T) {
	dir := t.TempDir()
	body := "this is not a key value pair\n"
	if err := os.WriteFile(filepath.Join(dir, "vars.yaml"), []byte(body), 0o644); err == nil {
		if err := (NewValues()).LoadVars(dir); err == nil {
			t.Fatal("LoadVars: expected error for malformed line")
		}
	}
}

func TestAutofillDoesNotClobberExisting(t *testing.T) {
	v := NewValues()
	v.Set("user", "override")
	v.Autofill()
	if got, _ := v.Get("user"); got != "override" {
		t.Errorf("user = %q, want %q", got, "override")
	}
}

func TestRender_CaseInsensitiveMatchToLowerKeyedVars(t *testing.T) {
	v := NewValues()
	v.Set("name", "Ryan")
	body := "Hi {{Name}}"
	if out := Render(body, v); !strings.HasPrefix(out, "Hi Ryan") {
		t.Errorf("Render = %q", out)
	}
}

func TestExtract_MultiToken_OrderPreserved(t *testing.T) {
	body := "{{a}} {{b}} {{c}} {{a}}"
	got := Extract(body, NewValues())
	if len(got) != 3 {
		t.Fatalf("got %d tokens, want 3", len(got))
	}
	names := []string{got[0].Name, got[1].Name, got[2].Name}
	if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Errorf("order = %v", names)
	}
}
