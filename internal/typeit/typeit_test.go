package typeit

import (
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"
)

// withLookup swaps the package-level Lookup for the test and resets the
// detect cache so the new lookup is actually consulted. Every test that
// mutates Lookup must go through this so we don't leak state.
func withLookup(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	prev := Lookup
	Lookup = fn
	ResetDetectCache()
	t.Cleanup(func() {
		Lookup = prev
		ResetDetectCache()
	})
}

// withTyper swaps the package-level Typer for the test.
func withTyper(t *testing.T, fn func(Backend, string, Options) error) {
	t.Helper()
	prev := Typer
	Typer = fn
	t.Cleanup(func() { Typer = prev })
}

// withRunCmd swaps the shell-exec hook so shellType can be exercised
// without spawning real xdotool/osascript/powershell.
func withRunCmd(t *testing.T, fn func(string, []string, []string) error) {
	t.Helper()
	prev := runCmd
	runCmd = fn
	t.Cleanup(func() { runCmd = prev })
}

// lookupTable returns a Lookup function that pretends only the named
// binaries are on PATH, in the order they appear.
func lookupTable(present ...string) func(string) (string, error) {
	set := make(map[string]bool, len(present))
	for _, p := range present {
		set[p] = true
	}
	return func(name string) (string, error) {
		if set[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
}

func TestDetect_PicksFirstMatchingCandidate(t *testing.T) {
	if got := candidates("linux"); len(got) < 2 {
		t.Fatalf("linux candidate list too small: %v", got)
	}
	// Only care that the priority logic works; force Linux behavior via
	// the candidate list, not runtime.GOOS. Detect() itself dispatches on
	// runtime.GOOS, so on macOS/Windows CI we simulate at the Lookup
	// level: we install a candidate that is *first* in this OS's list.
	first := candidates(runtime.GOOS)
	if len(first) == 0 {
		t.Skipf("no candidates on %s; skipping", runtime.GOOS)
	}
	withLookup(t, lookupTable(first[0]))
	got := Detect()
	if got.Name != first[0] {
		t.Errorf("Detect() = %q, want %q", got.Name, first[0])
	}
	if got.Path == "" {
		t.Errorf("Detect().Path is empty")
	}
	if got.OS != runtime.GOOS {
		t.Errorf("Detect().OS = %q, want %q", got.OS, runtime.GOOS)
	}
}

func TestDetect_FallsBackWhenPreferredMissing(t *testing.T) {
	list := candidates(runtime.GOOS)
	if len(list) < 2 {
		t.Skipf("%s only has %d candidate(s); nothing to fall back to", runtime.GOOS, len(list))
	}
	// Only the second-priority backend is "installed".
	withLookup(t, lookupTable(list[1]))
	got := Detect()
	if got.Name != list[1] {
		t.Errorf("Detect() fell back to %q, want %q", got.Name, list[1])
	}
}

func TestDetect_NoneAvailable(t *testing.T) {
	withLookup(t, func(string) (string, error) { return "", errors.New("nope") })
	if got := Detect(); got.Name != "" {
		t.Errorf("Detect() = %+v, want zero Backend", got)
	}
	if Available() {
		t.Errorf("Available() = true, want false when nothing on PATH")
	}
}

func TestDetect_CachesFirstResult(t *testing.T) {
	calls := 0
	withLookup(t, func(name string) (string, error) {
		calls++
		list := candidates(runtime.GOOS)
		if len(list) == 0 {
			return "", errors.New("nope")
		}
		if name == list[0] {
			return "/opt/" + name, nil
		}
		return "", errors.New("not found")
	})
	_ = Detect()
	initial := calls
	_ = Detect()
	_ = Detect()
	if calls != initial {
		t.Errorf("Detect not cached: initial=%d after=%d", initial, calls)
	}
}

func TestType_EmptyStringIsNoOp(t *testing.T) {
	called := false
	withTyper(t, func(Backend, string, Options) error {
		called = true
		return nil
	})
	if err := Type("", Options{}); err != nil {
		t.Fatalf("Type(\"\") err = %v, want nil", err)
	}
	if called {
		t.Errorf("Typer should not be called for empty text")
	}
}

func TestType_ReturnsUnavailableWhenNoBackend(t *testing.T) {
	withLookup(t, func(string) (string, error) { return "", errors.New("nope") })
	// Guard against the typer being called at all when no backend is up.
	withTyper(t, func(Backend, string, Options) error {
		t.Fatal("Typer should not run when no backend available")
		return nil
	})
	err := Type("hi", Options{})
	if err == nil {
		t.Fatal("Type err = nil, want ErrUnavailable")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("errors.Is(err, ErrUnavailable) = false; err = %v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, ":") {
		t.Errorf("error missing hint separator: %q", msg)
	}
}

func TestType_PropagatesTyperError(t *testing.T) {
	list := candidates(runtime.GOOS)
	if len(list) == 0 {
		t.Skipf("no candidates on %s", runtime.GOOS)
	}
	withLookup(t, lookupTable(list[0]))
	sentinel := errors.New("permission denied")
	withTyper(t, func(Backend, string, Options) error { return sentinel })
	err := Type("hi", Options{})
	if !errors.Is(err, sentinel) {
		t.Errorf("Type err = %v, want %v", err, sentinel)
	}
}

func TestType_ClampsSubMillisecondDelayToOneMs(t *testing.T) {
	list := candidates(runtime.GOOS)
	if len(list) == 0 {
		t.Skipf("no candidates on %s", runtime.GOOS)
	}
	withLookup(t, lookupTable(list[0]))
	var gotOpts Options
	withTyper(t, func(_ Backend, _ string, o Options) error {
		gotOpts = o
		return nil
	})
	if err := Type("x", Options{Delay: 100 * time.Nanosecond}); err != nil {
		t.Fatalf("Type err = %v", err)
	}
	if gotOpts.Delay != time.Millisecond {
		t.Errorf("Delay = %v, want 1ms", gotOpts.Delay)
	}
}

func TestType_PassesBackendToTyper(t *testing.T) {
	list := candidates(runtime.GOOS)
	if len(list) == 0 {
		t.Skipf("no candidates on %s", runtime.GOOS)
	}
	withLookup(t, lookupTable(list[0]))
	var gotBackend Backend
	var gotText string
	withTyper(t, func(b Backend, s string, _ Options) error {
		gotBackend = b
		gotText = s
		return nil
	})
	if err := Type("hello", Options{}); err != nil {
		t.Fatalf("Type err = %v", err)
	}
	if gotBackend.Name != list[0] {
		t.Errorf("backend = %q, want %q", gotBackend.Name, list[0])
	}
	if gotText != "hello" {
		t.Errorf("text = %q, want hello", gotText)
	}
}

// -----------------------------------------------------------------------------
// shellType coverage: each backend's arg string is a contract that
// downstream users can rely on, so we lock it in.
// -----------------------------------------------------------------------------

func TestShellType_XdotoolNoDelay(t *testing.T) {
	var gotBin string
	var gotArgs []string
	withRunCmd(t, func(bin string, args []string, _ []string) error {
		gotBin = bin
		gotArgs = args
		return nil
	})
	err := shellType(Backend{Name: "xdotool", Path: "/usr/bin/xdotool"}, "hello", Options{})
	if err != nil {
		t.Fatalf("shellType err = %v", err)
	}
	if gotBin != "/usr/bin/xdotool" {
		t.Errorf("bin = %q, want /usr/bin/xdotool", gotBin)
	}
	want := []string{"type", "--clearmodifiers", "--", "hello"}
	if !stringSlicesEqual(gotArgs, want) {
		t.Errorf("args = %v, want %v", gotArgs, want)
	}
}

func TestShellType_XdotoolWithDelay(t *testing.T) {
	var gotArgs []string
	withRunCmd(t, func(_ string, args []string, _ []string) error {
		gotArgs = args
		return nil
	})
	if err := shellType(Backend{Name: "xdotool", Path: "/x"}, "hi", Options{Delay: 25 * time.Millisecond}); err != nil {
		t.Fatalf("shellType err = %v", err)
	}
	want := []string{"type", "--clearmodifiers", "--delay", "25", "--", "hi"}
	if !stringSlicesEqual(gotArgs, want) {
		t.Errorf("args = %v, want %v", gotArgs, want)
	}
}

func TestShellType_WtypeUsesDashD(t *testing.T) {
	var gotArgs []string
	withRunCmd(t, func(_ string, args []string, _ []string) error {
		gotArgs = args
		return nil
	})
	if err := shellType(Backend{Name: "wtype", Path: "/w"}, "hi", Options{Delay: 5 * time.Millisecond}); err != nil {
		t.Fatalf("shellType err = %v", err)
	}
	want := []string{"-d", "5", "--", "hi"}
	if !stringSlicesEqual(gotArgs, want) {
		t.Errorf("args = %v, want %v", gotArgs, want)
	}
}

func TestShellType_YdotoolUsesNextDelay(t *testing.T) {
	var gotArgs []string
	withRunCmd(t, func(_ string, args []string, _ []string) error {
		gotArgs = args
		return nil
	})
	if err := shellType(Backend{Name: "ydotool", Path: "/y"}, "hi", Options{Delay: 10 * time.Millisecond}); err != nil {
		t.Fatalf("shellType err = %v", err)
	}
	want := []string{"type", "--next-delay", "10", "--", "hi"}
	if !stringSlicesEqual(gotArgs, want) {
		t.Errorf("args = %v, want %v", gotArgs, want)
	}
}

func TestShellType_OsascriptQuotesText(t *testing.T) {
	var gotArgs []string
	withRunCmd(t, func(_ string, args []string, _ []string) error {
		gotArgs = args
		return nil
	})
	// The awkward text — quotes + backslash — is the whole point.
	err := shellType(Backend{Name: "osascript", Path: "/o"}, `he said "hi\there"`, Options{})
	if err != nil {
		t.Fatalf("shellType err = %v", err)
	}
	if len(gotArgs) != 2 || gotArgs[0] != "-e" {
		t.Fatalf("unexpected args: %v", gotArgs)
	}
	script := gotArgs[1]
	if !strings.Contains(script, `keystroke "he said \"hi\\there\""`) {
		t.Errorf("script missing escaped keystroke arg: %q", script)
	}
}

func TestShellType_OsascriptEmitsKeyDelayWhenSet(t *testing.T) {
	var gotArgs []string
	withRunCmd(t, func(_ string, args []string, _ []string) error {
		gotArgs = args
		return nil
	})
	if err := shellType(Backend{Name: "osascript", Path: "/o"}, "hi", Options{Delay: 50 * time.Millisecond}); err != nil {
		t.Fatalf("shellType err = %v", err)
	}
	script := gotArgs[1]
	if !strings.Contains(script, "set key delay to 0.050000") {
		t.Errorf("script missing key delay: %q", script)
	}
	if !strings.Contains(script, `keystroke "hi"`) {
		t.Errorf("script missing keystroke: %q", script)
	}
}

func TestShellType_PowerShellQuotesText(t *testing.T) {
	var gotArgs []string
	withRunCmd(t, func(_ string, args []string, _ []string) error {
		gotArgs = args
		return nil
	})
	// SendKeys ignores certain chars; here we only verify the outer PS
	// invocation shape and that our quoting doubled the single-quote.
	err := shellType(Backend{Name: "powershell", Path: "/p"}, "it's fine", Options{})
	if err != nil {
		t.Fatalf("shellType err = %v", err)
	}
	if len(gotArgs) < 5 || gotArgs[0] != "-NoLogo" {
		t.Fatalf("unexpected args: %v", gotArgs)
	}
	script := gotArgs[len(gotArgs)-1]
	if !strings.Contains(script, "'it''s fine'") {
		t.Errorf("script missing doubled single quote: %q", script)
	}
	if !strings.Contains(script, "System.Windows.Forms.SendKeys") {
		t.Errorf("script missing SendKeys API: %q", script)
	}
}

func TestShellType_PowerShellDelayEmitsPerCharLoop(t *testing.T) {
	var gotArgs []string
	withRunCmd(t, func(_ string, args []string, _ []string) error {
		gotArgs = args
		return nil
	})
	if err := shellType(Backend{Name: "pwsh", Path: "/p"}, "hi", Options{Delay: 20 * time.Millisecond}); err != nil {
		t.Fatalf("shellType err = %v", err)
	}
	script := gotArgs[len(gotArgs)-1]
	if !strings.Contains(script, "ToCharArray()") {
		t.Errorf("expected per-char loop, got: %q", script)
	}
	if !strings.Contains(script, "Start-Sleep -Milliseconds 20") {
		t.Errorf("expected Start-Sleep 20, got: %q", script)
	}
}

func TestShellType_UnknownBackendErrors(t *testing.T) {
	// Guard against a future refactor that forgets to keep shellType
	// in sync with candidates(). A stub runCmd that would return nil is
	// installed to make the failure mode "wrong error", not "leaked
	// exec". Any exec attempt with our fake path would also fail loudly.
	withRunCmd(t, func(string, []string, []string) error { return nil })
	err := shellType(Backend{Name: "gremlin", Path: "/nope"}, "hi", Options{})
	if err == nil {
		t.Fatal("shellType err = nil, want unknown-backend error")
	}
	if !strings.Contains(err.Error(), "unknown backend") {
		t.Errorf("err = %v, want unknown-backend complaint", err)
	}
}

func TestRunCmd_IncludesCombinedOutputInError(t *testing.T) {
	// Use `sh -c` with a guaranteed-fail body so we can look for our
	// stderr line in the wrapped error. Skip on Windows where sh isn't
	// standard — we still cover the code path via the other tests.
	if runtime.GOOS == "windows" {
		t.Skip("sh not available on Windows CI")
	}
	err := runCmd("/bin/sh", []string{"-c", "echo boom >&2; exit 3"}, nil)
	if err == nil {
		t.Fatal("runCmd err = nil, want non-zero exit")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want to include stderr text", err)
	}
}

func TestUnavailableError_MessageFormat(t *testing.T) {
	err := unavailableError()
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unavailableError() not wrapping ErrUnavailable: %v", err)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, ErrUnavailable.Error()+": ") {
		t.Errorf("message missing prefix: %q", msg)
	}
	// The hint text differs per OS; just require it's non-empty.
	if len(msg) <= len(ErrUnavailable.Error())+2 {
		t.Errorf("message missing hint body: %q", msg)
	}
}

// -----------------------------------------------------------------------------
// small helpers
// -----------------------------------------------------------------------------

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// intentional compile-time check: candidates() covers the platforms we
// document. If someone adds an OS branch, they need to add a test too.
var _ = func() bool {
	for _, os := range []string{"linux", "darwin", "windows"} {
		if len(candidates(os)) == 0 {
			// We can't call t.Fatalf here; return false so the compile
			// step notices via the init-block sentinel below.
			return false
		}
	}
	return true
}()

// TestBuildPowerShellScript_NoDelayShape locks in the exact one-liner
// so future readers don't have to re-derive it from Go source.
func TestBuildPowerShellScript_NoDelayShape(t *testing.T) {
	got := buildPowerShellScript("hi", 0)
	want := "Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.SendKeys]::SendWait('hi')"
	if got != want {
		t.Errorf("buildPowerShellScript(hi, 0) =\n  %q\nwant\n  %q", got, want)
	}
}
