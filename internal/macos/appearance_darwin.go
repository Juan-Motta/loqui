//go:build darwin

package macos

/*
#cgo CFLAGS: -x objective-c -fmodules -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

// Set a window's appearance, or clear it so the window follows the system.
//
// WHY CGO. Wails applies MacOptions.Appearance ONCE, when the window is built, and exposes no way to
// change it afterwards — `windowSetAppearanceTypeByName` is internal to its darwin file. Without
// this, choosing "Oscuro" in Ajustes would be written to disk and take effect at the next launch:
// the user clicks, nothing changes, and the setting looks broken.
//
// A NIL appearance is not the same as a light one: it means "inherit", which is what "Sistema" has
// to do so the window follows the OS switching at dusk.
static void loqui_set_appearance(void *nsWindow, const char *name) {
	NSWindow *win = (__bridge NSWindow *)nsWindow;
	if (win == nil) { return; }
	NSString *wanted = (name == NULL || name[0] == '\0')
		? nil
		: [NSString stringWithUTF8String:name];
	dispatch_async(dispatch_get_main_queue(), ^{
		[win setAppearance:(wanted == nil ? nil : [NSAppearance appearanceNamed:wanted])];
	});
}
*/
import "C"

import (
	"unsafe"
)

// SetWindowAppearance applies a light/dark preference to a live window.
//
// The setting values are the app's own ("system", "light", "dark") rather than AppKit's names, so the
// mapping lives in one place instead of being repeated at every call site.
//
// It is asynchronous on the main queue, like the other AppKit calls here: a window property set from
// a Wails binding goroutine would otherwise be a UIKit-style threading violation.
func SetWindowAppearance(nsWindow unsafe.Pointer, appearance string) {
	var name string
	switch appearance {
	case "light":
		name = "NSAppearanceNameAqua"
	case "dark":
		name = "NSAppearanceNameDarkAqua"
	default:
		// "system" and anything unrecognised: clear it, so the window follows the OS. Guessing light
		// here would freeze the window out of the dusk switch.
		name = ""
	}
	c := C.CString(name)
	defer C.free(unsafe.Pointer(c))
	C.loqui_set_appearance(nsWindow, c)
}
