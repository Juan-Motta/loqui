// The provider-agnostic speech-to-text contract every Loqui engine implements.
//
// In the Electron build there was no such contract: Azure ran in a renderer window
// through the JS SDK, Grok/ElevenLabs ran over a WebSocket in main, and the local
// engines were child processes printing JSONL — three topologies, each wired by hand in
// main.ts, each converted to the same event shape at a different place.
//
// Here they all satisfy one interface, because Go can host all three. What differs is
// only WantsAudio: the cloud providers are fed the PCM the host captured, while the
// native helpers open the microphone themselves.
package stt

// EventType mirrors the Electron engine events one for one, so the session controller
// and the overlay reducer port without translation.
type EventType string

const (
	// Started means the provider is connected and listening. The overlay only shows
	// the pill from here on.
	Started EventType = "started"
	// Partial is an in-progress hypothesis. Shown live, never stored.
	Partial EventType = "partial"
	// Final is a confirmed segment. A provider emits one per VAD pause, NOT one per
	// dictation: the session buffers them and delivers once, when the user ends it.
	Final EventType = "final"
	// Stopped means teardown finished, including any final flushed on the way out.
	// It is the signal that the session's message is complete.
	Stopped EventType = "stopped"
	// Canceled is a failure. ErrorCode is what the reconnect policy reads.
	Canceled EventType = "canceled"
)

// Event is one report from a provider.
//
// Gen is the generation the event belongs to. It exists because a stopped or
// reconnecting recognizer keeps emitting for a while after we have moved on, and
// acting on those late events is how a previous dictation's text ends up pasted into
// the next one. The session controller drops anything whose Gen is stale.
type Event struct {
	Type EventType `json:"type"`
	Gen  int       `json:"gen"`
	Text string    `json:"text,omitempty"`
	// Language is the detected locale, when the provider does language identification.
	Language string `json:"language,omitempty"`
	// Error is human-readable and may be translated — never branch on it.
	Error string `json:"error,omitempty"`
	// ErrorCode is the STRUCTURED reason, e.g. "AuthenticationFailure" or
	// "NotConfigured". The retry policy keys off this and only this: the Electron
	// build once classified retryable-vs-fatal by matching Spanish words in Error,
	// and translating the app made English fall through to "retryable" and reconnect
	// for ever.
	ErrorCode string `json:"errorCode,omitempty"`
}

// Sink receives a provider's events. Implementations must tolerate being called from
// any goroutine — the SDK callbacks and WebSocket readers all arrive off the main one.
type Sink func(Event)

// Provider is one speech-to-text engine.
//
// Lifecycle: Start -> (PushAudio)* -> Stop, and the provider MUST emit Stopped once
// teardown completes, even on a failed start. The session controller waits for it, and
// emitting it early is how the tail of a dictation gets cut off: a helper asked to stop
// still has buffered audio to transcribe and one last Final to send.
type Provider interface {
	// Start opens a recognition session. Every event it emits carries gen.
	Start(gen int, sink Sink) error

	// PushAudio hands over one chunk of 16 kHz, 16-bit, mono, little-endian PCM.
	// A no-op for providers that capture their own audio.
	PushAudio(pcm []byte)

	// Stop ends the session. Returns immediately; Stopped arrives through the sink.
	Stop()

	// WantsAudio reports whether the host should capture the microphone and feed
	// PushAudio. False for the native helpers, which open the device themselves.
	WantsAudio() bool
}
