// Package frecency tracks per-snippet usage counts and last-used
// timestamps and turns them into a lightweight ranking score.
//
// The state lives in a single JSON file (default: <snippet-dir>/.state.json
// so it travels with the snippet folder and is trivially git-ignorable).
// Design goals:
//
//   - Zero new dependencies. Standard library only.
//   - Cheap to read on every `quipkit` invocation and cheap to write on
//     every select. Snippet libraries are small (dozens to hundreds of
//     entries), so a full read/rewrite each time is fine.
//   - Deterministic and testable: no goroutines, no clocks hidden inside
//     the package — callers pass `time.Now()` into [Values.Record] and
//     the package exposes [Values.ScoreAt] so tests can pin a reference
//     time.
//
// The score itself is a classic frecency blend: usage count multiplied
// by an exponential recency decay. The half-life defaults to 14 days,
// which experimentally makes "yesterday, used twice" beat "last month,
// used ten times" without letting a single click permanently dominate.
package frecency

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// StateFilename is the base name of the state file written inside the
// snippet directory. The leading dot keeps it out of the way of
// `quipkit list`/`store.Load` (which only picks up `*.md`).
const StateFilename = ".state.json"

// DefaultHalfLife is the exponential half-life applied to a snippet's
// last-used timestamp when computing its recency score. Two weeks is
// long enough to survive "I didn't work last week" but short enough
// that stale favorites eventually get out of the way.
const DefaultHalfLife = 14 * 24 * time.Hour

// Entry is the per-snippet frecency record persisted to disk.
type Entry struct {
	// Count is the number of times the snippet has been selected.
	// Never negative; [Values.Record] only ever increments.
	Count int `json:"count"`
	// LastUsedUnix is the last-used timestamp as a Unix epoch seconds
	// value. Zero means "never used". Stored as an int so the JSON is
	// stable across machines/timezones.
	LastUsedUnix int64 `json:"last_used_unix"`
}

// Values is the in-memory view of the frecency state file. It's a plain
// struct on purpose — callers can hand it to [match.RankWithFrecency]
// or drive it directly via [Values.Score]/[Values.Top].
type Values struct {
	// Entries is keyed by snippet ID (the file base name without the
	// extension, matching [store.Snippet.ID]).
	Entries map[string]Entry `json:"entries"`
	// HalfLife is the recency decay half-life. Zero means "use
	// [DefaultHalfLife]"; the field is exposed so future config plumbing
	// can override it without changing the JSON schema.
	HalfLife time.Duration `json:"-"`
}

// NewValues returns an empty Values ready for [Values.Record] calls.
func NewValues() *Values {
	return &Values{Entries: map[string]Entry{}}
}

// Load reads the state file from dir. A missing file is not an error —
// Load returns an empty Values in that case. A malformed file is a hard
// error: silently discarding usage history would surprise users worse
// than a visible failure.
func Load(dir string) (*Values, error) {
	if dir == "" {
		return NewValues(), nil
	}
	path := filepath.Join(dir, StateFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewValues(), nil
		}
		return nil, fmt.Errorf("read frecency state %s: %w", path, err)
	}
	// Empty file is treated the same as missing so a truncation midway
	// through a previous write doesn't wedge future runs.
	if len(data) == 0 {
		return NewValues(), nil
	}
	v := NewValues()
	if err := json.Unmarshal(data, v); err != nil {
		return nil, fmt.Errorf("parse frecency state %s: %w", path, err)
	}
	if v.Entries == nil {
		v.Entries = map[string]Entry{}
	}
	return v, nil
}

// Save writes the state atomically (temp file + rename) to
// <dir>/<StateFilename>. Directories are not created — callers should
// ensure dir exists (it always does when reached from the CLI because
// [store.Seed] runs first). Save is a no-op when dir is empty so tests
// can opt out cleanly.
func (v *Values) Save(dir string) error {
	if v == nil || dir == "" {
		return nil
	}
	if v.Entries == nil {
		v.Entries = map[string]Entry{}
	}
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode frecency state: %w", err)
	}
	// Append a trailing newline so the file is diff-friendly if a user
	// ever pokes at it by hand.
	buf = append(buf, '\n')

	path := filepath.Join(dir, StateFilename)
	tmp, err := os.CreateTemp(dir, ".state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create frecency temp file: %w", err)
	}
	tmpPath := tmp.Name()
	// Best-effort cleanup on any failure path below.
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(buf); err != nil {
		tmp.Close()
		return fmt.Errorf("write frecency temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close frecency temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename frecency state: %w", err)
	}
	return nil
}

// Record bumps the usage count for id and stamps its last-used time to
// now. It's safe to call for an id that has never been seen before.
func (v *Values) Record(id string, now time.Time) {
	if v == nil || id == "" {
		return
	}
	if v.Entries == nil {
		v.Entries = map[string]Entry{}
	}
	e := v.Entries[id]
	e.Count++
	e.LastUsedUnix = now.Unix()
	v.Entries[id] = e
}

// Get returns the (possibly zero) entry for id.
func (v *Values) Get(id string) Entry {
	if v == nil || v.Entries == nil {
		return Entry{}
	}
	return v.Entries[id]
}

// Score returns the frecency score for id relative to time.Now().
// Convenience wrapper around [Values.ScoreAt]; production code should
// use this, tests should prefer [Values.ScoreAt] with a fixed clock.
func (v *Values) Score(id string) float64 {
	return v.ScoreAt(id, time.Now())
}

// ScoreAt returns the frecency score for id at the given reference
// time. The score is:
//
//	count * 2^(-age / halfLife)
//
// Never-used snippets score 0. Callers should treat a zero score as
// "no signal" (not as "the worst"), because the ordering it produces
// is only meaningful relative to other non-zero scores.
func (v *Values) ScoreAt(id string, now time.Time) float64 {
	e := v.Get(id)
	if e.Count == 0 || e.LastUsedUnix == 0 {
		return 0
	}
	halfLife := v.HalfLife
	if halfLife <= 0 {
		halfLife = DefaultHalfLife
	}
	last := time.Unix(e.LastUsedUnix, 0)
	age := now.Sub(last)
	if age < 0 {
		// Clock went backwards (NTP jump, timezone weirdness). Treat as
		// "used just now" — never negative decay.
		age = 0
	}
	decay := math.Exp2(-age.Seconds() / halfLife.Seconds())
	return float64(e.Count) * decay
}

// TopEntry is a single row of a "top snippets" ranking.
type TopEntry struct {
	ID       string
	Count    int
	LastUsed time.Time
	Score    float64
}

// TopAt returns up to n snippets ordered by their frecency score at the
// given reference time (highest first). Ties break on higher raw count,
// then more recent last-used, then snippet id ascending.
//
// Passing n <= 0 returns all known entries in the same order.
func (v *Values) TopAt(now time.Time, n int) []TopEntry {
	if v == nil || len(v.Entries) == 0 {
		return nil
	}
	out := make([]TopEntry, 0, len(v.Entries))
	for id, e := range v.Entries {
		if e.Count == 0 && e.LastUsedUnix == 0 {
			continue
		}
		out = append(out, TopEntry{
			ID:       id,
			Count:    e.Count,
			LastUsed: time.Unix(e.LastUsedUnix, 0),
			Score:    v.ScoreAt(id, now),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if !out[i].LastUsed.Equal(out[j].LastUsed) {
			return out[i].LastUsed.After(out[j].LastUsed)
		}
		return out[i].ID < out[j].ID
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

// Top is [Values.TopAt] using time.Now() as the reference.
func (v *Values) Top(n int) []TopEntry {
	return v.TopAt(time.Now(), n)
}
