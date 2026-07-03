package match

import (
	"testing"

	"github.com/rwrife/quipkit/internal/store"
)

// mkSnip is a small test helper.
func mkSnip(id, title string, tags []string, body string) store.Snippet {
	return store.Snippet{ID: id, Title: title, Tags: tags, Body: body}
}

// TestRankEmptyQuery ensures Rank is an identity when the query is blank.
func TestRankEmptyQuery(t *testing.T) {
	in := []store.Snippet{
		mkSnip("a", "Alpha", nil, "body a"),
		mkSnip("b", "Beta", nil, "body b"),
	}

	for _, q := range []string{"", "   ", "\t\n"} {
		got := Rank(q, in)
		if len(got) != len(in) {
			t.Fatalf("q=%q len=%d want %d", q, len(got), len(in))
		}
		for i := range in {
			if got[i].ID != in[i].ID {
				t.Fatalf("q=%q pos %d id=%s want %s", q, i, got[i].ID, in[i].ID)
			}
		}
	}
}

// TestRankNilAndEmptyInput covers the boundary cases.
func TestRankNilAndEmptyInput(t *testing.T) {
	if got := Rank("hi", nil); len(got) != 0 {
		t.Fatalf("nil in: got %d results", len(got))
	}
	if got := Rank("hi", []store.Snippet{}); len(got) != 0 {
		t.Fatalf("empty in: got %d results", len(got))
	}
}

// TestRankDropsNonMatches ensures snippets that don't hit any field are excluded.
func TestRankDropsNonMatches(t *testing.T) {
	in := []store.Snippet{
		mkSnip("addr", "Mailing address", []string{"address", "info"}, "123 Main St"),
		mkSnip("weather", "Weather chit-chat", []string{"smalltalk"}, "Nice day out"),
	}
	got := Rank("addr", in)
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(got), got)
	}
	if got[0].ID != "addr" {
		t.Fatalf("got id=%s want addr", got[0].ID)
	}
}

// TestRankTitleBeatsBody encodes the "title > body" weighting.
// Two snippets both contain "hello": one in the title, one only in the body.
// The title match must rank first.
func TestRankTitleBeatsBody(t *testing.T) {
	in := []store.Snippet{
		mkSnip("body-only", "Greeting", nil, "hello there friend"),
		mkSnip("title-hit", "Hello world", nil, "unrelated content"),
	}
	got := Rank("hello", in)
	if len(got) < 2 {
		t.Fatalf("got %d results, want 2: %+v", len(got), got)
	}
	if got[0].ID != "title-hit" {
		t.Fatalf("first id=%s want title-hit; full order=%v", got[0].ID, ids(got))
	}
}

// TestRankTagsBeatBody encodes the "tags > body" weighting.
func TestRankTagsBeatBody(t *testing.T) {
	in := []store.Snippet{
		mkSnip("bodytag", "Something", nil, "we support ssh keys here"),
		mkSnip("tagtag", "Something else", []string{"ssh"}, "unrelated"),
	}
	got := Rank("ssh", in)
	if len(got) < 2 {
		t.Fatalf("got %d results, want 2: %+v", len(got), got)
	}
	if got[0].ID != "tagtag" {
		t.Fatalf("first id=%s want tagtag; full order=%v", got[0].ID, ids(got))
	}
}

// TestRankTitleBeatsTags encodes the "title > tags" weighting.
func TestRankTitleBeatsTags(t *testing.T) {
	in := []store.Snippet{
		mkSnip("tagonly", "Something", []string{"vpn"}, "body"),
		mkSnip("titlehit", "VPN setup", []string{"network"}, "body"),
	}
	got := Rank("vpn", in)
	if len(got) < 2 {
		t.Fatalf("got %d results, want 2: %+v", len(got), got)
	}
	if got[0].ID != "titlehit" {
		t.Fatalf("first id=%s want titlehit; full order=%v", got[0].ID, ids(got))
	}
}

// TestRankTieBreak checks the deterministic tiebreak: identical scores
// resolve by shorter title, then by ID.
func TestRankTieBreak(t *testing.T) {
	// Two snippets with identical titles matching identically; ID decides.
	in := []store.Snippet{
		mkSnip("zeta", "hello", nil, ""),
		mkSnip("alpha", "hello", nil, ""),
	}
	got := Rank("hello", in)
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	if got[0].ID != "alpha" || got[1].ID != "zeta" {
		t.Fatalf("ID tiebreak wrong: %v", ids(got))
	}

	// Same score but different title lengths: shorter title wins.
	in = []store.Snippet{
		mkSnip("longtitle", "hello world", nil, ""),
		mkSnip("short", "hello", nil, ""),
	}
	got = Rank("hello", in)
	if got[0].ID != "short" {
		t.Fatalf("shorter-title tiebreak wrong: %v", ids(got))
	}
}

// TestRankDeterministic runs the same query twice and demands identical order.
func TestRankDeterministic(t *testing.T) {
	in := []store.Snippet{
		mkSnip("a", "hello alpha", []string{"greeting"}, "hi there"),
		mkSnip("b", "hello beta", []string{"greeting"}, "hi there"),
		mkSnip("c", "hello gamma", []string{"greeting"}, "hi there"),
	}
	got1 := Rank("hello", in)
	got2 := Rank("hello", in)
	if len(got1) != len(got2) {
		t.Fatalf("nondeterministic length %d vs %d", len(got1), len(got2))
	}
	for i := range got1 {
		if got1[i].ID != got2[i].ID {
			t.Fatalf("nondeterministic at %d: %s vs %s", i, got1[i].ID, got2[i].ID)
		}
	}
}

// TestExactTagBeatsFuzzyTitleDust confirms that a snippet whose tag is an
// exact match for the query outranks a snippet whose title only fuzzily
// contains the same characters. This encodes the token-bonus behavior.
func TestExactTagBeatsFuzzyTitleDust(t *testing.T) {
	in := []store.Snippet{
		// Fuzzy title "support" would match "s_u_p_ort"-style scattering,
		// but not on a whole-word boundary. The exact tag on the other snippet
		// must win.
		mkSnip("scattered", "Sunset ports", nil, "nothing here"),
		mkSnip("tagged", "Something", []string{"support"}, "nothing here"),
	}
	got := Rank("support", in)
	if len(got) == 0 || got[0].ID != "tagged" {
		t.Fatalf("exact tag should outrank fuzzy title dust; got=%v", ids(got))
	}
}

// TestExactTitleTokenBeatsExactTagToken confirms field-weight ordering still
// holds when both fields have a full-word match on the query.
func TestExactTitleTokenBeatsExactTagToken(t *testing.T) {
	in := []store.Snippet{
		mkSnip("tagexact", "Something", []string{"vpn"}, ""),
		mkSnip("titleexact", "vpn setup", []string{"network"}, ""),
	}
	got := Rank("vpn", in)
	if len(got) == 0 || got[0].ID != "titleexact" {
		t.Fatalf("exact title token should beat exact tag token; got=%v", ids(got))
	}
}

// TestRankTableCases is a table-driven scan of representative queries.
func TestRankTableCases(t *testing.T) {
	corpus := []store.Snippet{
		mkSnip("addr", "Mailing address", []string{"address", "info"}, "123 Main Street\nAnytown, USA"),
		mkSnip("email", "Support email", []string{"email", "contact"}, "support@example.com"),
		mkSnip("hours", "Business hours", []string{"schedule"}, "Mon-Fri 9-5"),
		mkSnip("phone", "Phone number", []string{"contact", "phone"}, "+1 555 0100"),
		mkSnip("welcome", "Welcome greeting", []string{"greeting"}, "Hello and welcome!"),
	}

	cases := []struct {
		name      string
		query     string
		wantFirst string   // "" = expect zero results; "*" = don't check first
		wantIn    []string // must all appear
	}{
		{"exact title", "address", "addr", []string{"addr"}},
		{"tag hit", "contact", "*", []string{"email", "phone"}},
		{"fuzzy title", "phn", "phone", []string{"phone"}},
		{"body hit only", "hello", "welcome", []string{"welcome"}},
		{"unmatched", "quantum-computer-linguistics-xyz", "", nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Rank(c.query, corpus)
			if c.wantFirst == "" {
				if len(got) != 0 {
					t.Fatalf("query=%q expected zero results, got %v", c.query, ids(got))
				}
				return
			}
			if len(got) == 0 {
				t.Fatalf("query=%q got zero results, want first=%s", c.query, c.wantFirst)
			}
			if c.wantFirst != "*" && got[0].ID != c.wantFirst {
				t.Fatalf("query=%q first=%s want %s (order=%v)",
					c.query, got[0].ID, c.wantFirst, ids(got))
			}
			for _, id := range c.wantIn {
				if !contains(ids(got), id) {
					t.Fatalf("query=%q missing %s (order=%v)", c.query, id, ids(got))
				}
			}
		})
	}
}

func ids(xs []store.Snippet) []string {
	out := make([]string, len(xs))
	for i, s := range xs {
		out[i] = s.ID
	}
	return out
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
