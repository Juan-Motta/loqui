// Package azureopenai adapts Azure OpenAI's GA realtime transcription wire contract to Loqui's
// shared OpenAI realtime lifecycle. Azure Speech remains a separate SDK-backed provider.
package azureopenai

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/coder/websocket"

	"github.com/Juan-Motta/loqui-go/internal/stt/openai"
)

const endpointSuffix = ".openai.azure.com/openai/v1/realtime?intent=transcription"

// BuildEndpoint turns the Azure resource NAME from settings into the fixed GA transcription URL.
// It deliberately accepts no scheme, host, path, query, or port: those would turn a settings field
// into an arbitrary credential-bearing request target.
func BuildEndpoint(resource string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(resource))
	if name == "" || len(name) > 64 {
		return "", fmt.Errorf("el recurso Azure OpenAI debe tener entre 1 y 64 caracteres")
	}
	for i, r := range name {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'z' {
			continue
		}
		if r == '-' && i > 0 && i < len(name)-1 {
			continue
		}
		return "", fmt.Errorf("el recurso Azure OpenAI solo puede contener letras, números y guiones interiores")
	}
	if name[0] == '-' || name[len(name)-1] == '-' {
		return "", fmt.Errorf("el recurso Azure OpenAI debe empezar y terminar con una letra o un número")
	}
	return "wss://" + name + endpointSuffix, nil
}

// DialOptions authenticates before the WebSocket upgrade. The key stays in a header rather than the
// URL so request URLs remain safe to log.
func DialOptions(key string) *websocket.DialOptions {
	header := make(http.Header)
	header.Set("api-key", key)
	return &websocket.DialOptions{HTTPHeader: header}
}

// BuildSessionUpdate names the user's Azure deployment and disables server VAD, which
// gpt-realtime-whisper does not support.
func BuildSessionUpdate(deployment, language string) ([]byte, error) {
	return openai.BuildSessionUpdateWithManualCommit(deployment, language, true)
}
