//go:build darwin

// AppKit calls Wails v3 does not expose, reached through the NSWindow pointer that
// (*application.WebviewWindow).NativeWindow() hands out.
//
// WHY THIS FILE HAS TO EXIST. The overlay is a presence pill that appears while the
// user dictates INTO ANOTHER APP. Electron showed it with `showInactive()` on a
// window created `focusable: false`; Wails' Show() has no such variant and goes
// through makeKeyAndOrderFront, which activates Loqui.
//
// Stealing focus does not merely look wrong here — it breaks dictation outright:
//   - the keystrokes we synthesise for paste would land in Loqui, not in the app the
//     user was typing in;
//   - focusGuard compares the frontmost app at paste time against the one captured
//     when dictation started, so Loqui coming forward makes every paste look like
//     "focus drifted" and get skipped.
//
// orderFrontRegardless orders the window in front WITHOUT making it key and without
// activating the app, which is exactly the Electron behaviour.
package macos

/*
#cgo CFLAGS: -x objective-c -fmodules -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

// Show without taking focus, and keep the pill above full-screen apps and on every
// Space — a dictation overlay that only appears on one desktop is worse than none.
static void loqui_show_inactive(void *nsWindow) {
	NSWindow *win = (__bridge NSWindow *)nsWindow;
	if (win == nil) { return; }
	dispatch_async(dispatch_get_main_queue(), ^{
		[win setLevel:NSStatusWindowLevel];
		[win setCollectionBehavior:NSWindowCollectionBehaviorCanJoinAllSpaces
		                          | NSWindowCollectionBehaviorStationary
		                          | NSWindowCollectionBehaviorIgnoresCycle
		                          | NSWindowCollectionBehaviorFullScreenAuxiliary];
		[win setHidesOnDeactivate:NO];
		[win orderFrontRegardless];
	});
}

static void loqui_order_out(void *nsWindow) {
	NSWindow *win = (__bridge NSWindow *)nsWindow;
	if (win == nil) { return; }
	dispatch_async(dispatch_get_main_queue(), ^{
		[win orderOut:nil];
	});
}

// Read back what the window actually is, not what it was asked to be.
static void loqui_window_opacity(void *nsWindow, int *isOpaque, double *alpha) {
	NSWindow *win = (__bridge NSWindow *)nsWindow;
	if (win == nil) { *isOpaque = 1; *alpha = 1.0; return; }
	*isOpaque = [win isOpaque] ? 1 : 0;
	NSColor *bg = [win backgroundColor];
	*alpha = (bg == nil) ? 1.0 : [bg alphaComponent];
}

// What a window and the app REALLY are at this instant.
//
// "It opens minimised" and "it opens behind the terminal" look identical to a user and have
// different causes, so guessing between them from a screenshot is how time gets wasted. These are
// the four flags that tell them apart.
static void loqui_window_state(void *nsWindow, int *visible, int *miniaturized, int *key, int *appActive) {
	NSWindow *win = (__bridge NSWindow *)nsWindow;
	*appActive = [NSApp isActive] ? 1 : 0;
	if (win == nil) { *visible = 0; *miniaturized = 0; *key = 0; return; }
	*visible = [win isVisible] ? 1 : 0;
	*miniaturized = [win isMiniaturized] ? 1 : 0;
	*key = [win isKeyWindow] ? 1 : 0;
}

// Bring Loqui to the front at LAUNCH.
//
// Launched through LaunchServices (Finder, Dock, `open`) macOS activates the app for us. Launched as
// a plain process — a terminal, a task runner — it does NOT: the shell that spawned it stays
// frontmost and Loqui's window sits behind it, which reads as "it opened minimised" even though the
// window is on screen and not miniaturised at all.
//
// ONLY FOR STARTUP. Activating Loqui at any other moment breaks dictation, for the reasons at the top
// of this file: paste would land in Loqui and focusGuard would treat every paste as drifted focus.
// The overlay must keep using ShowInactive.
static void loqui_activate(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		[NSApp activateIgnoringOtherApps:YES];
	});
}

// Cocoa's mouse location, in Cocoa coordinates (origin bottom-left of the main
// screen). The overlay is placed on whichever display the cursor is on, because
// that is where the user is looking and typing.
static void loqui_cursor(double *x, double *y) {
	NSPoint p = [NSEvent mouseLocation];
	*x = p.x;
	*y = p.y;
}
*/
import "C"

import "unsafe"

// ShowInactive orders the window in front without activating the app or making the
// window key. The Electron equivalent is BrowserWindow.showInactive().
func ShowInactive(nsWindow unsafe.Pointer) {
	C.loqui_show_inactive(nsWindow)
}

// OrderOut hides the window without touching the app's activation state.
func OrderOut(nsWindow unsafe.Pointer) {
	C.loqui_order_out(nsWindow)
}

// CursorPosition reports the mouse location in Cocoa screen coordinates
// (origin at the bottom-left of the main display).
func CursorPosition() (x, y float64) {
	var cx, cy C.double
	C.loqui_cursor(&cx, &cy)
	return float64(cx), float64(cy)
}

// WindowOpacity reports whether the window is opaque and the alpha of its background colour.
//
// It exists because a misconfigured window looks correct from Go: the options say
// "transparent", and only the pixels disagree. A transparent overlay must report
// opaque=false and alpha=0; anything else means the pill is sitting on a solid rectangle.
func WindowOpacity(nsWindow unsafe.Pointer) (opaque bool, backgroundAlpha float64) {
	var isOpaque C.int
	var alpha C.double
	C.loqui_window_opacity(nsWindow, &isOpaque, &alpha)
	return isOpaque == 1, float64(alpha)
}

// WindowState reports what the window and the app actually are right now.
//
// Distinguishes the two failures that look the same to a user: a MINIMISED window (miniaturized) and
// a window that is on screen but behind whatever launched it (visible, app not active). The fix for
// each is different, so the first job is to tell them apart.
func WindowState(nsWindow unsafe.Pointer) (visible, miniaturized, key, appActive bool) {
	var v, m, k, a C.int
	C.loqui_window_state(nsWindow, &v, &m, &k, &a)
	return v == 1, m == 1, k == 1, a == 1
}

// ActivateApp brings Loqui to the front. Call it ONCE, at startup, to make a launch from a terminal
// behave like a launch from Finder — never during dictation, where activating Loqui breaks paste.
func ActivateApp() {
	C.loqui_activate()
}
