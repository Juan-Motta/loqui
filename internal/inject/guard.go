// The pure half of the focus guard, and the serial paste queue. Kept apart from the AX
// and pasteboard code so the rules are testable without a running desktop — which is the
// same split the Electron build made, for the same reason.
package inject

import "sync"

// GuardInput is everything the decisions below need.
type GuardInput struct {
	Text string
	// SecureField is whether the focused element is provably a password field.
	SecureField bool
	// SessionApp is the frontmost app when dictation STARTED, "" if unknown.
	SessionApp string
	// CurrentApp is the frontmost app now, "" if unknown.
	CurrentApp string
}

// ShouldInjectInto decides whether this final may be pasted where focus currently is.
//
// The app comparison blocks only on a PROVEN mismatch — both names known and different.
// Treating "unknown" as a mismatch would refuse to paste whenever the AX read failed,
// which is the common case for the user who never granted Accessibility: they would see
// dictation silently produce nothing at all.
func ShouldInjectInto(in GuardInput) bool {
	if !ShouldInject(in.Text, in.SecureField) {
		return false
	}
	if in.SessionApp != "" && in.CurrentApp != "" && in.SessionApp != in.CurrentApp {
		return false
	}
	return true
}

// ShouldStoreFinal decides whether a final may be written to local history.
//
// Secure-field dictation is never stored. An app mismatch, by contrast, does NOT block
// storage: those words are still a transcript of what the user said, we simply had nowhere
// safe to paste them — and losing them entirely would be worse than the drift.
func ShouldStoreFinal(secureField bool) bool {
	return !secureField
}

// Queue serialises paste operations.
//
// Two pastes must never overlap: each one snapshots the clipboard, replaces it, and
// restores it, so interleaving them makes the second snapshot capture the FIRST one's
// text as if it were the user's clipboard — and the user ends up with dictation on their
// clipboard permanently, plus interleaved text at the cursor.
type Queue struct {
	mu sync.Mutex
	// tail keeps the ordering: each task waits for the previous one's channel.
	tail chan struct{}
}

func NewQueue() *Queue {
	done := make(chan struct{})
	close(done) // the first task has nothing to wait for
	return &Queue{tail: done}
}

// Do runs task after every previously enqueued task has finished, and returns once it has
// run. A panicking task must not wedge the queue, so the gate is released on the way out
// regardless.
func (q *Queue) Do(task func()) {
	mine := make(chan struct{})

	q.mu.Lock()
	prev := q.tail
	q.tail = mine
	q.mu.Unlock()

	<-prev
	defer close(mine)
	task()
}

// Go is Do without waiting: the caller hands over a task and moves on, ordering still
// guaranteed. This is what the session controller needs, since it must not block a
// provider callback while a paste completes.
func (q *Queue) Go(task func()) {
	mine := make(chan struct{})

	q.mu.Lock()
	prev := q.tail
	q.tail = mine
	q.mu.Unlock()

	go func() {
		<-prev
		defer close(mine)
		task()
	}()
}
