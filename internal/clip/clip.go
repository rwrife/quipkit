// Package clip is quipkit's tiny cross-platform clipboard wrapper.
//
// It delegates the actual OS work to github.com/atotto/clipboard (which
// picks pbcopy on macOS, clip.exe on Windows, and xclip/xsel/wl-copy/
// termux-clipboard-set on Linux), then adds two things on top:
//
//  1. A stable [Copy] entrypoint with a nicer error when no backend is
//     available on the system (typical for a fresh headless Linux box
//     with no xclip/xsel/wl-clipboard installed).
//  2. An [Available] check the CLI can call before attempting a copy so
//     it can fall back gracefully (e.g. print the body to stdout with a
//     hint about how to install a clipboard tool).
//
// Copy is deliberately overridable via the exported [Copier] variable so
// tests (and, later, an --auto-type mode) can inject their own behavior
// without shelling out to a real clipboard.
package clip

import (
	"errors"
	"runtime"

	"github.com/atotto/clipboard"
)

// ErrUnavailable is returned by [Copy] when no clipboard backend was
// found at process start. On Linux this typically means none of xclip,
// xsel, wl-copy, or termux-clipboard-set are installed.
var ErrUnavailable = errors.New("clipboard: no backend available")

// Copier is the function [Copy] actually calls. It defaults to the real
// atotto/clipboard write, but tests can swap it out. Reassigning this
// variable is only safe from a single goroutine (typically the CLI
// startup path), which matches how the tests use it.
var Copier func(string) error = clipboard.WriteAll

// Copy writes text to the system clipboard.
//
// If no backend is available, it returns [ErrUnavailable] wrapped with a
// platform hint so the caller can surface actionable advice to the user
// without having to know the difference between macOS/Windows/Linux.
func Copy(text string) error {
	if !Available() {
		return unavailableError()
	}
	if err := Copier(text); err != nil {
		return err
	}
	return nil
}

// Available reports whether a clipboard backend is present. It is safe
// to call before [Copy] to decide whether to fall back (for example, by
// printing the snippet body to stdout instead).
//
// It reflects the atotto/clipboard init-time probe, so a value of false
// won't spontaneously flip to true during the process's lifetime.
func Available() bool {
	return !clipboard.Unsupported
}

// unavailableError returns ErrUnavailable wrapped with an OS-specific
// install hint. Kept small on purpose: the exact command list is what
// most users need, not an essay.
func unavailableError() error {
	var hint string
	switch runtime.GOOS {
	case "linux":
		hint = "install one of xclip, xsel, or wl-clipboard (e.g. `sudo apt install xclip`)"
	case "darwin":
		// pbcopy ships with macOS; if we got here something is very odd.
		hint = "pbcopy is missing — try `xcode-select --install`"
	case "windows":
		hint = "clip.exe / powershell must be on PATH"
	default:
		hint = "no supported clipboard tool found for this OS"
	}
	return &unavailableErr{hint: hint}
}

// unavailableErr wraps [ErrUnavailable] with an install hint. It uses a
// small custom type so callers can still do `errors.Is(err, ErrUnavailable)`.
type unavailableErr struct{ hint string }

func (e *unavailableErr) Error() string {
	return ErrUnavailable.Error() + ": " + e.hint
}
func (e *unavailableErr) Unwrap() error { return ErrUnavailable }
