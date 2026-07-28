package grok

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// The structured cancellation codes this provider reports. They must be members of the table
// in internal/session/policy.go: a code that is not there falls through to matching the error
// MESSAGE, and prose-matching is the bug that table was written to kill (policy.go:36) — once
// the app was translated, the same failure classified differently per language.
const (
	codeNoResponse    = "ConnectionFailure"
	codeAuth          = "AuthenticationFailure"
	codeForbidden     = "Forbidden"
	codeThrottled     = "TooManyRequests"
	codeBadRequest    = "BadRequest"
	codeUnavailable   = "ServiceUnavailable"
	codeReadyTimeout  = "ServiceTimeout"
	codeNotConfigured = "NotConfigured"

	// serverErrorCode is what an `error` EVENT from xAI maps to, and it is deliberately a
	// non-retryable one.
	//
	// The event carries only prose (`message`) — the schema defines no structured code — so
	// transient and permanent are indistinguishable. Meanwhile controller.go:278 resets the
	// reconnect budget on every successful connect, so any retryable classification here
	// becomes an UNBOUNDED reconnect loop against a service billed per hour. A misleading
	// message costs less than an open-ended bill.
	//
	// When the controller's retry budget is fixed (see the plan's out-of-scope section), this
	// becomes "ServiceError" with bounded retry.
	serverErrorCode = codeBadRequest
)

// handshakeFailure reads a rejected upgrade and returns the structured code plus a message for
// the user.
//
// WHY IT READS THE BODY. xAI's documented error table says a bad key is a 401. It is NOT —
// verified against the live service on 2026-07-28, an invalid key comes back as:
//
//	HTTP 400
//	{"code":"Client specified an invalid argument",
//	 "error":"Incorrect API key provided. You can obtain an API key from https://console.x.ai."}
//
// A message of "the request was rejected (status 400)" sends the user auditing their settings
// when the answer is "your key is wrong". The body is the only place that distinction exists.
//
// THIS DOES NOT REINTRODUCE PROSE-DRIVEN RETRIES. The retry decision comes from the CODE
// (internal/session/policy.go:36), and both outcomes here — AuthenticationFailure and
// BadRequest — are non-retryable, so reading the body changes the MESSAGE only, never the
// behaviour. That is the invariant to preserve if this is ever extended.
func handshakeFailure(resp *http.Response) (code, message string) {
	code = handshakeCode(resp)
	if resp == nil {
		return code, describeHandshake(code, 0, "")
	}

	reason := readReason(resp)
	// The one case the status alone gets wrong.
	if resp.StatusCode == http.StatusBadRequest && mentionsAPIKey(reason) {
		return codeAuth, "xAI rechazó la API key — revísala en Ajustes"
	}
	return code, describeHandshake(code, resp.StatusCode, reason)
}

// readReason pulls the human-readable reason out of the rejection body. The library documents
// that only the first 1024 bytes of a failed handshake's body are readable, so that is the cap.
func readReason(resp *http.Response) string {
	if resp.Body == nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil || len(raw) == 0 {
		return ""
	}
	var payload struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "" // an HTML error page from a proxy, say — nothing useful to quote
	}
	if payload.Error != "" {
		return payload.Error
	}
	return payload.Code
}

func mentionsAPIKey(reason string) bool {
	r := strings.ToLower(reason)
	return strings.Contains(r, "api key") || strings.Contains(r, "api_key")
}

// handshakeCode maps a failed WebSocket upgrade to a structured code. resp is nil when the
// upgrade never got a reply at all.
//
// The handshake is the ONLY place this provider gets a reliable machine-readable signal: auth
// is an HTTP header, so a bad key fails the upgrade with a status. xAI documents no close codes
// for /v1/stt at all — see docs/research/2026-07-28-xai-stt-streaming.md.
func handshakeCode(resp *http.Response) string {
	if resp == nil {
		return codeNoResponse
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return codeAuth
	case http.StatusForbidden:
		return codeForbidden
	case http.StatusTooManyRequests:
		return codeThrottled
	}
	if resp.StatusCode >= 500 {
		return codeUnavailable
	}
	if resp.StatusCode >= 400 {
		// A malformed request cannot be fixed by retrying it.
		return codeBadRequest
	}
	// A non-4xx/5xx status that still failed to upgrade: treat as transport trouble.
	return codeNoResponse
}

// describeHandshake turns a code into something the overlay can show, quoting the service's own
// reason when there is one — for a rejected request that is the only clue available.
//
// Kept apart from the retry decision on purpose: that keys off the CODE only
// (internal/session/policy.go:36).
func describeHandshake(code string, status int, reason string) string {
	var base string
	switch code {
	case codeAuth, codeForbidden:
		base = "xAI rechazó la API key — revísala en Ajustes"
	case codeThrottled:
		base = "xAI está limitando las peticiones; se reintentará"
	case codeUnavailable:
		base = fmt.Sprintf("el servicio de xAI no está disponible (status %d)", status)
	case codeBadRequest:
		base = fmt.Sprintf("xAI rechazó la petición (status %d)", status)
	default:
		base = "no se pudo conectar con xAI"
	}
	if reason != "" {
		return base + ": " + reason
	}
	return base
}
