package app

import (
	"errors"
	"strings"
	"testing"
)

func TestCopyingPassesTheTextThrough(t *testing.T) {
	var got string
	svc := &ClipboardService{copy: func(s string) error { got = s; return nil }}

	if msg := svc.Copy("Hola Hola Hola."); msg != "" {
		t.Fatalf("Copy reported %q", msg)
	}
	if got != "Hola Hola Hola." {
		t.Errorf("the clipboard received %q", got)
	}
}

// A failure comes back as a MESSAGE, not a Go error: Wails drops a bound method's result when it
// also returns an error, and the copy button has to say what happened on the button itself.
func TestCopyingReportsTheFailureAsAMessage(t *testing.T) {
	svc := &ClipboardService{copy: func(string) error { return errors.New("el portapapeles no respondió") }}

	msg := svc.Copy("algo")
	if msg == "" {
		t.Fatal("a failed copy reported success")
	}
	if !strings.Contains(msg, "portapapeles") {
		t.Errorf("message = %q, want it to name what failed", msg)
	}
}

// The real copier refuses blank text, and the service must surface that rather than swallow it.
//
// Why it matters: a copy button that clears the clipboard because the row it belonged to was empty
// destroys whatever the user had copied, to accomplish nothing.
func TestCopyingNothingIsRefused(t *testing.T) {
	called := false
	svc := &ClipboardService{copy: func(string) error {
		called = true
		return errors.New("inject: nothing to copy")
	}}

	if msg := svc.Copy("   "); msg == "" {
		t.Error("copying blank text reported success")
	}
	if !called {
		t.Error("the service short-circuited; the refusal belongs to the one place that owns the clipboard")
	}
}
