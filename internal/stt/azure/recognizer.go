// The Azure Speech provider. Ported from the Electron engine renderer
// (loqui/src/engine/engine.ts), which drove the JavaScript SDK inside a hidden window.
//
// SAME CONFIGURATION, DIFFERENT AUDIO SOURCE. The service side is identical to what
// Electron sent — universal v2 endpoint, LanguageIdMode=Continuous, an
// AutoDetectSourceLanguageConfig over the candidate locales, continuous recognition —
// so multilingual detection behaves the same. What changed is where the samples come
// from: the JS SDK opened the microphone itself through getUserMedia, while here the
// host captures once in Go and pushes PCM16 into a push stream. That is what lets one
// capture pipeline feed every provider (see internal/stt/stt.go).
//
// Verified end to end in docs/research/2026-07-27-azure-speech-go-macos.md.
package azure

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Microsoft/cognitive-services-speech-sdk-go/audio"
	"github.com/Microsoft/cognitive-services-speech-sdk-go/common"
	"github.com/Microsoft/cognitive-services-speech-sdk-go/speech"

	"github.com/Juan-Motta/loqui-go/internal/settings"
	"github.com/Juan-Motta/loqui-go/internal/stt"
)

// The push stream format. Fixed rather than configurable: 16 kHz mono is what Azure
// wants and what every other Loqui provider takes, so one capture format serves all.
const (
	SampleRate    = 16000
	BitsPerSample = 16
	Channels      = 1
)

// Tokens last 10 minutes. Swapping at 8 leaves room for a slow request without ever
// letting a live recognizer hold an expired credential.
const tokenRefreshEvery = 8 * time.Minute

// Config is everything the recognizer needs, resolved by the caller from settings.
type Config struct {
	// Region is used to build the v2 endpoint and to request tokens.
	Region string
	// Candidates are the LID locales, one per base language (see ValidateCandidates).
	Candidates []string
	// Tokens supplies short-lived authorization tokens. The subscription key never
	// reaches this package.
	Tokens *TokenService
}

// Recognizer is a single Azure Speech session. Not reusable: Start/Stop once, then
// build another — mirroring the Electron engine, which closed and rebuilt the SDK
// recognizer for every dictation rather than trying to reset one.
type Recognizer struct {
	cfg Config

	mu       sync.Mutex
	started  bool
	stopping bool
	gen      int
	sink     stt.Sink

	speechCfg *speech.SpeechConfig
	langCfg   *speech.AutoDetectSourceLanguageConfig
	format    *audio.AudioStreamFormat
	stream    *audio.PushAudioInputStream
	audioCfg  *audio.AudioConfig
	rec       *speech.SpeechRecognizer

	refreshStop chan struct{}
}

// New builds a recognizer for one session.
func New(cfg Config) *Recognizer {
	return &Recognizer{cfg: cfg}
}

// WantsAudio: yes — the host captures and pushes.
func (r *Recognizer) WantsAudio() bool { return true }

// Start opens the session. Every event emitted carries gen.
//
// On any failure it emits Canceled with a STRUCTURED ErrorCode and then Stopped, rather
// than returning the error alone: the session controller drives its state machine off
// events, so a start that fails silently would leave the session believing it is live.
func (r *Recognizer) Start(gen int, sink stt.Sink) error {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return fmt.Errorf("azure: recognizer already started")
	}
	r.started = true
	r.gen = gen
	r.sink = sink
	r.mu.Unlock()

	if err := r.build(); err != nil {
		r.fail(err)
		return err
	}
	if err := <-r.rec.StartContinuousRecognitionAsync(); err != nil {
		r.fail(fmt.Errorf("azure: could not start recognition: %w", err))
		return err
	}
	r.startTokenRefresh()
	return nil
}

func (r *Recognizer) build() error {
	endpoint, err := settings.BuildV2Endpoint(r.cfg.Region)
	if err != nil {
		return err
	}
	candidates, err := settings.ValidateCandidates(r.cfg.Candidates)
	if err != nil {
		return err
	}
	if r.cfg.Tokens == nil {
		return ErrNoKey
	}
	token, err := r.cfg.Tokens.Token(context.Background(), false)
	if err != nil {
		return err
	}

	if r.speechCfg, err = speech.NewSpeechConfigFromEndpoint(endpoint); err != nil {
		return fmt.Errorf("azure: speech config: %w", err)
	}
	if err = r.speechCfg.SetAuthorizationToken(token); err != nil {
		return fmt.Errorf("azure: authorization token: %w", err)
	}
	// Continuous LID: detect the language AND changes to it mid-session. Without this
	// property the v2 endpoint identifies once, at the start, and then commits — which
	// is precisely the behaviour Loqui exists to avoid for bilingual dictation.
	if err = r.speechCfg.SetProperty(common.SpeechServiceConnectionLanguageIDMode, "Continuous"); err != nil {
		return fmt.Errorf("azure: language id mode: %w", err)
	}
	if r.langCfg, err = speech.NewAutoDetectSourceLanguageConfigFromLanguages(candidates); err != nil {
		return fmt.Errorf("azure: language candidates: %w", err)
	}
	if r.format, err = audio.GetWaveFormatPCM(SampleRate, BitsPerSample, Channels); err != nil {
		return fmt.Errorf("azure: audio format: %w", err)
	}
	if r.stream, err = audio.CreatePushAudioInputStreamFromFormat(r.format); err != nil {
		return fmt.Errorf("azure: push stream: %w", err)
	}
	if r.audioCfg, err = audio.NewAudioConfigFromStreamInput(r.stream); err != nil {
		return fmt.Errorf("azure: audio config: %w", err)
	}
	if r.rec, err = speech.NewSpeechRecognizerFromAutoDetectSourceLangConfig(r.speechCfg, r.langCfg, r.audioCfg); err != nil {
		return fmt.Errorf("azure: recognizer: %w", err)
	}
	r.wire()
	return nil
}

func (r *Recognizer) wire() {
	r.rec.SessionStarted(func(e speech.SessionEventArgs) {
		defer e.Close()
		r.emit(stt.Event{Type: stt.Started})
	})
	r.rec.Recognizing(func(e speech.SpeechRecognitionEventArgs) {
		defer e.Close()
		if e.Result.Text != "" {
			r.emit(stt.Event{Type: stt.Partial, Text: e.Result.Text})
		}
	})
	r.rec.Recognized(func(e speech.SpeechRecognitionEventArgs) {
		defer e.Close()
		if e.Result.Reason != common.RecognizedSpeech || e.Result.Text == "" {
			return // NoMatch: a VAD pause with nothing in it
		}
		r.emit(stt.Event{
			Type:     stt.Final,
			Text:     e.Result.Text,
			Language: e.Result.Properties.GetProperty(common.SpeechServiceConnectionAutoDetectSourceLanguageResult, ""),
		})
	})
	r.rec.Canceled(func(e speech.SpeechRecognitionCanceledEventArgs) {
		defer e.Close()
		// ErrorCode is the SDK's own enum ("AuthenticationFailure", "ConnectionFailure",
		// …). The retry policy reads this and never the message, which may be localised.
		r.emit(stt.Event{
			Type:      stt.Canceled,
			Error:     e.ErrorDetails,
			ErrorCode: errorCodeName(e.ErrorCode),
		})
	})
	r.rec.SessionStopped(func(e speech.SessionEventArgs) {
		defer e.Close()
		r.emit(stt.Event{Type: stt.Stopped})
	})
}

// PushAudio hands one PCM16 chunk to the service. Silently ignored once stopping: the
// capture goroutine and Stop race by nature, and writing to a closed stream is a crash,
// not a diagnostic.
func (r *Recognizer) PushAudio(pcm []byte) {
	r.mu.Lock()
	stream, stopping := r.stream, r.stopping
	r.mu.Unlock()
	if stream == nil || stopping || len(pcm) == 0 {
		return
	}
	_ = stream.Write(pcm)
}

// Stop ends the session.
//
// It closes the push stream FIRST and only then asks the recognizer to stop. That order
// matters: closing the stream tells the service no more audio is coming, so it flushes
// whatever it was still holding and sends the last Final. Stopping the recognizer first
// would tear the session down with that segment undelivered — the tail of the dictation
// silently lost, which is the failure the Electron build hit and documented.
func (r *Recognizer) Stop() {
	r.mu.Lock()
	if !r.started || r.stopping {
		r.mu.Unlock()
		return
	}
	r.stopping = true
	stream, rec := r.stream, r.rec
	r.mu.Unlock()

	r.stopTokenRefresh()
	if stream != nil {
		stream.CloseStream()
	}
	if rec != nil {
		go func() {
			<-rec.StopContinuousRecognitionAsync() // SessionStopped -> Stopped event
			r.release()
		}()
		return
	}
	r.emit(stt.Event{Type: stt.Stopped})
	r.release()
}

// startTokenRefresh swaps a fresh token onto the LIVE recognizer before the current one
// expires, so a long dictation is not cut off mid-sentence by an auth failure. It forces
// a real fetch — reusing the cached token would set the same expiring value back.
func (r *Recognizer) startTokenRefresh() {
	stop := make(chan struct{})
	r.mu.Lock()
	r.refreshStop = stop
	r.mu.Unlock()

	go func() {
		ticker := time.NewTicker(tokenRefreshEvery)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				r.mu.Lock()
				rec := r.rec
				r.mu.Unlock()
				if rec == nil {
					return
				}
				token, err := r.cfg.Tokens.Token(context.Background(), true)
				if err != nil {
					// Not fatal on its own: the current token is still valid for a
					// couple more minutes, and a cancel will report it if it isn't.
					continue
				}
				_ = rec.SetAuthorizationToken(token)
			}
		}
	}()
}

func (r *Recognizer) stopTokenRefresh() {
	r.mu.Lock()
	stop := r.refreshStop
	r.refreshStop = nil
	r.mu.Unlock()
	if stop != nil {
		close(stop)
	}
}

// fail reports a start-time failure as the pair of events the session controller
// expects. NotConfigured is used for anything only the user can fix, because that code
// tells the policy never to retry.
func (r *Recognizer) fail(err error) {
	code := "StartFailed"
	if errorIs(err, ErrNoKey) || errorIs(err, ErrBadCredentials) {
		code = "NotConfigured"
	}
	r.emit(stt.Event{Type: stt.Canceled, Error: err.Error(), ErrorCode: code})
	r.emit(stt.Event{Type: stt.Stopped})
	r.release()
}

func (r *Recognizer) emit(e stt.Event) {
	r.mu.Lock()
	sink, gen := r.sink, r.gen
	r.mu.Unlock()
	if sink == nil {
		return
	}
	e.Gen = gen
	sink(e)
}

// release frees the C handles. Every one of these wraps a native object, so skipping it
// leaks memory that Go's collector cannot see.
func (r *Recognizer) release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rec != nil {
		r.rec.Close()
		r.rec = nil
	}
	if r.audioCfg != nil {
		r.audioCfg.Close()
		r.audioCfg = nil
	}
	if r.stream != nil {
		r.stream.Close()
		r.stream = nil
	}
	if r.format != nil {
		r.format.Close()
		r.format = nil
	}
	if r.langCfg != nil {
		r.langCfg.Close()
		r.langCfg = nil
	}
	if r.speechCfg != nil {
		r.speechCfg.Close()
		r.speechCfg = nil
	}
}

// errorCodeName renders the SDK's cancellation code as a stable string.
//
// The generated String() produces exactly the names the JavaScript SDK reported
// ("AuthenticationFailure", "ConnectionFailure", …), which is what the ported reconnect
// policy matches on, and degrades to "CancellationErrorCode(n)" for a value this SDK
// version doesn't know. Deferring to it beats a hand-written switch that would silently
// fall out of date as the enum grows.
func errorCodeName(code common.CancellationErrorCode) string {
	return code.String()
}

// Compile-time proof that the port satisfies the shared contract.
var _ stt.Provider = (*Recognizer)(nil)
