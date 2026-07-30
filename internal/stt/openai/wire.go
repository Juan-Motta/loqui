// The OpenAI realtime transcription backend, provider "openai".
//
// PORTED FROM src/shared/openaiRealtime.ts plus the shared parseRealtimeEvent in azureOpenAi.ts.
// Only the pure half is here: endpoint, auth shape, the session payload and event decoding.
//
// WHY THIS IS NOT THE GROK MOLD, which is what the port plan says and what this file makes concrete:
//
//  1. AUTH GOES IN THE WEBSOCKET SUBPROTOCOLS, not a header: ["realtime",
//     "openai-insecure-api-key.<KEY>"]. The "insecure" name is OpenAI's and means "a client is
//     passing a key directly" — acceptable for a local desktop app, not for a web page.
//  2. THE SESSION MUST BE CONFIGURED FIRST. Grok takes its settings in the query string and starts
//     transcribing; here nothing is transcribed until a `session.update` lands, so the lifecycle has
//     an extra state between "connected" and "ready" that Grok's does not have.
//  3. THE AUDIO IS 24 kHz. The app captures 16 kHz, so this provider is the first that needs
//     RESAMPLING — see SampleRate below. Sending 16 kHz PCM to a session declared at 24 kHz does not
//     fail: it transcribes a chirpy, sped-up voice, badly, which is far worse than an error.
//
// Deltas accumulate. The wire sends `...transcription.delta` fragments and then a
// `...transcription.completed` with the whole transcript, so a client that treats each delta as a
// standalone partial shows one word at a time instead of a growing phrase.
package openai

import (
	"encoding/json"
	"strings"
)

// RealtimeURL already carries the intent; the model goes in the session payload, not the query.
const RealtimeURL = "wss://api.openai.com/v1/realtime?intent=transcription"

// SampleRate is what a realtime session expects, and it is NOT what the app captures. Anything feeding
// this provider has to resample — see resample.go.
const SampleRate = 24000

// CaptureRate is what the host's audio pipeline delivers (internal/audio), and the rate the buffered
// PCM is in before it goes out. Named here rather than assumed, because the whole point of this
// provider's audio path is that the two numbers differ.
const CaptureRate = 16000

// Models usable for realtime transcription, default first.
var Models = []string{"gpt-realtime-whisper", "gpt-4o-transcribe", "gpt-4o-mini-transcribe"}

// DefaultModel is what an unset or blank preference resolves to.
const DefaultModel = "gpt-realtime-whisper"

// BuildSubprotocols carries the credential in the WebSocket handshake.
//
// Returned as a slice rather than a joined string because that is what a dialer takes, and joining
// them by hand is how a subprotocol list ends up with a stray space that the server rejects with a
// bare 400 — an error that says nothing about the cause.
func BuildSubprotocols(apiKey string) []string {
	return []string{"realtime", "openai-insecure-api-key." + apiKey}
}

// SessionUpdate is the message that makes a connected socket into a transcribing one.
//
// The schema is the NESTED v1 form (session.audio.input.*), which is deliberately not the flat form
// Azure OpenAI uses. Sending the flat one here is accepted and then ignored: the session stays at its
// defaults and the language hint and model silently do nothing.
func BuildSessionUpdate(model, language string) ([]byte, error) {
	m := strings.TrimSpace(model)
	if m == "" {
		m = DefaultModel
	}
	transcription := map[string]any{"model": m}
	if language != "" {
		transcription["language"] = language
	}
	return json.Marshal(map[string]any{
		"type": "session.update",
		"session": map[string]any{
			"type": "transcription",
			"audio": map[string]any{
				"input": map[string]any{
					"format":         map[string]any{"type": "audio/pcm", "rate": SampleRate},
					"transcription":  transcription,
					"turn_detection": map[string]any{"type": "server_vad"},
				},
			},
		},
	})
}

// OutcomeKind is what one server message means to the session.
type OutcomeKind int

const (
	// Ignore is the default, so an unknown event is never mistaken for text or for a failure.
	Ignore OutcomeKind = iota
	// PartialDelta is a FRAGMENT to append, not a whole partial. Getting this wrong shows the user
	// one word at a time instead of a growing phrase.
	PartialDelta
	// Final is the completed transcript for an utterance.
	Final
	// Error is a server-reported failure.
	Error
)

func (k OutcomeKind) String() string {
	switch k {
	case PartialDelta:
		return "partial-delta"
	case Final:
		return "final"
	case Error:
		return "error"
	default:
		return "ignore"
	}
}

// Outcome is a decoded server message. Delta carries a fragment; Text a complete transcript.
type Outcome struct {
	Kind  OutcomeKind
	Delta string
	Text  string
	Error string
}

type wireError struct {
	Message string `json:"message"`
}

type wireEvent struct {
	Type       string    `json:"type"`
	Delta      string    `json:"delta"`
	Transcript string    `json:"transcript"`
	Message    string    `json:"message"`
	Error      wireError `json:"error"`
}

// Decode turns one raw server message into an Outcome.
//
// The type checks are SUFFIX matches, ported as such: the events are named
// `conversation.item.input_audio_transcription.delta` and `.completed`, and that prefix has changed
// across revisions of this API while the tail has not. Matching the whole string would make the
// provider go silent — connected, no errors, no text — the next time OpenAI renames the container.
func Decode(raw []byte) Outcome {
	var ev wireEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return Outcome{Kind: Ignore}
	}
	switch {
	case ev.Type == "error":
		msg := ev.Error.Message
		if msg == "" {
			msg = ev.Message
		}
		if msg == "" {
			msg = "error de openai realtime"
		}
		return Outcome{Kind: Error, Error: msg}
	case strings.HasSuffix(ev.Type, "transcription.delta"):
		return Outcome{Kind: PartialDelta, Delta: ev.Delta}
	case strings.HasSuffix(ev.Type, "transcription.completed"):
		return Outcome{Kind: Final, Text: ev.Transcript}
	default:
		return Outcome{Kind: Ignore}
	}
}
