package clip

import (
	"errors"
	"testing"

	"github.com/atotto/clipboard"
)

// withCopier swaps the package-level Copier for the duration of the test,
// restoring it afterwards. The tests are single-goroutine so no lock.
func withCopier(t *testing.T, fn func(string) error) {
	t.Helper()
	prev := Copier
	Copier = fn
	t.Cleanup(func() { Copier = prev })
}

// withUnsupported temporarily flips atotto/clipboard's Unsupported flag
// so we can exercise the "no backend" path without uninstalling xclip.
func withUnsupported(t *testing.T, v bool) {
	t.Helper()
	prev := clipboard.Unsupported
	clipboard.Unsupported = v
	t.Cleanup(func() { clipboard.Unsupported = prev })
}

func TestCopy_UsesInjectedCopier(t *testing.T) {
	withUnsupported(t, false)
	var got string
	withCopier(t, func(s string) error { got = s; return nil })

	if err := Copy("hello world"); err != nil {
		t.Fatalf("Copy err = %v", err)
	}
	if got != "hello world" {
		t.Errorf("Copier saw %q, want %q", got, "hello world")
	}
}

func TestCopy_PropagatesCopierError(t *testing.T) {
	withUnsupported(t, false)
	sentinel := errors.New("boom")
	withCopier(t, func(string) error { return sentinel })

	err := Copy("x")
	if !errors.Is(err, sentinel) {
		t.Errorf("Copy err = %v, want %v", err, sentinel)
	}
}

func TestCopy_ReturnsUnavailableWhenNoBackend(t *testing.T) {
	withUnsupported(t, true)
	// Even if Copier would "work", Copy must not call it.
	withCopier(t, func(string) error {
		t.Fatal("Copier should not be called when backend unavailable")
		return nil
	})

	err := Copy("x")
	if err == nil {
		t.Fatal("Copy err = nil, want ErrUnavailable")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("errors.Is(err, ErrUnavailable) = false; err = %v", err)
	}
	// Message should include an install-hint substring (":" separator).
	if msg := err.Error(); len(msg) <= len(ErrUnavailable.Error()) {
		t.Errorf("error message %q missing install hint", msg)
	}
}

func TestAvailable_MatchesAtottoFlag(t *testing.T) {
	withUnsupported(t, false)
	if !Available() {
		t.Errorf("Available() = false, want true when Unsupported = false")
	}
	withUnsupported(t, true)
	if Available() {
		t.Errorf("Available() = true, want false when Unsupported = true")
	}
}
