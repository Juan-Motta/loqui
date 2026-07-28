// The xAI (Grok) speech-to-text provider. Ported from the Electron build's
// src/shared/grokStt.ts (the pure parts) and src/main/streamingStt.ts (the WebSocket
// session), but NOT verbatim — see events.go for the one place the original is wrong.
//
// Verified against the machine-readable schema at docs.x.ai/stt-streaming.ws.json; the
// findings, including what the docs do NOT say, are in
// docs/research/2026-07-28-xai-stt-streaming.md.
package grok

import (
	"net/url"
	"strconv"
	"strings"
)

// WSEndpoint is the streaming endpoint. Not the Voice Agent API
// (wss://api.x.ai/v1/realtime), which is a different protocol that takes a model name.
const WSEndpoint = "wss://api.x.ai/v1/stt"

// SampleRate is the model's native rate. Matching it is what avoids a resample on the
// service side, and it is what internal/audio captures for every provider.
const SampleRate = 16000

// autoLanguage is OUR sentinel for "let the service detect it", stored in the settings
// slot. It is never sent on the wire: xAI auto-detects by receiving NO language parameter,
// so forwarding the literal "auto" would be read as a language code.
const autoLanguage = "auto"

// BuildURL builds the streaming URL. All configuration lives in the query string — this
// endpoint takes no setup message after the socket opens.
//
// language is an xAI language code, or "auto"/empty to omit it. Note that for this service
// `language` is a FORMATTING switch (how numbers, currencies and units are written out),
// not a recognition hint: the model transcribes any supported language either way.
func BuildURL(language string) string {
	return buildURL(WSEndpoint, language)
}

// buildURL is the same against an arbitrary base, so the tests can point a session at a local
// server without a second URL-building path that could drift from this one.
func buildURL(base, language string) string {
	q := url.Values{}
	q.Set("encoding", "pcm")
	q.Set("sample_rate", strconv.Itoa(SampleRate))
	q.Set("interim_results", "true")
	if lang := strings.TrimSpace(language); lang != "" && lang != autoLanguage {
		q.Set("language", lang)
	}
	return base + "?" + q.Encode()
}
