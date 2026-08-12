package azureopenai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestConnectionRequiresSessionUpdatedAndUsesAzureHandshake(t *testing.T) {
	var header http.Header
	gotUpdate := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Clone()
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_, raw, err := conn.Read(context.Background())
		if err != nil {
			return
		}
		gotUpdate <- string(raw)
		_ = conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"session.updated"}`))
	}))
	defer srv.Close()

	result := TestConnection(context.Background(), "secret-sentinel", ProbeOptions{
		Endpoint:     "ws" + strings.TrimPrefix(srv.URL, "http"),
		Deployment:   "my-whisper-deployment",
		ReadyTimeout: time.Second,
	})

	if !result.OK {
		t.Fatalf("probe failed: %+v", result)
	}
	if got := header.Get("api-key"); got != "secret-sentinel" {
		t.Errorf("api-key = %q", got)
	}
	if got := header.Get("Sec-WebSocket-Protocol"); got != "" {
		t.Errorf("Azure handshake unexpectedly offered subprotocols: %q", got)
	}
	select {
	case raw := <-gotUpdate:
		if !strings.Contains(raw, `"model":"my-whisper-deployment"`) {
			t.Errorf("session.update = %s", raw)
		}
	default:
		t.Fatal("probe never configured the transcription session")
	}
}

func TestConnectionDoesNotTreatSessionCreatedAsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_, _, _ = conn.Read(context.Background())
		_ = conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"session.created"}`))
	}))
	defer srv.Close()

	result := TestConnection(context.Background(), "secret", ProbeOptions{
		Endpoint:     "ws" + strings.TrimPrefix(srv.URL, "http"),
		Deployment:   "deployment",
		ReadyTimeout: 100 * time.Millisecond,
	})
	if result.OK {
		t.Fatal("session.created only proves the socket opened; Azure has not accepted session.update")
	}
}
