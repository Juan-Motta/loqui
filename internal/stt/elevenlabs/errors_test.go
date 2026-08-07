package elevenlabs

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
		// leaked is the key material THIS body actually puts on the wire. For a verbatim echo that is
		// the whole key; for the masked echo it is only the tail the mask leaves visible.
		//
		// Naming it per case is what stops this test being vacuous. It used to assert a fixed
		// eight-character suffix of `key` against every case — and the masked fixture preserves five,
		// so the one case the whole test exists for could never fail. A cross-engine review found it,
		// and the leak it was hiding was real: a 401 appended the provider's prose to our wording.
		leaked string
	}{
		{"401 echoing the key verbatim", http.StatusUnauthorized, body("Invalid API key " + key), key},
		{"400 echoing the key verbatim", http.StatusBadRequest, body("Incorrect API key provided: " + key), key},
		{"401 echoing it partially masked, as the real service does", http.StatusUnauthorized,
			body("Incorrect API key provided: clave-****************jamas"), "jamas"},
		{"500 echoing the key, where the body IS passed through", http.StatusInternalServerError,
			body("upstream failed for " + key), key},
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
			if strings.Contains(message, c.leaked) {
				t.Errorf("the message carries key material (%q): %q", c.leaked, message)
			}
		})
	}
}

// body wraps a reason in the JSON shape THIS package's readReason parses. Using another provider's
// shape leaves the reason empty, which makes the whole test vacuous — a mutation caught exactly that.
func body(reason string) string {
	return `{"detail":{"status":"invalid_api_key","message":"` + reason + `"}}`
}
