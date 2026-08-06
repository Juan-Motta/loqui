// Testing a credential without starting a session.
//
// THE PROTOCOL, and it is the same for all three WebSocket providers: dial, wait for the FIRST server
// message, classify it, close. Nothing is ever sent — no audio, no setup message, no finalize — because
// those start, configure or flush transcription work, and this is a question about a key.
//
// WHY NOT JUST DIAL AND CLOSE, which is what the first design did. Because for two of the three
// providers a garbage key still gets an HTTP 101: measured on 2026-08-06, OpenAI and ElevenLabs both
// accept the upgrade and refuse afterwards, as an event
// (docs/research/2026-08-06-where-realtime-stt-auth-fails.md). xAI does refuse at the handshake — it is
// the one that behaves as its docs suggest — but the protocol is uniform so that no provider's good
// behaviour has to be relied on. A probe that answered "your key works" to any string would be worse
// than no probe at all.
//
// SUCCESS IS POSITIVE ONLY. A readiness message, recognised by name, is the sole thing that produces
// OK. A clean close is NOT success: ElevenLabs closes with code 1000 after refusing a credential, so
// inferring a good key from a tidy shutdown reproduces the false green through another door. Silence,
// EOF, an unrecognised event and a timeout are all failures.
package grok

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/Juan-Motta/loqui-go/internal/stt"
)

// defaultProbeReadyTimeout bounds the wait for that first message.
//
// Shorter than dictation's readiness budget on purpose: a person is watching a button spin, and a
// service that has not said anything in this long is not going to transcribe either.
const defaultProbeReadyTimeout = 10 * time.Second

// ProbeOptions is what the probe needs beyond the key. Endpoint and the timeout exist for the tests;
// production passes neither.
type ProbeOptions struct {
	Endpoint     string
	ReadyTimeout time.Duration
}

func (o ProbeOptions) endpoint() string {
	if o.Endpoint != "" {
		return o.Endpoint
	}
	return WSEndpoint
}

func (o ProbeOptions) readyTimeout() time.Duration {
	if o.ReadyTimeout > 0 {
		return o.ReadyTimeout
	}
	return defaultProbeReadyTimeout
}

// TestConnection reports whether xAI accepts this credential.
func TestConnection(ctx context.Context, key string, opts ProbeOptions) stt.ProbeResult {
	key = strings.TrimSpace(key)
	if key == "" {
		return stt.ProbeResult{
			Kind:    stt.ProbeNoKey,
			Message: "falta la clave: escríbela o guárdala antes de probar",
		}
	}

	ctx, cancel := context.WithTimeout(ctx, opts.readyTimeout())
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, buildURL(opts.endpoint(), ""), &websocket.DialOptions{
		// The same handshake dictation uses. A probe that authenticated differently would bless a
		// credential the microphone cannot use.
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + key}},
	})
	if err != nil {
		return handshakeResult(resp, key, err)
	}
	// CloseNow, not Close: Close has no context, writes a close frame and waits up to 5 s for the
	// peer's answer, then up to 15 s in waitGoroutines. CloseNow closes the socket outright. Since the
	// probe reads synchronously and never calls CloseRead, there is no goroutine for waitGoroutines to
	// wait on, so this returns immediately.
	defer conn.CloseNow()

	// A rejection body can be large; a first message never is. This also stops a hostile endpoint from
	// making the probe allocate.
	conn.SetReadLimit(1 << 20)

	_, raw, err := conn.Read(ctx)
	if err != nil {
		// EOF, a close frame, or the budget running out. None of them is a working credential.
		return firstMessageFailure(ctx, err)
	}

	switch out := decode(raw); out.Kind {
	case outcomeReady:
		return stt.ProbeResult{
			OK:      true,
			Kind:    stt.ProbeOK,
			Message: "Conexión correcta: xAI aceptó la clave",
		}
	case outcomeError:
		// xAI's error event carries PROSE ONLY — its schema gives no structured code, which is why the
		// provider never branches on it either (see outcome.Error). So there is nothing machine-readable
		// to show, and the prose itself goes nowhere near the user: it is server-written text, and these
		// services were measured echoing key material.
		return stt.ProbeResult{
			Kind:    stt.ProbeFailed,
			Message: "xAI rechazó la conexión",
		}
	default:
		// A transcript, an unknown event, malformed JSON. The session is not confirmed, so this is not
		// a pass — being conservative here is the whole point.
		return stt.ProbeResult{
			Kind:    stt.ProbeFailed,
			Message: "xAI respondió algo inesperado antes de confirmar la sesión",
		}
	}
}

// handshakeResult classifies a refused upgrade, reusing the same reader dictation uses.
func handshakeResult(resp *http.Response, key string, dialErr error) stt.ProbeResult {
	code, message := handshakeFailure(resp, key)
	if code == codeAuth || code == codeForbidden {
		return stt.ProbeResult{Kind: stt.ProbeKeyRejected, Message: message, Code: code}
	}
	if resp == nil {
		// Never reached xAI: DNS, TLS, a refused connection. dialErr's text is Go's and is not for a
		// user; the caller logs it.
		return stt.ProbeResult{
			Kind:    stt.ProbeFailed,
			Message: "no se pudo contactar con xAI — comprueba tu conexión a internet",
			Code:    code,
		}
	}
	return stt.ProbeResult{Kind: stt.ProbeFailed, Message: message, Code: code}
}

// firstMessageFailure words the case where the socket opened and then gave nothing usable.
func firstMessageFailure(ctx context.Context, err error) stt.ProbeResult {
	if ctx.Err() != nil {
		return stt.ProbeResult{
			Kind:    stt.ProbeFailed,
			Message: "xAI aceptó la conexión pero no confirmó la sesión a tiempo",
		}
	}
	var closeErr websocket.CloseError
	if errors.As(err, &closeErr) {
		// The close CODE is machine-readable and worth showing; the reason is server text and is not.
		return stt.ProbeResult{
			Kind:    stt.ProbeFailed,
			Message: "xAI cerró la conexión antes de confirmar la sesión",
			Code:    fmt.Sprintf("close %d", int(closeErr.Code)),
		}
	}
	return stt.ProbeResult{
		Kind:    stt.ProbeFailed,
		Message: "se perdió la conexión con xAI antes de confirmar la sesión",
	}
}
