package grok

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Juan-Motta/loqui-go/internal/session"
)

// The codes have to be the ones internal/session/policy.go actually keys off — a code that is
// not in its table falls through to matching the error PROSE, which is the bug that table
// exists to prevent (policy.go:36).
func TestHandshakeCodesClassifyAsIntended(t *testing.T) {
	cases := []struct {
		status int
		code   string
		class  session.CancelClass
	}{
		{http.StatusUnauthorized, "AuthenticationFailure", session.ClassAuth},
		{http.StatusForbidden, "Forbidden", session.ClassAuth},
		{http.StatusTooManyRequests, "TooManyRequests", session.ClassNetwork},
		{http.StatusBadRequest, "BadRequest", session.ClassConfig},
		{http.StatusNotFound, "BadRequest", session.ClassConfig},
		{http.StatusInternalServerError, "ServiceUnavailable", session.ClassNetwork},
		{http.StatusBadGateway, "ServiceUnavailable", session.ClassNetwork},
		{http.StatusServiceUnavailable, "ServiceUnavailable", session.ClassNetwork},
	}

	for _, tc := range cases {
		got := handshakeCode(&http.Response{StatusCode: tc.status})
		if got != tc.code {
			t.Errorf("status %d → %q, want %q", tc.status, got, tc.code)
		}
		if class := session.ClassifyCancel(session.Cancel{ErrorCode: got}); class != tc.class {
			t.Errorf("status %d → code %q → class %q, want %q", tc.status, got, class, tc.class)
		}
	}
}

// VERIFIED AGAINST THE REAL SERVICE, 2026-07-28. xAI rejects a bad API key with HTTP 400 and
// {"code":"Client specified an invalid argument","error":"Incorrect API key provided. …"} — NOT
// the 401 its own error table documents. A bare "status 400" message would send the user
// hunting through the request when the answer is "your key is wrong".
func TestABadKeyArrivesAsA400AndIsReportedAsAKeyProblem(t *testing.T) {
	body := `{"code":"Client specified an invalid argument","error":"Incorrect API key provided. You can obtain an API key from https://console.x.ai."}`
	code, msg := handshakeFailure(response(400, body), "")

	if code != "AuthenticationFailure" {
		t.Errorf("code = %q, want AuthenticationFailure", code)
	}
	if !strings.Contains(strings.ToLower(msg), "key") {
		t.Errorf("message = %q, want it to name the API key", msg)
	}
	// Either way it must not retry, so reading the body cannot change the retry decision —
	// only the message. That is what keeps this safe: policy.go:36 forbids prose driving the
	// retry, and both 400-config and auth are non-retryable.
	if session.ShouldReconnect(session.ClassifyCancel(session.Cancel{ErrorCode: code, Error: msg})) {
		t.Error("a bad key must never be retried")
	}
}

// A 400 that is NOT about the key stays a plain configuration failure, and surfaces the
// service's own reason, which is the only clue available.
func TestAGenericBadRequestKeepsTheServersReason(t *testing.T) {
	body := `{"code":"Client specified an invalid argument","error":"unsupported encoding: flac"}`
	code, msg := handshakeFailure(response(400, body), "")

	if code != "BadRequest" {
		t.Errorf("code = %q, want BadRequest", code)
	}
	if !strings.Contains(msg, "unsupported encoding") {
		t.Errorf("message = %q, want the service's own reason included", msg)
	}
}

// An unreadable or empty body must not stop the failure being reported.
func TestHandshakeFailureToleratesAnUnusableBody(t *testing.T) {
	for name, resp := range map[string]*http.Response{
		"empty":        response(400, ""),
		"not json":     response(400, "<html>gateway</html>"),
		"nil body":     {StatusCode: 400},
		"nil response": nil,
	} {
		code, msg := handshakeFailure(resp, "")
		if code == "" || msg == "" {
			t.Errorf("%s: got code=%q msg=%q, want both populated", name, code, msg)
		}
	}
}

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

// No response at all means the upgrade never happened: DNS, TCP, TLS. Transient, so it must
// classify as network and stay retryable.
func TestHandshakeWithNoResponseIsANetworkFailure(t *testing.T) {
	got := handshakeCode(nil)
	if got != "ConnectionFailure" {
		t.Errorf("got %q, want ConnectionFailure", got)
	}
	if class := session.ClassifyCancel(session.Cancel{ErrorCode: got}); class != session.ClassNetwork {
		t.Errorf("class = %q, want network", class)
	}
}

// A 401 must never be retried: reconnecting with the same wrong key bills nothing forever and
// never succeeds.
func TestBadCredentialsAreNotRetryable(t *testing.T) {
	class := session.ClassifyCancel(session.Cancel{ErrorCode: handshakeCode(&http.Response{StatusCode: 401})})
	if session.ShouldReconnect(class) {
		t.Error("a 401 must not be retryable")
	}
}

// A server-reported error arrives as prose with no structured code, so it is mapped to a
// TERMINAL code on purpose. See the plan: while controller.go resets the retry budget on every
// successful connect, anything retryable can loop forever against a service billed per hour.
func TestServerErrorIsTerminal(t *testing.T) {
	class := session.ClassifyCancel(session.Cancel{ErrorCode: serverErrorCode})
	if session.ShouldReconnect(class) {
		t.Error("a server error event must not open a reconnect loop")
	}
}

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
	return `{"code":"Client specified an invalid argument","error":"` + reason + `"}`
}
