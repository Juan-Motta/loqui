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
	codeServiceError  = "ServiceError"

	// serverErrorCode is what an `error` EVENT from xAI maps to.
	//
	// The event carries only prose (`message`) — the schema defines no structured code — so
	// transient and permanent are indistinguishable. The session controller applies one hard
	// reconnect budget across successful short-lived connections in handleCancelLocked, guarded by
	// internal/session's TestReconnectBudgetSurvivesShortLivedSuccessfulConnections. Treating this as
	// retryable therefore cannot become an unbounded billing loop. Handshake auth/config failures keep
	// their own structured, non-retryable codes.
	serverErrorCode = codeServiceError
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
// SECRET is taken so the message can be built without it. See redactSecret: a rejection body can
// contain the credential — measured, not hypothetical
// (docs/research/2026-08-06-where-realtime-stt-auth-fails.md).
func handshakeFailure(resp *http.Response, secret string) (code, message string) {
	code = handshakeCode(resp)
	if resp == nil {
		return code, describeHandshake(code, 0, "")
	}

	reason := readReason(resp)
	// The one case the status alone gets wrong.
	if resp.StatusCode == http.StatusBadRequest && mentionsAPIKey(reason) {
		return codeAuth, "xAI rechazó la API key — revísala en Ajustes" // OUR wording: see redactSecret
	}
	// The provider's prose IS the useful part for a non-auth failure — a 5xx says what broke — so it is
	// passed through, with the credential taken out of it.
	return code, describeHandshake(code, resp.StatusCode, redactSecret(reason, secret))
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
	// NEVER the server's prose on an authentication failure — the wording above is ours and it stays
	// alone. This is the P0, and it was open until a cross-engine review found it: the 400 path returns
	// our wording early, but a 401/403 arrived here and appended the reason, putting the provider's
	// text back on screen. A rejection can carry the key masked in the middle with its tail INTACT, so
	// redactSecret — which can only remove the exact secret — does not catch it. The comment on
	// redactSecret claimed this was already handled; it was handled for one status only.
	if reason != "" && code != codeAuth && code != codeForbidden {
		return base + ": " + reason
	}
	return base
}

// redactSecret removes the credential from text the SERVER wrote.
//
// The exact secret is what can be removed reliably, and it is the realistic leak on a non-auth path.
// What it deliberately does NOT try to catch is a partially masked echo — the real OpenAI rejection
// carries "sk-proj-********************ueba", which is not the exact secret and would survive any
// substring rule that did not also match innocent text.
//
// THAT CASE IS HANDLED BY NOT USING THE SERVER'S PROSE AT ALL for an authentication failure, which is
// the only outcome where these services were measured echoing key material. The wording there is ours.
// The residual, stated: a provider that echoed a partial key inside a 5xx body would still put those
// characters on screen. No service is known to; if one does, the fix is to stop passing that body
// through, not to invent a smarter filter.
func redactSecret(text, secret string) string {
	if secret == "" || text == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, "«clave redactada»")
}
