package grok

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// A local stand-in for wss://api.x.ai/v1/stt.
//
// A REAL WebSocket server, not a mock of the library: the things most likely to be wrong here
// are what actually goes on the wire (binary vs text frames, the auth header, the order of
// audio.done relative to buffered audio) and how the client behaves when a socket dies. A fake
// conn cannot show any of that.
type fakeGrok struct {
	t   *testing.T
	srv *httptest.Server
	url string

	mu     sync.Mutex
	frames []frame
	header http.Header
	query  url.Values

	arrived chan struct{} // signalled on every frame received
}

type frame struct {
	typ  websocket.MessageType
	data []byte
}

// newFakeGrok starts a server that accepts the upgrade and hands the connection to script.
// Every frame the client sends is recorded before script sees it.
func newFakeGrok(t *testing.T, script func(g *fakeGrok, conn *websocket.Conn)) *fakeGrok {
	t.Helper()
	g := &fakeGrok{t: t, arrived: make(chan struct{}, 256)}

	g.srv = httptest.NewServer(http.HandlerFunc(func(wr http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.header = r.Header.Clone()
		g.query = r.URL.Query()
		g.mu.Unlock()

		conn, err := websocket.Accept(wr, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		// Record everything the client sends, continuously, so a script can both react to
		// frames and assert on them afterwards.
		readDone := make(chan struct{})
		go func() {
			defer close(readDone)
			for {
				typ, data, err := conn.Read(context.Background())
				if err != nil {
					return
				}
				g.mu.Lock()
				g.frames = append(g.frames, frame{typ: typ, data: data})
				g.mu.Unlock()
				select {
				case g.arrived <- struct{}{}:
				default:
				}
			}
		}()

		if script != nil {
			script(g, conn)
		}
		<-readDone
	}))
	t.Cleanup(g.srv.Close)

	g.url = "ws" + strings.TrimPrefix(g.srv.URL, "http")
	return g
}

// newRejectingGrok refuses the upgrade with a status, which is how a bad key fails: auth is an
// HTTP header, so it never gets as far as a WebSocket frame.
func newRejectingGrok(t *testing.T, status int) *fakeGrok {
	t.Helper()
	g := &fakeGrok{t: t, arrived: make(chan struct{}, 1)}
	g.srv = httptest.NewServer(http.HandlerFunc(func(wr http.ResponseWriter, r *http.Request) {
		http.Error(wr, http.StatusText(status), status)
	}))
	t.Cleanup(g.srv.Close)
	g.url = "ws" + strings.TrimPrefix(g.srv.URL, "http")
	return g
}

func (g *fakeGrok) send(conn *websocket.Conn, json string) {
	g.t.Helper()
	if err := conn.Write(context.Background(), websocket.MessageText, []byte(json)); err != nil {
		g.t.Logf("fake server write failed (client may have closed): %v", err)
	}
}

func (g *fakeGrok) ready(conn *websocket.Conn) {
	g.send(conn, `{"type":"transcript.created","id":"test-session"}`)
}

// waitForText blocks until the client has sent a text frame containing want.
//
// FAILS THE TEST IF IT NEVER ARRIVES, rather than returning quietly. Scripts use this to wait
// for audio.done before replying transcript.done; a version whose timeout the caller ignored
// would let "the client never sent audio.done" pass as a success, since the reply would go out
// regardless. Reported with Errorf, not Fatalf: this runs on the server goroutine, and FailNow
// may only be called from the test's own.
func (g *fakeGrok) waitForText(want string) bool {
	deadline := time.After(3 * time.Second)
	for {
		if g.hasText(want) {
			return true
		}
		select {
		case <-g.arrived:
		case <-deadline:
			if g.hasText(want) {
				return true
			}
			g.t.Errorf("the client never sent a text frame containing %q", want)
			return false
		}
	}
}

func (g *fakeGrok) hasText(want string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, f := range g.frames {
		if f.typ == websocket.MessageText && strings.Contains(string(f.data), want) {
			return true
		}
	}
	return false
}

// binaryPayload concatenates every binary frame, which is the PCM the service received.
func (g *fakeGrok) binaryPayload() []byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []byte
	for _, f := range g.frames {
		if f.typ == websocket.MessageBinary {
			out = append(out, f.data...)
		}
	}
	return out
}

func (g *fakeGrok) snapshot() []frame {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]frame(nil), g.frames...)
}

func (g *fakeGrok) authHeader() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.header.Get("Authorization")
}

func (g *fakeGrok) queryParams() url.Values {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.query
}
