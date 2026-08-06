// The ElevenLabs (Scribe v2 Realtime) streaming backend, provider "elevenlabs".
//
// PORTED FROM src/shared/elevenLabs.ts, endpoint and event names included. Only the pure half lives
// here for now: the URL, the audio message and the event decoding. The socket lifecycle is NOT in this
// file — see the note at the bottom of this comment.
//
// HOW IT DIFFERS FROM GROK, which is the closest thing already ported:
//   - Audio goes as JSON with BASE64 inside ({"message_type":"input_audio_chunk", ...}), where Grok
//     sends raw binary frames. So the transport is text messages both ways.
//   - `commit_strategy=vad` in the query string makes the server cut utterances on silence, so the
//     client does not decide when a phrase ends.
//   - Finals arrive as `committed_transcript` (a prefix, not an exact match — the wire has carried
//     `committed_transcript_final` too), which is why the check below is a prefix check.
//
// The key travels in an `xi-api-key` HEADER, never in the URL: a URL ends up in logs, error messages
// and crash reports, and this one would carry a credential.
package elevenlabs

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
)

// WSEndpoint is the realtime speech-to-text socket.
const WSEndpoint = "wss://api.elevenlabs.io/v1/speech-to-text/realtime"

// Model is the realtime Scribe model, and SampleRate the rate it expects. 16 kHz matches what the
// app already captures, so unlike OpenAI realtime no resampling is needed.
const (
	Model      = "scribe_v2_realtime"
	SampleRate = 16000
)

// APIKeyHeader is where the credential goes.
const APIKeyHeader = "xi-api-key"

// BuildURL builds the streaming URL for a language, or for automatic detection when empty.
//
// language is a code the store has already validated for this engine's capability; an empty string
// means "let the server detect", which is why the parameter is simply omitted rather than sent as
// "auto" — that string is Grok's convention and would be a language name here.
func BuildURL(language string) string {
	return buildURL(WSEndpoint, language)
}

// buildURL is the testable core: the base is a parameter so a test can point it at a fake server.
func buildURL(base, language string) string {
	q := url.Values{}
	q.Set("model_id", Model)
	q.Set("audio_format", "pcm_16000")
	// The server commits an utterance when it hears silence. Without this the session would only
	// produce a transcript when the client closed the stream, which for hold-to-talk means the text
	// arrives all at once at the end and no partials ever appear.
	q.Set("commit_strategy", "vad")
	if language != "" {
		q.Set("language_code", language)
	}
	return base + "?" + q.Encode()
}

// audioMessage is the input_audio_chunk envelope.
type audioMessage struct {
	MessageType string `json:"message_type"`
	AudioBase64 string `json:"audio_base_64"`
	Commit      bool   `json:"commit"`
	SampleRate  int    `json:"sample_rate"`
}

// BuildAudioMessage wraps one chunk of PCM16 for the wire.
//
// The PCM is base64'd rather than sent as a binary frame: this endpoint reads JSON text messages, and
// a binary frame is silently ignored — a session that looks connected and transcribes nothing.
func BuildAudioMessage(pcm []byte, sampleRate int, commit bool) ([]byte, error) {
	return json.Marshal(audioMessage{
		MessageType: "input_audio_chunk",
		AudioBase64: base64.StdEncoding.EncodeToString(pcm),
		Commit:      commit,
		SampleRate:  sampleRate,
	})
}

// OutcomeKind is what one server message means to the session.
type OutcomeKind int

const (
	// Ignore is a message that carries nothing the session acts on. The default on purpose: an
	// unknown event must not be mistaken for a transcript or an error.
	Ignore OutcomeKind = iota
	// Ready is the session handshake, which is what unblocks sending audio.
	Ready
	// Partial is in-progress text, replaced by the next one.
	Partial
	// Final is a committed utterance.
	Final
	// Error is a server-reported failure.
	Error
)

func (k OutcomeKind) String() string {
	switch k {
	case Ready:
		return "ready"
	case Partial:
		return "partial"
	case Final:
		return "final"
	case Error:
		return "error"
	default:
		return "ignore"
	}
}

// Outcome is a decoded server message.
type Outcome struct {
	Kind OutcomeKind
	Text string
	// Error is the server's prose. Useful for a log, never for classifying — see Code.
	Error string
	// Code is the event NAME for an error, which is this service's machine-readable signal:
	// auth_error, quota_exceeded, unaccepted_terms and the rest. Without it a caller cannot tell
	// "out of credit" from "terms not accepted", which need different things from the user.
	Code string
}

// wireEvent is the subset of the server's schema that matters.
//
// Both `message_type` and `type` are read because the documented field is message_type while some
// events on this endpoint carry plain `type`; accepting either is what the Electron build did, and
// dropping one would silently ignore half the events.
type wireEvent struct {
	MessageType string `json:"message_type"`
	Type        string `json:"type"`
	Text        string `json:"text"`
	Error       string `json:"error"`
}

// Decode turns one raw server message into an Outcome.
//
// Malformed JSON is Ignore, not Error: a frame this build does not understand is not evidence that
// the session failed, and reporting it as an error would abort a working dictation.
func Decode(raw []byte) Outcome {
	var ev wireEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return Outcome{Kind: Ignore}
	}
	name := ev.MessageType
	if name == "" {
		name = ev.Type
	}
	switch {
	case name == "session_started":
		return Outcome{Kind: Ready}
	case name == "partial_transcript":
		return Outcome{Kind: Partial, Text: ev.Text}
	// A PREFIX, because the wire carries both `committed_transcript` and longer variants; an equality
	// check would drop the final and the dictation would end with only its partials.
	case name == "final_transcript" || strings.HasPrefix(name, "committed_transcript"):
		return Outcome{Kind: Final, Text: ev.Text}
	case isErrorEvent(name):
		msg := ev.Error
		if msg == "" {
			msg = "error de elevenlabs stt"
		}
		return Outcome{Kind: Error, Error: msg, Code: name}
	default:
		return Outcome{Kind: Ignore}
	}
}

// errorEvents are the documented failures that do NOT have "error" in their name.
//
// THEY WERE ALL BEING IGNORED. Decode matched an error only when the name contained "error", so every
// name below fell through to Ignore — and the session then reported whatever came next, usually the
// socket closing, as a generic connection loss. The specific cause was lost, which is the difference
// between telling a user "no se pudo conectar" and telling them their credit ran out.
var errorEvents = map[string]bool{
	"quota_exceeded":              true,
	"unaccepted_terms":            true,
	"rate_limited":                true,
	"queue_overflow":              true,
	"resource_exhausted":          true,
	"commit_throttled":            true,
	"session_time_limit_exceeded": true,
	"chunk_size_exceeded":         true,
	"insufficient_audio_activity": true,
}

// isErrorEvent keeps the substring rule — which catches auth_error, transcriber_error and any future
// *_error — and adds the documented names that do not contain it.
func isErrorEvent(name string) bool {
	return strings.Contains(name, "error") || errorEvents[name]
}
