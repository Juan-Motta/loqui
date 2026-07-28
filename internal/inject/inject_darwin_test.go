//go:build darwin

// Tests that touch the REAL system pasteboard, so they are opt-in:
//
//	LOQUI_TEST_CLIPBOARD=1 go test ./internal/inject/ -run Clipboard -v
//
// Opt-in because they overwrite whatever the person running them had copied. They put it
// back — that is precisely what is being tested — but a suite that silently rewrites your
// clipboard on every run is unacceptable, and CI has no pasteboard at all.
//
// This is CE5 of the PRD ("tras pegar, el portapapeles del usuario queda como estaba"),
// which the Electron build could only approximate. See pasteboard_darwin.go.
package inject

import (
	"os"
	"testing"
	"time"
)

func requireClipboardTests(t *testing.T) {
	t.Helper()
	if os.Getenv("LOQUI_TEST_CLIPBOARD") != "1" {
		t.Skip("set LOQUI_TEST_CLIPBOARD=1 to run tests that touch the real pasteboard")
	}
}

// noKeystroke keeps the suite from synthesising Cmd+V into whatever window the developer
// has focused. The clipboard contract is what is under test, not the keystroke.
func noKeystroke() Options {
	return Options{sendPaste: func() {}, Delay: 10 * time.Millisecond}
}

func TestClipboardIsRestoredAfterInjection(t *testing.T) {
	requireClipboardTests(t)

	const original = "loqui-test: the user's own clipboard"
	writeText(original)

	res := Text("loqui-test: dictated text", noKeystroke())

	if !res.Restored {
		t.Error("Restored = false, want true: nobody touched the clipboard, so it must go back")
	}
	if res.ClipboardKept {
		t.Error("ClipboardKept = true, but nothing else wrote to the pasteboard")
	}
	if got := readText(); got != original {
		t.Errorf("clipboard = %q, want the original %q", got, original)
	}
}

// The other half of the contract: when someone else copies during the paste window, THEIR
// content wins. Restoring would destroy something the user just copied — the failure the
// changeCount guard exists to prevent.
func TestClipboardIsLeftAloneWhenSomeoneElseWrites(t *testing.T) {
	requireClipboardTests(t)

	writeText("loqui-test: the user's own clipboard")
	const interloper = "loqui-test: copied by another app mid-paste"

	opts := noKeystroke()
	// Stand in for any other app writing to the pasteboard while the target reads ours.
	opts.Sleep = func(time.Duration) { writeText(interloper) }

	res := Text("loqui-test: dictated text", opts)

	if res.Restored {
		t.Error("Restored = true, but the clipboard had moved on — that overwrites the user's copy")
	}
	if !res.ClipboardKept {
		t.Error("ClipboardKept = false, want true so the app can tell the user")
	}
	if got := readText(); got != interloper {
		t.Errorf("clipboard = %q, want the interloper's %q left intact", got, interloper)
	}
}

// An empty clipboard is a normal starting state (fresh login), and restoring "nothing" must
// not leave our dictation behind as if the user had copied it.
func TestClipboardRestoreFromEmpty(t *testing.T) {
	requireClipboardTests(t)

	snap := snapshotPasteboard()
	snap.restore() // clears to whatever an empty snapshot means
	snap.release()

	res := Text("loqui-test: dictated text", noKeystroke())
	if !res.Restored {
		t.Error("Restored = false, want true")
	}
	if got := readText(); got == "loqui-test: dictated text" {
		t.Error("the dictated text was left on the clipboard")
	}
}
