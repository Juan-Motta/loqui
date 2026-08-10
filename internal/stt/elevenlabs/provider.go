package elevenlabs

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/Juan-Motta/loqui-go/internal/stt"
)

// One ElevenLabs dictation session.
//
// THE CONCURRENCY DESIGN IS GROK'S, ON PURPOSE. It is not copied for convenience: that shape was
// arrived at by fixing real failures, and the comments there record them — one channel for audio,
// stop and server messages meant a full channel either dropped the stop (hanging the session) or
// blocked PushAudio (freezing the microphone). Repeating a design that survived those is safer than
// inventing a second one.
//
// The DUPLICATION IS DELIBERATE AND TEMPORARY. docs/plans/loqui-go-port.md says the socket lifecycle
// gets extracted to a shared package "when ElevenLabs provides the second real implementation" — this
// file is that implementation. Extracting first would have meant refactoring the only working cloud
// engine against a guess at what the second one needs. With both here, and both test suites as the
// net, the extraction can be done without that risk.
//
// WHAT ACTUALLY DIFFERS FROM GROK:
//   - the credential is an `xi-api-key` header, not a Bearer token;
//   - audio goes out as JSON text with base64 inside, not as binary frames;
//   - the stream ends with a commit chunk, not with `{"type":"audio.done"}`;
//   - finals are plain text per utterance, so the assembled transcript is an ordered join — there is
//     no word-timing to reconcile, which is what grok/timeline.go exists for.
const (
	defaultDialTimeout     = 10 * time.Second
	defaultReadyTimeout    = 15 * time.Second
	defaultWriteTimeout    = 5 * time.Second
	defaultFinalizeTimeout = 10 * time.Second
)

// defaultAudioBufferBytes caps the audio held while the server has not confirmed the session:
// 30 s of 16 kHz, 16-bit mono. In BYTES, because the capture frame size depends on the device, so
// "N frames" bounds nothing.
const defaultAudioBufferBytes = 30 * SampleRate * 2

// readLimitBytes raises the library's 32 KiB default. A long committed transcript can exceed it and
// the library treats an oversized message as a fatal read error — it would kill the session
// mid-dictation.
const readLimitBytes = 1024 * 1024

// The cancellation codes. They must be members of the table in internal/session/policy.go: a code
// that is not there falls through to matching the error MESSAGE, and prose-matching breaks the moment
// the app is translated.
const (
	codeNoResponse    = "ConnectionFailure"
	codeAuth          = "AuthenticationFailure"
	codeForbidden     = "Forbidden"
	codeThrottled     = "TooManyRequests"
	codeBadRequest    = "BadRequest"
	codeUnavailable   = "ServiceUnavailable"
	codeReadyTimeout  = "ServiceTimeout"
	codeNotConfigured = "NotConfigured"
	// serverErrorCode remains terminal even though the controller now bounds retries. Unlike Grok,
	// ElevenLabs supplies a machine-readable event name (e.g. auth_error, quota_exceeded,
	// transcriber_error),
	// but handle currently collapses them all here. Retrying that undifferentiated bucket would retry
	// known-permanent auth/config failures. Map Outcome.Code to the shared session vocabulary before
	// making transient members retryable. See docs/research/2026-08-06-where-realtime-stt-auth-fails.md.
	serverErrorCode = codeBadRequest
)

// Config is everything the provider needs, resolved by the caller from settings.
type Config struct {
	// GetKey supplies the ElevenLabs API key. Called once per session, at Start.
	GetKey func() (string, error)
	// Language is a code the store validated for this engine, or "" to let the server detect.
	Language string
	// Endpoint overrides the service URL. Only the tests set it.
	Endpoint string
	// Log receives diagnostics. NEVER called with transcript text or with the key.
	Log func(tag, msg string)

	DialTimeout     time.Duration
	ReadyTimeout    time.Duration
	WriteTimeout    time.Duration
	FinalizeTimeout time.Duration

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

// Provider is one session. Not reusable: Start once, Stop once, then build another — a socket that
// has been closed cannot be reset.
type Provider struct {
	cfg Config

	startOnce sync.Once
	stopOnce  sync.Once

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

type readerMsg struct {
	out Outcome
	// err is set when the read loop ended, on the same channel as the messages so run cannot see
	// the two out of order.
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

// Start resolves the key, then dials and runs the session in the background. It returns without
// waiting for the connection: events arrive through the sink, and blocking here would stall the key
// press that started the dictation.
func (p *Provider) Start(gen int, sink stt.Sink) error {
	var err error
	ran := false
	p.startOnce.Do(func() {
		ran = true
		p.gen, p.sink = gen, sink

		key, keyErr := p.resolveKey()
		if keyErr != nil {
			// A missing key is a configuration problem, never transient, so it must not be
			// retried. Reported through the sink as well as returned, because the controller
			// drives its state machine off events.
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
		return fmt.Errorf("elevenlabs: already started")
	}
	return err
}

func (p *Provider) resolveKey() (string, error) {
	if p.cfg.GetKey == nil {
		return "", fmt.Errorf("configura la API key de ElevenLabs en Ajustes")
	}
	key, err := p.cfg.GetKey()
	if err != nil {
		return "", err
	}
	if key == "" {
		return "", fmt.Errorf("configura la API key de ElevenLabs en Ajustes")
	}
	return key, nil
}

// PushAudio buffers one PCM chunk. Never blocks: it runs on the capture pump, and blocking it would
// freeze the microphone.
func (p *Provider) PushAudio(pcm []byte) {
	if len(pcm) == 0 {
		return
	}
	p.audioMu.Lock()
	if p.audioClosed {
		p.audioMu.Unlock()
		return
	}
	// A chunk bigger than the whole budget is rejected outright: the drop-oldest loop always keeps
	// the newest chunk, so one absurd frame would sit above the bound indefinitely.
	if len(pcm) > p.cfg.audioBufferBytes() {
		p.audioMu.Unlock()
		p.cfg.Log("STT-ERR", fmt.Sprintf("descartando un frame de %d bytes: no cabe en el búfer", len(pcm)))
		return
	}
	// Copy: the capture buffer is reused by the next callback.
	chunk := append([]byte(nil), pcm...)
	p.audio = append(p.audio, chunk)
	p.audioBytes += len(chunk)
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
		close(p.stopCh)
	})
}

// bufferedBytes is how much PCM is waiting. For the tests that assert the buffer is bounded.
func (p *Provider) bufferedBytes() int {
	p.audioMu.Lock()
	defer p.audioMu.Unlock()
	return p.audioBytes
}

func (p *Provider) takeAudio() [][]byte {
	p.audioMu.Lock()
	defer p.audioMu.Unlock()
	out := p.audio
	p.audio, p.audioBytes = nil, 0
	return out
}

// ---- the session goroutine ---------------------------------------------------

// transcript accumulates what the session produced.
//
// Plain concatenation of committed utterances, which is all this service needs: with
// commit_strategy=vad the server closes each phrase itself and each committed_transcript is final and
// disjoint. Grok needs a word timeline instead because its finals can RESTATE earlier words, and
// joining those blindly duplicates text.
type transcript struct {
	parts []string
}

func (t *transcript) commit(text string) {
	if s := strings.TrimSpace(text); s != "" {
		t.parts = append(t.parts, s)
	}
}

func (t *transcript) text() string { return strings.Join(t.parts, " ") }

type sessionState struct {
	conn  *websocket.Conn
	text  transcript
	ready bool
	// stopping means the user released the key; we are waiting to flush and commit.
	stopping bool
	// finalized means the commit went out, so a last committed_transcript may still arrive.
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

	conn, dialErr := p.dial(ctx, key)
	if dialErr != nil {
		s.cancelCode, s.cancelText = dialErr.code, dialErr.message
		return
	}
	s.conn = conn
	conn.SetReadLimit(readLimitBytes)

	p.wg.Add(1)
	go p.read(ctx, conn)

	// Armed from the moment the socket is open: a service that accepts the connection and then says
	// nothing would otherwise hold the session open indefinitely.
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
	// armFinalize bounds everything after the user lets go. Armed on STOP, not only when the commit
	// goes out: if the release arrives before the handshake, the flush waits for the session to be
	// confirmed, and the only other bound would be the ready timeout — far longer than anyone should
	// watch a spinning pill after releasing the key.
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
			stopCh = nil // a closed channel is always ready; nil it to avoid a busy loop
			s.stopping = true
			armFinalize()
			if s.ready {
				// DRAIN BEFORE COMMITTING. Buffered audio may have a wake signal that has not been
				// selected yet — select picks at random among ready cases, so the stop can win the
				// race against its own audio. Committing first tells the service the phrase ended
				// before it heard the end of it, and it fails intermittently rather than
				// consistently.
				//
				// NOT COVERED BY A TEST, and said out loud rather than left implied: swapping these two
				// calls passes the whole suite. Forcing the race from outside does not work — `run`
				// observes the wake before Stop can close its channel almost every time — and a
				// repeat-until-it-happens test passed 24 rounds with the order inverted, so it was
				// removed for claiming coverage it did not have. Covering this needs a seam that lets a
				// test hold audio in the buffer while the stop is processed.
				if !p.flushAudio(ctx, s) {
					return
				}
				if !p.finalize(ctx, s) {
					return
				}
				armFinalize() // a fresh budget for the last committed transcript
			}

		case <-readyTimer.C:
			if !s.ready {
				s.cancelCode = codeReadyTimeout
				s.cancelText = "ElevenLabs aceptó la conexión pero no confirmó la sesión"
				return
			}

		case <-finalizeC():
			// No final arrived. Keep what was assembled rather than holding the session open.
			p.cfg.Log("STT", "ElevenLabs no envió el transcript final — cerrando con lo transcrito")
			return

		case <-p.wake:
			if s.ready && !s.finalized {
				if !p.flushAudio(ctx, s) {
					return
				}
			}

		case m := <-p.msgs:
			if m.err != nil {
				// After the commit this is the expected goodbye; before it, the connection dropped.
				if !s.finalized {
					s.cancelCode = codeNoResponse
					s.cancelText = "se perdió la conexión con ElevenLabs"
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
func (p *Provider) handle(ctx context.Context, s *sessionState, out Outcome, armFinalize func()) bool {
	switch out.Kind {
	case Ready:
		if s.ready {
			return false
		}
		s.ready = true
		p.emit(stt.Event{Type: stt.Started})
		// Everything buffered while connecting goes out now, IN ORDER, and only then the commit if
		// the user already let go.
		if !p.flushAudio(ctx, s) {
			return true
		}
		if s.stopping && !s.finalized {
			if !p.finalize(ctx, s) {
				return true
			}
			armFinalize()
		}
		return false

	case Partial:
		p.emit(stt.Event{Type: stt.Partial, Text: out.Text})
		return false

	case Final:
		s.text.commit(out.Text)
		// NOT terminal on its own: with VAD the server commits every utterance, so more may follow
		// while the key is still held. The session ends on stop, on the socket closing, or on the
		// finalize deadline — treating the first final as the end would truncate a long dictation to
		// its first phrase.
		//
		// Once the commit HAS gone out, though, a final is the last thing we were waiting for.
		return s.finalized

	case Error:
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
		// The key goes in a header, never the URL: a URL reaches logs, error messages and crash
		// reports.
		HTTPHeader: http.Header{APIKeyHeader: []string{key}},
	})
	if err != nil {
		code, message := handshakeFailure(resp, key)
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		// The status and our classification. Never the key.
		p.cfg.Log("STT-ERR", fmt.Sprintf("el handshake con ElevenLabs falló (status %d) → %s", status, code))
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
			continue // everything on this endpoint is text JSON
		}
		select {
		case p.msgs <- readerMsg{out: Decode(data)}:
		case <-ctx.Done():
			return
		}
	}
}

// flushAudio writes everything buffered, as JSON text with base64 inside. Returns false when the
// session must end.
func (p *Provider) flushAudio(ctx context.Context, s *sessionState) bool {
	for _, chunk := range p.takeAudio() {
		msg, err := BuildAudioMessage(chunk, SampleRate, false)
		if err != nil {
			// Encoding our own message cannot fail in practice; if it ever does, dropping the audio
			// silently would be worse than ending the session with a reason.
			p.cfg.Log("STT-ERR", "no se pudo codificar el audio: "+err.Error())
			if s.cancelCode == "" {
				s.cancelCode = codeBadRequest
				s.cancelText = "no se pudo codificar el audio para ElevenLabs"
			}
			return false
		}
		if err := p.write(ctx, s, msg); err != nil {
			return false
		}
	}
	return true
}

// finalize forces the server to commit what it has. An EMPTY chunk with commit=true, because there is
// no audio left to send at this point — the flush already went out — and the flag is what ends the
// utterance. There is no `audio.done` equivalent on this endpoint.
func (p *Provider) finalize(ctx context.Context, s *sessionState) bool {
	msg, err := BuildAudioMessage(nil, SampleRate, true)
	if err != nil {
		p.cfg.Log("STT-ERR", "no se pudo codificar el commit: "+err.Error())
		return false
	}
	if err := p.write(ctx, s, msg); err != nil {
		return false
	}
	s.finalized = true
	return true
}

func (p *Provider) write(ctx context.Context, s *sessionState, data []byte) error {
	writeCtx, cancel := context.WithTimeout(ctx, p.cfg.writeTimeout())
	defer cancel()
	if err := s.conn.Write(writeCtx, websocket.MessageText, data); err != nil {
		p.cfg.Log("STT-ERR", "no se pudo escribir en el socket de ElevenLabs: "+err.Error())
		if s.cancelCode == "" {
			s.cancelCode = codeNoResponse
			s.cancelText = "se perdió la conexión con ElevenLabs"
		}
		return err
	}
	return nil
}

// finish emits the closing events in the ONE order that does not lose the transcript: Final, then any
// Canceled, then Stopped.
//
// Emitting Canceled first loses everything on the retry path: the controller bumps the generation on
// cancel, and from then on it rejects the older one — so the Final that followed would be discarded
// along with the Stopped. Final first means it is accumulated while the generation is still current.
func (p *Provider) finish(s *sessionState) {
	if text := s.text.text(); text != "" {
		p.emit(stt.Event{Type: stt.Final, Text: text})
	}
	if s.cancelCode != "" || s.cancelText != "" {
		p.emit(stt.Event{Type: stt.Canceled, ErrorCode: s.cancelCode, Error: s.cancelText})
	}
	p.emit(stt.Event{Type: stt.Stopped})

	// Only now: cancelling earlier would close the socket before the commit could be sent.
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
