package azureopenai

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/Juan-Motta/loqui-go/internal/stt"
	"github.com/Juan-Motta/loqui-go/internal/stt/openai"
)

const defaultProbeReadyTimeout = 10 * time.Second

type ProbeOptions struct {
	Resource     string
	Deployment   string
	Language     string
	Endpoint     string
	ReadyTimeout time.Duration
}

func (o ProbeOptions) endpoint() (string, error) {
	if strings.TrimSpace(o.Endpoint) != "" {
		return strings.TrimSpace(o.Endpoint), nil
	}
	return BuildEndpoint(o.Resource)
}

func (o ProbeOptions) readyTimeout() time.Duration {
	if o.ReadyTimeout > 0 {
		return o.ReadyTimeout
	}
	return defaultProbeReadyTimeout
}

// TestConnection proves more than a successful upgrade: it sends the actual session.update and
// waits until Azure answers session.updated, the first point at which the deployment is usable.
func TestConnection(ctx context.Context, key string, opts ProbeOptions) stt.ProbeResult {
	key = strings.TrimSpace(key)
	if key == "" {
		return stt.ProbeResult{Kind: stt.ProbeNoKey, Message: "falta la clave: escríbela o guárdala antes de probar"}
	}
	endpoint, err := opts.endpoint()
	if err != nil {
		return stt.ProbeResult{Kind: stt.ProbeFailed, Message: err.Error(), Code: "invalid_resource"}
	}
	update, err := BuildSessionUpdate(opts.Deployment, opts.Language)
	if err != nil || strings.TrimSpace(opts.Deployment) == "" {
		return stt.ProbeResult{Kind: stt.ProbeFailed, Message: "el deployment de Azure OpenAI es obligatorio", Code: "invalid_deployment"}
	}

	probeCtx, cancel := context.WithTimeout(ctx, opts.readyTimeout())
	defer cancel()
	conn, resp, err := websocket.Dial(probeCtx, endpoint, DialOptions(key))
	if err != nil {
		return azureHandshakeResult(resp, err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)
	if err := conn.Write(probeCtx, websocket.MessageText, update); err != nil {
		return stt.ProbeResult{Kind: stt.ProbeFailed, Message: "se perdió la conexión con Azure OpenAI antes de configurar la sesión", Code: "network"}
	}
	for {
		_, raw, err := conn.Read(probeCtx)
		if err != nil {
			if probeCtx.Err() != nil {
				return stt.ProbeResult{Kind: stt.ProbeFailed, Message: "Azure OpenAI no confirmó la configuración de la sesión a tiempo", Code: "timeout"}
			}
			return stt.ProbeResult{Kind: stt.ProbeFailed, Message: "se perdió la conexión con Azure OpenAI antes de confirmar la sesión", Code: "network"}
		}
		out := openai.Decode(raw)
		switch out.Kind {
		case openai.Configured:
			return stt.ProbeResult{OK: true, Kind: stt.ProbeOK, Message: "Conexión correcta: Azure OpenAI aceptó la clave y el deployment"}
		case openai.Error:
			kind := stt.ProbeFailed
			if azureAuthCode(out.Code) {
				kind = stt.ProbeKeyRejected
			}
			return stt.ProbeResult{Kind: kind, Message: azureProbeMessage(kind), Code: safeCode(out.Code)}
		}
	}
}

func azureHandshakeResult(resp *http.Response, err error) stt.ProbeResult {
	if resp == nil {
		return stt.ProbeResult{Kind: stt.ProbeFailed, Message: "no se pudo contactar con Azure OpenAI — comprueba tu conexión a internet", Code: "network", Detail: fmt.Sprint(err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return stt.ProbeResult{Kind: stt.ProbeKeyRejected, Message: "Azure OpenAI rechazó la API key — revísala en Ajustes", Code: fmt.Sprintf("http_%d", resp.StatusCode)}
	}
	return stt.ProbeResult{Kind: stt.ProbeFailed, Message: fmt.Sprintf("Azure OpenAI rechazó la conexión (status %d)", resp.StatusCode), Code: fmt.Sprintf("http_%d", resp.StatusCode)}
}

func azureAuthCode(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "invalid_api_key", "auth_error", "authentication_error", "invalid_authentication", "unauthorized":
		return true
	}
	return false
}

func azureProbeMessage(kind stt.ProbeKind) string {
	if kind == stt.ProbeKeyRejected {
		return "Azure OpenAI rechazó la API key — revísala en Ajustes"
	}
	return "Azure OpenAI rechazó la configuración del recurso o deployment"
}

func safeCode(code string) string {
	code = strings.TrimSpace(code)
	if len(code) > 80 {
		return "provider_error"
	}
	for _, r := range code {
		if !(r == '_' || r == '-' || r == '.' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return "provider_error"
		}
	}
	return code
}
