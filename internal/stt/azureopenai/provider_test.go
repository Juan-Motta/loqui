package azureopenai

import (
	"testing"
	"time"
)

func TestNewRejectsAnUnsafeResourceBeforeOpeningANetworkConnection(t *testing.T) {
	_, err := New(Config{
		Resource:   "victim.example.com/path",
		Deployment: "gpt-realtime-whisper",
		GetKey:     func() (string, error) { return "secret", nil },
	})
	if err == nil {
		t.Fatal("New accepted a resource that can escape Azure's fixed hostname")
	}
}

func TestNewRequiresTheDeploymentNameAzureActuallyRoutes(t *testing.T) {
	_, err := New(Config{
		Resource: "mi-recurso",
		GetKey:   func() (string, error) { return "secret", nil },
	})
	if err == nil {
		t.Fatal("New accepted an empty deployment")
	}
}

func TestDefaultsMatchAzureRealtimeWhisperRequirements(t *testing.T) {
	if DefaultCommitInterval != 3*time.Second {
		t.Errorf("commit interval = %s, want 3s", DefaultCommitInterval)
	}
}
