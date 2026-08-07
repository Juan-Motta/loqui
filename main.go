// loqui — Wails v3 entry point. The Go counterpart of the Electron main process
// (loqui/src/main/main.ts): it owns the windows, the menu-bar tray, the single
// instance lock and — as the port progresses — the session controller, audio capture,
// the STT providers, the hotkey listener and text injection.
//
// TWO WINDOWS, NOT THREE. Electron needed a hidden third renderer (`engine`) to host
// the Azure Speech JS SDK and getUserMedia. Here Go captures the audio and drives
// every provider, so only the two USER-FACING windows survive the port:
//   - settings: the app shell (Inicio / Historial / Ajustes / About)
//   - overlay:  a non-activating presence pill, shown while dictating into other apps
//
// See docs/plans/loqui-go-port.md for the module-by-module map, and
// docs/research/2026-07-27-azure-speech-go-macos.md for why the engine window went away.
package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/Juan-Motta/loqui-go/internal/app"
	"github.com/Juan-Motta/loqui-go/internal/assets"
	"github.com/Juan-Motta/loqui-go/internal/i18n"
	"github.com/Juan-Motta/loqui-go/internal/macos"
	"github.com/Juan-Motta/loqui-go/internal/session"
	"github.com/Juan-Motta/loqui-go/internal/store"
)

//go:embed all:frontend/dist
var frontend embed.FS

// Overlay geometry, carried over from the Electron windowOptions.
//
// THE WINDOW IS BIGGER THAN THE PILL ON PURPOSE, and the amount matters. The pill draws its own
// rounded drop shadow (the OS window shadow is disabled, because that one would be a square behind a
// rounded pill), and `body { overflow: hidden }` clips anything that does not fit.
//
// Measured: the pill lays out at 64x28 centred in the window, and its shadow — `0 4px 14px` — reaches
// 18px below it (14 blur + 4 offset) and 14px above. At the old height of 60 the bottom of the shadow
// landed at 62 and was CUT SQUARE by the window edge, which is exactly what read as "a transparent
// container behind the pill": a straight horizontal line where a soft shadow should have faded out.
// Being clipped on one side only, it also made the pill look like it sat too high inside that shape.
//
// So the height is the pill plus the shadow's larger reach on both sides — 28 + 2x18 = 64 — with a
// little slack. The width follows the same rule against the pill's WIDEST state: it grows to
// max-width 176px when a status word is shown ("reconectando…"), so 176 + 2x18 = 212 fits inside 216.
const (
	overlayWidth  = 216
	overlayHeight = 68
	// overlayMargin is the gap from the work area to the WINDOW, so it absorbs the height change
	// above: 12 + (68-28)/2 puts the pill exactly where 16 + (60-28)/2 did. The pill does not move
	// on screen; only its shadow stops being cut off.
	overlayMargin = 12
)

type windows struct {
	settings *application.WebviewWindow
	overlay  *application.WebviewWindow
}

// Package-level so the SingleInstance callback, which is declared before the windows
// exist, can reach them once they do.
var wins windows

func main() {
	// The store is opened HERE, before the application, because the settings service is
	// registered as a construction option and needs it. The dictation engine then shares
	// this one instance: two Stores over the same directory would each keep their own lock
	// and could interleave writes to settings.json.
	st, err := store.New()
	if err != nil {
		log.Fatal("cannot open the data directory: ", err)
	}
	// The tray is built from this same instance — see the note above about not opening a second
	// Store. Assigned here because the tray is constructed later and has nowhere else to read the
	// interface language from.
	trayStore = st

	// Declared before New() rather than with `:=`, because one of the services below needs to reach
	// the app itself — the browser opener for the donation link. The closure runs when the user
	// clicks, long after this assignment, so capturing the variable is enough; referring to it inside
	// its own initialiser would not compile.
	// The settings service is held in a variable, not built inline in the Services list, because the
	// launch check that moves the app off an unusable engine needs the SAME instance the page talks to
	// — a second one over the same store would work, but two services of one kind is an invitation to
	// wire the next thing to the wrong one.
	var settingsSvc *app.SettingsService

	var wailsApp *application.App
	wailsApp = application.New(application.Options{
		Name:        "Loqui",
		Description: "Dictado por voz que inserta el texto donde está el cursor",
		// Wails logs every binding call's ARGUMENTS at debug level, and one of this app's bound
		// methods takes an API key. See logging.go.
		Logger: newWailsLogger(),
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(frontend),
		},
		// The Ajustes page renders from one call into this service. Until it existed there
		// was no way to configure the app from the interface at all — settings.json had to
		// be hand-edited and keys passed through env vars.
		Services: []application.Service{
			// The hooks connect the RUNNING engine and listener. Passed at construction rather than
			// through setters because Wails binds every exported method of a service to the webview.
			application.NewService(newSettingsService(st, &settingsSvc, app.LiveHooks{
				ModeChanged: func(mode string) {
					if dictation != nil {
						dictation.Controller().SetMode(session.Mode(mode))
					}
				},
				TriggerChanged: func(trigger string) error {
					if settingsUI == nil {
						return nil
					}
					return applyTriggerChange(settingsUI, trigger)
				},
				LanguageChanged: func(locale string) {
					logLine("LANG-HOOK", "interface language now "+locale)
					// The settings page retranslates itself; these two cannot. The overlay is a
					// separate window created once, and the tray menu is native — neither is reached
					// by the page's own pass.
					if a := wailsApp; a != nil {
						a.Event.Emit("settings:language", locale)
					}
					relabelTray(i18n.Locale(locale))
				},
				AppearanceChanged: func(appearance string) {
					// Both windows: the pill is transparent, but its own appearance still decides how
					// system-drawn chrome inside it resolves.
					for _, w := range []*application.WebviewWindow{wins.settings, wins.overlay} {
						if w != nil {
							macos.SetWindowAppearance(w.NativeWindow(), appearance)
						}
					}
				},
				// The settings service logs which configuration a connection test used — the region and
				// where the key came from, never the credential. Passed as a function because the ui that
				// owns the log format is built later, in wiring.
				Log: logLine,
			})),
			application.NewService(app.NewHistoryService(st)),
			application.NewService(app.NewClipboardService()),
			application.NewService(app.NewPermissionsService(func() i18n.Locale {
				// Read on EVERY call rather than captured: the user can change language with the
				// Permisos tab open, and "Volver a comprobar" rebuilds these rows from Go.
				return i18n.ResolveLocale(st.LoadSettings().AppLanguage, macos.SystemLocale())
			})),
			// Read-only: Acerca de reports the build and the machine, and takes the store only to
			// name the files it writes.
			application.NewService(app.NewAboutService(st)),
			// The opener is injected so internal/app stays free of Wails. The URL itself lives in the
			// service, not the page: a named action cannot be pointed at another site.
			application.NewService(app.NewLinksService(func(url string) error {
				return wailsApp.Browser.OpenURL(url)
			})),
			// The engine does not exist yet — it needs the windows and the tray this very call
			// creates — so the service resolves it lazily. By the time the page can call it,
			// startDictation has run.
			//
			// The nil check must return a nil INTERFACE, not a nil *Controller: the latter would be a
			// non-nil interface holding a nil pointer, and the service's guard would not catch it.
			application.NewService(app.NewDictationService(func() app.DictationControl {
				if dictation == nil {
					return nil
				}
				return dictation.Controller()
			})),
		},
		// Exactly one Loqui may run. A second instance would install its OWN fn
		// listener and its OWN recognizer, so one dictation gets transcribed twice,
		// pasted twice and written to history twice — and the two copies don't even
		// match, because they are two independent recognizers. Closing the window
		// only hides it to the tray, which makes "launch it again" an easy mistake.
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.jualopezmo.loquigo",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				// Surface the running app instead of silently doing nothing.
				if w := wins.settings; w != nil {
					w.Show()
					w.Focus()
				}
			},
		},
		Mac: application.MacOptions{
			// Loqui keeps a Dock icon (LSUIElement=false, as in electron-builder.yml):
			// it is a menu-bar app that also has a real settings window.
			ActivationPolicy: application.ActivationPolicyRegular,
			// A background/menu-bar app: closing the last window must not quit it.
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})

	wins.settings = newSettingsWindow(wailsApp)
	wins.overlay = newOverlayWindow(wailsApp)
	tray := newTray(wailsApp)

	// The engine needs the windows and the tray to already exist, since it drives both.
	if err := startDictation(wailsApp, tray, st, settingsSvc); err != nil {
		log.Fatal("cannot start the dictation engine: ", err)
	}
	defer stopDictation()

	// Dev affordance: show the pill a moment after launch, so the non-activating
	// show path can be checked without a human clicking the tray — including the
	// part that matters most, that the frontmost app does NOT change.
	if os.Getenv("LOQUI_DEBUG_OVERLAY") == "1" {
		go func() {
			time.Sleep(2 * time.Second)
			showOverlay(wailsApp, wins.overlay)
			// Assert the window really is transparent rather than trusting the options.
			// The white-rectangle bug looked exactly like a correctly configured window from
			// the Go side; only the pixels disagreed.
			opaque, alpha := macos.WindowOpacity(wins.overlay.NativeWindow())
			log.Printf("debug: overlay shown — opaque=%v backgroundAlpha=%.2f (want false / 0.00)", opaque, alpha)
		}()
	}

	// Come to the front on launch.
	//
	// MEASURED, not assumed: launched with `open` (Finder, Dock) the window came up
	// visible=true miniaturized=false key=true appActive=true — already correct. Launched as a plain
	// process from a terminal it came up visible=true miniaturized=false key=false appActive=false:
	// on screen, but behind the shell that started it, which is what "it opens minimised" actually
	// was. LaunchServices activates an app for us; a bare exec does not.
	//
	// Deliberately once and only here. Activating Loqui mid-dictation would send the synthesised
	// paste into Loqui and make focusGuard discard every transcript — see internal/macos.ActivateApp.
	if runtime.GOOS == "darwin" {
		macos.ActivateApp()
	}

	// Says what the window IS a few seconds in, for when this regresses: "minimised" and "behind the
	// terminal" look identical to a user and have different fixes. Gated like the other probes.
	if os.Getenv("LOQUI_DEBUG_WINDOW") == "1" {
		go func() {
			time.Sleep(3 * time.Second)
			if w := wins.settings; w != nil {
				visible, mini, key, active := macos.WindowState(w.NativeWindow())
				log.Printf("WIN-STATE: settings visible=%v miniaturized=%v key=%v appActive=%v",
					visible, mini, key, active)
			}
		}()
	}

	if err := wailsApp.Run(); err != nil {
		log.Fatal(err)
	}
}

func newSettingsWindow(app *application.App) *application.WebviewWindow {
	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      "settings",
		Title:     "Loqui",
		URL:       "/", // index.html — see frontend/vite.config.ts
		Width:     900,
		Height:    640,
		MinWidth:  740,
		MinHeight: 520,
		// The renderer needs no capabilities of its own: audio capture lives in Go.
		// Denying explicitly means a compromised page cannot even ask.
		Permissions: map[application.PermissionType]application.Permission{
			application.PermissionMicrophone:    application.PermissionDeny,
			application.PermissionCamera:        application.PermissionDeny,
			application.PermissionGeolocation:   application.PermissionDeny,
			application.PermissionNotifications: application.PermissionDeny,
		},
		Mac: application.MacWindow{
			// Inset traffic lights over a translucent sidebar — the same chrome the
			// Electron build asks for with titleBarStyle:"hiddenInset" + vibrancy.
			TitleBar: application.MacTitleBarHiddenInset,
			Backdrop: application.MacBackdropTranslucent,
			// Honour the stored appearance, which the Electron build applied through
			// nativeTheme.themeSource. Without it the window always followed the system and
			// a user who had chosen light or dark silently got neither.
			//
			// It matters more here than it looks: the translucent backdrop is an
			// NSVisualEffectView, so the appearance decides whether the material renders
			// light or dark. Leave it unset and the CSS can be in dark mode while the
			// window chrome behind it is light.
			Appearance: macAppearance(appearanceSetting()),
		},
		BackgroundType: application.BackgroundTypeTransparent,
	})

	// Closing Settings HIDES it; the window stays alive so the tray can bring it
	// back and so the session machinery it hosts is never torn down mid-dictation.
	win.OnWindowEvent(events.Common.WindowClosing, func(e *application.WindowEvent) {
		e.Cancel()
		win.Hide()
	})
	return win
}

func newOverlayWindow(app *application.App) *application.WebviewWindow {
	return app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:          "overlay",
		URL:           "/overlay.html",
		Width:         overlayWidth,
		Height:        overlayHeight,
		Frameless:     true,
		AlwaysOnTop:   true,
		Hidden:        true,
		DisableResize: true,
		// The pill is pure status: clicks must reach the app underneath it.
		IgnoreMouseEvents: true,
		// BackgroundType is the CROSS-PLATFORM knob and it does nothing on macOS —
		// `BackgroundType` appears zero times in Wails' darwin window code. Setting only
		// this is what left the pill floating inside an opaque white rounded rectangle:
		// the window kept its default background while the page drew a transparent body.
		// Kept for the eventual Windows build; Mac.Backdrop below is what actually applies.
		BackgroundType: application.BackgroundTypeTransparent,
		Mac: application.MacWindow{
			// THIS is what makes the window and the webview transparent on macOS, so only
			// the pill itself is visible over whatever is behind it.
			Backdrop: application.MacBackdropTransparent,
			// The pill draws its own rounded shadow; the OS window shadow would be a
			// square behind it.
			DisableShadow: true,
			WindowLevel:   application.MacWindowLevelStatus,
		},
	})
}

// showOverlay puts the pill on screen WITHOUT activating Loqui. Going through
// AppKit directly is deliberate — see internal/macos/window_darwin.go for why
// Wails' Show() cannot be used here.
func showOverlay(app *application.App, win *application.WebviewWindow) {
	positionOverlay(app, win)
	if runtime.GOOS == "darwin" {
		macos.ShowInactive(win.NativeWindow())
		return
	}
	win.Show()
}

func hideOverlay(win *application.WebviewWindow) {
	if runtime.GOOS == "darwin" {
		macos.OrderOut(win.NativeWindow())
		return
	}
	win.Hide()
}

// positionOverlay places the pill bottom-centre of the primary display's work area.
// Best-effort: a failure here must never block dictation, so it falls back to leaving
// the window where it was and never returns an error.
//
// TODO(port, phase 3): follow the display the CURSOR is on, like the Electron build
// does (screen.getDisplayNearestPoint(screen.getCursorScreenPoint())). The AppKit
// cursor read is already in place — macos.CursorPosition.
func positionOverlay(app *application.App, win *application.WebviewWindow) {
	target := app.Screen.GetPrimary()
	if target == nil {
		return
	}
	area := target.WorkArea
	x := area.X + (area.Width-overlayWidth)/2
	y := area.Y + area.Height - overlayHeight - overlayMargin
	win.SetPosition(x, y)
}

func newTray(app *application.App) *application.SystemTray {
	tray := app.SystemTray.New()
	tray.SetTemplateIcon(assets.TrayTemplate)
	trayRef, trayApp = tray, app
	trayLocaleShown = trayLocale()
	trayMenu = buildTrayMenu(app, trayLocaleShown)
	tray.SetMenu(trayMenu)
	return tray
}

// buildTrayMenu constructs the menu in one language. Separate from newTray so a language change can
// rebuild it — see relabelTray for why rebuilding is the only option here.
func buildTrayMenu(app *application.App, locale i18n.Locale) *application.Menu {
	menu := app.Menu.New()
	// The same toggle the trigger key performs, for when no shortcut is configured or
	// the fn listener could not start.
	menu.Add(i18n.T(locale, "Dictar (prueba)", nil)).OnClick(func(*application.Context) {
		if dictation == nil {
			return
		}
		c := dictation.Controller()
		if c.Desired() {
			c.RequestStop()
		} else {
			c.Press()
		}
	})
	menu.Add(i18n.T(locale, "Ajustes…", nil)).OnClick(func(*application.Context) {
		if w := wins.settings; w != nil {
			w.Show()
			w.Focus()
		}
	})
	menu.AddSeparator()
	menu.Add(i18n.T(locale, "Salir", nil)).OnClick(func(*application.Context) {
		app.Quit()
	})
	return menu
}

// appearanceSetting reads the stored appearance without needing the whole engine, because
// the window is built before the dictation store is opened.
func appearanceSetting() string {
	st, err := store.New()
	if err != nil {
		return "system"
	}
	return st.LoadSettings().Appearance
}

// macAppearance maps Loqui's setting to a Cocoa appearance. "system" (and anything
// unrecognised) means follow the OS, which is the default and a real choice the user can go
// back to — not merely the absence of one.
func macAppearance(setting string) application.MacAppearanceType {
	switch setting {
	case "light":
		return application.NSAppearanceNameAqua
	case "dark":
		return application.NSAppearanceNameDarkAqua
	default:
		return application.DefaultAppearance
	}
}

// newSettingsService builds the service and hands the same pointer back through out.
//
// It exists so the instance registered with Wails and the one the launch check uses are provably the
// one instance, without splitting the hooks literal away from the Services list where it reads.
func newSettingsService(st *store.Store, out **app.SettingsService, hooks app.LiveHooks) *app.SettingsService {
	svc := app.NewSettingsService(st, hooks)
	*out = svc
	return svc
}

// trayRef and trayApp let a language change rebuild the menu after startup.
var (
	trayRef *application.SystemTray
	trayApp *application.App
	// trayMenu is the menu currently installed, kept so it can be freed when replaced.
	trayMenu *application.Menu
	// trayLocaleShown is the language the installed menu is in, so an unchanged language rebuilds
	// nothing.
	trayLocaleShown i18n.Locale
)

// trayStore is where the tray reads the language from. Package-level because the tray is built
// before anything else has a reference to hand it, and it is only ever read.
var trayStore *store.Store

func trayLocale() i18n.Locale {
	if trayStore == nil {
		return i18n.Default
	}
	return i18n.ResolveLocale(trayStore.LoadSettings().AppLanguage, macos.SystemLocale())
}

// relabelTray rebuilds the menu in a new language.
//
// THE TRAY IS NATIVE AND BUILT ONCE, so unlike the settings page it cannot retranslate itself from a
// catalogue: the labels were baked into NSMenuItems at startup. Rebuilding the whole menu is the
// bluntest fix and the only one this Wails version offers — SystemTray has no per-item SetLabel.
//
// DECLARED LIMIT: the rebuild replaces the menu wholesale, so any state it held is reset. It holds
// none today (three stateless actions), and this comment is here for whoever adds a checkbox to it.
func relabelTray(locale i18n.Locale) {
	if trayRef == nil || trayApp == nil {
		logLine("TRAY", "relabel skipped: no tray yet")
		return
	}
	// SKIPPED when the language did not actually move. Every save of the Sistema panel used to arrive
	// here, so choosing the same language twice — or saving any other setting that reports a locale —
	// rebuilt a native menu for nothing.
	if trayLocaleShown == locale {
		return
	}
	previous := trayMenu
	trayMenu = buildTrayMenu(trayApp, locale)
	trayLocaleShown = locale
	trayRef.SetMenu(trayMenu)
	// DESTROYED AFTER the replacement is installed, never before: the old menu is what the tray is
	// showing until SetMenu returns, and freeing an NSMenu that is still attached is how a native
	// crash gets introduced.
	//
	// Without this the items stay rooted in Wails' global callback map and the native menu is never
	// released, so every language switch leaked a menu and three handlers. Found by a cross-engine
	// review, which checked the lifecycle against the pinned Wails source; `Menu.Destroy` walks its
	// items and drops them, which is exactly what was missing.
	if previous != nil {
		previous.Destroy()
	}
	// LOGGED because a native menu is the one surface no page report can reach: NSMenuItem titles are
	// not in any DOM, so without this line "the tray followed the language" is unverifiable from
	// outside. The labels are UI copy, never user data.
	logLine("TRAY", fmt.Sprintf("relabelled locale=%s items=%s|%s|%s", locale,
		i18n.T(locale, "Dictar (prueba)", nil),
		i18n.T(locale, "Ajustes…", nil),
		i18n.T(locale, "Salir", nil)))
}
