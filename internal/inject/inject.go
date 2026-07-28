// Text injection at the focused app's cursor. Ported from the Electron build's
// src/main/injection.ts, with the clipboard ownership check replaced by the real thing
// (see pasteboard_darwin.go).
//
// The sequence, and why each step is where it is:
//
//  1. Snapshot the clipboard, remembering its change count.
//  2. Write our text. Remember the change count that produced.
//  3. Press Cmd+V.
//  4. Wait briefly for the target app to consume the paste. It reads the clipboard
//     asynchronously, so restoring immediately can put the old contents back BEFORE the
//     app has read ours — which pastes the user's previous clipboard into their document.
//  5. Restore ONLY if the change count is still ours. If it moved, somebody else wrote to
//     the clipboard during the window and their content is now the user's expectation:
//     overwriting it would destroy something they just copied.
package inject

import (
	"strings"
	"time"
)

// restoreDelay is how long the target app gets to read the clipboard before we put the
// user's content back. Carried over from the Electron build, where it was tuned against
// real apps.
const restoreDelay = 150 * time.Millisecond

// Result reports what happened, for the log and for telling the user when their clipboard
// was deliberately left alone.
type Result struct {
	// Pasted is whether the keystroke was sent.
	Pasted bool
	// Restored is whether the previous clipboard was put back.
	Restored bool
	// ClipboardKept is true when the restore was SKIPPED because someone else wrote to
	// the clipboard during the paste. Not an error: it means we chose not to destroy
	// what the user copied. Worth surfacing, because their clipboard is not what it was
	// before dictation.
	ClipboardKept bool
}

// ShouldInject reports whether this text may be injected at all.
//
// A secure text field is never pasted into. Doing so would push the user's spoken words
// into a password box — which then very plausibly gets saved to a keychain or submitted.
func ShouldInject(text string, secureField bool) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	return !secureField
}

// ShouldRestoreClipboard decides step 5: only when the clipboard still holds exactly what
// we put there.
//
// The comparison is on the change count we OBSERVED after writing, not on content.
// Content comparison is what Electron had to do without this counter, and it cannot
// distinguish "still ours" from "someone copied the identical text".
func ShouldRestoreClipboard(ours, current int64) bool {
	return ours == current
}

// Options allows the tests to replace the parts that touch the machine.
type Options struct {
	// Sleep defaults to time.Sleep.
	Sleep func(time.Duration)
	// Delay defaults to restoreDelay.
	Delay time.Duration
}

// Text injects text at the cursor of whatever app has focus.
//
// The restore runs even when the paste keystroke fails, which is why it is not simply the
// last statement: without the Accessibility grant Cmd+V is silently swallowed, and
// leaving the user's clipboard replaced by dictation on top of that would be a second
// failure caused by the first.
func Text(text string, opts Options) Result {
	sleep := opts.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	delay := opts.Delay
	if delay == 0 {
		delay = restoreDelay
	}

	prev := snapshotPasteboard()
	defer prev.release()

	ours := writeText(text)
	sendPasteKeystroke()
	sleep(delay)

	res := Result{Pasted: true}
	if ShouldRestoreClipboard(ours, currentChangeCount()) {
		prev.restore()
		res.Restored = true
	} else {
		res.ClipboardKept = true
	}
	return res
}
