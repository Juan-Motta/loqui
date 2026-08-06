package elevenlabs

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Juan-Motta/loqui-go/internal/stt"
)

// THE FALSE GREEN THIS SERVICE ACTUALLY PRODUCES.
//
// Measured on 2026-08-06: an invalid key gets an HTTP 101 from ElevenLabs and the refusal arrives
// afterwards, as an event (docs/research/2026-08-06-where-realtime-stt-auth-fails.md). So "dial and
// close" — the first design — would have answered "your key works" to any string. This is the test that
// pins it shut.
func TestProbeNeverSucceedsWithoutReadiness(t *testing.T) {
	for _, c := range []struct {
		name   string
		script func(f *fakeEleven, conn *websocket.Conn)
	}{
		{"101 then the real auth refusal", func(f *fakeEleven, conn *websocket.Conn) {
			f.send(conn, `{"message_type":"auth_error","error":"You must be authenticated to use this endpoint."}`)
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}},
		{"101 then a NORMAL close and nothing else", func(f *fakeEleven, conn *websocket.Conn) {
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}},
		{"101 then silence", func(f *fakeEleven, conn *websocket.Conn) { time.Sleep(400 * time.Millisecond) }},
		{"101 then malformed JSON", func(f *fakeEleven, conn *websocket.Conn) {
			f.send(conn, `{no es json`)
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}},
		{"101 then an unrecognised event", func(f *fakeEleven, conn *websocket.Conn) {
			f.send(conn, `{"type":"algo.desconocido","message_type":"algo_desconocido"}`)
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeEleven(t, c.script)
			res := TestConnection(context.Background(), "una-clave", ProbeOptions{
				Endpoint: f.url, ReadyTimeout: 250 * time.Millisecond,
			})
			if res.OK || res.Kind == stt.ProbeOK {
				t.Errorf("a false green: %+v", res)
			}
		})
	}
}

// The green path: a named session confirmation, and nothing else produces it.
func TestProbeSucceedsOnTheSessionConfirmation(t *testing.T) {
	f := newFakeEleven(t, func(f *fakeEleven, conn *websocket.Conn) { f.ready(conn) })

	res := TestConnection(context.Background(), "una-clave", ProbeOptions{Endpoint: f.url})

	if !res.OK || res.Kind != stt.ProbeOK {
		t.Fatalf("OK=%v Kind=%v Message=%q", res.OK, res.Kind, res.Message)
	}
}

// A refusal DELIVERED AS AN EVENT is still a rejected key, and it is told apart from other failures by
// the service's own code — never by its prose.
func TestProbeClassifiesPostUpgradeErrorsByCode(t *testing.T) {
	for _, c := range []struct {
		name string
		raw  string
		kind stt.ProbeKind
		code string
	}{
		{"the credential", `{"message_type":"auth_error","error":"You must be authenticated to use this endpoint."}`, stt.ProbeKeyRejected, "auth_error"},
		{"something else", `{"message_type":"quota_exceeded","error":"out of credit"}`, stt.ProbeFailed, "quota_exceeded"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeEleven(t, func(f *fakeEleven, conn *websocket.Conn) { f.send(conn, c.raw) })

			res := TestConnection(context.Background(), "clave-secreta-inconfundible", ProbeOptions{Endpoint: f.url})

			if res.OK {
				t.Error("OK on an error event")
			}
			if res.Kind != c.kind {
				t.Errorf("Kind = %v, want %v", res.Kind, c.kind)
			}
			if res.Code != c.code {
				t.Errorf("Code = %q, want %q — the machine-readable signal is what the user searches for", res.Code, c.code)
			}
			// The prose was measured carrying key material. It must not reach the user.
			if strings.Contains(res.Message, "sk-proj") || strings.Contains(res.Message, "authenticated") {
				t.Errorf("the provider's prose reached the message: %q", res.Message)
			}
			if strings.Contains(res.Message, "clave-secreta-inconfundible") {
				t.Errorf("the credential is in the message: %q", res.Message)
			}
		})
	}
}

// An empty key never dials.
func TestProbeWithNoKeyNeverDials(t *testing.T) {
	dialed := false
	f := newFakeEleven(t, func(f *fakeEleven, conn *websocket.Conn) { dialed = true })

	res := TestConnection(context.Background(), "   ", ProbeOptions{Endpoint: f.url})

	if res.Kind != stt.ProbeNoKey || res.OK {
		t.Errorf("Kind = %v OK = %v, want ProbeNoKey", res.Kind, res.OK)
	}
	if dialed {
		t.Error("it dialled anyway")
	}
}

// A refused handshake is classified too — this service does not use it for auth, but a proxy or an
// outage will.
func TestProbeReportsARefusedHandshake(t *testing.T) {
	for _, c := range []struct {
		status int
		kind   stt.ProbeKind
	}{
		{http.StatusUnauthorized, stt.ProbeKeyRejected},
		{http.StatusForbidden, stt.ProbeKeyRejected},
		{http.StatusInternalServerError, stt.ProbeFailed},
	} {
		t.Run(http.StatusText(c.status), func(t *testing.T) {
			f := newRejectingEleven(t, c.status, "")
			res := TestConnection(context.Background(), "clave-secreta-inconfundible", ProbeOptions{Endpoint: f.url})

			if res.OK || res.Kind != c.kind {
				t.Errorf("Kind = %v, want %v (%+v)", res.Kind, c.kind, res)
			}
			if strings.Contains(res.Message, "clave-secreta-inconfundible") {
				t.Errorf("the credential is in the message: %q", res.Message)
			}
		})
	}
}

// The probe uses the SAME handshake dictation does, or it would bless a key the microphone cannot use.
func TestProbeSendsTheSameAuthAsDictation(t *testing.T) {
	f := newFakeEleven(t, func(f *fakeEleven, conn *websocket.Conn) { f.ready(conn) })

	if res := TestConnection(context.Background(), "la-clave", ProbeOptions{Endpoint: f.url}); !res.OK {
		t.Fatalf("setup failed: %+v", res)
	}
	if got := f.apiKeyHeader(); got != "la-clave" {
		t.Errorf("xi-api-key = %q, want la-clave", got)
	}
}

// An unreachable service is not a bad key.
func TestProbeReportsAnUnreachableService(t *testing.T) {
	res := TestConnection(context.Background(), "una-clave", ProbeOptions{Endpoint: "ws://127.0.0.1:1"})
	if res.OK || res.Kind != stt.ProbeFailed {
		t.Errorf("Kind = %v, want ProbeFailed: %+v", res.Kind, res)
	}
}
