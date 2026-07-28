// The fn/Globe trigger: the compiled Swift helper and the parser for its output. Ported
// from the Electron build's src/shared/globeProtocol.ts and src/main/hotkey.ts.
//
// WHY A SEPARATE PROCESS AT ALL. fn is not an ordinary key. It never reaches a global
// shortcut API, on any platform — the only way to see it is a global .flagsChanged
// monitor, which needs the Input Monitoring grant and an AppKit run loop. The helper
// (helpers/macos-globe-listener.swift, vendored unchanged from OpenWhispr) does exactly
// that and nothing else, and reports over stdout.
//
// It is also the ONLY trigger that can report key RELEASE, which is what makes
// hold-to-talk possible at all. Every other shortcut is press-only, hence toggle-only.
package hotkey

import "strings"

// Event is a trigger event from the helper.
type Event string

const (
	// FnDown: the key went down. Start dictating.
	FnDown Event = "fnDown"
	// FnUp: the key came up. In hold mode, finish.
	FnUp Event = "fnUp"
	// FnInterrupt: another key was pressed WHILE fn was held — fn+arrow, or the user
	// started typing. That is a shortcut, not dictation, so the session is cancelled and
	// its audio discarded rather than transcribed as noise.
	FnInterrupt Event = "fnInterrupt"
)

// lineEvents maps the helper's wire protocol to our events. The helper emits more than
// this (right-modifier and mouse-button lines, which OpenWhispr used for its own
// triggers); anything unrecognised is ignored rather than treated as an error, so the
// helper can stay byte-identical to its upstream.
var lineEvents = map[string]Event{
	"FN_DOWN":        FnDown,
	"FN_UP":          FnUp,
	"FN_INTERRUPTED": FnInterrupt,
}

// ParseLine turns one line of the helper's stdout into an event, or "" when the line is
// not one we act on.
func ParseLine(line string) Event {
	return lineEvents[strings.TrimSpace(line)]
}
