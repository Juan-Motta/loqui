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
	"strings"
	"sync/atomic"
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
func (u *ui) Log(tag, msg string) { logLine(tag, msg) }

// logLine is the one place the diagnostic format lives.
//
// Extracted because the settings service also needs to log — it reports which configuration a
// connection test used — and it is constructed in main before this ui exists, so it takes the
// function rather than the object. Two copies of the format would drift.
func logLine(tag, msg string) {
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

// settingsUI is the same ui the engine drives, kept package-level so a settings change made after
// startup can reach the logger and the listener. Set once, by startDictation.
var settingsUI *ui

// startDictation wires the store, the engine and the fn trigger together.
//
// The store arrives from main rather than being opened here: the settings service needs it at
// application-construction time, and both must be the SAME instance — two Stores over one
// directory each hold their own lock, so a settings write from the UI could interleave with
// one from the engine.
func startDictation(wailsApp *application.App, tray *application.SystemTray, st *store.Store, settingsSvc *app.SettingsService) error {
	u := &ui{wails: wailsApp, tray: tray}
	settingsUI = u
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
	// open to exist, and a failed credential write is the one thing on that page most likely to
	// go wrong on an unsigned build — it must not be invisible from the terminal.
	//
	// The UI sends the ACTION name, never the value: a key must not reach the log.
	wailsApp.Event.On("ui:action", func(e *application.CustomEvent) {
		u.Log("UI-ACTION", fmt.Sprintf("%v", e.Data))
	})
	wailsApp.Event.On("ui:painted", func(e *application.CustomEvent) {
		u.Log("UI-PAINT", fmt.Sprintf("view rendered: %v", e.Data))
	})
	// The connection test and the card probes. Same rule as UI-ACTION: the outcome and the state of
	// the card, never a credential.
	wailsApp.Event.On("ui:probe", func(e *application.CustomEvent) {
		u.Log("UI-PROBE", fmt.Sprintf("%v", e.Data))
	})
	wailsApp.Event.On("ui:conn-probe", func(e *application.CustomEvent) {
		u.Log("CONN-CLICK", fmt.Sprintf("%v", e.Data))
	})
	// What the page did with the catalogue: locale, how many strings it rewrote, and one sample.
	// Without it, "is the UI translated" is unanswerable from outside — every other report echoes
	// strings that come from Go rather than from the markup.
	wailsApp.Event.On("ui:lang-switch", func(e *application.CustomEvent) {
		u.Log("LANG-SWITCH", fmt.Sprintf("%v", e.Data))
	})
	// The model row's shape and verdict. No user data: a verdict, a class, and which buttons are up.
	wailsApp.Event.On("ui:model-row", func(e *application.CustomEvent) {
		u.Log("MODEL-ROW", fmt.Sprintf("%v", e.Data))
	})
	wailsApp.Event.On("ui:model-error", func(e *application.CustomEvent) {
		u.Log("MODEL-ERR", fmt.Sprintf("%v", e.Data))
	})
	wailsApp.Event.On("ui:i18n", func(e *application.CustomEvent) {
		u.Log("I18N", fmt.Sprintf("%v", e.Data))
	})
	wailsApp.Event.On("ui:conn-report", func(e *application.CustomEvent) {
		u.Log("CONN-CARD", fmt.Sprintf("%v", e.Data))
	})
	// What the page SENT for the key, as a classification: typed, empty, or masked-blocked. Never the
	// value — this line goes straight into the log.
	//
	// It exists because the mask's guard is otherwise unobservable: a run where the mask was correctly
	// withheld and a run where it was stored as the credential look identical from outside. Both leave
	// the slot reading "present" and the field showing the same mask.
	wailsApp.Event.On("ui:key-submitted", func(e *application.CustomEvent) {
		u.Log("KEY-SENT", fmt.Sprintf("%v", e.Data))
	})
	// A payload that arrived out of order and was dropped. Silent by design in the page — but if it
	// ever starts happening often, that is worth seeing rather than guessing at.
	wailsApp.Event.On("ui:stale-payload", func(e *application.CustomEvent) {
		u.Log("UI-STALE", fmt.Sprintf("%v", e.Data))
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
	// The CLASS NAMES the rendered rows use, so fidelity to the original markup is checkable.
	// Never carries transcript text — see reportShape in frontend/src/history.ts.
	wailsApp.Event.On("ui:perms", func(e *application.CustomEvent) {
		u.Log("PERMS", fmt.Sprintf("%v", e.Data))
	})

	wailsApp.Event.On("ui:system", func(e *application.CustomEvent) {
		u.Log("SYS", fmt.Sprintf("%v", e.Data))
	})

	// Says whether the appearance probe went through the real control or fell back to the binding —
	// with no Guardar button in Sistema, "it saved" only means something if the control's own
	// listener is what did it.
	wailsApp.Event.On("ui:system-probe", func(e *application.CustomEvent) {
		u.Log("SYS-PROBE", fmt.Sprintf("%v", e.Data))
	})

	// Reports the version label and how many rows landed — never the paths, which name the user's
	// home directory and have no business in a log.
	wailsApp.Event.On("ui:about", func(e *application.CustomEvent) {
		u.Log("ABOUT", fmt.Sprintf("%v", e.Data))
	})

	// The tutorial: which step is showing and how many controls actually landed in it. Counts, not
	// just "open": a wizard drawn with four empty panels would otherwise report success.
	wailsApp.Event.On("ui:wizard", func(e *application.CustomEvent) {
		u.Log("WIZARD", fmt.Sprintf("%v", e.Data))
	})

	// Every option the engine picker offers, with its label and disabled flag, plus the hint under it.
	// The picker was claiming engines were usable that the Conexiones list called unconfigured.
	wailsApp.Event.On("ui:engine-options", func(e *application.CustomEvent) {
		u.Log("ENGINE-OPTS", fmt.Sprintf("%v", e.Data))
	})

	// Opening a browser is invisible from the app's side, so the outcome is logged: this button spent
	// the whole port doing nothing and silence was indistinguishable from success.
	wailsApp.Event.On("ui:donate", func(e *application.CustomEvent) {
		u.Log("DONATE", fmt.Sprintf("%v", e.Data))
	})

	wailsApp.Event.On("ui:languages", func(e *application.CustomEvent) {
		u.Log("LANG", fmt.Sprintf("%v", e.Data))
	})

	// The Conexiones states, so the ported model is checkable from the log.
	wailsApp.Event.On("ui:connections", func(e *application.CustomEvent) {
		u.Log("CONN", fmt.Sprintf("%v", e.Data))
	})

	wailsApp.Event.On("ui:hist-shape", func(e *application.CustomEvent) {
		u.Log("HIST-SHAPE", fmt.Sprintf("%v", e.Data))
	})
	wailsApp.Event.On("ui:recent-shape", func(e *application.CustomEvent) {
		u.Log("RECENT-SHAPE", fmt.Sprintf("%v", e.Data))
	})

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

	// Dev affordance: change the interface language the way the user does.
	//
	// It exists because THE ONE THING THIS FEATURE COULD NOT PROVE was the live switch: a <select>
	// inside a Wails webview cannot be clicked from a script, so "the UI follows a language change"
	// rested on reading the code. This dispatches a real `change` on the real control, so the app's
	// own handler runs — the same rule the connection-card affordance follows.
	//
	// Delayed past the first paint on purpose: the point is to change the language of an interface
	// that is ALREADY drawn, which is the case that was never exercised.
	// Opts the history shape reports into reporting the rendered timestamp. Behind its own flag
	// because that is activity metadata — when the user dictated — and a verification aid must not
	// widen what an ordinary run writes to the log.
	if os.Getenv("LOQUI_DEBUG_TIME_TEXT") == "1" {
		wailsApp.Event.On("ui:ready", func(*application.CustomEvent) {
			wailsApp.Event.Emit("debug:time-text", "1")
		})
	}

	if lang := os.Getenv("LOQUI_DEBUG_SET_LANGUAGE"); lang != "" {
		go func() {
			time.Sleep(6 * time.Second)
			// "system" means the empty value — "Seguir el sistema" — which cannot be passed through an
			// environment variable as "" because that reads as unset.
			if lang == "system" {
				lang = ""
			}
			wailsApp.Event.Emit("debug:set-language", lang)
		}()
	}

	if a := os.Getenv("LOQUI_DEBUG_APPEARANCE"); a != "" {
		go func() {
			time.Sleep(4 * time.Second)
			wailsApp.Event.Emit("debug:set-appearance", a)
		}()
	}

	// The engine in use is checked once the page is up, and the page is told if it had to change.
	//
	// AFTER ui:painted, not before the windows exist. The reason used to be cost — the check read the
	// Keychain, which on this build took its full three seconds, and in front of the first paint that
	// is three seconds of nothing. Reading a file costs nothing, so what keeps the order now is the
	// other half: the outcome has somewhere to go. The page repaints from the payload and shows the
	// sentence, so the user learns their engine moved instead of finding out at the next dictation.
	// ONCE PER LAUNCH, and that is a limitation rather than a design. ui:painted is emitted from the
	// page's bootstrap (frontend/src/settings.ts), not from paint(), and the Settings window is created
	// once — closing it only hides it (newSettingsWindow) — so the webview never reloads and this fires
	// exactly once. Reopening Ajustes does not look again.
	//
	// What carries the weight instead is the retry inside EnsureUsableEngine: a check can legitimately
	// reach no conclusion (the user was mid-save while the payload was being built),
	// and with only one run there is no later paint to fall back on. Deleting the key of the engine in
	// use is the other moment it gets re-decided, inside DeleteKey itself.
	//
	// The flag is insurance, not load-bearing: it costs nothing and keeps a second paint — should the
	// page ever gain a reason to re-emit — from deciding off a payload this one is already acting on.
	var engineChecking atomic.Bool
	wailsApp.Event.On("ui:painted", func(*application.CustomEvent) {
		go func() {
			if !engineChecking.CompareAndSwap(false, true) {
				return
			}
			defer engineChecking.Store(false)

			res := settingsSvc.EnsureUsableEngine()
			switch {
			case res.Error != "":
				// Shown, not just logged: the engine is still unusable and the app has just failed to
				// do anything about it. Nobody reads the terminal of a packaged app.
				u.Log("ENGINE-CHECK", "no se pudo comprobar el motor: "+res.Error)
				failed := res
				failed.Notice = "No se pudo cambiar a un motor utilizable: " + res.Error
				wailsApp.Event.Emit("engine:blocked", failed)
			case res.Changed:
				u.Log("ENGINE-CHECK", res.Notice)
				// The WHOLE result travels, payload included. Sending the sentence alone made the page
				// fetch its own snapshot, and between the two the user can act: the page would then
				// paint the newer state and print a sentence describing the older one — macOS active
				// under the words "se cambió a Whisper".
				wailsApp.Event.Emit("engine:changed", res)
			case res.Notice != "":
				// Nothing moved, and the reason is worth saying — but not with a tick in front of it:
				// "no se pudo comprobar la clave" is not an accomplishment.
				u.Log("ENGINE-CHECK", res.Notice)
				wailsApp.Event.Emit("engine:blocked", res)
			}
		}()
	})

	// Dev affordance: drive the buttons of a connection card and report what the card looks like.
	//
	// Fired on ui:painted rather than after a fixed sleep, unlike the older hooks above. The page only
	// wires its handlers once Settings.Load() has resolved, and that call used to read the Keychain — which
	// on an ad-hoc-signed build can take the whole of the three seconds those hooks wait. A command
	// that arrives before the wiring is lost in silence, and the run then looks like a broken feature
	// rather than a mistimed probe.
	if steps := os.Getenv("LOQUI_DEBUG_CONN_CLICK"); steps != "" {
		wailsApp.Event.On("ui:painted", func(*application.CustomEvent) {
			// The ARGUMENTS are stripped before logging. The page only accepts fixed tokens for the
			// key field, but this line prints whatever the environment held — so a
			// `set-key:sk-live-…` typed by someone reaching for a shortcut would land in the log
			// before the page ever refused it. The actions are what makes the run readable; the
			// values are not worth the risk. The page reports back what it actually ran.
			u.Log("DEBUG", "asking the UI to run "+stepActions(steps))
			wailsApp.Event.Emit("debug:conn-click", steps)
		})
	}
	if provider := os.Getenv("LOQUI_DEBUG_CONN_REPORT"); provider != "" {
		wailsApp.Event.On("ui:painted", func(*application.CustomEvent) {
			// Delayed on purpose, unlike the click hook: this reports what the card SETTLED on, and
			// reading it in the same tick as the click would only ever capture the busy state.
			go func() {
				time.Sleep(6 * time.Second)
				wailsApp.Event.Emit("debug:conn-report", provider)
			}()
		})
	}

	// Dev affordance: ask the page to click a sidebar item and report what became visible.
	if view := os.Getenv("LOQUI_DEBUG_NAVIGATE"); view != "" {
		go func() {
			time.Sleep(3 * time.Second)
			wailsApp.Event.Emit("debug:navigate", view)
		}()
	}

	// Dev affordance: drive the tutorial without a mouse. "open" clicks the real footer button.
	if want := os.Getenv("LOQUI_DEBUG_WIZARD"); want != "" {
		go func() {
			time.Sleep(3 * time.Second)
			wailsApp.Event.Emit("debug:wizard", want)
		}()
	}

	// Dev affordance: click the real "Invítame un café" button. NOTE: this really does open a browser.
	if which := os.Getenv("LOQUI_DEBUG_DONATE"); which != "" {
		go func() {
			time.Sleep(3 * time.Second)
			wailsApp.Event.Emit("debug:donate", which)
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

// applyTriggerChange restarts the shortcut listener for a newly saved trigger.
//
// PERSISTING IS NOT ENOUGH. The fn listener is a CHILD PROCESS started at launch from the stored
// trigger, so without restarting it the new shortcut is saved while the old one keeps working —
// which is the most confusing possible outcome: the interface says one thing and the keyboard does
// another, with nothing to suggest they disagree.
func applyTriggerChange(u *ui, trigger string) error {
	// Stopped first and unconditionally: switching away from fn has to release the old listener even
	// though the new trigger starts nothing.
	if fnListener != nil {
		fnListener.Stop()
		fnListener = nil
	}
	if trigger == "fn" {
		return startFnListener(u)
	}
	if trigger != "" {
		// TODO(port, phase 4): ordinary accelerators. The stored format is Electron's
		// ("CommandOrControl+Shift+D") and there is no globalShortcut in Go, so this needs a mapping
		// plus either a hotkey library or an NSEvent monitor. Reported rather than swallowed: the
		// shortcut IS saved, and the user has to know it will not fire yet.
		return fmt.Errorf("los atajos que no son fn todavía no están implementados — usa el tray o “Probar dictado”")
	}
	u.Log("HOTKEY", "sin atajo configurado")
	return nil
}

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

// stepActions renders a debug step chain with every argument removed: "openai:set-key:xyz" becomes
// "openai:set-key:…". Only what was DONE survives, never what it was done with.
func stepActions(steps string) string {
	out := make([]string, 0, 4)
	for _, step := range strings.Split(steps, "+") {
		parts := strings.SplitN(step, ":", 3)
		if len(parts) < 3 {
			out = append(out, step) // no argument to hide
			continue
		}
		out = append(out, parts[0]+":"+parts[1]+":…")
	}
	return strings.Join(out, "+")
}
