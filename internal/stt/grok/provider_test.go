package grok

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Juan-Motta/loqui-go/internal/stt"
	"github.com/coder/websocket"
)

// ---- test plumbing -----------------------------------------------------------

// recorder collects the events a provider emits, in order. The sink is called from the
// provider's own goroutines, so it locks.
type recorder struct {
	mu     sync.Mutex
	events []stt.Event
	got    chan struct{}
}

func newRecorder() *recorder {
	return &recorder{got: make(chan struct{}, 256)}
}

func (r *recorder) sink(e stt.Event) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
	select {
	case r.got <- struct{}{}:
	default:
	}
}

func (r *recorder) all() []stt.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]stt.Event(nil), r.events...)
}

// types is the event sequence, which is what almost every case here asserts: the ORDER matters
// as much as the contents. Emitting Stopped before a Final loses the dictation
// (internal/session/controller.go:308), and emitting Canceled before a Final loses it on the
// retry path, because the controller bumps the generation and then rejects the stale Final
// (controller.go:350, tracker.go:57).
func (r *recorder) types() []stt.EventType {
	out := []stt.EventType{}
	for _, e := range r.all() {
		out = append(out, e.Type)
	}
	return out
}

// waitForStopped blocks until the session is fully over.
//
// It waits for Stopped AND THEN for the provider's goroutines to wind down. Returning on the
// first Stopped alone would let a later event — a stray Final, a second Stopped, an event from a
// goroutine still running — arrive after the assertions had already snapshotted, so the very
// mistakes these tests exist to catch could slip through.
func (r *recorder) waitForStopped(t *testing.T, p *Provider) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !r.has(stt.Stopped) {
		select {
		case <-r.got:
		case <-deadline:
			t.Fatalf("no Stopped after 5s; got %v", r.types())
		}
	}

	done := make(chan struct{})
	go func() { p.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("Stopped arrived but the provider's goroutines are still running; events=%v", r.types())
	}
}

func (r *recorder) has(typ stt.EventType) bool {
	for _, e := range r.all() {
		if e.Type == typ {
			return true
		}
	}
	return false
}

func (r *recorder) first(typ stt.EventType) (stt.Event, bool) {
	for _, e := range r.all() {
		if e.Type == typ {
			return e, true
		}
	}
	return stt.Event{}, false
}

func (r *recorder) count(typ stt.EventType) int {
	n := 0
	for _, e := range r.all() {
		if e.Type == typ {
			n++
		}
	}
	return n
}

func wantSequence(t *testing.T, r *recorder, want ...stt.EventType) {
	t.Helper()
	got := r.types()
	if len(got) != len(want) {
		t.Fatalf("event sequence = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event sequence = %v, want %v", got, want)
		}
	}
}

// testProvider builds a provider pointed at the fake server, with timeouts short enough that
// the suite does not pay seconds for the timeout cases.
func testProvider(g *fakeGrok, tweak func(*Config)) *Provider {
	p, _ := testProviderWithLogs(g, tweak)
	return p
}

func testProviderWithLogs(g *fakeGrok, tweak func(*Config)) (*Provider, *logCapture) {
	logs := &logCapture{}
	cfg := Config{
		Endpoint:        g.url,
		GetKey:          func() (string, error) { return "xai-test-key", nil },
		Language:        "auto",
		Log:             logs.log,
		DialTimeout:     2 * time.Second,
		ReadyTimeout:    2 * time.Second,
		WriteTimeout:    2 * time.Second,
		FinalizeTimeout: 500 * time.Millisecond,
	}
	if tweak != nil {
		tweak(&cfg)
	}
	return New(cfg), logs
}

type logCapture struct {
	mu    sync.Mutex
	lines []string
}

func (l *logCapture) log(tag, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, tag+" "+msg)
}

func (l *logCapture) joined() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

// ---- the contract ------------------------------------------------------------

func TestWantsAudio(t *testing.T) {
	// The host captures once and pushes to every cloud provider; only the native helpers open
	// the microphone themselves.
	if !New(Config{}).WantsAudio() {
		t.Error("the Grok provider is fed PCM by the host, so WantsAudio must be true")
	}
}

// ---- the happy path ----------------------------------------------------------

func TestHappyPathAssemblesOneFinal(t *testing.T) {
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) {
		g.ready(conn)
		g.send(conn, `{"type":"transcript.partial","text":"hola","words":[],"is_final":false,"start":0,"duration":0.4}`)
		g.send(conn, `{"type":"transcript.partial","text":"hola mundo","words":[{"text":"hola","start":0,"end":0.5},{"text":"mundo","start":0.5,"end":1}],"is_final":true,"speech_final":true,"start":0,"duration":1}`)
		g.waitForText("audio.done")
		g.send(conn, `{"type":"transcript.done","text":"","words":[],"duration":1.2}`)
	})

	p := testProvider(g, nil)
	r := newRecorder()
	if err := p.Start(7, r.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	p.PushAudio(make([]byte, 3200))
	time.Sleep(150 * time.Millisecond)
	p.Stop()
	r.waitForStopped(t, p)

	wantSequence(t, r, stt.Started, stt.Partial, stt.Final, stt.Stopped)

	final, _ := r.first(stt.Final)
	if final.Text != "hola mundo" {
		t.Errorf("final text = %q, want %q", final.Text, "hola mundo")
	}
	// Every event has to carry the generation, or the controller cannot tell this session's
	// events from the next one's.
	for _, e := range r.all() {
		if e.Gen != 7 {
			t.Errorf("%s event carried gen %d, want 7", e.Type, e.Gen)
		}
	}
	// The streaming API reports no detected language, so claiming one would be a lie.
	if final.Language != "" {
		t.Errorf("final language = %q, want empty (Grok streaming reports none)", final.Language)
	}
}

// Several is_final=true events before the terminal done. Closing on the first one — which is
// what the Electron session did — truncates everything after it.
func TestSeveralFinalsBeforeDoneAreAllKept(t *testing.T) {
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) {
		g.ready(conn)
		g.send(conn, `{"type":"transcript.partial","text":"uno","words":[{"text":"uno","start":0,"end":1}],"is_final":true,"speech_final":false,"start":0,"duration":1}`)
		g.send(conn, `{"type":"transcript.partial","text":"dos","words":[{"text":"dos","start":1,"end":2}],"is_final":true,"speech_final":true,"start":1,"duration":1}`)
		g.send(conn, `{"type":"transcript.partial","text":"tres","words":[{"text":"tres","start":2,"end":3}],"is_final":true,"speech_final":true,"start":2,"duration":1}`)
		g.waitForText("audio.done")
		g.send(conn, `{"type":"transcript.done","text":"","words":[],"duration":3}`)
	})

	p := testProvider(g, nil)
	r := newRecorder()
	_ = p.Start(1, r.sink)
	time.Sleep(200 * time.Millisecond)
	p.Stop()
	r.waitForStopped(t, p)

	if n := r.count(stt.Final); n != 1 {
		t.Errorf("got %d Final events, want exactly 1 (the provider assembles)", n)
	}
	final, _ := r.first(stt.Final)
	if final.Text != "uno dos tres" {
		t.Errorf("final text = %q, want %q — a truncating provider loses the tail", final.Text, "uno dos tres")
	}
}

// The wire format, asserted on the server side. These are the details that a unit test with a
// fake connection cannot check, and they are exactly what a service rejects.
func TestWireFormat(t *testing.T) {
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) {
		g.ready(conn)
		g.waitForText("audio.done")
		g.send(conn, `{"type":"transcript.done","text":"ok","words":[{"text":"ok","start":0,"end":1}],"duration":1}`)
	})

	p := testProvider(g, func(c *Config) { c.Language = "es" })
	r := newRecorder()
	_ = p.Start(1, r.sink)

	pcm := []byte{0x01, 0x02, 0x03, 0x04}
	p.PushAudio(pcm)
	time.Sleep(150 * time.Millisecond)
	p.Stop()
	r.waitForStopped(t, p)

	if got := g.authHeader(); got != "Bearer xai-test-key" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer xai-test-key")
	}
	q := g.queryParams()
	for k, want := range map[string]string{
		"encoding": "pcm", "sample_rate": "16000", "interim_results": "true", "language": "es",
	} {
		if got := q.Get(k); got != want {
			t.Errorf("query %s = %q, want %q", k, got, want)
		}
	}

	// PCM must be RAW BINARY frames. Sending it as text (or base64) is how the service
	// silently transcribes nothing.
	if got := string(g.binaryPayload()); got != string(pcm) {
		t.Errorf("binary payload = %v, want %v", g.binaryPayload(), pcm)
	}
	// ...and the control message must be text.
	for _, f := range g.snapshot() {
		if strings.Contains(string(f.data), "audio.done") && f.typ != websocket.MessageText {
			t.Error("audio.done must be a TEXT frame")
		}
	}
}

// Audio pushed before the server says it is ready has to be buffered and then sent IN ORDER.
// Dropping it loses the start of every dictation, since the user speaks immediately.
func TestAudioBeforeReadyIsBufferedInOrder(t *testing.T) {
	release := make(chan struct{})
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) {
		<-release // stay silent: no transcript.created yet
		g.ready(conn)
		g.waitForText("audio.done")
		g.send(conn, `{"type":"transcript.done","text":"ok","words":[{"text":"ok","start":0,"end":1}],"duration":1}`)
	})

	p := testProvider(g, nil)
	r := newRecorder()
	_ = p.Start(1, r.sink)

	// Three distinguishable chunks, pushed while the server has not acknowledged anything.
	p.PushAudio([]byte{1, 1})
	p.PushAudio([]byte{2, 2})
	p.PushAudio([]byte{3, 3})
	time.Sleep(100 * time.Millisecond)

	if got := g.binaryPayload(); len(got) != 0 {
		t.Fatalf("audio reached the server before it was ready: %v", got)
	}

	close(release)
	time.Sleep(200 * time.Millisecond)
	p.Stop()
	r.waitForStopped(t, p)

	if got, want := string(g.binaryPayload()), string([]byte{1, 1, 2, 2, 3, 3}); got != want {
		t.Errorf("buffered audio arrived as %v, want it in push order %v", []byte(got), []byte(want))
	}
}

// THE TAIL OF THE DICTATION. Audio still buffered when the user lets go has to reach the
// service BEFORE audio.done, on the normal ready path too — not just the stop-before-ready one.
//
// `select` picks at random among ready cases, so a run loop that finalizes without draining the
// buffer first will sometimes send audio.done ahead of the last frames. The service then flushes
// and closes having never heard the end of the sentence, and it fails intermittently, which is
// the worst way for it to fail.
func TestPendingAudioIsSentBeforeAudioDone(t *testing.T) {
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) {
		g.ready(conn)
		g.waitForText("audio.done")
		g.send(conn, `{"type":"transcript.done","text":"ok","words":[{"text":"ok","start":0,"end":1}],"duration":1}`)
	})

	p := testProvider(g, nil)
	r := newRecorder()
	_ = p.Start(1, r.sink)

	// Let the session go ready, then push and release with no pause: the buffer is very likely
	// still un-drained when the stop is handled.
	time.Sleep(100 * time.Millisecond)
	const chunks = 40
	for i := 0; i < chunks; i++ {
		p.PushAudio([]byte{byte(i), byte(i)})
	}
	p.Stop()
	r.waitForStopped(t, p)

	// Every byte must have arrived...
	if got, want := len(g.binaryPayload()), chunks*2; got != want {
		t.Errorf("service received %d bytes of PCM, want %d — the tail was dropped", got, want)
	}
	// ...and audio.done must be the LAST thing it saw.
	frames := g.snapshot()
	doneAt := -1
	for i, f := range frames {
		if f.typ == websocket.MessageText && strings.Contains(string(f.data), "audio.done") {
			doneAt = i
		}
	}
	if doneAt < 0 {
		t.Fatal("audio.done was never sent")
	}
	for _, f := range frames[doneAt+1:] {
		if f.typ == websocket.MessageBinary {
			t.Error("PCM was sent AFTER audio.done — the service had already stopped listening")
		}
	}
}

// A short press: the user releases before the socket is even ready. The queued audio must
// still be sent, and it must go out BEFORE audio.done — otherwise the service is told the
// stream ended before it received any of it.
func TestStopBeforeReadyStillSendsTheAudioFirst(t *testing.T) {
	release := make(chan struct{})
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) {
		<-release
		g.ready(conn)
		g.waitForText("audio.done")
		g.send(conn, `{"type":"transcript.done","text":"sí","words":[{"text":"sí","start":0,"end":0.3}],"duration":0.3}`)
	})

	p := testProvider(g, nil)
	r := newRecorder()
	_ = p.Start(1, r.sink)
	p.PushAudio([]byte{9, 9})
	p.Stop() // released while still dialing/waiting

	close(release)
	r.waitForStopped(t, p)

	frames := g.snapshot()
	sawAudio := false
	for _, f := range frames {
		if f.typ == websocket.MessageBinary {
			sawAudio = true
		}
		if f.typ == websocket.MessageText && strings.Contains(string(f.data), "audio.done") {
			if !sawAudio {
				t.Fatal("audio.done was sent before the buffered PCM")
			}
		}
	}
	if !sawAudio {
		t.Error("the audio queued before Stop was never sent")
	}
	final, ok := r.first(stt.Final)
	if !ok || final.Text != "sí" {
		t.Errorf("final = %+v, want the transcript for a short press", final)
	}
}
