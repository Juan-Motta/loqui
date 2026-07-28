package grok

import "encoding/json"

// The server events, decoded. Ported from ../loqui/src/shared/grokStt.ts — but NOT verbatim,
// and this is the one deliberate divergence from the Electron build.
//
// WHAT THE ORIGINAL GOT WRONG. parseGrokEvent mapped every `transcript.partial` to "interim"
// and took a final only from `transcript.done`. Two problems, both verified against
// docs.x.ai/stt-streaming.ws.json:
//
//  1. The final is signalled by the `is_final` / `speech_final` FLAGS, not by the event name.
//     Ignoring them discards every finalized utterance in a multi-sentence session.
//  2. The official example of `transcript.done` carries `text: ""` after 6.43 s of audio. If
//     the only final comes from there, the dictation is delivered EMPTY.
//
// So both flags are read, and every final-bearing event contributes to a timeline (timeline.go)
// that the provider assembles into one Final on the way out.

type outcomeKind int

const (
	// outcomeIgnore is anything we do not recognise. Deliberately not an error: a new event
	// type in a future API version must not cancel a working dictation.
	outcomeIgnore outcomeKind = iota
	// outcomeReady is transcript.created — the session exists and audio may flow.
	outcomeReady
	// outcomePartial is an in-progress hypothesis. Shown live, never committed.
	outcomePartial
	// outcomeFinal carries words to commit to the timeline.
	outcomeFinal
	// outcomeError is a server-reported failure.
	outcomeError
)

func (k outcomeKind) String() string {
	switch k {
	case outcomeReady:
		return "ready"
	case outcomePartial:
		return "partial"
	case outcomeFinal:
		return "final"
	case outcomeError:
		return "error"
	default:
		return "ignore"
	}
}

// word is one recognised word with its position in the stream. The times are documented as
// seconds from STREAM start, which is what makes the timeline's interval arithmetic possible.
type word struct {
	Start float64
	End   float64
	Text  string
}

// commit is what one final-bearing event contributes to the timeline.
type commit struct {
	// Words is empty when the server sent none — transcript.done routinely does.
	Words []word
	// Text is the event's own full text, used only when Words is empty.
	Text string
	// Start and Duration are the event-level span. transcript.done has NO top-level start,
	// so HasSpan reports whether they can be trusted.
	Start    float64
	Duration float64
	HasSpan  bool
	// FullRestatement means this event is authoritative for its whole span, so anything the
	// timeline holds inside that span is replaced wholesale rather than word by word.
	//
	// It comes from `speech_final` — the docs call that event the "complete stitched
	// utterance" — and from transcript.done, which is terminal. WHY IT MATTERS: it is the only
	// signal that distinguishes a restatement which RETRACTS a word ("a b c" restated as
	// "a c", where "b" must disappear) from a chunk final that simply never mentioned it
	// (where it must survive). Without it, one of those two is always wrong.
	FullRestatement bool
}

// outcome is one decoded message.
type outcome struct {
	Kind outcomeKind
	// SessionID is set on Ready. Logged for correlation; it is not a transcript.
	SessionID string
	// Text is the live hypothesis, on Partial.
	Text string
	// Commit is set on Final.
	Commit commit
	// Terminal marks transcript.done: the server closes the socket right after it, so it is
	// the last text we will ever see on this connection.
	Terminal bool
	// Error is the server's message, on Error. Human-readable prose only — the schema gives
	// no structured code, which is why the provider never branches on it.
	Error string
}

// wireEvent is the JSON as it arrives. `start` and `duration` are pointers because their
// ABSENCE is meaningful: transcript.done omits start entirely.
type wireEvent struct {
	Type        string     `json:"type"`
	ID          string     `json:"id"`
	Text        string     `json:"text"`
	Words       []wireWord `json:"words"`
	IsFinal     bool       `json:"is_final"`
	SpeechFinal bool       `json:"speech_final"`
	Start       *float64   `json:"start"`
	Duration    *float64   `json:"duration"`
	Message     string     `json:"message"`
}

// wireWord deliberately omits `confidence`: we never use it, and the schema says it is
// "omitted when 0", so decoding it would only add a field that can legitimately vanish.
type wireWord struct {
	Text  string  `json:"text"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// decode turns one server message into an outcome. Never panics and never returns an error:
// anything it cannot make sense of is outcomeIgnore, because a malformed frame is not a
// reason to abandon a dictation that is otherwise working.
func decode(raw []byte) outcome {
	var ev wireEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return outcome{Kind: outcomeIgnore}
	}

	switch ev.Type {
	case "transcript.created":
		return outcome{Kind: outcomeReady, SessionID: ev.ID}

	case "transcript.partial":
		// The flags, not the event name. See the package comment.
		if !ev.IsFinal {
			return outcome{Kind: outcomePartial, Text: ev.Text}
		}
		// speech_final marks the complete stitched utterance, which is authoritative for its
		// span. A chunk final (speech_final=false) is only incremental.
		c := commitFrom(ev)
		c.FullRestatement = ev.SpeechFinal
		return outcome{Kind: outcomeFinal, Commit: c}

	case "transcript.done":
		c := commitFrom(ev)
		c.FullRestatement = true // terminal, so whatever it says about its span is final
		return outcome{Kind: outcomeFinal, Terminal: true, Commit: c}

	case "error":
		msg := ev.Message
		if msg == "" {
			// Still reportable: a cancellation with no text at all would show the user an
			// empty reason.
			msg = "el servicio de xAI reportó un error sin detalle"
		}
		return outcome{Kind: outcomeError, Error: msg}

	default:
		return outcome{Kind: outcomeIgnore}
	}
}

func commitFrom(ev wireEvent) commit {
	c := commit{Text: ev.Text}
	for _, w := range ev.Words {
		c.Words = append(c.Words, word{Start: w.Start, End: w.End, Text: w.Text})
	}
	if ev.Start != nil && ev.Duration != nil {
		c.Start, c.Duration, c.HasSpan = *ev.Start, *ev.Duration, true
	}
	return c
}
