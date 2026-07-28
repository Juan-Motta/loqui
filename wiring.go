// The window layer's side of the dictation engine: it satisfies app.UI, installs the fn
// trigger, and keeps the tray in step with what the session is doing.
//
// Kept out of internal/app deliberately — that package must not import Wails, so the
// dictation logic can be built and reasoned about without a GUI toolkit attached.
package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Juan-Motta/loqui-go/internal/app"
	"github.com/Juan-Motta/loqui-go/internal/assets"
	"github.com/Juan-Motta/loqui-go/internal/hotkey"
	"github.com/Juan-Motta/loqui-go/internal/inject"
	"github.com/Juan-Motta/loqui-go/internal/permissions"
	"github.com/Juan-Motta/loqui-go/internal/session"
	"github.com/Juan-Motta/loqui-go/internal/store"
)

// ui bridges the dictation engine to the windows and the tray.
type ui struct {
	wails *application.App
	tray  *application.SystemTray
}

func (u *ui) ShowOverlay() {
	if w := wins.overlay; w != nil {
		showOverlay(u.wails, w)
	}
	// The settings window animates its Home waveform off this.
	u.emit("dictation:state", true)
	u.setTrayActive(true)
}

func (u *ui) HideOverlay() {
	if w := wins.overlay; w != nil {
		hideOverlay(w)
	}
	u.emit("dictation:state", false)
	u.setTrayActive(false)
}

func (u *ui) EmitOverlay(state session.OverlayState) {
	u.emit("overlay:state", state)
}

func (u *ui) EmitLevel(level float64) {
	u.emit("meter:level", level)
}

func (u *ui) HistoryChanged() {
	u.emit("history:changed", nil)
}

// Log writes a diagnostic line. Transcript text is NEVER passed here — the log is what
// gets attached to a bug report.
func (u *ui) Log(tag, msg string) {
	log.Printf("%-14s %s", tag, msg)
}

func (u *ui) emit(name string, data any) {
	if u.wails == nil {
		return
	}
	u.wails.Event.Emit(name, data)
}

// setTrayActive swaps the menu-bar glyph. The active one is real red, which a template
// image cannot be — macOS would tint the red away.
func (u *ui) setTrayActive(active bool) {
	if u.tray == nil {
		return
	}
	if active {
		u.tray.SetIcon(assets.TrayActive)
	} else {
		u.tray.SetTemplateIcon(assets.TrayTemplate)
	}
}

// dictation is the running engine, built on ready.
var dictation *app.Dictation

// startDictation wires the store, the engine and the fn trigger together.
//
// The store arrives from main rather than being opened here: the settings service needs it at
// application-construction time, and both must be the SAME instance — two Stores over one
// directory each hold their own lock, so a settings write from the UI could interleave with
// one from the engine.
func startDictation(wailsApp *application.App, tray *application.SystemTray, st *store.Store) error {
	u := &ui{wails: wailsApp, tray: tray}
	dictation = app.NewDictation(st, u)

	// Log each window announcing itself. See frontend/src/settings.ts for why: a broken
	// asset server looks identical to a healthy one from here, until nothing reports in.
	wailsApp.Event.On("ui:ready", func(e *application.CustomEvent) {
		u.Log("UI", fmt.Sprintf("page loaded: %v", e.Data))
	})

	// The bootstrap round trip reports itself. Worth its own line: the service is registered
	// as a construction option and the binding is generated code, so a failure to register
	// shows up as nothing at all happening in the UI — with no error on either side.
	wailsApp.Event.On("ui:bootstrap", func(e *application.CustomEvent) {
		u.Log("BOOTSTRAP", fmt.Sprintf("payload delivered: %v", e.Data))
	})
	wailsApp.Event.On("ui:bootstrap-failed", func(e *application.CustomEvent) {
		u.Log("BOOTSTRAP-ERR", fmt.Sprintf("%v", e.Data))
	})

	// Every settings write reports its outcome here. The webview's own console needs devtools
	// open to exist, and a failed Keychain write is the one thing on that page most likely to
	// go wrong on an unsigned build — it must not be invisible from the terminal.
	//
	// The UI sends the ACTION name, never the value: a key must not reach the log.
	wailsApp.Event.On("ui:action", func(e *application.CustomEvent) {
		u.Log("UI-ACTION", fmt.Sprintf("%v", e.Data))
	})
	wailsApp.Event.On("ui:painted", func(e *application.CustomEvent) {
		u.Log("UI-PAINT", fmt.Sprintf("view rendered: %v", e.Data))
	})
	// Which view the user is on. Logged because navigation is the one thing whose absence made
	// everything else look broken: every control this port wired lives inside a view that could not
	// be reached until the sidebar was hooked up.
	wailsApp.Event.On("ui:view", func(e *application.CustomEvent) {
		u.Log("UI-VIEW", fmt.Sprintf("%v", e.Data))
	})
	wailsApp.Event.On("ui:tab", func(e *application.CustomEvent) {
		u.Log("UI-TAB", fmt.Sprintf("%v", e.Data))
	})
	// How many transcripts the Historial actually painted. Worth a line: a stored transcript that
	// never reaches the list is exactly the failure that made the history look lost.
	wailsApp.Event.On("ui:history", func(e *application.CustomEvent) {
		u.Log("UI-HIST", fmt.Sprintf("%v", e.Data))
	})

	// Dev affordance: ask the page to perform one real settings write.
	//
	// Same reason as LOQUI_DEBUG_DICTATE — the real trigger cannot be scripted. A <select> inside
	// a Wails webview cannot be clicked from a shell script, so without this the write half of the
	// settings loop could only ever be checked by hand.
	wailsApp.Event.On("ui:overlay-geometry", func(e *application.CustomEvent) {
		u.Log("OVERLAY-GEO", fmt.Sprintf("%v", e.Data))
	})

	wailsApp.Event.On("ui:nav-probe", func(e *application.CustomEvent) {
		u.Log("UI-NAV", fmt.Sprintf("%v", e.Data))
	})
	wailsApp.Event.On("ui:record-probe", func(e *application.CustomEvent) {
		u.Log("UI-REC", fmt.Sprintf("%v", e.Data))
	})

	// Dev affordance: ask the page to click its record button.
	if os.Getenv("LOQUI_DEBUG_RECORD_CLICK") == "1" {
		go func() {
			time.Sleep(3 * time.Second)
			wailsApp.Event.Emit("debug:record-click")
		}()
	}

	// Dev affordance: announce a stored transcript without dictating one.
	//
	// This is the case that made the history look lost — dictating with Historial already on screen —
	// and the live refresh depends on the page having subscribed to this event. Silence produces no
	// transcript, so a scripted dictation cannot exercise it.
	if os.Getenv("LOQUI_DEBUG_HISTORY_EVENT") == "1" {
		go func() {
			time.Sleep(6 * time.Second)
			u.Log("DEBUG", "announcing a history change")
			u.HistoryChanged()
		}()
	}

	// Dev affordance: ask the page to click a sidebar item and report what became visible.
	if view := os.Getenv("LOQUI_DEBUG_NAVIGATE"); view != "" {
		go func() {
			time.Sleep(3 * time.Second)
			wailsApp.Event.Emit("debug:navigate", view)
		}()
	}

	if provider := os.Getenv("LOQUI_DEBUG_SET_PROVIDER"); provider != "" {
		go func() {
			// After the page has loaded and wired its handlers.
			time.Sleep(3 * time.Second)
			u.Log("DEBUG", "asking the UI to set provider="+provider)
			wailsApp.Event.Emit("debug:exercise-write", provider)
		}()
	}

	settings := st.LoadSettings()
	u.Log("MAIN", fmt.Sprintf("ready — provider=%s mode=%s trigger=%s data=%s",
		settings.Provider, settings.Mode, settings.TriggerKey, st.Dir()))

	// Ask for the microphone BEFORE anything needs it.
	//
	// This is not politeness, it is load-bearing for the local engines: the native helpers
	// are the processes that open the device, and a child's access is attributed to the
	// responsible parent app. Without the grant the helper stops one step short of
	// reporting `started` and produces nothing, with no error in its own output — observed
	// exactly that with the Apple engine.
	go func() {
		askFor(u, "microphone", permissions.Microphone, permissions.RequestMicrophone)
		// The Apple on-device engine needs Speech Recognition TOO, and it is a separate
		// grant. Without it the helper stops one step short of reporting `started` and says
		// nothing about why.
		askFor(u, "speech recognition", permissions.SpeechRecognition, permissions.RequestSpeechRecognition)
	}()

	// Say so up front rather than at the first failed paste: without this grant the paste
	// keystroke is swallowed silently and the secure-field guard cannot read anything, so
	// dictation appears to work and produces nothing.
	if !inject.AccessibilityTrusted() {
		u.Log("PERM", "Accessibility NOT granted — paste will be silently ignored until it is")
	}

	// Dev affordance: drive a full dictation without a keypress.
	//
	// It exists because the fn trigger cannot be exercised from a script — it needs the
	// Input Monitoring grant on a binary whose ad-hoc signature changes every build — and
	// the tray item needs a human with a mouse. This runs the same controller path both of
	// them do, so the capture → provider → paste chain can actually be checked.
	if secs := os.Getenv("LOQUI_DEBUG_DICTATE"); secs != "" {
		go debugDictate(u, secs)
	}

	if settings.TriggerKey == "fn" {
		if err := startFnListener(u); err != nil {
			// Not fatal: the tray item and the settings window still start dictation.
			u.Log("HOTKEY", err.Error())
		}
	} else if settings.TriggerKey != "" {
		// TODO(port, phase 3): ordinary accelerators. The stored format is Electron's
		// ("CommandOrControl+Shift+D") and there is no globalShortcut in Go, so this
		// needs a mapping plus either a hotkey library or an NSEvent monitor.
		u.Log("HOTKEY", fmt.Sprintf("trigger %q not supported yet — use the tray", settings.TriggerKey))
	}
	return nil
}

var fnListener *hotkey.Listener

func startFnListener(u *ui) error {
	bin, err := globeListenerPath()
	if err != nil {
		return err
	}
	controller := dictation.Controller()

	// Our own paste synthesises Cmd+V. While fn is held, that keystroke trips the
	// helper's FN_INTERRUPTED — so without this window the app would cancel the very
	// dictation it just delivered.
	var suppressInterruptUntil time.Time

	fnListener, err = hotkey.Start(bin, hotkey.Handlers{
		OnFnDown: func() {
			suppressInterruptUntil = time.Now().Add(1500 * time.Millisecond)
			controller.Press()
		},
		OnFnUp: func() { controller.Release() },
		OnFnInterrupt: func() {
			if time.Now().Before(suppressInterruptUntil) {
				u.Log("FN", "interrupt ignored (our own paste keystroke)")
				return
			}
			controller.Interrupt()
		},
		OnStderr: func(s string) { u.Log("HOTKEY-ERR", s) },
		OnError:  func(err error) { u.Log("HOTKEY-ERR", err.Error()) },
		// FAIL CLOSED. If the listener dies mid-hold, nothing will ever report the key
		// coming up, so the microphone would stay open with no way to close it.
		OnExit: func(err error) {
			u.Log("HOTKEY", fmt.Sprintf("fn listener exited: %v", err))
			controller.HelperFailed()
		},
	})
	if err != nil {
		return err
	}
	u.Log("HOTKEY", "fn listener running (hold to dictate)")
	return nil
}

// globeListenerPath locates the fn listener. One implementation of the bundled-vs-dev
// lookup lives in internal/app; duplicating it here is how the two drift apart.
func globeListenerPath() (string, error) {
	if bin := app.HelperPath("globe-listener"); bin != "" {
		return bin, nil
	}
	return "", fmt.Errorf("fn listener not built — run scripts/build-globe-listener.sh")
}

// stopDictation tears everything down on quit. A global event tap or an open microphone
// left behind by a quitting app is a system-wide problem, not just ours.
func stopDictation() {
	if fnListener != nil {
		fnListener.Stop()
		fnListener = nil
	}
	if dictation != nil {
		dictation.Shutdown()
	}
}

// debugDictate runs one dictation for the given number of seconds, driving the controller
// exactly as the trigger key and the tray item do.
func debugDictate(u *ui, secs string) {
	n, err := strconv.Atoi(secs)
	if err != nil || n <= 0 {
		n = 5
	}
	c := dictation.Controller()

	time.Sleep(2 * time.Second) // let the windows finish loading
	u.Log("DEBUG", fmt.Sprintf("starting a %ds dictation (LOQUI_DEBUG_DICTATE)", n))
	c.Press()

	time.Sleep(time.Duration(n) * time.Second)
	u.Log("DEBUG", "ending the dictation")
	c.Release() // hold mode; in toggle this is a no-op and the press below ends it
	if c.Desired() {
		c.RequestStop()
	}
}

// askFor reports a permission's state and prompts when it has never been asked.
//
// The three outcomes are kept distinct on purpose. "Denied" is the user's decision and only
// they can undo it; "the prompt never appeared" is OUR problem — it means macOS would not
// present it, which on a dev build means the signature is not stable. Reporting the second as
// the first would send someone to System Settings to fix something that is not broken there.
func askFor(u *ui, name string, status func() permissions.Status, request func() (bool, bool)) {
	switch st := status(); st {
	case permissions.Granted:
		u.Log("PERM", name+": granted")
	case permissions.NotDetermined:
		u.Log("PERM", name+": asking…")
		granted, answered := request()
		switch {
		case !answered:
			u.Log("PERM-ERR", name+": the prompt never appeared — is the app signed with a stable identity?")
		case granted:
			u.Log("PERM", name+": granted")
		default:
			u.Log("PERM-ERR", name+": DENIED by the user")
		}
	default:
		u.Log("PERM-ERR", name+": "+string(st)+" — grant it in Ajustes de Sistema")
	}
}
