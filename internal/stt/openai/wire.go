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
// The schema is the GA nested v1 form (session.audio.input.*) shared by public OpenAI and Azure
// OpenAI realtime transcription.
func BuildSessionUpdate(model, language string) ([]byte, error) {
	return BuildSessionUpdateWithManualCommit(model, language, false)
}

// SessionUpdateOptions holds the model-specific portion of the shared GA transcription envelope.
// Azure supplies a deployment in Model and uses either Language or Languages depending on the base
// model; public OpenAI continues to use the singular field through the compatibility wrappers below.
type SessionUpdateOptions struct {
	Model        string
	Language     string
	Languages    []string
	ManualCommit bool
}

// BuildSessionUpdateWithManualCommit builds the GA transcription session payload while making the
// turn policy explicit. Public OpenAI uses server VAD; Azure's gpt-realtime-whisper deployment
// rejects VAD and requires the client to commit its audio buffer.
func BuildSessionUpdateWithManualCommit(model, language string, manualCommit bool) ([]byte, error) {
	return BuildSessionUpdateWithOptions(SessionUpdateOptions{
		Model:        model,
		Language:     language,
		ManualCommit: manualCommit,
	})
}

// BuildSessionUpdateWithOptions builds the shared GA transcription session while allowing the
// narrow model-specific language shape to remain explicit.
func BuildSessionUpdateWithOptions(opts SessionUpdateOptions) ([]byte, error) {
	m := strings.TrimSpace(opts.Model)
	if m == "" {
		m = DefaultModel
	}
	transcription := map[string]any{"model": m}
	if opts.Language != "" {
		transcription["language"] = opts.Language
	}
	if len(opts.Languages) > 0 {
		transcription["languages"] = opts.Languages
	}
	var turnDetection any = map[string]any{"type": "server_vad"}
	if opts.ManualCommit {
		turnDetection = nil
	}
	return json.Marshal(map[string]any{
		"type": "session.update",
		"session": map[string]any{
			"type": "transcription",
			"audio": map[string]any{
				"input": map[string]any{
					"format":         map[string]any{"type": "audio/pcm", "rate": SampleRate},
					"transcription":  transcription,
					"turn_detection": turnDetection,
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
	// Ready is session.created: the service has opened a session, which is the only positive proof the
	// credential was accepted. The dictation path does not need it — it treats "socket open and
	// configured" as ready — so this event used to fall through to Ignore. A probe DOES need it: for
	// this service an invalid key still gets an HTTP 101 (measured 2026-08-06), so the upgrade proves
	// nothing and only a named session confirmation does.
	Ready
	// Configured is session.updated: the service accepted the session.update payload. Azure OpenAI
	// needs this stronger acknowledgement because a valid key with a wrong deployment can still open
	// the socket and then reject the configuration.
	Configured
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
	case Ready:
		return "ready"
	case Configured:
		return "configured"
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
	// Error is the server's prose. For a log, never for classifying — see Code.
	Error string
	// Code is error.code, falling back to error.type: "invalid_api_key", "insufficient_quota",
	// "server_error". Short, non-prose, and exactly what a user would search for.
	Code string
}

type wireError struct {
	Message string `json:"message"`
	// Type and Code are this service's machine-readable signal — code is "invalid_api_key" for a
	// rejected credential (measured 2026-08-06). Keeping only Message left prose as the sole thing to
	// classify from, which is the failure internal/session/policy.go:36 documents.
	Type string `json:"type"`
	Code string `json:"code"`
}

type wireEvent struct {
	Type       string    `json:"type"`
	Delta      string    `json:"delta"`
	Transcript string    `json:"transcript"`
	Text       string    `json:"text"`
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
	case ev.Type == "session.created":
		return Outcome{Kind: Ready}
	case ev.Type == "session.updated":
		return Outcome{Kind: Configured}
	case ev.Type == "error" || strings.HasSuffix(ev.Type, "transcription.failed"):
		msg := ev.Error.Message
		if msg == "" {
			msg = ev.Message
		}
		if msg == "" {
			msg = "error de openai realtime"
		}
		// code first, type as the fallback: code is the specific one ("invalid_api_key"), type the
		// family ("invalid_request_error"). Empty when the service reported neither.
		code := ev.Error.Code
		if code == "" {
			code = ev.Error.Type
		}
		return Outcome{Kind: Error, Error: msg, Code: code}
	case strings.HasSuffix(ev.Type, "transcription.delta"):
		return Outcome{Kind: PartialDelta, Delta: ev.Delta}
	case strings.HasSuffix(ev.Type, "transcription.completed"):
		text := ev.Text
		if text == "" {
			text = ev.Transcript
		}
		return Outcome{Kind: Final, Text: text}
	default:
		return Outcome{Kind: Ignore}
	}
}
