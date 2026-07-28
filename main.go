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
	"log"
	"os"
	"runtime"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/Juan-Motta/loqui-go/internal/app"
	"github.com/Juan-Motta/loqui-go/internal/assets"
	"github.com/Juan-Motta/loqui-go/internal/macos"
	"github.com/Juan-Motta/loqui-go/internal/store"
)

//go:embed all:frontend/dist
var frontend embed.FS

// Overlay geometry, carried over from the Electron windowOptions: the pill is 176px
// wide at most, inside a slightly larger transparent window so its own drop shadow
// has room and is not clipped square at the window edge.
const (
	overlayWidth  = 216
	overlayHeight = 60
	overlayMargin = 16 // gap from the bottom of the work area
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

	wailsApp := application.New(application.Options{
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
			application.NewService(app.NewSettingsService(st)),
			application.NewService(app.NewHistoryService(st)),
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
	if err := startDictation(wailsApp, tray, st); err != nil {
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

	menu := app.Menu.New()
	// The same toggle the trigger key performs, for when no shortcut is configured or
	// the fn listener could not start.
	menu.Add("Dictar (prueba)").OnClick(func(*application.Context) {
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
	menu.Add("Ajustes…").OnClick(func(*application.Context) {
		if w := wins.settings; w != nil {
			w.Show()
			w.Focus()
		}
	})
	menu.AddSeparator()
	menu.Add("Salir").OnClick(func(*application.Context) {
		app.Quit()
	})
	tray.SetMenu(menu)
	return tray
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
