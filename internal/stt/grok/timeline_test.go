package grok

import "testing"

// finalWith builds the commit a CHUNK final produces (is_final=true, speech_final=false):
// incremental, so it replaces only the words it actually overlaps.
func finalWith(start, duration float64, words ...word) commit {
	return commit{Words: words, Start: start, Duration: duration, HasSpan: true}
}

// utteranceWith builds the commit an UTTERANCE final produces (speech_final=true), which the
// docs call the "complete stitched utterance" — authoritative for its whole span.
func utteranceWith(start, duration float64, words ...word) commit {
	c := finalWith(start, duration, words...)
	c.FullRestatement = true
	return c
}

func w(start, end float64, text string) word {
	return word{Start: start, End: end, Text: text}
}

func assembled(t *testing.T, tl *timeline, want string) {
	t.Helper()
	if got := tl.text(); got != want {
		t.Errorf("assembled = %q, want %q", got, want)
	}
}

func TestTimelineJoinsNonOverlappingFinalsInOrder(t *testing.T) {
	tl := &timeline{}
	tl.commit(finalWith(0, 1.5, w(0, 0.5, "hola"), w(0.5, 1.5, "mundo")))
	tl.commit(finalWith(2, 1.0, w(2, 3, "otra")))

	assembled(t, tl, "hola mundo otra")
}

// THE CASE THAT KILLED THE PREVIOUS DESIGN. Storing whole segments and replacing any segment
// whose hull overlapped the new event meant a segment spanning a silence gap — words at [0,1)
// and [4,5), hull [0,5) — was deleted entirely by a later event at [2,3), losing two
// perfectly good words. Replacement is per WORD for exactly this reason.
func TestTimelineKeepsWordsAcrossASilenceGap(t *testing.T) {
	tl := &timeline{}
	tl.commit(finalWith(0, 5, w(0, 1, "principio"), w(4, 5, "final")))
	tl.commit(finalWith(2, 1, w(2, 3, "medio")))

	assembled(t, tl, "principio medio final")
}

// THE SAME GAP, THE OTHER WAY AROUND. Here the INCOMING event straddles a silence: words at
// [0,1) and [4,5). Deleting everything between them — the convex hull [0,5) — throws away a
// stored word at [2,3) that no incoming word actually overlaps.
//
// This is why replacement compares against each incoming word's own interval and never against
// their hull. The mirror-image test above passes either way, which is exactly how this slipped
// through the first time.
func TestTimelineDoesNotDeleteAcrossAnIncomingGap(t *testing.T) {
	tl := &timeline{}
	tl.commit(finalWith(2, 1, w(2, 3, "medio")))
	tl.commit(finalWith(0, 5, w(0, 1, "principio"), w(4, 5, "final")))

	assembled(t, tl, "principio medio final")
}

// The server re-sends an utterance it already sent in chunks ("complete stitched utterance").
// Appending it would duplicate every sentence longer than ~3 s.
func TestTimelineReplacesAStitchedRepeat(t *testing.T) {
	tl := &timeline{}
	tl.commit(finalWith(0, 3.2, w(0.2, 1.0, "el"), w(1.0, 3.2, "saldo")))
	// Same span, re-sent as one stitched result.
	tl.commit(finalWith(0, 3.2, w(0.2, 3.2, "el saldo")))

	assembled(t, tl, "el saldo")
}

// A RETRACTION inside an utterance. The server restates a span it already sent, but this time
// with FEWER words: "a b c" becomes "a c" because it decided "b" was noise.
//
// Per-word replacement alone cannot express this — "b" overlaps nothing incoming, so it would
// survive as stale text. What makes it decidable is `speech_final`: the docs call that event the
// "complete stitched utterance", i.e. authoritative for its whole span, so the span is cleared
// before the new words go in. A chunk final (speech_final=false) is incremental and must NOT do
// this, which is why the two are distinguished rather than treated alike.
func TestTimelineDropsRetractedWordsOnAStitchedUtterance(t *testing.T) {
	tl := &timeline{}
	tl.commit(finalWith(0, 3, w(0, 1, "a"), w(1, 2, "b"), w(2, 3, "c")))
	tl.commit(utteranceWith(0, 3, w(0, 1, "a"), w(2, 3, "c")))

	assembled(t, tl, "a c")
}

// A LEADING retraction. The utterance covered [0,3) and now restates only "b c" — so "a" was
// retracted, and it sits OUTSIDE the surviving words' extent.
//
// This is why an authoritative event clears its DECLARED span and not the hull of whatever
// survived: with the hull ([1,3)) "a" is never even considered. Same for a trailing retraction.
func TestTimelineDropsALeadingRetraction(t *testing.T) {
	tl := &timeline{}
	tl.commit(finalWith(0, 3, w(0, 1, "a"), w(1, 2, "b"), w(2, 3, "c")))
	tl.commit(utteranceWith(0, 3, w(1, 2, "b"), w(2, 3, "c")))

	assembled(t, tl, "b c")
}

func TestTimelineDropsATrailingRetraction(t *testing.T) {
	tl := &timeline{}
	tl.commit(finalWith(0, 3, w(0, 1, "a"), w(1, 2, "b"), w(2, 3, "c")))
	tl.commit(utteranceWith(0, 3, w(0, 1, "a"), w(1, 2, "b")))

	assembled(t, tl, "a b")
}

// The declared span can also UNDERSTATE what the event carries — the two do not always agree, and
// a word past the declared end must not be left behind by its own restatement. So the span
// cleared is the union of the declared one and the words' own extent.
func TestTimelineAuthoritativeSpanCoversWordsPastTheDeclaredEnd(t *testing.T) {
	tl := &timeline{}
	tl.commit(finalWith(0, 4, w(3, 4, "viejo")))
	// Declared span stops at 3, but a word reaches 4 — the union has to include it.
	tl.commit(utteranceWith(0, 3, w(0, 1, "nuevo"), w(3, 4, "NUEVO")))

	assembled(t, tl, "nuevo NUEVO")
}

// The same shape as a CHUNK final must stay incremental: a chunk covers ~3 s of an ongoing
// utterance and says nothing about what it did not mention, so it must not clear the gaps
// between its own words.
func TestTimelineChunkFinalStaysIncremental(t *testing.T) {
	tl := &timeline{}
	tl.commit(finalWith(1, 1, w(1, 2, "medio")))
	tl.commit(finalWith(0, 3, w(0, 1, "a"), w(2, 3, "c")))

	assembled(t, tl, "a medio c")
}

// transcript.done being authoritative must NOT let it wipe session text it never restated. The
// span it clears comes from its OWN words, so a tail-only done clears only the tail region and
// everything earlier survives. This is the case that makes "authoritative" safe despite the docs
// never saying whether done repeats the whole session or just the tail.
func TestTimelineTailOnlyDoneKeepsTheEarlierSession(t *testing.T) {
	tl := &timeline{}
	tl.commit(finalWith(0, 3, w(0, 1, "uno"), w(1, 2, "dos"), w(2, 3, "tres")))
	// done flushes only what was left: one word, after everything already committed.
	tl.commit(commit{
		Words:           []word{w(3, 4, "cuatro")},
		Duration:        4,
		FullRestatement: true,
	})

	assembled(t, tl, "uno dos tres cuatro")
}

// transcript.done is authoritative too: whether it repeats the whole session or only the tail,
// clearing its span and inserting its words lands correctly either way.
func TestTimelineDoneWithWordsReplacesItsSpan(t *testing.T) {
	tl := &timeline{}
	tl.commit(finalWith(0, 3, w(0, 1, "uno"), w(1, 2, "dos"), w(2, 3, "tres")))
	tl.commit(commit{
		Words:           []word{w(0, 1, "UNO"), w(2, 3, "TRES")},
		Duration:        3,
		FullRestatement: true,
	})

	assembled(t, tl, "UNO TRES")
}

// A correction over an already-committed span wins, even with a different word count. This is
// what append-only Finals could never express.
func TestTimelineLetsACorrectionWin(t *testing.T) {
	tl := &timeline{}
	tl.commit(finalWith(0, 2, w(0, 1, "gira"), w(1, 2, "izquierda")))
	tl.commit(finalWith(0, 2, w(0, 2, "gira a la derecha")))

	assembled(t, tl, "gira a la derecha")
}

// A done (or any final) that overlaps only the MIDDLE of what we have must not take the ends
// with it.
func TestTimelinePartiallyOverlappingCommitKeepsTheEnds(t *testing.T) {
	tl := &timeline{}
	tl.commit(finalWith(0, 4, w(0, 1, "uno"), w(1, 2, "dos"), w(2, 3, "tres"), w(3, 4, "cuatro")))
	tl.commit(finalWith(1, 2, w(1, 2, "DOS"), w(2, 3, "TRES")))

	assembled(t, tl, "uno DOS TRES cuatro")
}

// The event's span and its words' extent do not have to agree. A word starting inside the
// previous event's declared duration but after its last word must survive — this is precisely
// what a start+duration watermark got wrong.
func TestTimelineKeepsAWordStraddlingTheDeclaredBoundary(t *testing.T) {
	tl := &timeline{}
	// duration reaches 3.2, but the last word ends at 2.9.
	tl.commit(finalWith(0, 3.2, w(2.5, 2.9, "antes")))
	tl.commit(finalWith(3.1, 0.3, w(3.1, 3.4, "después")))

	assembled(t, tl, "antes después")
}

// The API emits adjacent words where one ends exactly where the next begins (The 0.24→0.48,
// balance 0.48→0.96). With closed intervals every word would delete its neighbour, so the
// overlap test has to be half-open: [start, end).
func TestTimelineTreatsAdjacentWordsAsDisjoint(t *testing.T) {
	tl := &timeline{}
	tl.commit(finalWith(0.24, 0.24, w(0.24, 0.48, "The")))
	tl.commit(finalWith(0.48, 0.48, w(0.48, 0.96, "balance")))

	assembled(t, tl, "The balance")
}

// A final that arrives out of order lands in its temporal place, not at the end.
func TestTimelineInsertsOutOfOrderFinalsByTime(t *testing.T) {
	tl := &timeline{}
	tl.commit(finalWith(2, 1, w(2, 3, "segundo")))
	tl.commit(finalWith(0, 1, w(0, 1, "primero")))

	assembled(t, tl, "primero segundo")
}

// Identical timestamps must not reorder between runs. Arrival order breaks the tie.
func TestTimelineIsDeterministicOnIdenticalTimestamps(t *testing.T) {
	for i := 0; i < 50; i++ {
		tl := &timeline{}
		tl.commit(finalWith(1, 0, w(1, 1, "a")))
		tl.commit(finalWith(1, 0, w(1, 1, "b")))
		tl.commit(finalWith(1, 0, w(1, 1, "c")))
		assembled(t, tl, "a b c")
	}
}

// The documented example of transcript.done is `{"text":"","words":[],"duration":6.43}`.
// Electron took its only final from here, so a dictation ended up EMPTY. It must not erase
// what the partials already established.
func TestTimelineDoneWithEmptyTextErasesNothing(t *testing.T) {
	tl := &timeline{}
	tl.commit(finalWith(0, 1, w(0, 1, "conservado")))

	res := tl.commit(commit{Text: "", Duration: 6.43}) // done: no words, no span

	if res != commitNothing {
		t.Errorf("result = %v, want nothing to commit", res)
	}
	assembled(t, tl, "conservado")
}

// A done with no words carries NO positional evidence, so no string rule can be correct: a
// tail ("I agree" → "I agree again") and a correction ("I agree" → "I disagree") are
// indistinguishable. The conservative choice is to keep the word-timed timeline and say so,
// which can miss a final correction but can never duplicate.
func TestTimelineIgnoresADoneWithNoEvidenceWhenItHasWords(t *testing.T) {
	tl := &timeline{}
	tl.commit(finalWith(0, 1, w(0, 1, "estoy"), w(1, 2, "de acuerdo")))

	res := tl.commit(commit{Text: "no estoy de acuerdo", Duration: 2})

	if res != commitIgnoredNoEvidence {
		t.Errorf("result = %v, want ignored-no-evidence (so the caller can log it)", res)
	}
	assembled(t, tl, "estoy de acuerdo")
}

// ...but when nothing was committed at all, that text is all we have. This is the real short
// utterance: audio briefer than the ~3 s chunk window, where done IS the only text event.
func TestTimelineUsesADoneWithNoEvidenceWhenItIsAllThereIs(t *testing.T) {
	tl := &timeline{}

	res := tl.commit(commit{Text: "sí", Duration: 0.4})

	if res != commitUsedAsFallback {
		t.Errorf("result = %v, want used-as-fallback", res)
	}
	assembled(t, tl, "sí")
}

// An event with a span but no per-word times still has positional evidence, so it takes part
// in replacement normally — as one run covering its span.
func TestTimelineCommitsASpanWithoutWords(t *testing.T) {
	tl := &timeline{}
	tl.commit(commit{Text: "una frase", Start: 0, Duration: 2, HasSpan: true})
	tl.commit(commit{Text: "UNA FRASE", Start: 0, Duration: 2, HasSpan: true})

	assembled(t, tl, "UNA FRASE")
}

func TestTimelineEmptyAssemblesToNothing(t *testing.T) {
	tl := &timeline{}
	if !tl.empty() {
		t.Error("a fresh timeline must be empty")
	}
	assembled(t, tl, "")
}

// Blank word text must not produce double spaces in the pasted message.
func TestTimelineSkipsBlankWords(t *testing.T) {
	tl := &timeline{}
	tl.commit(finalWith(0, 3, w(0, 1, "uno"), w(1, 2, "   "), w(2, 3, "dos")))

	assembled(t, tl, "uno dos")
}
