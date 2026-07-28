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
	"path/filepath"
	"strconv"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Juan-Motta/loqui-go/internal/app"
	"github.com/Juan-Motta/loqui-go/internal/assets"
	"github.com/Juan-Motta/loqui-go/internal/hotkey"
	"github.com/Juan-Motta/loqui-go/internal/inject"
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
func startDictation(wailsApp *application.App, tray *application.SystemTray) error {
	st, err := store.New()
	if err != nil {
		return err
	}
	u := &ui{wails: wailsApp, tray: tray}
	dictation = app.NewDictation(st, u)

	settings := st.LoadSettings()
	u.Log("MAIN", fmt.Sprintf("ready — provider=%s mode=%s trigger=%s data=%s",
		settings.Provider, settings.Mode, settings.TriggerKey, st.Dir()))

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

// globeListenerPath finds the compiled helper: inside the bundle for a packaged app, in
// helpers/bin during development.
func globeListenerPath() (string, error) {
	exe, err := os.Executable()
	if err == nil {
		// bin/loqui.app/Contents/MacOS/loqui -> Contents/Resources/helpers/globe-listener
		bundled := filepath.Join(filepath.Dir(exe), "..", "Resources", "helpers", "globe-listener")
		if _, err := os.Stat(bundled); err == nil {
			return bundled, nil
		}
	}
	dev := filepath.Join("helpers", "bin", "globe-listener")
	if _, err := os.Stat(dev); err == nil {
		return dev, nil
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
