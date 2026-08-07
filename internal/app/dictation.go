// The dictation engine: everything between a key press and text appearing at the cursor.
// This is the Go counterpart of the wiring that lived in the Electron main process
// (loqui/src/main/main.ts) around the tested SessionController.
//
// It implements session.IO, so all the DECISIONS stay in internal/session (tested without
// a machine) and this file only performs them: pick a provider from settings, open the
// microphone, pump frames, read focus, paste, store history, run the timers.
package app

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/Juan-Motta/loqui-go/internal/audio"
	"github.com/Juan-Motta/loqui-go/internal/history"
	"github.com/Juan-Motta/loqui-go/internal/inject"
	"github.com/Juan-Motta/loqui-go/internal/session"
	"github.com/Juan-Motta/loqui-go/internal/store"
	"github.com/Juan-Motta/loqui-go/internal/stt"
	"github.com/Juan-Motta/loqui-go/internal/stt/azure"
	"github.com/Juan-Motta/loqui-go/internal/stt/elevenlabs"
	"github.com/Juan-Motta/loqui-go/internal/stt/grok"
	"github.com/Juan-Motta/loqui-go/internal/stt/helper"
	"github.com/Juan-Motta/loqui-go/internal/stt/openai"
)

// idleLimit stops a session left open with no speech.
//
// It is a BILLING guard, not a cap on how long you may dictate: a cloud recognizer
// streaming an empty room costs money and produces nothing. Any speech resets it, so a
// long dictation is never cut short — what gets stopped is a session the user forgot about.
const idleLimit = 60 * time.Second

// UI is what the dictation engine needs from the window layer. Narrow on purpose: this
// package must not import Wails, so it stays testable and so the windows can change
// without touching the engine.
type UI interface {
	ShowOverlay()
	HideOverlay()
	// EmitOverlay pushes the reduced display state to the pill.
	EmitOverlay(session.OverlayState)
	// EmitLevel pushes a 0..1 microphone level for the bars and the Home waveform.
	EmitLevel(float64)
	// HistoryChanged tells the UI a record landed, so lists refresh live.
	HistoryChanged()
	// Log records a diagnostic line. NEVER called with transcript text.
	Log(tag, msg string)
}

// Dictation owns one dictation pipeline at a time.
type Dictation struct {
	store *store.Store
	ui    UI

	controller *session.Controller
	// pastes serialises injection: two overlapping pastes corrupt the clipboard
	// save/restore and interleave the text.
	pastes *inject.Queue

	mu       sync.Mutex
	provider stt.Provider
	capture  *audio.Capture
	// pumpDone stops the goroutine feeding audio to the provider.
	pumpDone chan struct{}
	// sessionApp is the frontmost app when this dictation STARTED, used to detect focus
	// drift before pasting.
	sessionApp string

	lastActivity time.Time
	idleTicker   *time.Ticker
	idleStop     chan struct{}
	reconnect    *time.Timer

	// peakLevel is the loudest microphone level seen this session, 0..1.
	//
	// Logged when the session ends, because "did the microphone hear anything at all" is the first
	// question every empty-transcript report raises and the only one nothing else answers. A peak of
	// zero says the audio never arrived, which is a different problem from a transcript that came back
	// empty — and they send you to completely different places.
	peakLevel float64
	// metering is whether a session is live and its peak may still move. It exists because levels
	// arrive from a CHILD PROCESS that outlives the stop by up to 300 ms — see noteLevel.
	metering bool

	// getSecret overrides the credential read. Only the tests set it — see secretReader.
	getSecret func(store.KeySlot) (string, error)
}

func NewDictation(st *store.Store, ui UI) *Dictation {
	d := &Dictation{store: st, ui: ui, pastes: inject.NewQueue()}
	mode := session.Mode(st.LoadSettings().Mode)
	d.controller = session.NewController(mode, d)
	return d
}

// Controller exposes the controller so the tray, the hotkey and the UI can drive it.
func (d *Dictation) Controller() *session.Controller { return d.controller }

// ---- session.IO -------------------------------------------------------------

func (d *Dictation) StartEngine(gen int) {
	d.noteActivity()
	d.clearTimers()

	// Opened HERE, at the start, and closed where the peak is reported. Between those two points a
	// level may move the peak; outside them it is a straggler from a helper that has not died yet.
	d.mu.Lock()
	d.peakLevel = 0
	d.metering = true
	d.mu.Unlock()

	// Capture the frontmost app before anything else: the whole point is to compare it
	// against the app in focus when the paste happens, and by then Loqui may have been
	// activated for other reasons.
	go func() {
		st := inject.ReadFocusState()
		d.mu.Lock()
		d.sessionApp = st.App
		d.mu.Unlock()
	}()

	provider, err := d.buildProvider()
	if err != nil {
		d.ui.Log("STT-ERR", err.Error())
		// Report it through the same event path a provider would, so the controller
		// tears the session down instead of believing it is live.
		d.controller.ProviderEvent(stt.Event{
			Type: stt.Canceled, Gen: gen,
			ErrorCode: "NotConfigured", Error: err.Error(),
		})
		d.controller.ProviderEvent(stt.Event{Type: stt.Stopped, Gen: gen})
		return
	}

	d.mu.Lock()
	d.provider = provider
	d.mu.Unlock()

	if err := provider.Start(gen, d.controller.ProviderEvent); err != nil {
		d.ui.Log("STT-ERR", fmt.Sprintf("provider start failed: %v", err))
		return // the provider already emitted canceled + stopped
	}

	if provider.WantsAudio() {
		if err := d.startCapture(provider); err != nil {
			d.ui.Log("MIC-ERR", err.Error())
			d.controller.ProviderEvent(stt.Event{
				Type: stt.Canceled, Gen: gen,
				ErrorCode: "NotConfigured",
				Error:     "No se pudo abrir el micrófono — revisa el permiso en Ajustes",
			})
			return
		}
	}

	d.startIdleGuard()
	d.ui.Log("CTRL", fmt.Sprintf("startEngine gen=%d", gen))
}

func (d *Dictation) StopEngine(gen int) {
	d.ui.Log("CTRL", fmt.Sprintf("stopEngine gen=%d", gen))
	d.clearTimers()
	d.stopCapture()

	d.mu.Lock()
	provider := d.provider
	d.provider = nil
	d.mu.Unlock()

	if provider != nil {
		// Stop is asynchronous by contract: the provider emits Stopped once it has
		// flushed whatever it still had, and the controller waits for that rather than
		// assuming teardown is instant. Delivering earlier truncates the dictation.
		provider.Stop()
	}
}

func (d *Dictation) ShowOverlay() { d.ui.ShowOverlay() }
func (d *Dictation) HideOverlay() { d.ui.HideOverlay() }

func (d *Dictation) Overlay(state session.OverlayState) {
	d.ui.EmitOverlay(state)
}

// DeliverFinal is the payoff: the session's complete message, delivered once.
//
// Focus is read ONCE and gates paste and history together — they are different questions
// about the same instant, and reading twice could see two different apps.
func (d *Dictation) DeliverFinal(text, language string, trigger session.Mode) {
	d.mu.Lock()
	sessionApp := d.sessionApp
	d.mu.Unlock()

	focus := inject.ReadFocusState()

	if inject.ShouldInjectInto(inject.GuardInput{
		Text:        text,
		SecureField: focus.SecureField,
		SessionApp:  sessionApp,
		CurrentApp:  focus.App,
	}) {
		d.pastes.Go(func() {
			res := inject.Text(text, inject.Options{})
			if res.ClipboardKept {
				// Not an error: someone copied something during the paste window and we
				// chose their content over restoring. Worth logging, because the user's
				// clipboard is not what it was before dictation.
				d.ui.Log("PASTE", "clipboard changed during paste — left the new contents alone")
			}
		})
	} else if focus.SecureField {
		d.ui.Log("PASTE-SKIP", "secure field")
	} else {
		d.ui.Log("PASTE-SKIP", "app changed since dictation started")
	}

	if !inject.ShouldStoreFinal(focus.SecureField) {
		d.ui.Log("HISTORY-SKIP", "secure field") // never persist password-field dictation
		return
	}
	rec := history.MakeRecord(text, language, string(trigger), time.Now().UnixMilli())
	if err := d.store.AppendHistory(rec); err != nil {
		d.ui.Log("HISTORY-ERR", err.Error())
		return
	}
	d.ui.HistoryChanged()
}

func (d *Dictation) ScheduleReconnect(delay time.Duration, fn func()) {
	d.ui.Log("RECONNECT", fmt.Sprintf("retry in %s", delay))
	d.mu.Lock()
	if d.reconnect != nil {
		d.reconnect.Stop()
	}
	d.reconnect = time.AfterFunc(delay, fn)
	d.mu.Unlock()
}

// ---- providers ---------------------------------------------------------------

// buildProvider resolves the configured engine.
//
// Only Azure exists so far; the others land with phase 3. An unimplemented provider is
// reported as a configuration problem rather than silently substituting a different
// engine, because dictating into the wrong service is worse than not dictating.
func (d *Dictation) buildProvider() (stt.Provider, error) {
	settings := d.store.LoadSettings()

	switch settings.Provider {
	case "azure":
		if settings.Region == "" {
			return nil, fmt.Errorf("configura la región de Azure en Ajustes")
		}
		getKey := d.keyReaderFor(store.SlotAzureSpeech)
		// Read the key up front rather than asking HasKey, so the three outcomes stay
		// distinguishable. "No configuraste la clave" and "no pude leer tus claves" are
		// completely different problems, and reporting the first for the second sends the
		// user to re-enter a key that is already there.
		if _, err := getKey(); err != nil {
			switch {
			case errors.Is(err, store.ErrNoSecret):
				return nil, fmt.Errorf("configura la clave de Azure Speech en Ajustes")
			case errors.Is(err, store.ErrSecretsUnreadable):
				return nil, fmt.Errorf("no se pudieron leer las claves guardadas — revisa %s, o pasa la clave en LOQUI_AZURE_KEY para probar", d.store.SecretsPath())
			default:
				return nil, fmt.Errorf("no se pudo leer la clave guardada: %w", err)
			}
		}
		tokens := azure.NewTokenService(azure.TokenOptions{
			Region: settings.Region,
			GetKey: getKey,
		})
		return azure.New(azure.Config{
			Region:     settings.Region,
			Candidates: d.store.LanguagesFor("azure-speech"),
			Tokens:     tokens,
		}), nil
	case "grok":
		return d.buildGrokProvider()

	case "elevenlabs":
		return d.buildElevenLabsProvider()

	case "openai":
		return d.buildOpenAIProvider()

	case "macos":
		return d.buildAppleProvider()

	case "whisper":
		return d.buildWhisperProvider()

	default:
		return nil, fmt.Errorf("el motor %q todavía no está portado — elige otro en Ajustes", settings.Provider)
	}
}

// buildGrokProvider streams to xAI over a WebSocket. Cloud, paid by the hour, and fed by the
// host's single capture pipeline like Azure.
//
// The key is read UP FRONT rather than asking HasKey, so the three outcomes stay
// distinguishable — the same reasoning as Azure above. "You never configured a key" and "the
// keys could not be read" send the user to completely different places.
func (d *Dictation) buildGrokProvider() (stt.Provider, error) {
	getKey := d.keyReaderFor(store.SlotGrok)
	if _, err := getKey(); err != nil {
		switch {
		case errors.Is(err, store.ErrNoSecret):
			return nil, fmt.Errorf("configura la API key de xAI en Ajustes")
		case errors.Is(err, store.ErrSecretsUnreadable):
			return nil, fmt.Errorf("no se pudieron leer las claves guardadas — revisa %s, o pasa la clave en LOQUI_GROK_KEY para probar", d.store.SecretsPath())
		default:
			return nil, fmt.Errorf("no se pudo leer la API key de xAI guardada: %w", err)
		}
	}
	return grok.New(grok.Config{
		GetKey: getKey,
		// One optional language, or "auto" to omit it entirely. Note that for xAI this only
		// controls how numbers and units are written out — the model transcribes any
		// supported language either way.
		Language: d.store.LanguagesFor("grok")[0],
		Log:      d.ui.Log,
	}), nil
}

// buildElevenLabsProvider streams to ElevenLabs Scribe v2 over a WebSocket.
//
// Same shape as Grok — cloud, metered, fed by the host's single capture pipeline — and the key is read
// UP FRONT for the same reason: "you never configured a key" and "your keys could not be read" send
// the user to completely different places, and HasKey collapses them.
func (d *Dictation) buildElevenLabsProvider() (stt.Provider, error) {
	getKey := d.keyReaderFor(store.SlotElevenLabs)
	if _, err := getKey(); err != nil {
		switch {
		case errors.Is(err, store.ErrNoSecret):
			return nil, fmt.Errorf("configura la API key de ElevenLabs en Ajustes")
		case errors.Is(err, store.ErrSecretsUnreadable):
			return nil, fmt.Errorf("no se pudieron leer las claves guardadas — revisa %s, o pasa la clave en LOQUI_ELEVENLABS_KEY para probar", d.store.SecretsPath())
		default:
			return nil, fmt.Errorf("no se pudo leer la API key de ElevenLabs guardada: %w", err)
		}
	}
	// One optional language, and the sentinel matters: this endpoint has no "auto" value, so the
	// parameter is omitted entirely for automatic detection. Sending the literal "auto" would be read
	// as a language name.
	language := d.store.LanguagesFor("elevenlabs")[0]
	if language == "auto" {
		language = ""
	}
	return elevenlabs.New(elevenlabs.Config{
		GetKey:   getKey,
		Language: language,
		Log:      d.ui.Log,
	}), nil
}

// buildOpenAIProvider streams to OpenAI's realtime transcription endpoint.
//
// TWO THINGS SET IT APART from the other cloud providers, both handled inside the provider: the session
// must be configured with a session.update before anything is transcribed, and its audio is 24 kHz while
// the capture pipeline delivers 16 — so every chunk is resampled on the way out. Sending 16 kHz into a
// 24 kHz session is accepted and transcribes a sped-up voice, which is why that conversion is not
// optional.
func (d *Dictation) buildOpenAIProvider() (stt.Provider, error) {
	getKey := d.keyReaderFor(store.SlotOpenAI)
	if _, err := getKey(); err != nil {
		switch {
		case errors.Is(err, store.ErrNoSecret):
			return nil, fmt.Errorf("configura la API key de OpenAI en Ajustes")
		case errors.Is(err, store.ErrSecretsUnreadable):
			return nil, fmt.Errorf("no se pudieron leer las claves guardadas — revisa %s, o pasa la clave en LOQUI_OPENAI_KEY para probar", d.store.SecretsPath())
		default:
			return nil, fmt.Errorf("no se pudo leer la API key de OpenAI guardada: %w", err)
		}
	}
	// "auto" is this app's sentinel for "detect", and the API has no such value: the hint is simply
	// omitted. Sending the literal string would be read as a language name.
	language := d.store.LanguagesFor("openai")[0]
	if language == "auto" {
		language = ""
	}
	return openai.New(openai.Config{
		GetKey:   getKey,
		Language: language,
		// The stored model, reused from the Azure OpenAI field the settings already carry. Empty falls
		// back to the provider's default rather than failing.
		Model: d.store.LoadSettings().AzureOpenAiDeployment,
		Log:   d.ui.Log,
	}), nil
}

// buildAppleProvider runs Apple's on-device SpeechAnalyzer (macOS 26+): free, offline,
// private, and single-language per session because Apple has no continuous LID.
func (d *Dictation) buildAppleProvider() (stt.Provider, error) {
	bin := HelperPath("macos-stt")
	if bin == "" {
		return nil, fmt.Errorf("el helper de Apple no está compilado — corre `./scripts/build-macos-stt.sh`")
	}
	// The Apple engine cannot auto-detect, so its slot always holds a real locale. Sending
	// "auto" would make the helper fail with "locale not supported" instead of dictating.
	locale := d.store.LanguagesFor("macos")[0]
	if locale == "auto" {
		locale = "es-CO"
	}
	return helper.New(helper.Config{
		Bin:      bin,
		BuildCmd: "./scripts/build-macos-stt.sh",
		Locale:   locale,
		Log:      d.ui.Log,
		// The helper opens the microphone itself, so the host cannot meter it. If this helper
		// reports levels they reach the UI; if it does not, the meter simply stays quiet — which
		// is the honest outcome, and better than an animation that implies audio is arriving.
		OnLevel: d.noteLevel,
	}), nil
}

// buildWhisperProvider runs whisper.cpp locally: free, offline, and the DEFAULT engine,
// because it is the only one that works with no account, no key and no network.
func (d *Dictation) buildWhisperProvider() (stt.Provider, error) {
	bin := HelperPath("whisper-stt")
	if bin == "" {
		return nil, fmt.Errorf("el helper de Whisper no está compilado — corre `./scripts/build-whisper-stt.sh`")
	}
	model := WhisperModelPath(d.store.Dir())
	if _, err := os.Stat(model); err != nil {
		// Without the model the helper would start and immediately die; say what is
		// actually missing instead.
		return nil, fmt.Errorf("falta el modelo de Whisper — descárgalo desde Ajustes")
	}
	// The GPU is used unless it already proved fatal on this machine.
	gpu := d.store.WhisperGPUAllowed()
	gpuEnv := "0"
	if gpu {
		gpuEnv = "1"
	}
	return helper.New(helper.Config{
		Bin:        bin,
		BuildCmd:   "./scripts/build-whisper-stt.sh",
		Locale:     d.store.LanguagesFor("whisper")[0], // whisper understands "auto"
		ExtraArgs:  []string{model},
		Env:        map[string]string{"LOQUI_WHISPER_GPU": gpuEnv},
		GPUEnabled: gpu,
		Log:        d.ui.Log,
		OnGPUCrash: d.store.MarkWhisperGPUBroken,
		// Levels come from the helper because it, not the host, owns the microphone.
		OnLevel: d.noteLevel,
	}), nil
}

// noteLevel forwards a microphone level to the UI and remembers the session peak.
//
// Every level goes through here, whoever measured it — the host's own capture for the cloud
// providers, or the helper's reports for the local engines — so the meter and the peak mean the same
// thing regardless of which engine ran.
func (d *Dictation) noteLevel(level float64) {
	d.mu.Lock()
	// IGNORED WHEN NO SESSION IS RUNNING, and this is a correctness fix rather than tidiness.
	//
	// StopEngine resets the peak and then stops the helper, and the Apple one is only signalled 300 ms
	// later (helper/provider.go). Levels arriving in that window used to seed the NEXT session's peak,
	// so a dictation that heard nothing could be logged as having had audio — destroying the one line
	// whose whole purpose is telling "no audio reached us" apart from "audio arrived and the engine
	// returned nothing". Found by a cross-engine review.
	if !d.metering {
		d.mu.Unlock()
		return
	}
	if level > d.peakLevel {
		d.peakLevel = level
	}
	d.mu.Unlock()
	d.ui.EmitLevel(level)
}

// ---- capture -----------------------------------------------------------------

func (d *Dictation) startCapture(provider stt.Provider) error {
	cap, err := audio.StartCapture(d.store.LoadSettings().InputDeviceID, nil)
	if err != nil {
		return fmt.Errorf("microphone: %w", err)
	}
	done := make(chan struct{})

	d.mu.Lock()
	d.capture = cap
	d.pumpDone = done
	d.mu.Unlock()

	go func() {
		for {
			select {
			case <-done:
				return
			case frame, ok := <-cap.Frames():
				if !ok {
					return
				}
				provider.PushAudio(frame.PCM)
				d.noteLevel(frame.Level)
				// Any sound at all counts as activity for the idle guard. Using the
				// transcript instead would stop a session whenever the provider was slow
				// to recognise, which is exactly when the user is still talking.
				if frame.Level > 0 {
					d.noteActivity()
				}
			}
		}
	}()
	return nil
}

func (d *Dictation) stopCapture() {
	d.mu.Lock()
	cap, done := d.capture, d.pumpDone
	d.capture, d.pumpDone = nil, nil
	d.mu.Unlock()

	if done != nil {
		close(done)
	}
	if cap != nil {
		cap.Close()
	}
	d.ui.EmitLevel(0)

	// Report what the microphone actually heard. A peak of zero is the single most useful fact when a
	// dictation produced nothing: it separates "no audio reached us" from "audio arrived and the
	// engine returned nothing", and no other line in the log distinguishes those.
	d.mu.Lock()
	peak := d.peakLevel
	d.peakLevel = 0
	// Closed BEFORE the peak is reported, so a late level from a helper that has not died yet cannot
	// land between the reset and the next session's start.
	d.metering = false
	d.mu.Unlock()
	d.ui.Log("MIC", fmt.Sprintf("peak level this session: %.2f", peak))
}

// ---- timers ------------------------------------------------------------------

func (d *Dictation) noteActivity() {
	d.mu.Lock()
	d.lastActivity = time.Now()
	d.mu.Unlock()
}

func (d *Dictation) startIdleGuard() {
	ticker := time.NewTicker(5 * time.Second)
	stop := make(chan struct{})

	d.mu.Lock()
	d.idleTicker, d.idleStop = ticker, stop
	d.mu.Unlock()

	go func() {
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				d.mu.Lock()
				last := d.lastActivity
				d.mu.Unlock()
				if d.controller.Desired() && session.IsIdleExpired(last, time.Now(), idleLimit) {
					d.ui.Log("IDLE", "auto-stop after 60s of silence (billing safety)")
					d.controller.StopByGuard()
					return
				}
			}
		}
	}()
}

func (d *Dictation) clearTimers() {
	d.mu.Lock()
	ticker, stop, reconnect := d.idleTicker, d.idleStop, d.reconnect
	d.idleTicker, d.idleStop, d.reconnect = nil, nil, nil
	d.mu.Unlock()

	if ticker != nil {
		ticker.Stop()
	}
	if stop != nil {
		close(stop)
	}
	if reconnect != nil {
		reconnect.Stop()
	}
}

// Shutdown stops everything on the way out. A global event tap or an open microphone left
// behind by a quitting app is a system-wide problem, not just ours.
func (d *Dictation) Shutdown() {
	d.clearTimers()
	d.stopCapture()

	d.mu.Lock()
	provider := d.provider
	d.provider = nil
	d.mu.Unlock()

	if provider != nil {
		provider.Stop()
	}
}

// Compile-time proof that the wiring satisfies the tested contract.
var _ session.IO = (*Dictation)(nil)

// envKeyOverride names the variable that lets a development build supply one provider's API
// key without storing it at all.
//
// WHY IT EXISTS. On an ad-hoc-signed build — which is every local build, since the
// signature changes each time — SecItemCopyMatching never returns: macOS wants to ask
// permission and cannot show the prompt. That makes the entire app untestable for a reason
// that has nothing to do with the code under test.
//
// So this is an escape hatch, not a feature: it is checked BEFORE the stored credentials, keyed off
// an environment variable a packaged app will never have set, and every use is logged so it
// can never be mistaken for the real path. The real fix is a stable signing identity.
//
// PER SLOT, deliberately. This used to be a single Azure-only constant, so a key for any
// other provider was silently ignored and the read fell through to the Keychain that did
// not answer — which made every new provider untestable for an unrelated reason. One
// variable per slot also means one provider's credential can never satisfy another's
// read: dictating into the wrong service is worse than not dictating.
func envKeyOverride(slot store.KeySlot) string {
	switch slot {
	case store.SlotAzureSpeech:
		return "LOQUI_AZURE_KEY"
	case store.SlotAzureOpenAI:
		return "LOQUI_AZURE_OPENAI_KEY"
	case store.SlotOpenAI:
		return "LOQUI_OPENAI_KEY"
	case store.SlotGrok:
		return "LOQUI_GROK_KEY"
	case store.SlotElevenLabs:
		return "LOQUI_ELEVENLABS_KEY"
	default:
		return ""
	}
}

// keyReader returns the function the provider should use to fetch its key.
func (d *Dictation) keyReaderFor(slot store.KeySlot) func() (string, error) {
	if name := envKeyOverride(slot); name != "" {
		if v := os.Getenv(name); v != "" {
			d.ui.Log("DEV", name+" is set — using it instead of the stored credentials")
			return func() (string, error) { return v, nil }
		}
	}
	return func() (string, error) { return d.secretReader()(slot) }
}

// secretReader is the store's GetKey unless a test replaced it.
//
// The seam still earns its place now that the credentials are a file rather than the Keychain: the
// real one reads the REAL data directory, so a test for "what happens with no key configured" would
// otherwise depend on whether the developer running it happens to have one saved.
func (d *Dictation) secretReader() func(store.KeySlot) (string, error) {
	if d.getSecret != nil {
		return d.getSecret
	}
	return d.store.GetKey
}
