package grok

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Juan-Motta/loqui-go/internal/stt"
)

// An empty key never touches the network. Dialling to be told what we already know would cost a round
// trip and, for a metered service, a connection.
func TestProbeWithNoKeyNeverDials(t *testing.T) {
	dialed := false
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) { dialed = true })

	res := TestConnection(context.Background(), "  ", ProbeOptions{Endpoint: g.url})

	if res.Kind != stt.ProbeNoKey {
		t.Errorf("Kind = %v, want ProbeNoKey", res.Kind)
	}
	if res.OK {
		t.Error("OK with no key")
	}
	if dialed {
		t.Error("it dialled anyway")
	}
}

// THE GREEN PATH, and the only thing that produces it: a readiness message, by name.
func TestProbeSucceedsOnlyOnTheReadinessMessage(t *testing.T) {
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) { g.ready(conn) })

	res := TestConnection(context.Background(), "una-clave", ProbeOptions{Endpoint: g.url})

	if !res.OK || res.Kind != stt.ProbeOK {
		t.Fatalf("OK=%v Kind=%v Message=%q — a readiness message must pass", res.OK, res.Kind, res.Message)
	}
	if res.Message == "" {
		t.Error("a success says nothing")
	}
}

// THE FALSE GREEN THIS DESIGN EXISTS TO PREVENT. A server that accepts the upgrade and then closes,
// says nothing, or reports an error must NEVER read as a working credential.
//
// Measured, not imagined: OpenAI and ElevenLabs both return 101 for a garbage key and refuse afterwards
// (docs/research/2026-08-06-where-realtime-stt-auth-fails.md). xAI refuses at the handshake, but the
// same protocol is used for all three so that no provider's behaviour has to be trusted.
func TestProbeNeverSucceedsWithoutReadiness(t *testing.T) {
	for _, c := range []struct {
		name   string
		script func(g *fakeGrok, conn *websocket.Conn)
	}{
		{"closes immediately with a NORMAL code", func(g *fakeGrok, conn *websocket.Conn) {
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}},
		{"sends an error event", func(g *fakeGrok, conn *websocket.Conn) {
			g.send(conn, `{"type":"error","message":"algo pasó"}`)
		}},
		{"sends a transcript without ever being ready", func(g *fakeGrok, conn *websocket.Conn) {
			g.send(conn, `{"type":"transcript.partial","text":"hola","is_final":false}`)
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}},
		{"says nothing at all", func(g *fakeGrok, conn *websocket.Conn) { time.Sleep(400 * time.Millisecond) }},
		{"sends malformed JSON", func(g *fakeGrok, conn *websocket.Conn) {
			g.send(conn, `{no es json`)
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := newFakeGrok(t, c.script)
			res := TestConnection(context.Background(), "una-clave", ProbeOptions{
				Endpoint: g.url, ReadyTimeout: 250 * time.Millisecond,
			})
			if res.OK {
				t.Errorf("OK=true — a false green: %+v", res)
			}
			if res.Kind == stt.ProbeOK {
				t.Errorf("Kind = ProbeOK: %+v", res)
			}
		})
	}
}

// A refused handshake is a rejected KEY, and it says so in our words — never the provider's, which were
// measured carrying key material.
func TestProbeReportsARefusedHandshake(t *testing.T) {
	for _, c := range []struct {
		status int
		kind   stt.ProbeKind
	}{
		{http.StatusUnauthorized, stt.ProbeKeyRejected},
		{http.StatusForbidden, stt.ProbeKeyRejected},
		{http.StatusTooManyRequests, stt.ProbeFailed},
		{http.StatusInternalServerError, stt.ProbeFailed},
	} {
		t.Run(http.StatusText(c.status), func(t *testing.T) {
			g := newRejectingGrok(t, c.status)
			res := TestConnection(context.Background(), "clave-secreta-inconfundible", ProbeOptions{Endpoint: g.url})

			if res.OK {
				t.Error("OK on a refused handshake")
			}
			if res.Kind != c.kind {
				t.Errorf("Kind = %v, want %v (message %q)", res.Kind, c.kind, res.Message)
			}
			if strings.Contains(res.Message, "clave-secreta-inconfundible") ||
				strings.Contains(res.Code, "clave-secreta-inconfundible") {
				t.Errorf("the credential is in the result: %+v", res)
			}
		})
	}
}

// An unreachable service is not a bad key, and saying so would send the user to replace a good one.
func TestProbeReportsAnUnreachableService(t *testing.T) {
	res := TestConnection(context.Background(), "una-clave", ProbeOptions{
		Endpoint: "ws://127.0.0.1:1", // nothing listens on port 1
	})
	if res.OK || res.Kind != stt.ProbeFailed {
		t.Errorf("Kind = %v, want ProbeFailed: %+v", res.Kind, res)
	}
}

// A cancelled context returns promptly and does not report a credential problem.
func TestProbeHonoursACancelledContext(t *testing.T) {
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) { time.Sleep(2 * time.Second) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	res := TestConnection(ctx, "una-clave", ProbeOptions{Endpoint: g.url})

	if res.OK || res.Kind != stt.ProbeFailed {
		t.Errorf("Kind = %v, want ProbeFailed", res.Kind)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %v — a cancelled context must not be waited out", elapsed)
	}
}

// The probe SENDS NOTHING. Audio, a setup message or a finalize would start, configure or flush
// transcription work — the probe is about the credential, not a session.
func TestProbeSendsNothing(t *testing.T) {
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) {
		g.ready(conn)
		time.Sleep(150 * time.Millisecond)
	})

	if res := TestConnection(context.Background(), "una-clave", ProbeOptions{Endpoint: g.url}); !res.OK {
		t.Fatalf("setup failed: %+v", res)
	}

	// WAITED FOR, not sampled. Asserting on the snapshot the instant TestConnection returns races a
	// frame still travelling: the check passed against a mutant that DID write one. The server signals
	// arrived on every frame it reads, so waiting for silence is the deterministic form of "nothing
	// was sent".
	select {
	case <-g.arrived:
		frames := g.snapshot()
		t.Errorf("the probe sent %d frame(s), the first being %q", len(frames), frames[0].data)
	case <-time.After(300 * time.Millisecond):
	}
}

// The credential travels in the header the runtime uses, so the probe tests the same handshake
// dictation does. A probe that authenticated differently would bless a key dictation cannot use.
func TestProbeSendsTheSameAuthAsDictation(t *testing.T) {
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) { g.ready(conn) })

	if res := TestConnection(context.Background(), "la-clave", ProbeOptions{Endpoint: g.url}); !res.OK {
		t.Fatalf("setup failed: %+v", res)
	}
	if got := g.authHeader(); got != "Bearer la-clave" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer la-clave")
	}
}
