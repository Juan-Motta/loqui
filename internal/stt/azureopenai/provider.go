package azureopenai

import (
	"fmt"
	"strings"
	"time"

	"github.com/Juan-Motta/loqui-go/internal/stt/openai"
)

// DefaultCommitInterval follows Microsoft's reference client. gpt-realtime-whisper does not
// support server VAD, so Loqui commits non-empty audio buffers itself while dictation is active.
const DefaultCommitInterval = 3 * time.Second

// Config identifies one Azure OpenAI deployment. Resource is the short resource name, never a URL;
// BuildEndpoint pins the hostname and rejects anything that could redirect a credential elsewhere.
type Config struct {
	Resource   string
	Deployment string
	Language   string
	GetKey     func() (string, error)
	Log        func(tag, msg string)

	Endpoint       string
	CommitInterval time.Duration
}

// New adapts Azure's handshake and manual-commit rules to the shared OpenAI realtime lifecycle.
func New(cfg Config) (*openai.Provider, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		var err error
		endpoint, err = BuildEndpoint(cfg.Resource)
		if err != nil {
			return nil, err
		}
	}
	deployment := strings.TrimSpace(cfg.Deployment)
	if deployment == "" {
		return nil, fmt.Errorf("azure openai: el deployment es obligatorio")
	}
	interval := cfg.CommitInterval
	if interval <= 0 {
		interval = DefaultCommitInterval
	}
	return openai.New(openai.Config{
		GetKey:                cfg.GetKey,
		Language:              cfg.Language,
		Model:                 deployment,
		Endpoint:              endpoint,
		ServiceName:           "Azure OpenAI",
		DialOptions:           DialOptions,
		SessionUpdate:         BuildSessionUpdate,
		RequireSessionUpdated: true,
		CommitInterval:        interval,
		SanitizeServerErrors:  true,
		Log:                   cfg.Log,
	}), nil
}
