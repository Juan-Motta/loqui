package openai

import (
	"encoding/binary"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Juan-Motta/loqui-go/internal/stt"
)

type collector struct {
	mu     sync.Mutex
	events []stt.Event
	done   chan struct{}
	once   sync.Once
}

func (c *collector) waitFor(kind stt.EventType, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if _, ok := c.first(kind); ok {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func newCollector() *collector { return &collector{done: make(chan struct{})} }

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

func (c *collector) first(kind stt.EventType) (stt.Event, bool) {
	for _, e := range c.snapshot() {
		if e.Type == kind {
			return e, true
		}
	}
	return stt.Event{}, false
}

func (c *collector) all(kind stt.EventType) []stt.Event {
	var out []stt.Event
	for _, e := range c.snapshot() {
		if e.Type == kind {
			out = append(out, e)
		}
	}
	return out
}

func testConfig(url string) Config {
	return Config{
		GetKey:          func() (string, error) { return "sk-test-123", nil },
		Endpoint:        url,
		DialTimeout:     2 * time.Second,
		ReadyTimeout:    2 * time.Second,
		WriteTimeout:    2 * time.Second,
		FinalizeTimeout: 2 * time.Second,
	}
}

func manualCommitConfig(url string) Config {
	cfg := testConfig(url)
	cfg.ServiceName = "Azure OpenAI"
	cfg.DialOptions = func(key string) *websocket.DialOptions {
		header := make(http.Header)
		header.Set("api-key", key)
		return &websocket.DialOptions{HTTPHeader: header}
	}
	cfg.SessionUpdate = func(model, language string) ([]byte, error) {
		return BuildSessionUpdateWithManualCommit(model, language, true)
	}
	cfg.RequireSessionUpdated = true
	cfg.CommitInterval = 40 * time.Millisecond
	cfg.FinalizeTimeout = 500 * time.Millisecond
	return cfg
}

func TestAConfiguredSessionIsRequiredBeforeAzureStarts(t *testing.T) {
	allowConfigured := make(chan struct{})
	srv := newFakeOpenAI(t, func(f *fakeOpenAI, conn *websocket.Conn) {
		if !f.waitForSessionUpdate(2 * time.Second) {
			return
		}
		<-allowConfigured
		f.send(conn, `{"type":"session.updated"}`)
	})
	p := New(manualCommitConfig(srv.url))
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !srv.waitForSessionUpdate(2 * time.Second) {
		t.Fatal("session.update did not arrive")
	}
	if c.waitFor(stt.Started, 100*time.Millisecond) {
		t.Fatal("Started arrived before session.updated")
	}
	if got := srv.header.Get("api-key"); got != "sk-test-123" {
		t.Errorf("api-key header = %q", got)
	}
	close(allowConfigured)
	if !c.waitFor(stt.Started, time.Second) {
		t.Fatal("Started did not arrive after session.updated")
	}
	p.Stop()
	c.wait(t, time.Second)
}

func TestManualCommitSendsOnlyNonEmptyBuffers(t *testing.T) {
	committed := make(chan struct{})
	srv := newFakeOpenAI(t, func(f *fakeOpenAI, conn *websocket.Conn) {
		if !f.waitForSessionUpdate(2 * time.Second) {
			return
		}
		f.send(conn, `{"type":"session.updated"}`)
		if !f.waitForType("input_audio_buffer.commit", 1, 2*time.Second) {
			return
		}
		f.send(conn, `{"type":"conversation.item.input_audio_transcription.completed","text":"hola"}`)
		close(committed)
		time.Sleep(180 * time.Millisecond)
	})
	p := New(manualCommitConfig(srv.url))
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !c.waitFor(stt.Started, time.Second) {
		t.Fatal("session never started")
	}
	p.PushAudio(pcmOf(1, 2, 3, 4))
	select {
	case <-committed:
	case <-time.After(2 * time.Second):
		t.Fatal("the non-empty remote buffer was never committed")
	}
	time.Sleep(120 * time.Millisecond)
	if got := srv.countType("input_audio_buffer.commit"); got != 1 {
		t.Errorf("commit count = %d, want exactly one; empty timer ticks must do nothing", got)
	}
	p.Stop()
	c.wait(t, time.Second)
	if final, ok := c.first(stt.Final); !ok || final.Text != "hola" {
		t.Errorf("final = %+v, ok=%v", final, ok)
	}
}

func TestManualCommitStopWaitsForEveryOutstandingTranscript(t *testing.T) {
	releaseFinals := make(chan struct{})
	oneSent := make(chan struct{})
	srv := newFakeOpenAI(t, func(f *fakeOpenAI, conn *websocket.Conn) {
		if !f.waitForSessionUpdate(2 * time.Second) {
			return
		}
		f.send(conn, `{"type":"session.updated"}`)
		if !f.waitForType("input_audio_buffer.commit", 2, 2*time.Second) {
			return
		}
		<-releaseFinals
		f.send(conn, `{"type":"conversation.item.input_audio_transcription.completed","text":"uno"}`)
		close(oneSent)
		time.Sleep(80 * time.Millisecond)
		f.send(conn, `{"type":"conversation.item.input_audio_transcription.completed","transcript":"dos"}`)
	})
	p := New(manualCommitConfig(srv.url))
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !c.waitFor(stt.Started, time.Second) {
		t.Fatal("session never started")
	}
	p.PushAudio(pcmOf(1, 2, 3, 4))
	if !srv.waitForType("input_audio_buffer.commit", 1, time.Second) {
		t.Fatal("first commit missing")
	}
	p.PushAudio(pcmOf(5, 6, 7, 8))
	if !srv.waitForType("input_audio_buffer.commit", 2, time.Second) {
		t.Fatal("second commit missing")
	}
	p.Stop()
	close(releaseFinals)
	<-oneSent
	if c.waitFor(stt.Stopped, 30*time.Millisecond) {
		t.Fatal("session stopped after the first of two outstanding transcripts")
	}
	c.wait(t, time.Second)
	final, ok := c.first(stt.Final)
	if !ok || final.Text != "uno dos" {
		t.Errorf("final = %q (ok=%v), want both committed transcripts", final.Text, ok)
	}
}

// Nothing transcribes until session.update lands, so this is the first thing to prove.
func TestTheSessionIsConfiguredBeforeAnyAudioGoesOut(t *testing.T) {
	srv := newFakeOpenAI(t, func(f *fakeOpenAI, conn *websocket.Conn) {
		f.ready(conn)
		if f.waitForAudio(1, 2*time.Second) {
			f.send(conn, `{"type":"conversation.item.input_audio_transcription.completed","transcript":"listo"}`)
		}
	})
	p := New(testConfig(srv.url))
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !srv.waitForSessionUpdate(2 * time.Second) {
		t.Fatal("nunca llegó session.update: el servicio queda conectado y sordo")
	}
	p.PushAudio(pcmOf(1, 2, 3, 4))
	time.Sleep(150 * time.Millisecond)
	p.Stop()
	c.wait(t, 5*time.Second)

	// The ORDER matters as much as the presence: audio before the configuration is discarded silently.
	srv.mu.Lock()
	frames := append([]string(nil), srv.texts...)
	srv.mu.Unlock()
	iCfg, iAudio := -1, -1
	for i, raw := range frames {
		if strings.Contains(raw, `"session.update"`) && iCfg < 0 {
			iCfg = i
		}
		if strings.Contains(raw, `"input_audio_buffer.append"`) && iAudio < 0 {
			iAudio = i
		}
	}
	if iCfg < 0 {
		t.Fatal("no se envió session.update")
	}
	if iAudio >= 0 && iAudio < iCfg {
		t.Errorf("el audio salió antes de configurar la sesión (audio en %d, config en %d)", iAudio, iCfg)
	}
	// The nested schema, checked on the wire and not only in the builder's unit test.
	msg, _ := srv.sessionUpdate()
	session, ok := msg["session"].(map[string]any)
	if !ok {
		t.Fatalf("session.update sin objeto session: %v", msg)
	}
	if session["type"] != "transcription" {
		t.Errorf("session.type = %v", session["type"])
	}
}

// The key goes in the subprotocols, which is this API's client-auth scheme, and nowhere else.
func TestTheKeyTravelsInTheSubprotocols(t *testing.T) {
	srv := newFakeOpenAI(t, func(f *fakeOpenAI, conn *websocket.Conn) { f.ready(conn) })
	p := New(testConfig(srv.url))
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	srv.waitForSessionUpdate(2 * time.Second)
	p.Stop()
	c.wait(t, 5*time.Second)

	offered := srv.requestedSubprotocols()
	if !strings.Contains(offered, "openai-insecure-api-key.sk-test-123") {
		t.Errorf("subprotocolos ofrecidos = %q, falta la clave", offered)
	}
	if !strings.Contains(offered, "realtime") {
		t.Errorf("subprotocolos ofrecidos = %q, falta \"realtime\"", offered)
	}
	for name, values := range srv.queryParams() {
		for _, v := range values {
			if strings.Contains(v, "sk-test-123") {
				t.Errorf("la clave apareció en la query: %s=%s", name, v)
			}
		}
	}
}

// THE 24 kHz REQUIREMENT, end to end. The capture pipeline gives 16 kHz; what reaches the wire must be
// 1.5× as many samples. Sending 16 kHz into a 24 kHz session is accepted and transcribes a sped-up
// voice, so nothing downstream would report this.
func TestAudioIsResampledToTwentyFourKilohertzOnTheWire(t *testing.T) {
	srv := newFakeOpenAI(t, func(f *fakeOpenAI, conn *websocket.Conn) {
		f.ready(conn)
		if f.waitForAudio(1, 2*time.Second) {
			f.send(conn, `{"type":"conversation.item.input_audio_transcription.completed","transcript":"ok"}`)
		}
	})
	p := New(testConfig(srv.url))
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	srv.waitForSessionUpdate(2 * time.Second)

	const inSamples = 320 // 20 ms at 16 kHz
	in := make([]int16, inSamples)
	for i := range in {
		in[i] = int16(i * 10)
	}
	p.PushAudio(pcmOf(in...))
	if !srv.waitForAudio(1, 2*time.Second) {
		t.Fatal("el servidor no recibió audio")
	}
	p.Stop()
	c.wait(t, 5*time.Second)

	var got int
	for _, chunk := range srv.audioChunks() {
		got += len(chunk) / 2
	}
	want := inSamples * SampleRate / CaptureRate
	if got != want {
		t.Errorf("llegaron %d muestras por %d enviadas; con %d->%d Hz esperaba %d",
			got, inSamples, CaptureRate, SampleRate, want)
	}
}

// The deltas are FRAGMENTS: each partial must be the phrase so far, not the last word.
func TestDeltasAreShownAsAGrowingPhrase(t *testing.T) {
	srv := newFakeOpenAI(t, func(f *fakeOpenAI, conn *websocket.Conn) {
		f.ready(conn)
		f.send(conn, `{"type":"conversation.item.input_audio_transcription.delta","delta":"ho"}`)
		f.send(conn, `{"type":"conversation.item.input_audio_transcription.delta","delta":"la "}`)
		f.send(conn, `{"type":"conversation.item.input_audio_transcription.delta","delta":"mundo"}`)
		f.send(conn, `{"type":"conversation.item.input_audio_transcription.completed","transcript":"hola mundo"}`)
	})
	p := New(testConfig(srv.url))
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	p.Stop()
	c.wait(t, 5*time.Second)

	partials := c.all(stt.Partial)
	if len(partials) < 3 {
		t.Fatalf("parciales = %d, quería al menos 3; eventos: %s", len(partials), c.summary())
	}
	want := []string{"ho", "hola ", "hola mundo"}
	for i, w := range want {
		if partials[i].Text != w {
			t.Errorf("parcial %d = %q, quería %q — un delta suelto mostraría una palabra sola", i, partials[i].Text, w)
		}
	}
	final, ok := c.first(stt.Final)
	if !ok || final.Text != "hola mundo" {
		t.Errorf("final = %q (ok=%v)", final.Text, ok)
	}
}

// A completed event with no transcript of its own falls back to the accumulated deltas — otherwise the
// utterance is lost even though every word of it arrived.
func TestACompletedWithoutTranscriptFallsBackToTheDeltas(t *testing.T) {
	srv := newFakeOpenAI(t, func(f *fakeOpenAI, conn *websocket.Conn) {
		f.ready(conn)
		f.send(conn, `{"type":"conversation.item.input_audio_transcription.delta","delta":"solo deltas"}`)
		f.send(conn, `{"type":"conversation.item.input_audio_transcription.completed"}`)
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
	if !ok || final.Text != "solo deltas" {
		t.Errorf("final = %q (ok=%v); eventos: %s", final.Text, ok, c.summary())
	}
}

// Deltas with no completed at all: the socket dies mid-utterance. What the user said must survive.
func TestUnclosedDeltasStillReachTheFinal(t *testing.T) {
	srv := newFakeOpenAI(t, func(f *fakeOpenAI, conn *websocket.Conn) {
		f.ready(conn)
		f.send(conn, `{"type":"conversation.item.input_audio_transcription.delta","delta":"frase a medias"}`)
		time.Sleep(100 * time.Millisecond)
		conn.CloseNow() // dropped connection
	})
	p := New(testConfig(srv.url))
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	c.wait(t, 5*time.Second)

	final, ok := c.first(stt.Final)
	if !ok || !strings.Contains(final.Text, "frase a medias") {
		t.Errorf("final = %q (ok=%v): se perdió lo que el usuario ya había dictado", final.Text, ok)
	}
}

// Several utterances while the key is held must be joined, not truncated to the first.
func TestSeveralUtterancesAreJoined(t *testing.T) {
	srv := newFakeOpenAI(t, func(f *fakeOpenAI, conn *websocket.Conn) {
		f.ready(conn)
		f.send(conn, `{"type":"conversation.item.input_audio_transcription.completed","transcript":"primera"}`)
		f.send(conn, `{"type":"conversation.item.input_audio_transcription.completed","transcript":"segunda"}`)
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
	for _, want := range []string{"primera", "segunda"} {
		if !strings.Contains(final.Text, want) {
			t.Errorf("el final %q perdió %q", final.Text, want)
		}
	}
}

// A missing key is configuration, never transient.
func TestAMissingKeyIsReportedAsNotConfigured(t *testing.T) {
	p := New(Config{GetKey: func() (string, error) { return "", nil }})
	c := newCollector()
	if err := p.Start(1, c.sink); err == nil {
		t.Fatal("Start devolvió nil sin clave")
	}
	c.wait(t, 2*time.Second)
	cancel, ok := c.first(stt.Canceled)
	if !ok || cancel.ErrorCode != "NotConfigured" {
		t.Errorf("código = %q (ok=%v)", cancel.ErrorCode, ok)
	}
}

// The handshake is where a bad key shows up — and on this endpoint it shows up as a 400, because the
// credential is a subprotocol and a rejected subprotocol is not a 401.
func TestARejectedHandshakeIsClassified(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{401, "", "AuthenticationFailure"},
		{403, "", "Forbidden"},
		{429, "", "TooManyRequests"},
		{500, "", "ServiceUnavailable"},
		// A bare 400: the shape a rejected subprotocol takes. Classified as BadRequest it would tell the
		// user to fix their request when what is wrong is the key.
		{400, "", "AuthenticationFailure"},
		{400, `{"error":{"message":"Invalid API key provided"}}`, "AuthenticationFailure"},
		// A 400 that is genuinely about the request stays BadRequest.
		{400, `{"error":{"message":"unsupported audio format"}}`, "BadRequest"},
	}
	for _, tc := range cases {
		srv := newRejectingOpenAI(t, tc.status, tc.body)
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
			t.Errorf("status %d (%q): código = %q, quería %q", tc.status, tc.body, cancel.ErrorCode, tc.want)
		}
	}
}

// An error event stays terminal until the provider maps its preserved machine-readable code into the
// shared session vocabulary; retrying the collapsed bucket would retry known-permanent auth failures.
func TestAServerErrorEndsTheSessionWithoutRetrying(t *testing.T) {
	srv := newFakeOpenAI(t, func(f *fakeOpenAI, conn *websocket.Conn) {
		f.ready(conn)
		f.send(conn, `{"type":"error","error":{"message":"sesión inválida"}}`)
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
		t.Errorf("código = %q, quería uno NO reintentable", cancel.ErrorCode)
	}
	if !strings.Contains(cancel.Error, "sesión inválida") {
		t.Errorf("el mensaje del servidor no llegó: %q", cancel.Error)
	}
}

// Azure can return provider prose after the upgrade. It must never reach the user because a vendor
// may echo request material in that sentence; only our fixed wording and machine-readable code are
// safe to expose.
func TestConfiguredProviderCanSanitizeServerErrorProse(t *testing.T) {
	const secret = "azure-secret-sentinel"
	srv := newFakeOpenAI(t, func(f *fakeOpenAI, conn *websocket.Conn) {
		if !f.waitForSessionUpdate(2 * time.Second) {
			return
		}
		f.send(conn, `{"type":"session.updated"}`)
		f.send(conn, `{"type":"conversation.item.input_audio_transcription.failed","error":{"code":"audio_invalid","message":"request contained azure-secret-sentinel"}}`)
	})
	cfg := manualCommitConfig(srv.url)
	cfg.GetKey = func() (string, error) { return secret, nil }
	cfg.SanitizeServerErrors = true
	p := New(cfg)
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	c.wait(t, 2*time.Second)
	cancel, ok := c.first(stt.Canceled)
	if !ok {
		t.Fatalf("no Canceled event; events: %s", c.summary())
	}
	if strings.Contains(cancel.Error, secret) || strings.Contains(cancel.Error, "request contained") {
		t.Fatalf("raw provider prose reached the user: %q", cancel.Error)
	}
	if !strings.Contains(cancel.Error, "Azure OpenAI") {
		t.Errorf("sanitized error does not identify the service: %q", cancel.Error)
	}
}

// Audio pushed before the session is configured must still arrive, in order.
func TestAudioBufferedBeforeConfigurationIsFlushedInOrder(t *testing.T) {
	release := make(chan struct{})
	srv := newFakeOpenAI(t, func(f *fakeOpenAI, conn *websocket.Conn) {
		<-release
		f.ready(conn)
	})
	p := New(testConfig(srv.url))
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// One sample per chunk keeps the resampling arithmetic trivial to read back.
	p.PushAudio(pcmOf(1000, 1000))
	p.PushAudio(pcmOf(2000, 2000))
	close(release)
	if !srv.waitForAudio(2, 2*time.Second) {
		t.Fatal("el audio previo a la configuración no llegó")
	}
	p.Stop()
	c.wait(t, 5*time.Second)

	chunks := srv.audioChunks()
	first := int16(binary.LittleEndian.Uint16(chunks[0]))
	if first != 1000 {
		t.Errorf("el primer chunk empieza en %d, quería 1000 — llegó fuera de orden", first)
	}

	// AND the configuration still goes first. This is the case that distinguishes it: with audio already
	// waiting in the buffer, a flush placed before the configuration would put PCM on the wire while the
	// service is still deaf. The other ordering test cannot see it — there the buffer is empty at that
	// moment, so the flush sends nothing and the order looks right either way. A mutation that swapped
	// them passed until this assertion existed.
	srv.mu.Lock()
	frames := append([]string(nil), srv.texts...)
	srv.mu.Unlock()
	iCfg, iAudio := -1, -1
	for i, raw := range frames {
		if strings.Contains(raw, `"session.update"`) && iCfg < 0 {
			iCfg = i
		}
		if strings.Contains(raw, `"input_audio_buffer.append"`) && iAudio < 0 {
			iAudio = i
		}
	}
	if iCfg < 0 {
		t.Fatal("no se envió session.update")
	}
	if iAudio < 0 {
		t.Fatal("no llegó audio")
	}
	if iAudio < iCfg {
		t.Errorf("el audio del búfer salió ANTES de session.update (audio en %d, config en %d): el servicio lo descarta en silencio", iAudio, iCfg)
	}
}

// The closing events must be ordered so a transcript is never discarded by the controller.
func TestTheClosingEventsAreOrderedSoTheTranscriptSurvives(t *testing.T) {
	srv := newFakeOpenAI(t, func(f *fakeOpenAI, conn *websocket.Conn) {
		f.ready(conn)
		f.send(conn, `{"type":"conversation.item.input_audio_transcription.completed","transcript":"texto salvado"}`)
		f.send(conn, `{"type":"error","error":{"message":"y luego falló"}}`)
	})
	p := New(testConfig(srv.url))
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	c.wait(t, 5*time.Second)

	events := c.snapshot()
	if events[len(events)-1].Type != stt.Stopped {
		t.Errorf("el último evento fue %s, quería Stopped: %s", events[len(events)-1].Type, c.summary())
	}
	iFinal, iCancel := -1, -1
	for i, e := range events {
		if e.Type == stt.Final && iFinal < 0 {
			iFinal = i
		}
		if e.Type == stt.Canceled && iCancel < 0 {
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

// The pre-flush buffer is bounded in bytes, at the CAPTURE rate.
func TestTheAudioBufferIsBounded(t *testing.T) {
	release := make(chan struct{})
	srv := newFakeOpenAI(t, func(f *fakeOpenAI, conn *websocket.Conn) {
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
		p.PushAudio(pcmOf(int16(i), int16(i)))
	}
	if got := p.bufferedBytes(); got > 10 {
		t.Errorf("el búfer creció a %d bytes con un tope de 10", got)
	}
	close(release)
	p.Stop()
	c.wait(t, 5*time.Second)
}

// A silent service must not hold the session open for ever.
func TestASilentServiceHitsTheFinalizeDeadline(t *testing.T) {
	srv := newFakeOpenAI(t, func(f *fakeOpenAI, conn *websocket.Conn) {
		f.ready(conn)
		time.Sleep(3 * time.Second) // never sends a transcript
	})
	cfg := testConfig(srv.url)
	cfg.FinalizeTimeout = 300 * time.Millisecond
	p := New(cfg)
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	srv.waitForSessionUpdate(2 * time.Second)
	p.Stop()
	c.wait(t, 3*time.Second) // the deadline, not the 3 s the server sleeps
}

func TestStartIsOnlyHonouredOnce(t *testing.T) {
	srv := newFakeOpenAI(t, func(f *fakeOpenAI, conn *websocket.Conn) { f.ready(conn) })
	p := New(testConfig(srv.url))
	c := newCollector()
	if err := p.Start(1, c.sink); err != nil {
		t.Fatalf("primer Start: %v", err)
	}
	if err := p.Start(2, c.sink); err == nil {
		t.Error("el segundo Start no dio error")
	}
	p.Stop()
	p.Stop() // must not panic on a closed channel
	c.wait(t, 5*time.Second)
}
