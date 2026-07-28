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

	"github.com/Juan-Motta/loqui-go/internal/assets"
	"github.com/Juan-Motta/loqui-go/internal/macos"
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
	app := application.New(application.Options{
		Name:        "Loqui",
		Description: "Dictado por voz que inserta el texto donde está el cursor",
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(frontend),
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

	wins.settings = newSettingsWindow(app)
	wins.overlay = newOverlayWindow(app)
	newTray(app)

	// Dev affordance: show the pill a moment after launch, so the non-activating
	// show path can be checked without a human clicking the tray — including the
	// part that matters most, that the frontmost app does NOT change.
	if os.Getenv("LOQUI_DEBUG_OVERLAY") == "1" {
		go func() {
			time.Sleep(2 * time.Second)
			showOverlay(app, wins.overlay)
			log.Println("debug: overlay shown via orderFrontRegardless")
		}()
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func newSettingsWindow(app *application.App) *application.WebviewWindow {
	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      "settings",
		Title:     "Loqui",
		URL:       "/settings.html",
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
		BackgroundType:    application.BackgroundTypeTransparent,
		Mac: application.MacWindow{
			// The pill draws its own rounded shadow; the OS window shadow would be
			// a square behind it.
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
	// TODO(port, phase 3): this becomes controller.Press() / RequestStop(). For now it
	// toggles the overlay alone, which is what exercises the non-activating show path.
	overlayShown := false
	menu.Add("Dictar (prueba)").OnClick(func(*application.Context) {
		overlayShown = !overlayShown
		if overlayShown {
			showOverlay(app, wins.overlay)
			tray.SetIcon(assets.TrayActive) // real red: a template image would tint it away
		} else {
			hideOverlay(wins.overlay)
			tray.SetTemplateIcon(assets.TrayTemplate)
		}
		log.Println("tray: overlay shown =", overlayShown)
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
