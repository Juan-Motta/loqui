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
		// distinguishable. "No configuraste la clave" and "el Keychain no contesta" are
		// completely different problems, and reporting the first for the second sends the
		// user to re-enter a key that is already there.
		if _, err := getKey(); err != nil {
			switch {
			case errors.Is(err, store.ErrNoSecret):
				return nil, fmt.Errorf("configura la clave de Azure Speech en Ajustes")
			case errors.Is(err, store.ErrKeychainTimeout):
				return nil, fmt.Errorf("el Keychain no respondió — firma la app con una identidad estable, o pasa la clave en LOQUI_AZURE_KEY para probar")
			default:
				return nil, fmt.Errorf("no se pudo leer la clave del Keychain: %w", err)
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
	default:
		return nil, fmt.Errorf("el motor %q todavía no está portado — elige Azure en Ajustes", settings.Provider)
	}
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
				d.ui.EmitLevel(frame.Level)
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

// envKeyOverride lets a development build supply an API key without the Keychain.
//
// WHY IT EXISTS. On an ad-hoc-signed build — which is every local build, since the
// signature changes each time — SecItemCopyMatching never returns: macOS wants to ask
// permission and cannot show the prompt. That makes the entire app untestable for a reason
// that has nothing to do with the code under test.
//
// So this is an escape hatch, not a feature: it is checked BEFORE the Keychain, keyed off
// an environment variable a packaged app will never have set, and every use is logged so it
// can never be mistaken for the real path. The real fix is a stable signing identity.
const envKeyOverride = "LOQUI_AZURE_KEY"

// keyReader returns the function the token service should use to fetch the key.
func (d *Dictation) keyReaderFor(slot store.KeySlot) func() (string, error) {
	if v := os.Getenv(envKeyOverride); v != "" && slot == store.SlotAzureSpeech {
		d.ui.Log("DEV", envKeyOverride+" is set — using it instead of the Keychain")
		return func() (string, error) { return v, nil }
	}
	return func() (string, error) { return store.GetKey(slot) }
}
