package openai

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// A PROVIDER THAT ECHOES THE KEY BACK MUST NOT GET IT ONTO A SCREEN OR INTO A LOG.
//
// Measured, not imagined (docs/research/2026-08-06-where-realtime-stt-auth-fails.md): OpenAI's real
// rejection carries "Incorrect API key provided: sk-proj-********************ueba" — the key masked in
// the middle with its last four characters intact. `readReason` extracts server-controlled text and
// `describeHandshake` concatenates it into the message the webview shows.
//
// Two rules, and the first is what makes the partial mask harmless: for an AUTH failure the wording is
// ours and the provider's prose is never included. For anything else the prose IS the useful part, so it
// is passed through with the exact secret removed.
func TestARejectionThatEchoesTheKeyDoesNotLeakIt(t *testing.T) {
	const key = "clave-secreta-que-no-debe-aparecer-jamas"

	for _, c := range []struct {
		name   string
		status int
		body   string
	}{
		{"401 echoing the key verbatim", http.StatusUnauthorized, body("Invalid API key " + key)},
		{"400 echoing the key verbatim", http.StatusBadRequest, body("Incorrect API key provided: " + key)},
		{"401 echoing it partially masked, as the real service does", http.StatusUnauthorized,
			body("Incorrect API key provided: clave-****************jamas")},
		{"500 echoing the key, where the body IS passed through", http.StatusInternalServerError,
			body("upstream failed for " + key)},
	} {
		t.Run(c.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: c.status,
				Body:       io.NopCloser(strings.NewReader(c.body)),
			}
			_, message := handshakeFailure(resp, key)

			if strings.Contains(message, key) {
				t.Errorf("the message carries the key: %q", message)
			}
			// The last eight characters are enough to prove a suffix survived, and short enough not to
			// collide with ordinary Spanish words in our own wording.
			if suffix := key[len(key)-8:]; strings.Contains(message, suffix) {
				t.Errorf("the message carries a suffix of the key (%q): %q", suffix, message)
			}
		})
	}
}

// body wraps a reason in the JSON shape THIS package's readReason parses. Using another provider's
// shape leaves the reason empty, which makes the whole test vacuous — a mutation caught exactly that.
func body(reason string) string {
	return `{"error":{"code":"invalid_api_key","message":"` + reason + `"}}`
}
