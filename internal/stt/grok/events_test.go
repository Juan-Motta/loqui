package grok

import "testing"

func TestDecodeReadyCarriesTheSessionID(t *testing.T) {
	got := decode([]byte(`{"type":"transcript.created","id":"83f2f6fd-1cd1-4747-bc52-cebddc961c32"}`))
	if got.Kind != outcomeReady {
		t.Fatalf("kind = %v, want ready", got.Kind)
	}
	// Logged so a session can be correlated with xAI's side. Never a transcript.
	if got.SessionID != "83f2f6fd-1cd1-4747-bc52-cebddc961c32" {
		t.Errorf("session id = %q, want the id from the event", got.SessionID)
	}
}

func TestDecodeInterimIsAPartial(t *testing.T) {
	got := decode([]byte(`{"type":"transcript.partial","text":"the bal","words":[],"is_final":false,"speech_final":false,"start":0.0,"duration":0.8}`))
	if got.Kind != outcomePartial {
		t.Fatalf("kind = %v, want partial", got.Kind)
	}
	if got.Text != "the bal" {
		t.Errorf("text = %q, want the raw hypothesis", got.Text)
	}
}

// The bug this port exists to avoid: the final is signalled by is_final, NOT by the event
// name. Electron's parser mapped every transcript.partial to interim and silently dropped
// every finalized utterance in a multi-sentence session.
func TestDecodeChunkFinalIsAFinal(t *testing.T) {
	got := decode([]byte(`{"type":"transcript.partial","text":"The balance is $167,983.15.",
	  "words":[{"text":"The","start":0.24,"end":0.48,"confidence":0.95},
	           {"text":"balance","start":0.48,"end":0.96,"confidence":0.92}],
	  "is_final":true,"speech_final":false,"start":0.0,"duration":3.2}`))

	if got.Kind != outcomeFinal {
		t.Fatalf("kind = %v, want final (is_final=true)", got.Kind)
	}
	if got.Terminal {
		t.Error("a chunk final must not be terminal — the session continues")
	}
	if len(got.Commit.Words) != 2 {
		t.Fatalf("got %d words, want 2", len(got.Commit.Words))
	}
	if w := got.Commit.Words[0]; w.Text != "The" || w.Start != 0.24 || w.End != 0.48 {
		t.Errorf("first word = %+v, want The [0.24,0.48)", w)
	}
	if !got.Commit.HasSpan || got.Commit.Start != 0.0 || got.Commit.Duration != 3.2 {
		t.Errorf("span = %+v, want start 0.0 duration 3.2 present", got.Commit)
	}
}

func TestDecodeUtteranceFinalIsAlsoAFinal(t *testing.T) {
	got := decode([]byte(`{"type":"transcript.partial","text":"I will buy two.","words":[{"text":"I","start":3.2,"end":3.4}],"is_final":true,"speech_final":true,"start":3.2,"duration":2.4}`))
	if got.Kind != outcomeFinal {
		t.Fatalf("kind = %v, want final", got.Kind)
	}
	if got.Terminal {
		t.Error("an utterance final is not terminal: only transcript.done closes the socket")
	}
}

// transcript.done is the ONLY terminal text event, and it carries no top-level start —
// which is why the timeline has to dedupe it by word times.
func TestDecodeDoneIsTerminalAndHasNoSpan(t *testing.T) {
	got := decode([]byte(`{"type":"transcript.done","text":"","words":[],"duration":6.43}`))
	if got.Kind != outcomeFinal {
		t.Fatalf("kind = %v, want final", got.Kind)
	}
	if !got.Terminal {
		t.Error("transcript.done must be terminal — the server closes right after it")
	}
	if got.Commit.HasSpan {
		t.Error("transcript.done has no top-level start; HasSpan must be false")
	}
}

// The error payload is FLAT. The nested {"error":{"message":…}} shape Electron looked for
// first belongs to a different endpoint (the Responses API).
func TestDecodeErrorReadsTheFlatMessage(t *testing.T) {
	got := decode([]byte(`{"type":"error","message":"Invalid message: expected {\"type\": \"audio.done\"}"}`))
	if got.Kind != outcomeError {
		t.Fatalf("kind = %v, want error", got.Kind)
	}
	if got.Error == "" {
		t.Error("the flat message field was not read")
	}
}

func TestDecodeErrorWithNoMessageStillReports(t *testing.T) {
	got := decode([]byte(`{"type":"error"}`))
	if got.Kind != outcomeError {
		t.Fatalf("kind = %v, want error", got.Kind)
	}
	if got.Error == "" {
		t.Error("an error with no message must still carry something reportable")
	}
}

// Anything unrecognised is ignored rather than treated as a failure: a new event type in a
// future API version must not cancel a working dictation.
func TestDecodeIgnoresWhatItDoesNotKnow(t *testing.T) {
	for name, raw := range map[string]string{
		"unknown type":             `{"type":"transcript.speculative","text":"hm"}`,
		"no type":                  `{"text":"hm"}`,
		"invalid json":             `{"type":`,
		"empty":                    ``,
		"not an object":            `["transcript.done"]`,
		"wrong field type on text": `{"type":"transcript.partial","text":5}`,
	} {
		if got := decode([]byte(raw)); got.Kind != outcomeIgnore {
			t.Errorf("%s: kind = %v, want ignore", name, got.Kind)
		}
	}
}

// confidence is "omitted when 0" per the schema, and we do not use it. Its absence, or any
// extra field a future version adds, must not break the decode.
func TestDecodeToleratesMissingAndExtraFields(t *testing.T) {
	got := decode([]byte(`{"type":"transcript.partial","text":"hi","words":[{"text":"hi","start":0,"end":0.3}],"is_final":true,"start":0,"duration":0.3,"end_of_turn_confidence":0.98,"channel":0}`))
	if got.Kind != outcomeFinal {
		t.Fatalf("kind = %v, want final", got.Kind)
	}
	if len(got.Commit.Words) != 1 || got.Commit.Words[0].Text != "hi" {
		t.Errorf("words = %+v, want the single word", got.Commit.Words)
	}
}
