// Package typeit is quipkit's tiny cross-platform keystroke-injection
// wrapper. It's the "auto-type" counterpart to [github.com/rwrife/quipkit/internal/clip]:
// same shape (mockable [Typer], stable [Type] entrypoint, [Available]
// probe), different backend.
//
// Rather than pull in a heavy CGO/robot binding — which would break
// quipkit's "single static binary, no deps" promise from PLAN.md — we
// shell out to the platform-native tool that's almost certainly already
// present (or a `brew install` / `apt install` away):
//
//   - macOS   → `osascript` (System keystroke via AppleScript). Requires
//     Accessibility permission for the terminal that ran quipkit.
//   - Linux   → the first of `wtype` (Wayland), `ydotool`, or `xdotool`
//     found on PATH. Wayland-first because most modern desktops have
//     migrated; xdotool still wins on classic X11 boxes.
//   - Windows → PowerShell's `System.Windows.Forms.SendKeys.SendWait`
//     via `powershell.exe`. No install required.
//
// The picked backend is captured at [Detect] time (called from [Type]
// and [Available]) so the CLI can print a helpful "install one of X"
// hint before even trying to type.
//
// A configurable per-keystroke delay (see [Type]) works around finicky
// targets that eat characters when fed too fast (looking at you, some
// Electron chat apps). 0 means "as fast as the backend goes".
package typeit

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ErrUnavailable is returned by [Type] when no keystroke-injection
// backend was found for the current OS. On Linux this typically means
// none of wtype / ydotool / xdotool are installed; the error message
// includes an install hint tailored to the platform.
var ErrUnavailable = errors.New("typeit: no backend available")

// Backend describes the tool that will actually inject keystrokes.
// The zero value ("") means no backend is available.
type Backend struct {
	// Name is a short, human-friendly identifier ("xdotool", "wtype",
	// "osascript", "powershell"). Empty when no backend is available.
	Name string
	// Path is the absolute path to the executable that Detect resolved.
	// Empty when no backend is available.
	Path string
	// OS is the runtime.GOOS the backend was picked for. Retained so
	// callers can differentiate "backend just went away" from "we're
	// running on an unsupported OS entirely".
	OS string
}

// Available reports whether a keystroke backend is present on this
// machine. It's safe to call cheaply (it caches after the first probe)
// so the CLI can decide up-front whether to fall back to clipboard
// mode or abort with an install hint before even opening the picker.
func Available() bool {
	return Detect().Name != ""
}

// Detect probes the OS for a supported keystroke-injection backend and
// returns the first one it finds. The result is cached — probing shells
// out to `exec.LookPath`, which is cheap but not free, and the answer
// won't change during a single quipkit run.
//
// Detect is safe to call before [Type] and is also what powers
// [Available]. When no backend is found, it returns the zero [Backend].
func Detect() Backend {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cachedProbed {
		return cached
	}
	cached = probe()
	cachedProbed = true
	return cached
}

// ResetDetectCache clears the [Detect] cache. Tests use this after
// swapping [Lookup] to force a fresh probe; production code has no
// reason to call it.
func ResetDetectCache() {
	cacheMu.Lock()
	cached = Backend{}
	cachedProbed = false
	cacheMu.Unlock()
}

// Options tune a single [Type] call.
type Options struct {
	// Delay is the pause between keystrokes. 0 means "no explicit
	// delay" — the backend will type as fast as it can. Values under
	// a millisecond are clamped up to 1ms because none of the backends
	// meaningfully honor sub-millisecond delays.
	Delay time.Duration
}

// Typer is the function [Type] actually calls once a backend has been
// resolved. Tests (and, in the future, alternate front-ends) can
// override it without touching the shell-out logic.
//
// Reassigning Typer is only safe from a single goroutine (typically
// CLI startup or a test's setup), mirroring how [clip.Copier] works.
var Typer func(Backend, string, Options) error = shellType

// Lookup is used by [Detect] to find backend executables on PATH.
// Tests swap it out with an in-memory table so probing is deterministic
// and doesn't depend on what happens to be installed on the CI runner.
var Lookup func(string) (string, error) = exec.LookPath

// Type injects text as keystrokes into whatever window currently has
// focus. It blocks until the backend has finished typing (or errored).
//
// If no backend is available it returns [ErrUnavailable] wrapped with a
// platform-specific install hint, so callers can surface actionable
// advice without needing to reason about macOS/Linux/Windows themselves.
//
// Typing is inherently side-effecting on the user's desktop — Type
// deliberately does *not* try to be smart about focus, timing, or
// retries beyond the configured per-keystroke delay. If the wrong
// window catches the input, that's a user-scheduling problem, not one
// this package can fix.
func Type(text string, opts Options) error {
	if text == "" {
		return nil
	}
	if opts.Delay > 0 && opts.Delay < time.Millisecond {
		opts.Delay = time.Millisecond
	}
	backend := Detect()
	if backend.Name == "" {
		return unavailableError()
	}
	return Typer(backend, text, opts)
}

// -----------------------------------------------------------------------------
// internals
// -----------------------------------------------------------------------------

// cached / cacheMu memoize Detect's result so repeated calls (which
// happen naturally on the CLI hot path: Available() → Type()) don't
// shell out to LookPath again.
var (
	cached       Backend
	cachedProbed bool
	cacheMu      sync.Mutex
)

// probe walks the per-OS backend priority list and returns the first
// entry whose executable is on PATH. It's the only place that hard-codes
// backend names/paths.
func probe() Backend {
	goos := runtime.GOOS
	for _, cand := range candidates(goos) {
		path, err := Lookup(cand)
		if err == nil && path != "" {
			return Backend{Name: cand, Path: path, OS: goos}
		}
	}
	return Backend{}
}

// candidates returns the ordered list of executables to try for a given
// GOOS. The order is deliberate:
//
//   - Linux tries Wayland-native `wtype` first (modern default), then
//     `ydotool` (Wayland+X11, needs uinput perms), then `xdotool`
//     (classic X11, ubiquitous on older distros).
//   - macOS uses `osascript`; there's no realistic alternative worth
//     probing.
//   - Windows uses `powershell` (SendKeys). We prefer `pwsh` (PS 7+)
//     when present so users on modern installs don't drag in the
//     legacy Windows PowerShell.
func candidates(goos string) []string {
	switch goos {
	case "linux":
		return []string{"wtype", "ydotool", "xdotool"}
	case "darwin":
		return []string{"osascript"}
	case "windows":
		return []string{"pwsh", "powershell", "powershell.exe"}
	default:
		return nil
	}
}

// shellType runs the resolved backend with args tailored to its CLI.
// Each backend gets its own tiny adapter — kept in one function so the
// mapping stays readable and easy to audit.
func shellType(b Backend, text string, opts Options) error {
	switch b.Name {
	case "xdotool":
		args := []string{"type", "--clearmodifiers"}
		if opts.Delay > 0 {
			// xdotool's --delay is milliseconds.
			args = append(args, "--delay", fmt.Sprintf("%d", opts.Delay/time.Millisecond))
		}
		args = append(args, "--", text)
		return runCmd(b.Path, args, nil)
	case "ydotool":
		args := []string{"type"}
		if opts.Delay > 0 {
			args = append(args, "--next-delay", fmt.Sprintf("%d", opts.Delay/time.Millisecond))
		}
		args = append(args, "--", text)
		return runCmd(b.Path, args, nil)
	case "wtype":
		args := []string{}
		if opts.Delay > 0 {
			// wtype takes -d in milliseconds between keystrokes.
			args = append(args, "-d", fmt.Sprintf("%d", opts.Delay/time.Millisecond))
		}
		// "--" tells wtype "everything after this is text, not a flag".
		args = append(args, "--", text)
		return runCmd(b.Path, args, nil)
	case "osascript":
		// AppleScript's `keystroke` handles arbitrary text; we quote it
		// safely and set the per-keystroke delay via `key delay`.
		script := buildOsascript(text, opts.Delay)
		return runCmd(b.Path, []string{"-e", script}, nil)
	case "pwsh", "powershell", "powershell.exe":
		script := buildPowerShellScript(text, opts.Delay)
		return runCmd(b.Path, []string{
			"-NoLogo", "-NoProfile", "-NonInteractive",
			"-Command", script,
		}, nil)
	default:
		return fmt.Errorf("typeit: unknown backend %q", b.Name)
	}
}

// runCmd is a hook so tests can stub out the actual exec.Cmd call
// without spawning real processes.
var runCmd = func(bin string, args []string, env []string) error {
	cmd := exec.Command(bin, args...)
	if len(env) > 0 {
		cmd.Env = append(cmd.Env, env...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Include the tool's stderr in the wrapped error — otherwise
		// permission problems ("not authorized to send keystrokes")
		// vanish and users get a bare exit-status message.
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("%s: %w", bin, err)
		}
		return fmt.Errorf("%s: %w: %s", bin, err, msg)
	}
	return nil
}

// buildOsascript constructs an AppleScript that types `text` via System
// Events. Delays are converted to seconds (AppleScript's unit) with a
// small floor so 0 → no delay setting at all.
func buildOsascript(text string, delay time.Duration) string {
	// Quote for AppleScript: escape backslash and double-quote.
	escaped := strings.ReplaceAll(text, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	if delay <= 0 {
		return fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, escaped)
	}
	secs := float64(delay) / float64(time.Second)
	return fmt.Sprintf(
		`tell application "System Events"
	set key delay to %f
	keystroke "%s"
end tell`, secs, escaped)
}

// buildPowerShellScript constructs a SendKeys invocation that types
// `text` after loading the WinForms assembly. Delay is per-keystroke,
// implemented by sending one character at a time with Start-Sleep in
// between — SendKeys itself has no built-in throttle.
func buildPowerShellScript(text string, delay time.Duration) string {
	// PS single-quoted strings only need doubling of single quotes.
	psQuote := func(s string) string {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	if delay <= 0 {
		return fmt.Sprintf(
			`Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.SendKeys]::SendWait(%s)`,
			psQuote(text),
		)
	}
	ms := delay / time.Millisecond
	return fmt.Sprintf(
		`Add-Type -AssemblyName System.Windows.Forms; `+
			`foreach ($c in %s.ToCharArray()) { `+
			`[System.Windows.Forms.SendKeys]::SendWait([string]$c); `+
			`Start-Sleep -Milliseconds %d `+
			`}`,
		psQuote(text), ms,
	)
}

// unavailableError returns [ErrUnavailable] wrapped with a per-OS
// install hint. Kept short on purpose — most users just need to know
// what command to run.
func unavailableError() error {
	var hint string
	switch runtime.GOOS {
	case "linux":
		hint = "install one of wtype (Wayland), ydotool, or xdotool (X11) — e.g. `sudo apt install xdotool`"
	case "darwin":
		hint = "osascript ships with macOS; if it's missing, reinstall Command Line Tools"
	case "windows":
		hint = "powershell.exe / pwsh must be on PATH"
	default:
		hint = "no supported keystroke tool for this OS"
	}
	return &unavailableErr{hint: hint}
}

type unavailableErr struct{ hint string }

func (e *unavailableErr) Error() string { return ErrUnavailable.Error() + ": " + e.hint }
func (e *unavailableErr) Unwrap() error { return ErrUnavailable }
