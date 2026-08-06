package grok

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/Juan-Motta/loqui-go/internal/stt"
)

// Defaults for the four timeouts. Only the tests override them — a suite that waited the real
// finalize timeout per case would be unusable.
const (
	defaultDialTimeout     = 10 * time.Second
	defaultReadyTimeout    = 15 * time.Second
	defaultWriteTimeout    = 5 * time.Second
	defaultFinalizeTimeout = 10 * time.Second
)

// defaultAudioBufferBytes caps the audio held while the server has not said it is ready:
// 30 s of 16 kHz, 16-bit mono.
//
// Counted in BYTES, not frames. The capture callback's frame size depends on the device
// (internal/audio/capture.go), so "N frames" bounds nothing — it could be four seconds or
// several minutes.
const defaultAudioBufferBytes = 30 * SampleRate * 2

// readLimitBytes raises coder/websocket's 32 KiB default. Grok's events carry the full `words`
// array, so a long utterance can exceed it, and the library treats an oversized message as a
// fatal read error — it would kill the session mid-dictation. Same reasoning and same number as
// the local helper's scanner buffer (internal/stt/helper/provider.go).
const readLimitBytes = 1024 * 1024

// audioDoneMessage ends the stream. It is what makes the service flush its buffered audio and
// emit transcript.done.
//
// NOT `finalize`: that one keeps the session open for another turn and never produces a
// transcript.done, so waiting for one after sending it would hang until the timeout.
const audioDoneMessage = `{"type":"audio.done"}`

// Config is everything the provider needs, resolved by the caller from settings.
type Config struct {
	// GetKey supplies the xAI API key. Called once per session, at Start.
	GetKey func() (string, error)
	// Language is an xAI language code, or "auto"/empty to let the service detect.
	Language string
	// Endpoint overrides the service URL. Only the tests set it.
	Endpoint string
	// Log receives diagnostics. NEVER called with transcript text or with the key.
	Log func(tag, msg string)

	// The timeouts. Zero means the default; only the tests set them.
	DialTimeout     time.Duration
	ReadyTimeout    time.Duration
	WriteTimeout    time.Duration
	FinalizeTimeout time.Duration

	// AudioBufferBytes caps the pre-ready audio buffer. Zero means the default.
	AudioBufferBytes int
}

func (c Config) dialTimeout() time.Duration  { return orDefault(c.DialTimeout, defaultDialTimeout) }
func (c Config) readyTimeout() time.Duration { return orDefault(c.ReadyTimeout, defaultReadyTimeout) }
func (c Config) writeTimeout() time.Duration { return orDefault(c.WriteTimeout, defaultWriteTimeout) }
func (c Config) finalizeTimeout() time.Duration {
	return orDefault(c.FinalizeTimeout, defaultFinalizeTimeout)
}

func (c Config) audioBufferBytes() int {
	if c.AudioBufferBytes > 0 {
		return c.AudioBufferBytes
	}
	return defaultAudioBufferBytes
}

func orDefault(v, def time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return def
}

// Provider is one Grok dictation session. Not reusable: Start once, Stop once, then build
// another — like the Azure recognizer, and for the same reason (a socket that has been closed
// cannot be reset).
//
// CONCURRENCY. Exactly one goroutine (run) owns the session state and is the only writer to the
// socket, so frames go out in the order they were accepted. Three separate paths reach it, each
// with its own guarantee:
//
//   - AUDIO goes into a byte-bounded ring under its own mutex, and PushAudio signals run
//     without blocking. When the ring is full the OLDEST PCM is dropped — never a control
//     signal, and never the newest frame.
//   - STOP closes a channel, exactly once. Closing cannot block and cannot be lost, so Stop
//     returns immediately as the contract requires (internal/stt/stt.go:73) and is safe to call
//     from inside a sink callback.
//   - SERVER MESSAGES arrive on a channel from the reader goroutine. A blocking send there is
//     fine: run always comes back to it.
//
// The earlier design put all three on one channel, which meant a full channel either dropped
// the stop (hanging the session) or blocked PushAudio (freezing the microphone).
type Provider struct {
	cfg Config

	startOnce sync.Once
	stopOnce  sync.Once

	// audio ring, with its own lock so PushAudio never contends with the session state.
	audioMu     sync.Mutex
	audio       [][]byte
	audioBytes  int
	audioClosed bool
	dropped     bool

	wake   chan struct{}  // capacity 1: "there is audio to send"
	stopCh chan struct{}  // closed once by Stop
	msgs   chan readerMsg // from the reader goroutine
	wg     sync.WaitGroup
	cancel context.CancelFunc

	sink stt.Sink
	gen  int
}

// readerMsg is one message from the socket reader, or the end of it.
type readerMsg struct {
	out outcome
	// err is set when the read loop ended. A closed socket is reported this way rather than
	// as a separate channel, so run cannot see the two out of order.
	err error
}

func New(cfg Config) *Provider {
	if cfg.Log == nil {
		cfg.Log = func(string, string) {}
	}
	return &Provider{
		cfg:    cfg,
		wake:   make(chan struct{}, 1),
		stopCh: make(chan struct{}),
		msgs:   make(chan readerMsg, 16),
	}
}

// WantsAudio: yes. The host captures once and pushes to every cloud provider.
func (p *Provider) WantsAudio() bool { return true }

// Start resolves the key, then dials and runs the session in the background. It returns
// without waiting for the connection: the contract is that events arrive through the sink, and
// blocking here would stall the key press that triggered the dictation.
func (p *Provider) Start(gen int, sink stt.Sink) error {
	var err error
	ran := false
	p.startOnce.Do(func() {
		ran = true
		p.gen, p.sink = gen, sink

		key, keyErr := p.resolveKey()
		if keyErr != nil {
			// A missing key is a configuration problem, never a transient one, so it must
			// not be retried. Reported through the sink as well as returned, because the
			// controller drives its state machine off events.
			p.emit(stt.Event{Type: stt.Canceled, ErrorCode: codeNotConfigured, Error: keyErr.Error()})
			p.emit(stt.Event{Type: stt.Stopped})
			err = keyErr
			return
		}

		ctx, cancel := context.WithCancel(context.Background())
		p.cancel = cancel
		p.wg.Add(1)
		go p.run(ctx, key)
	})
	if !ran {
		return fmt.Errorf("grok: already started")
	}
	return err
}

func (p *Provider) resolveKey() (string, error) {
	if p.cfg.GetKey == nil {
		return "", fmt.Errorf("configura la API key de xAI en Ajustes")
	}
	key, err := p.cfg.GetKey()
	if err != nil {
		return "", err
	}
	if key == "" {
		return "", fmt.Errorf("configura la API key de xAI en Ajustes")
	}
	return key, nil
}

// PushAudio buffers one PCM chunk. Never blocks: it runs on the capture pump
// (internal/app/dictation.go), and blocking it would freeze the microphone.
func (p *Provider) PushAudio(pcm []byte) {
	if len(pcm) == 0 {
		return
	}
	p.audioMu.Lock()
	if p.audioClosed {
		// Stop already ran. `stopCapture` signals the pump but does not wait for it
		// (internal/app/dictation.go), so a late frame after Stop is expected, not a bug —
		// Azure ignores audio once stopping for the same reason.
		p.audioMu.Unlock()
		return
	}
	// A single chunk bigger than the whole budget is rejected outright. Without this the
	// drop-oldest loop below cannot bring the total back under the cap — it always keeps the
	// newest chunk — so one absurd frame would sit above the bound indefinitely. Real capture
	// frames are ~3 KB against a 960 KB budget, so this means something is badly wrong upstream.
	if len(pcm) > p.cfg.audioBufferBytes() {
		p.audioMu.Unlock()
		p.cfg.Log("STT-ERR", fmt.Sprintf("descartando un frame de %d bytes: no cabe en el búfer", len(pcm)))
		return
	}

	// Copy: the capture buffer is reused by the next callback.
	chunk := append([]byte(nil), pcm...)
	p.audio = append(p.audio, chunk)
	p.audioBytes += len(chunk)
	// Over the cap, drop the OLDEST PCM. Never the newest — losing the audio the user is
	// speaking right now would be the wrong end to give up — and never a control signal, which
	// is why control does not share this buffer.
	sayDropped := false
	for p.audioBytes > p.cfg.audioBufferBytes() && len(p.audio) > 1 {
		p.audioBytes -= len(p.audio[0])
		p.audio = p.audio[1:]
		if !p.dropped {
			p.dropped, sayDropped = true, true
		}
	}
	p.audioMu.Unlock()

	if sayDropped {
		// Once per session: a line per dropped frame would bury everything else.
		p.cfg.Log("STT", "el búfer de audio se llenó — descartando el más viejo")
	}

	select {
	case p.wake <- struct{}{}:
	default: // already signalled; run will drain everything
	}
}

// Stop ends the session. Returns immediately; Stopped arrives through the sink.
func (p *Provider) Stop() {
	p.stopOnce.Do(func() {
		p.audioMu.Lock()
		p.audioClosed = true // reject new audio atomically, keeping what was accepted
		p.audioMu.Unlock()
		close(p.stopCh) // cannot block, cannot be lost
	})
}

// bufferedBytes is how much PCM is waiting. For the tests that assert the buffer is bounded.
func (p *Provider) bufferedBytes() int {
	p.audioMu.Lock()
	defer p.audioMu.Unlock()
	return p.audioBytes
}

// takeAudio removes and returns everything buffered.
func (p *Provider) takeAudio() [][]byte {
	p.audioMu.Lock()
	defer p.audioMu.Unlock()
	out := p.audio
	p.audio, p.audioBytes = nil, 0
	return out
}

// ---- the session goroutine ---------------------------------------------------

// session is run's local state. Deliberately not on Provider: nothing else may touch it, and
// keeping it here makes that structural rather than a convention.
type sessionState struct {
	conn     *websocket.Conn
	timeline timeline
	ready    bool
	// stopping means the user released the key; we are waiting to flush and finalize.
	stopping bool
	// finalized means audio.done went out, so transcript.done may arrive.
	finalized bool

	cancelCode string
	cancelText string
}

func (p *Provider) run(ctx context.Context, key string) {
	defer p.wg.Done()

	s := &sessionState{}
	defer func() {
		if s.conn != nil {
			s.conn.CloseNow()
		}
		p.finish(s)
	}()

	conn, err := p.dial(ctx, key)
	if err != nil {
		s.cancelCode, s.cancelText = err.code, err.message
		return
	}
	s.conn = conn
	conn.SetReadLimit(readLimitBytes)

	p.wg.Add(1)
	go p.read(ctx, conn)

	// Armed from the moment the socket is open: a service that accepts the connection and then
	// says nothing would otherwise hold the session open indefinitely.
	readyTimer := time.NewTimer(p.cfg.readyTimeout())
	defer readyTimer.Stop()
	var finalizeTimer *time.Timer
	defer func() {
		if finalizeTimer != nil {
			finalizeTimer.Stop()
		}
	}()
	finalizeC := func() <-chan time.Time {
		if finalizeTimer == nil {
			return nil
		}
		return finalizeTimer.C
	}
	// armFinalize bounds everything that happens after the user lets go.
	//
	// IT IS ARMED ON STOP, NOT ONLY WHEN audio.done GOES OUT. If the release arrives before
	// transcript.created, the flush has to wait for the service to confirm the session — and if
	// that never happens, the only other bound would be the ready timeout, which is far longer
	// than anyone should watch a spinning pill after letting go of the key. It is re-armed when
	// audio.done actually goes out, so waiting for transcript.done gets its own full budget.
	armFinalize := func() {
		if finalizeTimer != nil {
			finalizeTimer.Stop()
		}
		finalizeTimer = time.NewTimer(p.cfg.finalizeTimeout())
	}

	stopCh := p.stopCh
	for {
		select {
		case <-ctx.Done():
			return

		case <-stopCh:
			// Selected once: a closed channel is always ready, so nil it out to avoid a
			// busy loop.
			stopCh = nil
			s.stopping = true
			armFinalize()
			if s.ready {
				// DRAIN BEFORE FINALIZING. There may be buffered audio whose wake signal
				// has not been selected yet — `select` chooses at random among ready cases,
				// so the stop can win the race against its own audio. Finalizing first tells
				// the service the stream ended before it heard the end of the sentence, and
				// it fails intermittently rather than consistently.
				if !p.flushAudio(ctx, s) {
					return
				}
				if !p.finalize(ctx, s) {
					return
				}
				armFinalize() // a fresh budget for transcript.done
			}
			// Not ready yet: the flush and the finalize happen when transcript.created
			// arrives, and the timer just armed bounds the wait for it.

		case <-readyTimer.C:
			if !s.ready {
				s.cancelCode = codeReadyTimeout
				s.cancelText = "xAI aceptó la conexión pero no confirmó la sesión"
				return
			}

		case <-finalizeC():
			// The service never sent transcript.done. Keep what was assembled rather than
			// holding the session open for ever.
			p.cfg.Log("STT", "xAI no envió transcript.done — cerrando con lo transcrito")
			return

		case <-p.wake:
			if s.ready && !s.finalized {
				if !p.flushAudio(ctx, s) {
					return
				}
			}

		case m := <-p.msgs:
			if m.err != nil {
				// The socket ended. After audio.done that is the expected goodbye; before
				// it, the connection dropped under us.
				if !s.finalized {
					s.cancelCode = codeNoResponse
					s.cancelText = "se perdió la conexión con xAI"
				}
				return
			}
			if done := p.handle(ctx, s, m.out, armFinalize); done {
				return
			}
		}
	}
}

// handle folds one server message into the session. Returns true when the session is over.
// armFinalize re-arms the post-release deadline; it is owned by run, which owns the timers.
func (p *Provider) handle(ctx context.Context, s *sessionState, out outcome, armFinalize func()) bool {
	switch out.Kind {
	case outcomeReady:
		if s.ready {
			return false
		}
		s.ready = true
		if out.SessionID != "" {
			// The service's own id, for correlating with xAI's logs. Not a transcript.
			p.cfg.Log("STT", "sesión de xAI "+out.SessionID)
		}
		p.emit(stt.Event{Type: stt.Started})
		// Everything buffered while connecting goes out now, IN ORDER, and only then the
		// finalize if the user already let go.
		if !p.flushAudio(ctx, s) {
			return true
		}
		if s.stopping && !s.finalized {
			// The user let go before the service confirmed the session. Now that the queued
			// audio is out, end the stream — and give transcript.done its own budget.
			if !p.finalize(ctx, s) {
				return true
			}
			armFinalize()
		}
		return false

	case outcomePartial:
		p.emit(stt.Event{Type: stt.Partial, Text: out.Text})
		return false

	case outcomeFinal:
		switch res := s.timeline.commit(out.Commit); res {
		case commitIgnoredNoEvidence:
			// transcript.done with neither word times nor a span, while we already hold
			// word-timed text. Logged because it is the one branch where the design
			// knowingly trades a possible missed correction for never duplicating.
			p.cfg.Log("STT", "transcript.done sin posiciones — conservando lo ya transcrito")
		case commitUsedAsFallback:
			p.cfg.Log("STT", "transcript.done sin posiciones — usado como único texto")
		}
		// transcript.done is terminal: the service closes right after it.
		return out.Terminal

	case outcomeError:
		s.cancelCode = serverErrorCode
		s.cancelText = out.Error
		return true

	default:
		return false
	}
}

type dialFailure struct {
	code    string
	message string
}

func (p *Provider) dial(ctx context.Context, key string) (*websocket.Conn, *dialFailure) {
	endpoint := p.cfg.Endpoint
	if endpoint == "" {
		endpoint = WSEndpoint
	}

	dialCtx, cancel := context.WithTimeout(ctx, p.cfg.dialTimeout())
	defer cancel()

	conn, resp, err := websocket.Dial(dialCtx, buildURL(endpoint, p.cfg.Language), &websocket.DialOptions{
		// The key goes in a header, which is why this socket cannot live in a browser and
		// why the handshake status is the only machine-readable failure signal we get.
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + key}},
	})
	if err != nil {
		code, message := handshakeFailure(resp, key)
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		// The status and our own classification. Never the key.
		p.cfg.Log("STT-ERR", fmt.Sprintf("el handshake con xAI falló (status %d) → %s", status, code))
		return nil, &dialFailure{code: code, message: message}
	}
	return conn, nil
}

// read pumps the socket into p.msgs. The only reader, as the library requires.
func (p *Provider) read(ctx context.Context, conn *websocket.Conn) {
	defer p.wg.Done()
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			select {
			case p.msgs <- readerMsg{err: err}:
			case <-ctx.Done():
			}
			return
		}
		if typ != websocket.MessageText {
			continue // transcripts and control are text JSON; binary is not ours
		}
		select {
		case p.msgs <- readerMsg{out: decode(data)}:
		case <-ctx.Done():
			return
		}
	}
}

// flushAudio writes everything buffered. Returns false when the session must end.
func (p *Provider) flushAudio(ctx context.Context, s *sessionState) bool {
	for _, chunk := range p.takeAudio() {
		if err := p.write(ctx, s, websocket.MessageBinary, chunk); err != nil {
			return false
		}
	}
	return true
}

// finalize sends audio.done, which is what makes the service flush and reply transcript.done.
func (p *Provider) finalize(ctx context.Context, s *sessionState) bool {
	if err := p.write(ctx, s, websocket.MessageText, []byte(audioDoneMessage)); err != nil {
		return false
	}
	s.finalized = true
	return true
}

func (p *Provider) write(ctx context.Context, s *sessionState, typ websocket.MessageType, data []byte) error {
	writeCtx, cancel := context.WithTimeout(ctx, p.cfg.writeTimeout())
	defer cancel()
	if err := s.conn.Write(writeCtx, typ, data); err != nil {
		p.cfg.Log("STT-ERR", "no se pudo escribir en el socket de xAI: "+err.Error())
		if s.cancelCode == "" {
			s.cancelCode = codeNoResponse
			s.cancelText = "se perdió la conexión con xAI"
		}
		return err
	}
	return nil
}

// finish emits the session's closing events, in the ONE order that does not lose the
// transcript: Final, then any Canceled, then Stopped.
//
// WHY THIS ORDER. Emitting Canceled first is what the previous design did, and it loses
// everything on the retry path: handleCancelLocked bumps the generation
// (internal/session/controller.go:350), and from then on Accepts() rejects the older
// generation (tracker.go:57), so the Final that followed would be discarded along with the
// Stopped. With the Final first it is accumulated while the generation is still current, and it
// survives into the retry — or is flushed immediately if the cancel turns out to be terminal.
//
// Stopped goes last because the controller treats it as the moment the message is complete
// (controller.go:308).
func (p *Provider) finish(s *sessionState) {
	if text := s.timeline.text(); text != "" {
		p.emit(stt.Event{Type: stt.Final, Text: text})
	}
	if s.cancelCode != "" || s.cancelText != "" {
		p.emit(stt.Event{Type: stt.Canceled, ErrorCode: s.cancelCode, Error: s.cancelText})
	}
	p.emit(stt.Event{Type: stt.Stopped})

	// Only now: cancelling earlier would close the socket before audio.done could be sent.
	if p.cancel != nil {
		p.cancel()
	}
}

func (p *Provider) emit(evt stt.Event) {
	if p.sink == nil {
		return
	}
	evt.Gen = p.gen
	p.sink(evt)
}

var _ stt.Provider = (*Provider)(nil)
