package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// A stand-in for the OpenAI realtime endpoint.
//
// A REAL SOCKET, not a mocked interface. What this provider gets wrong is ORDERING and TIMING — audio
// flushed after the commit, a stop racing its own audio, a session that never confirms — and none of
// those are reachable by stubbing the library. The Grok suite is built the same way for the same
// reason.
type fakeOpenAI struct {
	t      *testing.T
	server *httptest.Server
	url    string

	mu      sync.Mutex
	texts   []string    // every text frame the client sent, in order
	header  http.Header // the upgrade request's headers
	query   url.Values  // its query string
	updates chan struct{}
}

func newFakeOpenAI(t *testing.T, script func(f *fakeOpenAI, conn *websocket.Conn)) *fakeOpenAI {
	t.Helper()
	f := &fakeOpenAI{t: t, updates: make(chan struct{}, 64)}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.header = r.Header.Clone()
		f.query = r.URL.Query()
		f.mu.Unlock()

		// The client offers ["realtime", "openai-insecure-api-key.<KEY>"]; accepting the first is what
		// the real service does, and recording them is how the key-placement test checks the handshake.
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols: []string{"realtime"},
		})
		if err != nil {
			return
		}
		defer conn.CloseNow()

		// Reading in the background so the script can send whenever it likes; the client's frames are
		// recorded in order.
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				typ, data, err := conn.Read(context.Background())
				if err != nil {
					return
				}
				if typ != websocket.MessageText {
					continue
				}
				f.mu.Lock()
				f.texts = append(f.texts, string(data))
				f.mu.Unlock()
				select {
				case f.updates <- struct{}{}:
				default:
				}
			}
		}()
		script(f, conn)
		<-done
	}))
	f.url = "ws" + strings.TrimPrefix(f.server.URL, "http")
	t.Cleanup(f.server.Close)
	return f
}

// newRejectingOpenAI refuses the upgrade with a status, which is how a bad key actually presents.
func newRejectingOpenAI(t *testing.T, status int, body string) *fakeOpenAI {
	t.Helper()
	f := &fakeOpenAI{t: t, updates: make(chan struct{}, 1)}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.header = r.Header.Clone()
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != "" {
			_, _ = w.Write([]byte(body))
		}
	}))
	f.url = "ws" + strings.TrimPrefix(f.server.URL, "http")
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeOpenAI) send(conn *websocket.Conn, payload string) {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(payload)); err != nil {
		f.t.Logf("el servidor falso no pudo escribir: %v", err)
	}
}

// The service's own handshake events. This provider does not WAIT for them — it configures the session
// and starts — but a realistic server sends them, and a test that never emitted them could hide a
// provider that mistakenly blocked on one.
func (f *fakeOpenAI) ready(conn *websocket.Conn) {
	f.send(conn, `{"type":"session.created"}`)
	f.send(conn, `{"type":"session.updated"}`)
}

// audioChunks returns the decoded PCM of every input_audio_buffer.append received, in order.
func (f *fakeOpenAI) audioChunks() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][]byte
	for _, raw := range f.texts {
		var msg struct {
			Type  string `json:"type"`
			Audio string `json:"audio"`
		}
		if err := json.Unmarshal([]byte(raw), &msg); err != nil || msg.Type != "input_audio_buffer.append" {
			continue
		}
		data, _ := base64.StdEncoding.DecodeString(msg.Audio)
		out = append(out, data)
	}
	return out
}

// sessionUpdate returns the session.update the client sent, if any. Nothing transcribes until it lands,
// so its absence is a silent, total failure.
func (f *fakeOpenAI) sessionUpdate() (map[string]any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, raw := range f.texts {
		var msg map[string]any
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			continue
		}
		if msg["type"] == "session.update" {
			return msg, true
		}
	}
	return nil, false
}

// waitForSessionUpdate blocks until the configuration message arrives.
func (f *fakeOpenAI) waitForSessionUpdate(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if _, ok := f.sessionUpdate(); ok {
			return true
		}
		select {
		case <-f.updates:
		case <-time.After(5 * time.Millisecond):
		}
	}
	return false
}

// waitForAudio blocks until at least n append messages have arrived.
func (f *fakeOpenAI) waitForAudio(n int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if len(f.audioChunks()) >= n {
			return true
		}
		select {
		case <-f.updates:
		case <-time.After(5 * time.Millisecond):
		}
	}
	return false
}

func (f *fakeOpenAI) countType(want string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, raw := range f.texts {
		var msg struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(raw), &msg) == nil && msg.Type == want {
			count++
		}
	}
	return count
}

func (f *fakeOpenAI) waitForType(want string, n int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if f.countType(want) >= n {
			return true
		}
		select {
		case <-f.updates:
		case <-time.After(5 * time.Millisecond):
		}
	}
	return false
}

// requestedSubprotocols is what the client offered in the handshake — where this API's credential goes.
func (f *fakeOpenAI) requestedSubprotocols() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.header.Get("Sec-WebSocket-Protocol")
}

func (f *fakeOpenAI) queryParams() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.query
}
