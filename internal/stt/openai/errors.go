package openai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Classifying a failed handshake.
//
// THE HANDSHAKE IS NOT THE ONLY MACHINE-READABLE SIGNAL, and believing so was wrong: measured on
// 2026-08-06, an invalid key gets a 101 and then error.code="invalid_api_key" plus a close 3000
// (docs/research/2026-08-06-where-realtime-stt-auth-fails.md). What follows still holds for the
// credential
// travels as an HTTP header, so a bad key fails the UPGRADE with a status code. Once the socket is
// open, failures arrive as prose in an `error` event with no structured code, which is why the runtime
// error path is deliberately non-retryable (see serverErrorCode).
//
// Same structure as grok/errors.go and elevenlabs/errors.go, and the same reason for existing. The
// strings differ because they are shown to the user and name the service; the CODES are the shared
// vocabulary from internal/session/policy.go, and the retry decision keys off the code only, never the
// prose.
//
// ONE EXTRA REASON IT MATTERS HERE: the credential travels in a WebSocket SUBPROTOCOL, and a rejected
// subprotocol comes back as a bare 400 with no body at all. So "400 and nothing else" is a plausible
// symptom of a bad key on this endpoint, which is why the api-key sniffing below also treats an empty
// reason on a 400 as authentication rather than as a malformed request.

// handshakeFailure maps a rejected upgrade to a code and a message.
// SECRET is taken so the message can be built without it. See redactSecret: a rejection body can
// contain the credential — measured, not hypothetical
// (docs/research/2026-08-06-where-realtime-stt-auth-fails.md).
func handshakeFailure(resp *http.Response, secret string) (code, message string) {
	code = handshakeCode(resp)
	if resp == nil {
		return code, describeHandshake(code, 0, "")
	}

	reason := readReason(resp)
	// The cases the status alone gets wrong. A bad key here fails the UPGRADE as a 400, because the key
	// is a subprotocol and a rejected subprotocol is not a 401 — so both a body that mentions the key
	// and a 400 with NO body at all are classified as authentication. Calling those BadRequest would
	// send the user to fix a request when what they need to fix is the key.
	if resp.StatusCode == http.StatusBadRequest && (reason == "" || mentionsAPIKey(reason)) {
		return codeAuth, "OpenAI rechazó la API key — revísala en Ajustes" // OUR wording: see redactSecret
	}
	// The provider's prose IS the useful part for a non-auth failure — a 5xx says what broke — so it is
	// passed through, with the credential taken out of it.
	return code, describeHandshake(code, resp.StatusCode, redactSecret(reason, secret))
}

// readReason pulls the human-readable reason out of the rejection body. Only the first 1024 bytes of a
// failed handshake's body are documented as readable, so that is the cap.
func readReason(resp *http.Response) string {
	if resp.Body == nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil || len(raw) == 0 {
		return ""
	}
	// OpenAI wraps its errors as {"error":{"message":..,"code":..}}. `message` and a bare `error` string
	// are read too: proxies and gateways in front of the API use those shapes, and guessing only one
	// leaves the user with a bare status number for the rest.
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
		Message string `json:"message"`
		Detail  string `json:"detail"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "" // an HTML error page from a proxy, say — nothing useful to quote
	}
	for _, candidate := range []string{
		payload.Error.Message,
		payload.Error.Code,
		payload.Message,
		payload.Detail,
	} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func mentionsAPIKey(reason string) bool {
	r := strings.ToLower(reason)
	return strings.Contains(r, "api key") ||
		strings.Contains(r, "api_key") ||
		strings.Contains(r, "openai-insecure-api-key") ||
		strings.Contains(r, "subprotocol")
}

// handshakeCode maps a failed upgrade to a structured code. resp is nil when the upgrade never got a
// reply at all.
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
	// A non-4xx/5xx status that still failed to upgrade: transport trouble.
	return codeNoResponse
}

// describeHandshake turns a code into something the overlay can show, quoting the service's own reason
// when there is one — for a rejected request that is the only clue available.
//
// Kept apart from the retry decision on purpose: that keys off the CODE only.
func describeHandshake(code string, status int, reason string) string {
	var base string
	switch code {
	case codeAuth, codeForbidden:
		base = "OpenAI rechazó la API key — revísala en Ajustes"
	case codeThrottled:
		base = "OpenAI está limitando las peticiones; se reintentará"
	case codeUnavailable:
		base = fmt.Sprintf("el servicio de OpenAI no está disponible (status %d)", status)
	case codeBadRequest:
		base = fmt.Sprintf("OpenAI rechazó la petición (status %d)", status)
	default:
		base = "no se pudo conectar con OpenAI"
	}
	if reason != "" {
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
