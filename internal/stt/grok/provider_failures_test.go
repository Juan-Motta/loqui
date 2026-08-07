package grok

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Juan-Motta/loqui-go/internal/session"
	"github.com/Juan-Motta/loqui-go/internal/stt"
	"github.com/coder/websocket"
)

// ---- failures that must not lose the transcript -------------------------------

// A server error mid-session. What was already said has to survive, and it has to be emitted
// BEFORE the Canceled — see finish() for why the order is load-bearing.
func TestServerErrorKeepsWhatWasAlreadySaid(t *testing.T) {
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) {
		g.ready(conn)
		g.send(conn, `{"type":"transcript.partial","text":"esto sí","words":[{"text":"esto sí","start":0,"end":1}],"is_final":true,"speech_final":true,"start":0,"duration":1}`)
		time.Sleep(50 * time.Millisecond)
		g.send(conn, `{"type":"error","message":"pipeline failure"}`)
	})

	p := testProvider(g, nil)
	r := newRecorder()
	_ = p.Start(3, r.sink)
	r.waitForStopped(t, p)

	wantSequence(t, r, stt.Started, stt.Final, stt.Canceled, stt.Stopped)

	final, _ := r.first(stt.Final)
	if final.Text != "esto sí" {
		t.Errorf("final text = %q, want the text said before the error", final.Text)
	}
	cancel, _ := r.first(stt.Canceled)
	if cancel.ErrorCode != serverErrorCode {
		t.Errorf("error code = %q, want %q", cancel.ErrorCode, serverErrorCode)
	}
	if !strings.Contains(cancel.Error, "pipeline failure") {
		t.Errorf("error text = %q, want the server's message", cancel.Error)
	}
}

// THE ORDERING REGRESSION TEST. Run the real controller and assert the transcript is actually
// delivered. With Canceled emitted before Final, the controller bumps the generation on the
// retry path and then drops the Final as stale — the dictation vanishes. A provider-level
// sequence assertion alone would not catch that; this drives the real consumer.
func TestTranscriptSurvivesARetryableCancelThroughTheController(t *testing.T) {
	io := &fakeIO{delivered: make(chan string, 4)}
	c := session.NewController(session.ModeHold, io)
	c.Press()

	// Exactly what the provider emits on a retryable failure, in the order finish() uses.
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.ProviderEvent(stt.Event{Type: stt.Final, Gen: 1, Text: "no me pierdas"})
	c.ProviderEvent(stt.Event{Type: stt.Canceled, Gen: 1, ErrorCode: "ConnectionFailure", Error: "se perdió la conexión"})
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: 1})

	// The cancel was retryable, so the controller bumped the generation and scheduled a retry.
	// The retry connects, hears the rest, the user lets go, and its provider stops. The text
	// from BEFORE the failure has to still be in the message.
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 2})
	c.ProviderEvent(stt.Event{Type: stt.Final, Gen: 2, Text: "y esto tampoco"})
	c.Release()
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: 2})

	select {
	case got := <-io.delivered:
		if !strings.Contains(got, "no me pierdas") {
			t.Errorf("delivered %q — the text from before the retryable failure was lost", got)
		}
		if !strings.Contains(got, "y esto tampoco") {
			t.Errorf("delivered %q — the text from after the retry was lost", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing was delivered at all")
	}
}

// The mirror image: emitting Canceled first loses it. This documents WHY the order in finish()
// matters, so a future refactor that flips it fails here with an explanation.
func TestCanceledBeforeFinalWouldLoseTheTranscript(t *testing.T) {
	io := &fakeIO{delivered: make(chan string, 4)}
	c := session.NewController(session.ModeHold, io)
	c.Press()

	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 1})
	c.ProviderEvent(stt.Event{Type: stt.Canceled, Gen: 1, ErrorCode: "ConnectionFailure", Error: "se perdió"})
	c.ProviderEvent(stt.Event{Type: stt.Final, Gen: 1, Text: "esto se pierde"})
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: 1})

	// Same retry cycle as the test above, so the only difference is where the Final sits.
	c.ProviderEvent(stt.Event{Type: stt.Started, Gen: 2})
	c.Release()
	c.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: 2})

	select {
	case got := <-io.delivered:
		if strings.Contains(got, "esto se pierde") {
			t.Fatal("the controller now accepts a Final after a retryable Canceled — " +
				"finish()'s ordering comment is stale and should be revisited")
		}
	case <-time.After(300 * time.Millisecond):
		// Expected: the Final was stale and dropped. This is the loss the ordering avoids.
	}
}

// The socket dies without warning. Retryable, and it must not lose what was transcribed.
func TestAbruptCloseIsANetworkFailure(t *testing.T) {
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) {
		g.ready(conn)
		g.send(conn, `{"type":"transcript.partial","text":"a medias","words":[{"text":"a medias","start":0,"end":1}],"is_final":true,"speech_final":true,"start":0,"duration":1}`)
		time.Sleep(50 * time.Millisecond)
		conn.CloseNow() // no close handshake, no transcript.done
	})

	p := testProvider(g, nil)
	r := newRecorder()
	_ = p.Start(1, r.sink)
	r.waitForStopped(t, p)

	wantSequence(t, r, stt.Started, stt.Final, stt.Canceled, stt.Stopped)

	cancel, _ := r.first(stt.Canceled)
	if cancel.ErrorCode != codeNoResponse {
		t.Errorf("error code = %q, want %q", cancel.ErrorCode, codeNoResponse)
	}
	if class := session.ClassifyCancel(session.Cancel{ErrorCode: cancel.ErrorCode}); !session.ShouldReconnect(class) {
		t.Error("a dropped connection should be retryable")
	}
	if final, _ := r.first(stt.Final); final.Text != "a medias" {
		t.Errorf("final = %q, want what was transcribed before the drop", final.Text)
	}
}

// The service accepts the socket and then says nothing. Without this bound the session would
// stay open for ever with the pill spinning.
func TestReadyTimeoutCancels(t *testing.T) {
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) {
		time.Sleep(3 * time.Second) // never sends transcript.created
	})

	p := testProvider(g, func(c *Config) { c.ReadyTimeout = 150 * time.Millisecond })
	r := newRecorder()
	_ = p.Start(1, r.sink)
	r.waitForStopped(t, p)

	wantSequence(t, r, stt.Canceled, stt.Stopped)
	cancel, _ := r.first(stt.Canceled)
	if cancel.ErrorCode != codeReadyTimeout {
		t.Errorf("error code = %q, want %q", cancel.ErrorCode, codeReadyTimeout)
	}
	// It never became live, so no Started must have been claimed.
	if r.has(stt.Started) {
		t.Error("Started was emitted although the session was never confirmed")
	}
}

// audio.done went out and transcript.done never came. Close anyway and keep the transcript —
// the alternative is a dictation that never lands.
func TestFinalizeTimeoutStillDelivers(t *testing.T) {
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) {
		g.ready(conn)
		g.send(conn, `{"type":"transcript.partial","text":"quedó dicho","words":[{"text":"quedó dicho","start":0,"end":1}],"is_final":true,"speech_final":true,"start":0,"duration":1}`)
		g.waitForText("audio.done")
		time.Sleep(2 * time.Second) // no transcript.done, ever
	})

	p := testProvider(g, func(c *Config) { c.FinalizeTimeout = 150 * time.Millisecond })
	r := newRecorder()
	_ = p.Start(1, r.sink)
	time.Sleep(150 * time.Millisecond)
	p.Stop()
	r.waitForStopped(t, p)

	wantSequence(t, r, stt.Started, stt.Final, stt.Stopped)
	if final, _ := r.first(stt.Final); final.Text != "quedó dicho" {
		t.Errorf("final = %q, want the transcript kept despite the missing done", final.Text)
	}
}

// ---- handshake failures ------------------------------------------------------

func TestHandshakeStatusesMapToCodes(t *testing.T) {
	for status, want := range map[int]string{
		http.StatusUnauthorized:        codeAuth,
		http.StatusForbidden:           codeForbidden,
		http.StatusTooManyRequests:     codeThrottled,
		http.StatusNotFound:            codeBadRequest,
		http.StatusInternalServerError: codeUnavailable,
	} {
		g := newRejectingGrok(t, status)
		p := testProvider(g, nil)
		r := newRecorder()
		_ = p.Start(1, r.sink)
		r.waitForStopped(t, p)

		wantSequence(t, r, stt.Canceled, stt.Stopped)
		cancel, _ := r.first(stt.Canceled)
		if cancel.ErrorCode != want {
			t.Errorf("status %d → %q, want %q", status, cancel.ErrorCode, want)
		}
		if cancel.Error == "" {
			t.Errorf("status %d produced no message for the user", status)
		}
	}
}

// A bad key must be reported once and never retried: reconnecting with it bills nothing and
// never succeeds.
func TestBadKeyIsReportedOnceAndNotRetryable(t *testing.T) {
	g := newRejectingGrok(t, http.StatusUnauthorized)
	p := testProvider(g, nil)
	r := newRecorder()
	_ = p.Start(1, r.sink)
	r.waitForStopped(t, p)

	if n := r.count(stt.Canceled); n != 1 {
		t.Errorf("got %d Canceled events, want exactly 1", n)
	}
	cancel, _ := r.first(stt.Canceled)
	class := session.ClassifyCancel(session.Cancel{ErrorCode: cancel.ErrorCode, Error: cancel.Error})
	if session.ShouldReconnect(class) {
		t.Errorf("a rejected key classified as %q, which retries", class)
	}
}

func TestDialFailureIsANetworkFailure(t *testing.T) {
	p := New(Config{
		// A port nothing listens on.
		Endpoint:    "ws://127.0.0.1:1",
		GetKey:      func() (string, error) { return "k", nil },
		DialTimeout: time.Second,
	})
	r := newRecorder()
	_ = p.Start(1, r.sink)
	r.waitForStopped(t, p)

	wantSequence(t, r, stt.Canceled, stt.Stopped)
	if cancel, _ := r.first(stt.Canceled); cancel.ErrorCode != codeNoResponse {
		t.Errorf("error code = %q, want %q", cancel.ErrorCode, codeNoResponse)
	}
}

// ---- configuration ----------------------------------------------------------

func TestMissingKeyIsAConfigurationProblem(t *testing.T) {
	for name, get := range map[string]func() (string, error){
		"no reader":  nil,
		"empty":      func() (string, error) { return "", nil },
		"read error": func() (string, error) { return "", fmt.Errorf("el Keychain no respondió") },
	} {
		p := New(Config{GetKey: get})
		r := newRecorder()
		err := p.Start(1, r.sink)
		if err == nil {
			t.Errorf("%s: Start returned no error", name)
		}
		r.waitForStopped(t, p)

		wantSequence(t, r, stt.Canceled, stt.Stopped)
		cancel, _ := r.first(stt.Canceled)
		if cancel.ErrorCode != codeNotConfigured {
			t.Errorf("%s: error code = %q, want %q", name, cancel.ErrorCode, codeNotConfigured)
		}
		// Never retried: only the user can fix it.
		class := session.ClassifyCancel(session.Cancel{ErrorCode: cancel.ErrorCode})
		if session.ShouldReconnect(class) {
			t.Errorf("%s: a missing key must not be retried", name)
		}
	}
}

// ---- lifecycle edges --------------------------------------------------------

func TestStartTwiceIsRejected(t *testing.T) {
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) { g.ready(conn) })
	p := testProvider(g, nil)
	r := newRecorder()

	if err := p.Start(1, r.sink); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := p.Start(2, r.sink); err == nil {
		t.Error("the second Start was accepted; a session is not reusable")
	}
	p.Stop()
	r.waitForStopped(t, p)
}

// Stop and PushAudio before Start must be harmless: the hotkey can produce a release before
// the engine finished starting.
func TestStopAndPushBeforeStartAreHarmless(t *testing.T) {
	p := New(Config{GetKey: func() (string, error) { return "k", nil }})
	p.PushAudio([]byte{1, 2})
	p.Stop()
	p.Stop() // idempotent
}

func TestStopIsIdempotent(t *testing.T) {
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) {
		g.ready(conn)
		g.waitForText("audio.done")
		g.send(conn, `{"type":"transcript.done","text":"ok","words":[{"text":"ok","start":0,"end":1}],"duration":1}`)
	})
	p := testProvider(g, nil)
	r := newRecorder()
	_ = p.Start(1, r.sink)
	time.Sleep(100 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); p.Stop() }()
	}
	wg.Wait()
	r.waitForStopped(t, p)

	if n := r.count(stt.Stopped); n != 1 {
		t.Errorf("got %d Stopped events, want exactly 1", n)
	}
}

// Stop called from INSIDE a sink callback. The controller can do this (a cancel handler runs on
// the same path that emits), so a Stop that blocked on the session goroutine would deadlock the
// whole app.
func TestStopFromInsideTheSinkDoesNotDeadlock(t *testing.T) {
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) {
		g.ready(conn)
		g.waitForText("audio.done")
		g.send(conn, `{"type":"transcript.done","text":"ok","words":[{"text":"ok","start":0,"end":1}],"duration":1}`)
	})
	p := testProvider(g, nil)
	r := newRecorder()

	done := make(chan struct{})
	var once sync.Once
	sink := func(e stt.Event) {
		r.sink(e)
		if e.Type == stt.Started {
			p.Stop() // reentrant, from the provider's own goroutine
		}
		if e.Type == stt.Stopped {
			once.Do(func() { close(done) })
		}
	}
	_ = p.Start(1, sink)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop from inside the sink deadlocked")
	}
}

// Audio arriving after Stop is expected: stopCapture signals the pump but does not wait for it
// (internal/app/dictation.go). It must not panic, block, or reach the wire after audio.done.
func TestPushAudioAfterStopIsRejected(t *testing.T) {
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) {
		g.ready(conn)
		g.waitForText("audio.done")
		g.send(conn, `{"type":"transcript.done","text":"ok","words":[{"text":"ok","start":0,"end":1}],"duration":1}`)
	})
	p := testProvider(g, nil)
	r := newRecorder()
	_ = p.Start(1, r.sink)
	time.Sleep(100 * time.Millisecond)
	p.Stop()

	for i := 0; i < 50; i++ {
		p.PushAudio([]byte{0xFF, 0xFF}) // must all be ignored
	}
	r.waitForStopped(t, p)

	for _, f := range g.snapshot() {
		if f.typ == websocket.MessageBinary && len(f.data) == 2 && f.data[0] == 0xFF {
			t.Error("audio pushed after Stop reached the service")
		}
	}
}

// ---- backpressure and bounds -------------------------------------------------

// THE LIVENESS CASE. A server that accepts the socket but never reads, with the audio buffer
// saturated. Stop must still get through and the session must end — the earlier single-channel
// design could drop the stop here and hang the app, or block the capture pump and freeze the
// microphone.
func TestStopSurvivesASaturatedAudioBuffer(t *testing.T) {
	block := make(chan struct{})
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) {
		// Deliberately never say ready, so nothing can be flushed and the buffer fills.
		<-block
	})
	t.Cleanup(func() { close(block) })

	p := testProvider(g, func(c *Config) {
		c.AudioBufferBytes = 4096
		c.ReadyTimeout = 10 * time.Second // do not let the ready timeout be what saves us
	})
	logs := &logCapture{}
	p.cfg.Log = logs.log
	r := newRecorder()
	_ = p.Start(1, r.sink)

	// Far more than the cap.
	pushDone := make(chan struct{})
	go func() {
		defer close(pushDone)
		for i := 0; i < 2000; i++ {
			p.PushAudio(make([]byte, 320))
		}
	}()
	select {
	case <-pushDone:
	case <-time.After(3 * time.Second):
		t.Fatal("PushAudio blocked under saturation — this freezes the microphone")
	}

	p.Stop()
	r.waitForStopped(t, p) // must not need the ready timeout

	if !strings.Contains(logs.joined(), "búfer de audio se llenó") {
		t.Error("the buffer overflowed without saying so")
	}
}

// The buffer is bounded in BYTES. A frame count would bound nothing, since the capture frame
// size depends on the device.
func TestAudioBufferIsBoundedInBytes(t *testing.T) {
	p := New(Config{
		GetKey:           func() (string, error) { return "k", nil },
		AudioBufferBytes: 1000,
	})
	for i := 0; i < 100; i++ {
		p.PushAudio(make([]byte, 100)) // 10 000 bytes offered
	}

	if got := p.bufferedBytes(); got > 1000 {
		t.Errorf("buffered %d bytes, want at most the 1000-byte cap", got)
	}
}

// A single chunk larger than the whole budget has to be refused, not stored. The drop-oldest
// loop always keeps the newest chunk, so without this the cap would not actually bound anything.
func TestAnOversizedChunkIsRefused(t *testing.T) {
	logs := &logCapture{}
	p := New(Config{
		GetKey:           func() (string, error) { return "k", nil },
		AudioBufferBytes: 1000,
		Log:              logs.log,
	})

	p.PushAudio(make([]byte, 5000))

	if got := p.bufferedBytes(); got != 0 {
		t.Errorf("buffered %d bytes from an oversized chunk, want it refused", got)
	}
	if !strings.Contains(logs.joined(), "no cabe") {
		t.Error("an oversized frame was dropped without saying so")
	}
}

// A long utterance's event carries the whole words array and can exceed coder/websocket's
// 32 KiB default read limit, which the library treats as a fatal read error.
func TestALargeServerMessageIsProcessed(t *testing.T) {
	big := strings.Repeat("palabra ", 12000) // ~96 KB of text
	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) {
		g.ready(conn)
		g.send(conn, fmt.Sprintf(`{"type":"transcript.partial","text":%q,"words":[{"text":%q,"start":0,"end":9}],"is_final":true,"speech_final":true,"start":0,"duration":9}`, big, big))
		g.waitForText("audio.done")
		g.send(conn, `{"type":"transcript.done","text":"","words":[],"duration":9}`)
	})

	p := testProvider(g, nil)
	r := newRecorder()
	_ = p.Start(1, r.sink)
	time.Sleep(200 * time.Millisecond)
	p.Stop()
	r.waitForStopped(t, p)

	final, ok := r.first(stt.Final)
	if !ok {
		t.Fatalf("no Final; a message over 32 KiB killed the session. events=%v", r.types())
	}
	if len(final.Text) < 90000 {
		t.Errorf("final text is %d bytes, want the whole ~96 KB message", len(final.Text))
	}
}

// ---- privacy and goroutines --------------------------------------------------

// The log is written to disk and shown in the UI. Neither the API key nor a transcript may
// ever appear there.
func TestNeitherKeyNorTranscriptIsLogged(t *testing.T) {
	const secret = "xai-super-secret-key"
	const spoken = "esto es lo que dije en voz alta"

	g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) {
		g.ready(conn)
		g.send(conn, fmt.Sprintf(`{"type":"transcript.partial","text":%q,"words":[{"text":%q,"start":0,"end":2}],"is_final":true,"speech_final":true,"start":0,"duration":2}`, spoken, spoken))
		g.waitForText("audio.done")
		g.send(conn, `{"type":"error","message":"algo falló"}`)
	})

	p, logs := testProviderWithLogs(g, func(c *Config) {
		c.GetKey = func() (string, error) { return secret, nil }
	})
	r := newRecorder()
	_ = p.Start(1, r.sink)
	time.Sleep(150 * time.Millisecond)
	p.Stop()
	r.waitForStopped(t, p)

	got := logs.joined()
	if strings.Contains(got, secret) {
		t.Error("the API key was written to the log")
	}
	if strings.Contains(got, spoken) {
		t.Error("transcript text was written to the log")
	}
}

// Every terminal route must wind up the provider's own goroutines. Asserted on the provider's
// WaitGroup rather than a global goroutine count, which would be flaky.
func TestEveryTerminalRouteWindsDownItsGoroutines(t *testing.T) {
	t.Run("normal stop", func(t *testing.T) {
		g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) {
			g.ready(conn)
			g.waitForText("audio.done")
			g.send(conn, `{"type":"transcript.done","text":"ok","words":[{"text":"ok","start":0,"end":1}],"duration":1}`)
		})
		p := testProvider(g, nil)
		r := newRecorder()
		_ = p.Start(1, r.sink)
		time.Sleep(100 * time.Millisecond)
		p.Stop()
		r.waitForStopped(t, p)
		waitForWindDown(t, p)
	})

	t.Run("server error", func(t *testing.T) {
		g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) {
			g.ready(conn)
			g.send(conn, `{"type":"error","message":"boom"}`)
		})
		p := testProvider(g, nil)
		r := newRecorder()
		_ = p.Start(1, r.sink)
		r.waitForStopped(t, p)
		waitForWindDown(t, p)
	})

	t.Run("handshake rejected", func(t *testing.T) {
		g := newRejectingGrok(t, http.StatusUnauthorized)
		p := testProvider(g, nil)
		r := newRecorder()
		_ = p.Start(1, r.sink)
		r.waitForStopped(t, p)
		waitForWindDown(t, p)
	})

	t.Run("stop during dial", func(t *testing.T) {
		block := make(chan struct{})
		g := newFakeGrok(t, func(g *fakeGrok, conn *websocket.Conn) { <-block })
		t.Cleanup(func() { close(block) })

		p := testProvider(g, func(c *Config) { c.ReadyTimeout = 200 * time.Millisecond })
		r := newRecorder()
		_ = p.Start(1, r.sink)
		p.Stop() // released while the server is still silent
		r.waitForStopped(t, p)
		waitForWindDown(t, p)
	})
}

func waitForWindDown(t *testing.T, p *Provider) {
	t.Helper()
	done := make(chan struct{})
	go func() { p.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the provider's goroutines did not wind down")
	}
}

// ---- a controller stand-in ---------------------------------------------------

// fakeIO is the minimum session.IO needed to prove a transcript reaches delivery.
type fakeIO struct {
	delivered chan string
	mu        sync.Mutex
}

func (f *fakeIO) StartEngine(int) {}
func (f *fakeIO) StopEngine(int)  {}
func (f *fakeIO) ShowOverlay()    {}
func (f *fakeIO) HideOverlay()    {}
func (f *fakeIO) DeliverFinal(text, language string, trigger session.Mode) {
	select {
	case f.delivered <- text:
	default:
	}
}
func (f *fakeIO) Overlay(session.OverlayState) {}

// ScheduleReconnect deliberately DROPS the retry instead of running it: these cases are about
// whether the transcript survives the cancel, not about reconnecting.
func (f *fakeIO) ScheduleReconnect(d time.Duration, fn func()) {}
func (f *fakeIO) ReconnectExhausted(int)                       {}

var _ session.IO = (*fakeIO)(nil)
