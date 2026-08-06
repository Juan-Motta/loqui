// Testing a credential without starting a session. See internal/stt/grok/probe.go for the protocol and
// why it is not simply "dial and close" — for THIS service that shortcut is provably wrong: measured on
// 2026-08-06, an invalid key still gets an HTTP 101 and the refusal arrives afterwards as an event
// (docs/research/2026-08-06-where-realtime-stt-auth-fails.md).
package elevenlabs

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

// defaultProbeReadyTimeout bounds the wait for the first message. Shorter than dictation's readiness
// budget: someone is watching a button spin.
const defaultProbeReadyTimeout = 10 * time.Second

// ProbeOptions is what the probe needs beyond the key. Both fields exist for the tests.
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

// TestConnection reports whether ElevenLabs accepts this credential.
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
		// The same header dictation uses. A probe that authenticated differently would bless a
		// credential the microphone cannot use.
		HTTPHeader: http.Header{APIKeyHeader: []string{key}},
	})
	if err != nil {
		return handshakeResult(resp, key)
	}
	// CloseNow, never Close: Close takes no context, writes a close frame and waits for the peer. With a
	// synchronous read and no CloseRead there is no goroutine to join, so this returns at once.
	defer conn.CloseNow()

	conn.SetReadLimit(1 << 20)

	_, raw, err := conn.Read(ctx)
	if err != nil {
		return firstMessageFailure(ctx, err)
	}

	switch out := Decode(raw); out.Kind {
	case Ready:
		return stt.ProbeResult{
			OK:      true,
			Kind:    stt.ProbeOK,
			Message: "Conexión correcta: ElevenLabs abrió la sesión con esa clave",
		}
	case Error:
		// The service's own machine-readable code is shown; its PROSE is not. These services were
		// measured echoing key material back, masked in the middle with the tail intact.
		kind := stt.ProbeFailed
		if isAuthCode(out.Code) {
			kind = stt.ProbeKeyRejected
		}
		return stt.ProbeResult{
			Kind:    kind,
			Message: probeMessageFor(kind),
			Code:    out.Code,
		}
	default:
		return stt.ProbeResult{
			Kind:    stt.ProbeFailed,
			Message: "ElevenLabs respondió algo inesperado antes de confirmar la sesión",
		}
	}
}

// isAuthCode reports whether the service's own code means "your credential is wrong".
//
// Matched on the CODE, never on the message: prose is the thing that changes between versions and
// between locales, and classifying by it is the bug internal/session/policy.go:36 documents.
func isAuthCode(code string) bool {
	switch code {
	case "invalid_api_key", "auth_error", "authentication_error", "invalid_authentication":
		return true
	}
	return false
}

func probeMessageFor(kind stt.ProbeKind) string {
	if kind == stt.ProbeKeyRejected {
		return "ElevenLabs rechazó la API key — revísala en Ajustes"
	}
	return "ElevenLabs rechazó la conexión"
}

// handshakeResult classifies a refused upgrade with the same reader dictation uses.
func handshakeResult(resp *http.Response, key string) stt.ProbeResult {
	code, message := handshakeFailure(resp, key)
	if code == codeAuth || code == codeForbidden {
		return stt.ProbeResult{Kind: stt.ProbeKeyRejected, Message: message, Code: code}
	}
	if resp == nil {
		return stt.ProbeResult{
			Kind:    stt.ProbeFailed,
			Message: "no se pudo contactar con ElevenLabs — comprueba tu conexión a internet",
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
			Message: "ElevenLabs aceptó la conexión pero no confirmó la sesión a tiempo",
		}
	}
	var closeErr websocket.CloseError
	if errors.As(err, &closeErr) {
		// The close CODE is machine-readable and worth showing; its reason is server text and is not.
		return stt.ProbeResult{
			Kind:    stt.ProbeFailed,
			Message: "ElevenLabs cerró la conexión antes de confirmar la sesión",
			Code:    fmt.Sprintf("close %d", int(closeErr.Code)),
		}
	}
	return stt.ProbeResult{
		Kind:    stt.ProbeFailed,
		Message: "se perdió la conexión con ElevenLabs antes de confirmar la sesión",
	}
}
