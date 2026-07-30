package elevenlabs

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

// A stand-in for the ElevenLabs realtime endpoint.
//
// A REAL SOCKET, not a mocked interface. What this provider gets wrong is ORDERING and TIMING — audio
// flushed after the commit, a stop racing its own audio, a session that never confirms — and none of
// those are reachable by stubbing the library. The Grok suite is built the same way for the same
// reason.
type fakeEleven struct {
	t      *testing.T
	server *httptest.Server
	url    string

	mu      sync.Mutex
	texts   []string    // every text frame the client sent, in order
	header  http.Header // the upgrade request's headers
	query   url.Values  // its query string
	updates chan struct{}
}

func newFakeEleven(t *testing.T, script func(f *fakeEleven, conn *websocket.Conn)) *fakeEleven {
	t.Helper()
	f := &fakeEleven{t: t, updates: make(chan struct{}, 64)}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.header = r.Header.Clone()
		f.query = r.URL.Query()
		f.mu.Unlock()

		conn, err := websocket.Accept(w, r, nil)
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

// newRejectingEleven refuses the upgrade with a status, which is how a bad key actually presents.
func newRejectingEleven(t *testing.T, status int, body string) *fakeEleven {
	t.Helper()
	f := &fakeEleven{t: t, updates: make(chan struct{}, 1)}
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

func (f *fakeEleven) send(conn *websocket.Conn, payload string) {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(payload)); err != nil {
		f.t.Logf("el servidor falso no pudo escribir: %v", err)
	}
}

func (f *fakeEleven) ready(conn *websocket.Conn) {
	f.send(conn, `{"message_type":"session_started"}`)
}

// audioChunks returns the decoded PCM of every input_audio_chunk received, and whether each committed.
func (f *fakeEleven) audioChunks() (pcm [][]byte, commits []bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, raw := range f.texts {
		var msg struct {
			MessageType string `json:"message_type"`
			AudioBase64 string `json:"audio_base_64"`
			Commit      bool   `json:"commit"`
		}
		if err := json.Unmarshal([]byte(raw), &msg); err != nil || msg.MessageType != "input_audio_chunk" {
			continue
		}
		data, _ := base64.StdEncoding.DecodeString(msg.AudioBase64)
		pcm = append(pcm, data)
		commits = append(commits, msg.Commit)
	}
	return pcm, commits
}

// waitForCommit blocks until a chunk with commit=true has arrived, or the deadline passes.
func (f *fakeEleven) waitForCommit(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if _, commits := f.audioChunks(); len(commits) > 0 && commits[len(commits)-1] {
			return true
		}
		select {
		case <-f.updates:
		case <-time.After(10 * time.Millisecond):
		}
	}
	return false
}

func (f *fakeEleven) apiKeyHeader() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.header.Get(APIKeyHeader)
}

func (f *fakeEleven) queryParams() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.query
}
