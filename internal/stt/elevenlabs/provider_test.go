package elevenlabs

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Juan-Motta/loqui-go/internal/stt"
)

// collector records what the session emitted, in order.
type collector struct {
	mu     sync.Mutex
	events []stt.Event
	done   chan struct{}
	once   sync.Once
}

func newCollector() *collector {
	return &collector{done: make(chan struct{})}
}

func (c *collector) sink(evt stt.Event) {
	c.mu.Lock()
	c.events = append(c.events, evt)
	c.mu.Unlock()
	if evt.Type == stt.Stopped {
		c.once.Do(func() { close(c.done) })
	}
}

func (c *collector) wait(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(d):
		t.Fatalf("la sesión no terminó en %s; eventos: %s", d, c.summary())
	}
}

func (c *collector) snapshot() []stt.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]stt.Event(nil), c.events...)
}

func (c *collector) summary() string {
	var b strings.Builder
	for _, e := range c.snapshot() {
		b.WriteString(string(e.Type))
		if e.ErrorCode != "" {
			b.WriteString("(" + e.ErrorCode + ")")
		}
		b.WriteString(" ")
	}
	return b.String()
}

func (c *collector) kinds() []stt.EventType {
	var out []stt.EventType
	for _, e := range c.snapshot() {
		out = append(out, e.Type)
	}
	return out
}

func (c *collector) first(kind stt.EventType) (stt.Event, bool) {
	for _, e := range c.snapshot() {
		if e.Type == kind {
			return e, true
		}
	}
	return stt.Event{}, false
}

func testConfig(url string) Config {
	return Config{
		GetKey:          func() (string, error) { return "xi-test-key", nil },
		Endpoint:        url,
		DialTimeout:     2 * time.Second,
		ReadyTimeout:    2 * time.Second,
		WriteTimeout:    2 * time.Second,
		FinalizeTimeout: 2 * time.Second,
	}
}

// The happy path: handshake, audio, release, commit, final.
func TestSessionStreamsAudioAndReportsTheTranscript(t *testing.T) {
	var srv *fakeEleven
	srv = newFakeEleven(t, func(f *fakeEleven, conn *websocket.Conn) {
		f.ready(conn)
		f.send(conn, `{"message_type":"partial_transcript","text":"hola mu"}`)
		// The final only after the client asked to commit, which is what the release does.
		if f.waitForCommit(2 * time.Second) {
			f.send(conn, `{"message_type":"committed_transcript","text":"hola mundo"}`)
		}
	})
	_ = srv

	p := New(testConfig(srv.url))
	c := newCollector()
	if err := p.Start(7, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	p.PushAudio([]byte{1, 2, 3, 4})
	// Give the flush a moment before releasing, so this test exercises the ordinary path rather than
	// the stop-before-ready one.
	time.Sleep(150 * time.Millisecond)
	p.Stop()
	c.wait(t, 5*time.Second)

	if _, ok := c.first(stt.Started); !ok {
		t.Errorf("no hubo Started; eventos: %s", c.summary())
	}
	partial, ok := c.first(stt.Partial)
	if !ok || partial.Text != "hola mu" {
		t.Errorf("parcial = %q (ok=%v)", partial.Text, ok)
	}
	final, ok := c.first(stt.Final)
	if !ok || final.Text != "hola mundo" {
		t.Errorf("final = %q (ok=%v); eventos: %s", final.Text, ok, c.summary())
	}
	// The generation travels on every event: the controller rejects anything from an older one.
	for _, e := range c.snapshot() {
		if e.Gen != 7 {
			t.Errorf("evento %s con gen %d, quería 7", e.Type, e.Gen)
		}
	}

	pcm, commits := srv.audioChunks()
	if len(pcm) == 0 {
		t.Fatal("el servidor no recibió audio")
	}
	if string(pcm[0]) != string([]byte{1, 2, 3, 4}) {
		t.Errorf("el primer chunk llegó como %v", pcm[0])
	}
	// The commit is LAST: committing before the audio is flushed tells the service the phrase ended
	// before it heard the end of it.
	if !commits[len(commits)-1] {
		t.Errorf("el último mensaje no llevaba commit=true: %v", commits)
	}
	for i, c := range commits[:len(commits)-1] {
		if c {
			t.Errorf("el chunk de audio %d llevaba commit=true antes del final", i)
		}
	}
}

// The credential must be in the header and nowhere else.
func TestTheKeyTravelsInTheHeaderAndNotTheURL(t *testing.T) {
	srv := newFakeEleven(t, func(f *fakeEleven, conn *websocket.Conn) {
		f.ready(conn)
	})
	p := New(testConfig(srv.url))
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	p.Stop()
	c.wait(t, 5*time.Second)

	if got := srv.apiKeyHeader(); got != "xi-test-key" {
		t.Errorf("cabecera %s = %q", APIKeyHeader, got)
	}
	for name, values := range srv.queryParams() {
		for _, v := range values {
			if strings.Contains(v, "xi-test-key") {
				t.Errorf("la clave apareció en la query: %s=%s", name, v)
			}
		}
	}
}

// A missing key is a configuration problem: it must be reported as NotConfigured and never retried.
func TestAMissingKeyIsReportedAsNotConfigured(t *testing.T) {
	p := New(Config{GetKey: func() (string, error) { return "", nil }})
	c := newCollector()
	err := p.Start(1, c.sink)
	if err == nil {
		t.Fatal("Start devolvió nil sin clave")
	}
	c.wait(t, 2*time.Second)
	cancel, ok := c.first(stt.Canceled)
	if !ok {
		t.Fatalf("no hubo Canceled; eventos: %s", c.summary())
	}
	if cancel.ErrorCode != "NotConfigured" {
		t.Errorf("código = %q, quería NotConfigured", cancel.ErrorCode)
	}
}

// A rejected upgrade is the only reliable machine-readable failure this provider gets.
func TestARejectedHandshakeIsClassified(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{401, "", "AuthenticationFailure"},
		{403, "", "Forbidden"},
		{429, "", "TooManyRequests"},
		{503, "", "ServiceUnavailable"},
		// The case the status alone gets wrong: a bad key rejected as 400. Classified as BadRequest it
		// would tell the user to fix their request instead of their key.
		{400, `{"detail":{"message":"Invalid api key provided"}}`, "AuthenticationFailure"},
		{400, `{"detail":{"message":"bad audio format"}}`, "BadRequest"},
	}
	for _, tc := range cases {
		srv := newRejectingEleven(t, tc.status, tc.body)
		p := New(testConfig(srv.url))
		c := newCollector()
		_ = p.Start(1, c.sink)
		c.wait(t, 5*time.Second)
		cancel, ok := c.first(stt.Canceled)
		if !ok {
			t.Errorf("status %d: no hubo Canceled; eventos: %s", tc.status, c.summary())
			continue
		}
		if cancel.ErrorCode != tc.want {
			t.Errorf("status %d (%s): código = %q, quería %q", tc.status, tc.body, cancel.ErrorCode, tc.want)
		}
	}
}

// A service that accepts the socket and then says nothing must not hold the session open.
func TestASilentServiceHitsTheReadyTimeout(t *testing.T) {
	srv := newFakeEleven(t, func(f *fakeEleven, conn *websocket.Conn) {
		time.Sleep(3 * time.Second) // never sends session_started
	})
	cfg := testConfig(srv.url)
	cfg.ReadyTimeout = 300 * time.Millisecond
	p := New(cfg)
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	c.wait(t, 5*time.Second)
	cancel, ok := c.first(stt.Canceled)
	if !ok || cancel.ErrorCode != "ServiceTimeout" {
		t.Errorf("código = %q (ok=%v); eventos: %s", cancel.ErrorCode, ok, c.summary())
	}
}

// Audio pushed BEFORE the handshake must still arrive, in order: the session is confirmed
// asynchronously and the user is already speaking.
func TestAudioBufferedBeforeReadyIsFlushedInOrder(t *testing.T) {
	release := make(chan struct{})
	srv := newFakeEleven(t, func(f *fakeEleven, conn *websocket.Conn) {
		<-release // hold the handshake while the client pushes
		f.ready(conn)
		if f.waitForCommit(2 * time.Second) {
			f.send(conn, `{"message_type":"committed_transcript","text":"ok"}`)
		}
	})
	p := New(testConfig(srv.url))
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	p.PushAudio([]byte{1})
	p.PushAudio([]byte{2})
	p.PushAudio([]byte{3})
	close(release)
	time.Sleep(200 * time.Millisecond)
	p.Stop()
	c.wait(t, 5*time.Second)

	pcm, _ := srv.audioChunks()
	var got []byte
	for _, chunk := range pcm {
		got = append(got, chunk...)
	}
	if string(got) != string([]byte{1, 2, 3}) {
		t.Errorf("el audio llegó como %v, quería 1,2,3 en orden", got)
	}
}

// Releasing before the handshake: the flush and the commit have to happen once the session is
// confirmed, not be dropped.
func TestStopBeforeReadyStillCommitsAfterTheHandshake(t *testing.T) {
	stopped := make(chan struct{})
	srv := newFakeEleven(t, func(f *fakeEleven, conn *websocket.Conn) {
		<-stopped // the client has already released the key
		f.ready(conn)
		if f.waitForCommit(2 * time.Second) {
			f.send(conn, `{"message_type":"committed_transcript","text":"tardío"}`)
		}
	})
	p := New(testConfig(srv.url))
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	p.PushAudio([]byte{9, 9})
	time.Sleep(100 * time.Millisecond)
	p.Stop()
	close(stopped)
	c.wait(t, 5*time.Second)

	if !srv.waitForCommit(time.Second) {
		t.Error("nunca llegó el commit tras un stop previo al handshake")
	}
	final, ok := c.first(stt.Final)
	if !ok || final.Text != "tardío" {
		t.Errorf("final = %q (ok=%v); eventos: %s", final.Text, ok, c.summary())
	}
}

// With VAD the server commits every utterance, so several finals can arrive while the key is held.
// Treating the first as the end would truncate a long dictation to its first phrase.
func TestSeveralCommittedTranscriptsAreJoinedNotTruncated(t *testing.T) {
	srv := newFakeEleven(t, func(f *fakeEleven, conn *websocket.Conn) {
		f.ready(conn)
		f.send(conn, `{"message_type":"committed_transcript","text":"primera frase"}`)
		f.send(conn, `{"message_type":"committed_transcript","text":"segunda frase"}`)
		if f.waitForCommit(2 * time.Second) {
			f.send(conn, `{"message_type":"committed_transcript","text":"tercera"}`)
		}
	})
	p := New(testConfig(srv.url))
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(250 * time.Millisecond)
	p.Stop()
	c.wait(t, 5*time.Second)

	final, ok := c.first(stt.Final)
	if !ok {
		t.Fatalf("no hubo Final; eventos: %s", c.summary())
	}
	for _, want := range []string{"primera frase", "segunda frase", "tercera"} {
		if !strings.Contains(final.Text, want) {
			t.Errorf("el final %q perdió %q", final.Text, want)
		}
	}
}

// An error event ends the session with a non-retryable code: the event carries only prose, so
// transient and permanent are indistinguishable, and a retryable guess becomes an unbounded loop
// against a metered service.
func TestAServerErrorEndsTheSessionWithoutRetrying(t *testing.T) {
	srv := newFakeEleven(t, func(f *fakeEleven, conn *websocket.Conn) {
		f.ready(conn)
		f.send(conn, `{"message_type":"error","error":"algo se rompió"}`)
	})
	p := New(testConfig(srv.url))
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	c.wait(t, 5*time.Second)
	cancel, ok := c.first(stt.Canceled)
	if !ok {
		t.Fatalf("no hubo Canceled; eventos: %s", c.summary())
	}
	if cancel.ErrorCode != "BadRequest" {
		t.Errorf("código = %q, quería un código NO reintentable", cancel.ErrorCode)
	}
	if !strings.Contains(cancel.Error, "algo se rompió") {
		t.Errorf("el mensaje del servidor no llegó: %q", cancel.Error)
	}
}

// Stopped must be LAST, and a Final that exists must come before any Canceled — otherwise the
// controller bumps the generation on the cancel and discards the transcript that followed.
func TestTheClosingEventsAreOrderedSoTheTranscriptSurvives(t *testing.T) {
	srv := newFakeEleven(t, func(f *fakeEleven, conn *websocket.Conn) {
		f.ready(conn)
		f.send(conn, `{"message_type":"committed_transcript","text":"texto salvado"}`)
		f.send(conn, `{"message_type":"error","error":"y luego falló"}`)
	})
	p := New(testConfig(srv.url))
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	c.wait(t, 5*time.Second)

	kinds := c.kinds()
	if kinds[len(kinds)-1] != stt.Stopped {
		t.Errorf("el último evento fue %s, quería Stopped: %s", kinds[len(kinds)-1], c.summary())
	}
	iFinal, iCancel := -1, -1
	for i, k := range kinds {
		if k == stt.Final && iFinal < 0 {
			iFinal = i
		}
		if k == stt.Canceled && iCancel < 0 {
			iCancel = i
		}
	}
	if iFinal < 0 {
		t.Fatalf("se perdió el Final; eventos: %s", c.summary())
	}
	if iCancel >= 0 && iFinal > iCancel {
		t.Errorf("Final llegó DESPUÉS de Canceled — el controlador lo descartaría: %s", c.summary())
	}
}

// The pre-ready buffer is bounded in BYTES, and the newest audio is what survives.
func TestTheAudioBufferIsBoundedAndDropsTheOldest(t *testing.T) {
	release := make(chan struct{})
	srv := newFakeEleven(t, func(f *fakeEleven, conn *websocket.Conn) {
		<-release
		f.ready(conn)
	})
	cfg := testConfig(srv.url)
	cfg.AudioBufferBytes = 10
	p := New(cfg)
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	for i := 0; i < 20; i++ {
		p.PushAudio([]byte{byte(i), byte(i), byte(i), byte(i)})
	}
	if got := p.bufferedBytes(); got > 10 {
		t.Errorf("el búfer creció a %d bytes con un tope de 10", got)
	}
	// A single chunk over the whole budget is rejected outright, or the drop-oldest loop could never
	// bring the total back under the bound.
	before := p.bufferedBytes()
	p.PushAudio(make([]byte, 64))
	if after := p.bufferedBytes(); after != before {
		t.Errorf("un frame de 64 bytes entró en un búfer de 10: %d -> %d", before, after)
	}
	close(release)
	p.Stop()
	c.wait(t, 5*time.Second)
}

// Start twice must not open two sockets.
func TestStartIsOnlyHonouredOnce(t *testing.T) {
	srv := newFakeEleven(t, func(f *fakeEleven, conn *websocket.Conn) { f.ready(conn) })
	p := New(testConfig(srv.url))
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("primer Start: %v", err)
	}
	if err := p.Start(2, c.sink); err == nil {
		t.Error("el segundo Start no dio error")
	}
	p.Stop()
	p.Stop() // Stop twice must not panic on a closed channel
	c.wait(t, 5*time.Second)
}
