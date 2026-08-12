package azureopenai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildEndpointUsesTheGATranscriptionIntent(t *testing.T) {
	got, err := BuildEndpoint("mi-recurso-openai")
	if err != nil {
		t.Fatalf("BuildEndpoint: %v", err)
	}
	want := "wss://mi-recurso-openai.openai.azure.com/openai/v1/realtime?intent=transcription"
	if got != want {
		t.Errorf("endpoint = %q, want %q", got, want)
	}
}

func TestBuildEndpointRejectsAnythingThatCouldEscapeTheAzureHostname(t *testing.T) {
	for _, resource := range []string{
		"", "   ", "https://example.com", "name.openai.azure.com", "name/path", "name?api-key=secret",
		"-starts-with-hyphen", "ends-with-hyphen-", "espacio aquí",
	} {
		t.Run(resource, func(t *testing.T) {
			if endpoint, err := BuildEndpoint(resource); err == nil {
				t.Errorf("BuildEndpoint(%q) = %q, want an error", resource, endpoint)
			}
		})
	}
}

func TestDialOptionsPutTheKeyOnlyInTheAPIKeyHeader(t *testing.T) {
	const key = "azure-openai-secret-sentinel"
	opts := DialOptions(key)
	if got := opts.HTTPHeader.Get("api-key"); got != key {
		t.Errorf("api-key header = %q, want the supplied key", got)
	}
	if len(opts.Subprotocols) != 0 {
		t.Errorf("subprotocols = %v, Azure OpenAI authenticates with api-key", opts.Subprotocols)
	}
	for name, values := range opts.HTTPHeader {
		if name != "Api-Key" && strings.Contains(strings.Join(values, ""), key) {
			t.Errorf("credential appeared in unexpected header %q", name)
		}
	}
}

func TestBuildSessionUpdateUsesTheDeploymentAndDisablesVAD(t *testing.T) {
	raw, err := BuildSessionUpdate("mi-deployment-whisper", "es")
	if err != nil {
		t.Fatalf("BuildSessionUpdate: %v", err)
	}

	var message struct {
		Type    string `json:"type"`
		Session struct {
			Type  string `json:"type"`
			Audio struct {
				Input struct {
					Format struct {
						Type string `json:"type"`
						Rate int    `json:"rate"`
					} `json:"format"`
					Transcription struct {
						Model    string `json:"model"`
						Language string `json:"language"`
					} `json:"transcription"`
					TurnDetection json.RawMessage `json:"turn_detection"`
				} `json:"input"`
			} `json:"audio"`
		} `json:"session"`
	}
	if err := json.Unmarshal(raw, &message); err != nil {
		t.Fatalf("unmarshal session.update: %v", err)
	}
	if message.Type != "session.update" || message.Session.Type != "transcription" {
		t.Errorf("session envelope = type %q session.type %q", message.Type, message.Session.Type)
	}
	input := message.Session.Audio.Input
	if input.Format.Type != "audio/pcm" || input.Format.Rate != 24000 {
		t.Errorf("format = %+v, want 24 kHz audio/pcm", input.Format)
	}
	if input.Transcription.Model != "mi-deployment-whisper" || input.Transcription.Language != "es" {
		t.Errorf("transcription = %+v", input.Transcription)
	}
	if string(input.TurnDetection) != "null" {
		t.Errorf("turn_detection = %s, gpt-realtime-whisper requires null", input.TurnDetection)
	}
}
