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
