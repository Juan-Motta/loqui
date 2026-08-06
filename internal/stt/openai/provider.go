package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/Juan-Motta/loqui-go/internal/stt"
)

// One OpenAI realtime transcription session.
//
// THE LIFECYCLE IS GROK'S AND ELEVENLABS', for the reasons documented there: one channel each for
// audio, stop and server messages, because a single channel either dropped the stop (hanging the
// session) or blocked PushAudio (freezing the microphone). This is now the THIRD copy, and the
// extraction the port plan calls for is overdue — noted at the bottom of this comment.
//
// FOUR THINGS DIFFER, and each one is a silent failure if got wrong:
//
//  1. AUTH IS IN THE SUBPROTOCOLS, not a header. A wrong one fails the upgrade with a bare 400.
//  2. THE SESSION MUST BE CONFIGURED BEFORE ANYTHING TRANSCRIBES. `session.update` goes out the moment
//     the socket opens; until then the service is connected and deaf. Unlike ElevenLabs there is no
//     handshake event to wait for — the Electron build emits `started` right after sending it, and this
//     does the same, so "ready" here means "the socket is open and configured".
//  3. THE AUDIO IS 24 kHz. The capture pipeline gives 16, so every chunk goes through Resample. Sending
//     16 kHz into a 24 kHz session is accepted and transcribes a sped-up voice, badly.
//  4. FINALS ARRIVE AS DELTAS PLUS A COMPLETED. The deltas are FRAGMENTS to accumulate; treating each
//     as a partial shows one word at a time instead of a growing phrase. And there is no commit message
//     to send: with server_vad the service closes utterances itself, and a manual commit is an error.
//
// TODO(port): with three implementations of this loop in the tree, extract it. The differences are
// exactly the four above plus the error strings, which is a small enough surface to parameterise; doing
// it now, with three test suites as the net, is the cheapest it will ever be.

const (
	defaultDialTimeout     = 10 * time.Second
	defaultReadyTimeout    = 15 * time.Second
	defaultWriteTimeout    = 5 * time.Second
	defaultFinalizeTimeout = 10 * time.Second
)

// defaultAudioBufferBytes caps the audio held before the session is configured: 30 s of the CAPTURE
// rate, 16-bit mono. Counted at 16 kHz, not this provider's 24, because what is buffered is what the
// capture pipeline handed over — the resampling happens on the way out.
const defaultAudioBufferBytes = 30 * CaptureRate * 2

// readLimitBytes raises the library's 32 KiB default. A long completed transcript can exceed it and the
// library treats an oversized message as a fatal read error — it would kill the session mid-dictation.
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
	// serverErrorCode is what an error EVENT maps to, and it is deliberately non-retryable for the
	// same reason as the other cloud providers': the event carries only prose, so transient and permanent are
	// indistinguishable, and the controller resets its reconnect budget on every successful connect —
	// a retryable classification here becomes an unbounded loop against a metered service.
	serverErrorCode = codeBadRequest
)

// Config is everything the provider needs, resolved by the caller from settings.
type Config struct {
	// GetKey supplies the ElevenLabs API key. Called once per session, at Start.
	GetKey func() (string, error)
	// Language is a code the store validated for this engine, or "" to let the server detect.
	Language string
	// Model is the transcription model; empty resolves to DefaultModel.
	Model string
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
		return fmt.Errorf("openai: already started")
	}
	return err
}

func (p *Provider) resolveKey() (string, error) {
	if p.cfg.GetKey == nil {
		return "", fmt.Errorf("configura la API key de OpenAI en Ajustes")
	}
	key, err := p.cfg.GetKey()
	if err != nil {
		return "", err
	}
	if key == "" {
		return "", fmt.Errorf("configura la API key de OpenAI en Ajustes")
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
// TWO LEVELS, because this service sends fragments and then a whole utterance. `growing` collects the
// deltas so a partial can be shown as a phrase that grows rather than one word at a time; `parts` holds
// the utterances the service declared complete.
//
// The completed event's own transcript WINS over the accumulated deltas when it has one, and the
// deltas are the fallback when it does not — the Electron build does the same (`r.text || partial`).
// Preferring the accumulation would keep any fragment the service later corrected.
type transcript struct {
	parts   []string
	growing strings.Builder
}

func (t *transcript) delta(fragment string) string {
	t.growing.WriteString(fragment)
	return t.growing.String()
}

func (t *transcript) commit(text string) {
	final := strings.TrimSpace(text)
	if final == "" {
		final = strings.TrimSpace(t.growing.String())
	}
	t.growing.Reset()
	if final != "" {
		t.parts = append(t.parts, final)
	}
}

func (t *transcript) text() string {
	all := t.parts
	// Anything still accumulating when the session ends is text the user spoke and the service never
	// closed — usually because the socket died mid-utterance. Dropping it would silently lose a phrase.
	if tail := strings.TrimSpace(t.growing.String()); tail != "" {
		all = append(append([]string(nil), all...), tail)
	}
	return strings.Join(all, " ")
}

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

	// CONFIGURE FIRST, then everything else. Until session.update lands the service is connected and
	// deaf: audio sent before it is discarded, and no event says so. There is no handshake event to wait
	// for either, so a socket that is open AND configured is what "ready" means here.
	if !p.configure(ctx, s) {
		return
	}
	s.ready = true
	p.emit(stt.Event{Type: stt.Started})
	// Audio buffered while dialling goes out now, in order.
	if !p.flushAudio(ctx, s) {
		return
	}
	if s.stopping && !s.finalized {
		p.finalizeNow(s)
	}

	// Armed even though "ready" is local here: a socket that opens and then never accepts a write, or a
	// service that accepts the session and sends nothing at all, would otherwise hold the session open
	// indefinitely.
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
				p.finalizeNow(s)
				armFinalize() // a fresh budget for the last completed transcript
			}

		case <-readyTimer.C:
			if !s.ready {
				s.cancelCode = codeReadyTimeout
				s.cancelText = "OpenAI aceptó la conexión pero no respondió a la sesión"
				return
			}

		case <-finalizeC():
			// No final arrived. Keep what was assembled rather than holding the session open.
			p.cfg.Log("STT", "OpenAI no envió el transcript final — cerrando con lo transcrito")
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
					s.cancelText = "se perdió la conexión con OpenAI"
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
//
// NO Ready CASE, unlike the other two providers: this service sends no handshake event this code waits
// on, and readiness is decided locally once session.update has gone out.
func (p *Provider) handle(ctx context.Context, s *sessionState, out Outcome, armFinalize func()) bool {
	switch out.Kind {
	case PartialDelta:
		// A FRAGMENT, not a partial. Emitting out.Delta alone would show the user one word at a time
		// instead of a phrase that grows.
		p.emit(stt.Event{Type: stt.Partial, Text: s.text.delta(out.Delta)})
		return false

	case Final:
		s.text.commit(out.Text)
		// Not terminal on its own: with server_vad the service closes every utterance, so more may
		// follow while the key is still held. Once the stop has been processed, though, this is the last
		// thing we were waiting for.
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
		endpoint = RealtimeURL
	}

	dialCtx, cancel := context.WithTimeout(ctx, p.cfg.dialTimeout())
	defer cancel()

	conn, resp, err := websocket.Dial(dialCtx, endpoint, &websocket.DialOptions{
		// The key rides in the SUBPROTOCOLS, which is this API's client-auth scheme. Not the URL: a URL
		// reaches logs, error messages and crash reports.
		Subprotocols: BuildSubprotocols(key),
	})
	if err != nil {
		code, message := handshakeFailure(resp, key)
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		// The status and our classification. Never the key.
		p.cfg.Log("STT-ERR", fmt.Sprintf("el handshake con OpenAI falló (status %d) → %s", status, code))
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

// flushAudio writes everything buffered as input_audio_buffer.append messages.
//
// EACH CHUNK IS RESAMPLED HERE, on the way out, rather than on the way in: the buffer holds what the
// capture pipeline produced, so its byte budget stays comparable with every other provider's, and audio
// dropped by the ring is never audio we spent CPU converting.
func (p *Provider) flushAudio(ctx context.Context, s *sessionState) bool {
	for _, chunk := range p.takeAudio() {
		msg, err := buildAppendMessage(Resample(chunk, CaptureRate, SampleRate))
		if err != nil {
			p.cfg.Log("STT-ERR", "no se pudo codificar el audio: "+err.Error())
			if s.cancelCode == "" {
				s.cancelCode = codeBadRequest
				s.cancelText = "no se pudo codificar el audio para OpenAI"
			}
			return false
		}
		if err := p.write(ctx, s, msg); err != nil {
			return false
		}
	}
	return true
}

// buildAppendMessage wraps resampled PCM16 as base64 for the wire.
func buildAppendMessage(pcm []byte) ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":  "input_audio_buffer.append",
		"audio": base64.StdEncoding.EncodeToString(pcm),
	})
}

// configure sends session.update, which is what turns a connected socket into a transcribing one.
func (p *Provider) configure(ctx context.Context, s *sessionState) bool {
	msg, err := BuildSessionUpdate(p.cfg.Model, p.cfg.Language)
	if err != nil {
		p.cfg.Log("STT-ERR", "no se pudo construir session.update: "+err.Error())
		s.cancelCode, s.cancelText = codeBadRequest, "no se pudo configurar la sesión de OpenAI"
		return false
	}
	return p.write(ctx, s, msg) == nil
}

// finalizeNow marks the stream as ended WITHOUT sending anything.
//
// There is no message to send: with server_vad the service decides when an utterance is over, and a
// manual input_audio_buffer.commit while that is on is an error response — the Electron build sends
// nothing either and simply closes. So "finalized" here means "the audio is out, we are waiting for the
// last completed", and the finalize timer is what bounds that wait.
func (p *Provider) finalizeNow(s *sessionState) {
	s.finalized = true
}

func (p *Provider) write(ctx context.Context, s *sessionState, data []byte) error {
	writeCtx, cancel := context.WithTimeout(ctx, p.cfg.writeTimeout())
	defer cancel()
	if err := s.conn.Write(writeCtx, websocket.MessageText, data); err != nil {
		p.cfg.Log("STT-ERR", "no se pudo escribir en el socket de OpenAI: "+err.Error())
		if s.cancelCode == "" {
			s.cancelCode = codeNoResponse
			s.cancelText = "se perdió la conexión con OpenAI"
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
