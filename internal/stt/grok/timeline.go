package grok

import (
	"sort"
	"strings"
)

// The transcript being assembled, as a timeline of timed words.
//
// WHY THE PROVIDER ASSEMBLES INSTEAD OF STREAMING FINALS. The session controller only ever
// APPENDS the finals it receives (internal/session/controller.go:293, internal/history:40), so
// a provider that emits text progressively can never take anything back. Grok needs to take
// things back: it re-sends an utterance it already sent in chunks ("complete stitched
// utterance"), it can correct what it already sent, and its terminal transcript.done may
// legitimately repeat the whole session, only the tail, or nothing at all — the docs do not
// say which, and the official example shows an EMPTY one after 6.43 s of speech.
//
// So the provider keeps the timeline and emits ONE final on the way out.
//
// WHY WORDS AND NOT SEGMENTS. The first design stored whole text segments and dropped any
// segment whose span overlapped a new event. That loses text whenever a stored segment spans a
// silence: words at [0,1) and [4,5) have a hull of [0,5), so a later event at [2,3) deleted
// both. Replacement is per word, so only what the server is genuinely restating disappears.
//
// INTERVALS ARE HALF-OPEN, [start, end). The API emits adjacent words where one ends exactly
// where the next begins ("The" 0.24→0.48, "balance" 0.48→0.96); with closed intervals every
// word would delete its neighbour.
//
// The whole thing rests on one documented fact: word times are "seconds from stream start",
// i.e. session-relative. There is no multi-utterance example in the docs to confirm it — see
// risk 1 in docs/plans/grok-stt-provider.md.
type timeline struct {
	entries []entry
	// seq breaks ties between words with identical timestamps, so assembly is deterministic
	// rather than dependent on sort implementation.
	seq int
}

type entry struct {
	word
	seq int
}

// commitResult says what a commit did, so the caller can log the interesting cases without
// the timeline needing a logger.
type commitResult int

const (
	// commitApplied: words were committed normally.
	commitApplied commitResult = iota
	// commitNothing: the event carried no text at all (the documented empty transcript.done).
	commitNothing
	// commitIgnoredNoEvidence: text with no word times AND no span, while we already hold
	// word-timed text. Dropped on purpose — see commit.
	commitIgnoredNoEvidence
	// commitUsedAsFallback: text with no positional evidence, accepted because the timeline
	// was empty and it is all we have.
	commitUsedAsFallback
)

func (r commitResult) String() string {
	switch r {
	case commitNothing:
		return "nothing"
	case commitIgnoredNoEvidence:
		return "ignored-no-evidence"
	case commitUsedAsFallback:
		return "used-as-fallback"
	default:
		return "applied"
	}
}

// commit folds one final-bearing event into the timeline, replacing whatever it overlaps.
func (t *timeline) commit(c commit) commitResult {
	words := nonBlank(c.Words)

	// No per-word times. Fall back to the event's own span, treating its text as one run.
	if len(words) == 0 {
		if strings.TrimSpace(c.Text) == "" {
			return commitNothing
		}
		if c.HasSpan {
			words = []word{{Start: c.Start, End: c.Start + c.Duration, Text: c.Text}}
		} else {
			// NO POSITIONAL EVIDENCE AT ALL — this is transcript.done with `words: []`, which
			// carries no top-level start either.
			//
			// No string rule can be correct here. With "I agree" already committed, a done of
			// "I agree again" (a tail) and one of "I disagree" (a correction) are
			// indistinguishable, so prefix matching gets one right and duplicates the other.
			// Keeping the word-timed timeline can miss a final correction but can never
			// duplicate, and duplication is the silent failure — so that is the trade taken.
			if !t.empty() {
				return commitIgnoredNoEvidence
			}
			// Nothing committed yet: this is the genuinely short utterance, where audio was
			// briefer than the chunk window and done is the only text-bearing event.
			t.insert([]word{{Start: 0, End: c.Duration, Text: c.Text}})
			return commitUsedAsFallback
		}
	}

	if c.FullRestatement {
		// Authoritative for its whole span: clear the span, then insert. This is what makes a
		// RETRACTION expressible — "a b c" restated as "a c" has to lose "b", and "b" overlaps
		// neither incoming word, so per-word replacement alone could never remove it.
		start, end := authoritativeSpan(c, words)
		t.removeWithin(start, end)
	} else {
		t.removeOverlappedBy(words)
	}
	t.insert(words)
	return commitApplied
}

// authoritativeSpan is the region a full restatement is entitled to clear: the span the event
// DECLARED, widened to include its own words.
//
// NOT just the surviving words' extent. A retraction at either END sits outside it — an utterance
// covering [0,3) that restates only "b c" has retracted "a", and the hull [1,3) would never even
// consider it. The declared span is what says how much of the timeline this utterance speaks for.
//
// Widened by the words because the two do not always agree, and a word past the declared end must
// not survive its own restatement.
//
// transcript.done carries NO declared span, so it falls back to its words' extent — which is what
// keeps it safe despite the docs never saying whether it repeats the whole session or just the
// tail: a tail-only done then clears only the tail.
func authoritativeSpan(c commit, words []word) (float64, float64) {
	start, end := span(words)
	if c.HasSpan {
		if c.Start < start {
			start = c.Start
		}
		if declaredEnd := c.Start + c.Duration; declaredEnd > end {
			end = declaredEnd
		}
	}
	return start, end
}

// removeWithin clears everything inside an authoritative span.
func (t *timeline) removeWithin(start, end float64) {
	kept := t.entries[:0]
	for _, e := range t.entries {
		if e.Start < end && e.End > start {
			continue
		}
		kept = append(kept, e)
	}
	t.entries = kept
}

// span is the extent a set of words covers. Taken from the WORDS, not from the event's declared
// start+duration: the two do not always agree, and trusting the declared span is how a word just
// past the previous event's stated end gets dropped.
func span(words []word) (float64, float64) {
	start, end := words[0].Start, words[0].End
	for _, w := range words[1:] {
		if w.Start < start {
			start = w.Start
		}
		if w.End > end {
			end = w.End
		}
	}
	return start, end
}

// removeOverlappedBy drops the stored words the incoming event is restating.
//
// PER INCOMING WORD, NOT THEIR HULL. Using the extent of the whole event would delete anything
// sitting in a silence INSIDE it: incoming words at [0,1) and [4,5) have a hull of [0,5), which
// would take out a stored word at [2,3) that nothing incoming actually overlaps. Only what is
// genuinely being restated disappears.
//
// Half-open comparison, because the API emits adjacent words where one ends exactly where the
// next begins; with closed intervals every word would delete its neighbour.
//
// This is the INCREMENTAL rule, for chunk finals. A restatement that retracts a word needs the
// span cleared instead — see removeWithin, and the FullRestatement field for the signal that
// tells the two apart.
func (t *timeline) removeOverlappedBy(words []word) {
	kept := t.entries[:0] // in-place filter; each read precedes its write, so it cannot alias
	for _, e := range t.entries {
		if overlapsAny(e.word, words) {
			continue // the server is replacing this
		}
		kept = append(kept, e)
	}
	t.entries = kept
}

func overlapsAny(stored word, incoming []word) bool {
	for _, in := range incoming {
		if stored.Start < in.End && stored.End > in.Start {
			return true
		}
	}
	return false
}

func (t *timeline) insert(words []word) {
	for _, w := range words {
		t.seq++
		t.entries = append(t.entries, entry{word: w, seq: t.seq})
	}
	// Stable by (start, end, arrival): two words at the same instant keep the order the
	// server sent them in, every run.
	sort.SliceStable(t.entries, func(i, j int) bool {
		a, b := t.entries[i], t.entries[j]
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		if a.End != b.End {
			return a.End < b.End
		}
		return a.seq < b.seq
	})
}

func (t *timeline) empty() bool { return len(t.entries) == 0 }

// text assembles the message. Joined with single spaces, which is the same convention
// history.JoinTranscript uses for a session's segments.
func (t *timeline) text() string {
	parts := make([]string, 0, len(t.entries))
	for _, e := range t.entries {
		if s := strings.TrimSpace(e.Text); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

func nonBlank(words []word) []word {
	out := make([]word, 0, len(words))
	for _, w := range words {
		if strings.TrimSpace(w.Text) != "" {
			out = append(out, w)
		}
	}
	return out
}
