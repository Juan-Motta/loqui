// The active-session tracker. Ported from the Electron build's
// src/shared/sessionTracker.ts.
//
// WHY GENERATIONS EXIST. A recognizer that has been told to stop keeps talking for a
// while: a WebSocket takes time to close, a helper process still has buffered audio to
// transcribe, a reconnect leaves the previous connection alive until it times out. Those
// late events are indistinguishable from live ones by content alone.
//
// So every session mints a generation, every event carries the generation it belongs to,
// and anything stale is dropped. Without this, the tail of one dictation gets pasted
// into the next one — the user sees words they said thirty seconds ago appear in the
// wrong window.
package session

// Tracker owns the current generation and whether dictation is wanted.
type Tracker struct {
	gen       int
	discarded map[int]bool
	// desired is what the USER wants, as opposed to what the engine is currently
	// doing. The two diverge constantly — during startup, during teardown, during a
	// reconnect — and conflating them is how a session ends up half-running.
	desired bool
}

func NewTracker() *Tracker {
	return &Tracker{discarded: make(map[int]bool)}
}

// Start begins a new session and returns its generation.
func (t *Tracker) Start() int {
	t.desired = true
	t.gen++
	return t.gen
}

// Stop marks dictation as no longer wanted. The generation stays, so events still in
// flight can be recognised as belonging to the session that just ended.
func (t *Tracker) Stop() { t.desired = false }

// Bump advances the generation while staying active. Used for reconnects: the failed
// recognizer's late events become stale immediately, so its trailing `stopped` cannot be
// mistaken for the new attempt finishing.
func (t *Tracker) Bump() int {
	t.gen++
	return t.gen
}

func (t *Tracker) Generation() int { return t.gen }
func (t *Tracker) Desired() bool   { return t.desired }

// Discard marks a generation's transcript as unwanted (an interrupted dictation).
func (t *Tracker) Discard(gen int) { t.discarded[gen] = true }

// Accepts reports whether a transcript from gen may be used: it must be the current
// generation and must not have been discarded.
func (t *Tracker) Accepts(gen int) bool {
	return gen == t.gen && !t.discarded[gen]
}
