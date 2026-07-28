//go:build darwin

// Pre-paste focus guard. Ported from the Electron build's src/main/focusGuard.ts, with the
// AppleScript replaced by a direct AX read.
//
// Right before injecting, two things can make the paste unsafe or plain wrong:
//
//  1. the focused element is a SECURE text field (a password box) — never paste dictation
//     into it, and never keep it in history either;
//  2. the frontmost app is no longer the one that was frontmost when dictation started —
//     focus drifted, so the text would land in the wrong window.
//
// WHY NOT osascript, WHICH IS WHAT ELECTRON USED: it spawns a process and asks System
// Events, so it needs the Apple Events permission on top of Accessibility, costs tens of
// milliseconds, and sits directly in the paste path. The AX API answers the same question
// in-process, using the Accessibility grant the paste already requires.
//
// FAILURE POSTURE: fail SAFE toward usability, exactly as the original did. An unreadable
// app name cannot prove a mismatch, and an unreadable subrole is not treated as secure —
// that is the same behaviour as having no guard at all, rather than refusing to paste.
// Which means secure-field protection depends on the Accessibility grant, and that is
// documented in onboarding.
package inject

/*
#cgo CFLAGS: -x objective-c -fmodules -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework ApplicationServices
#import <Cocoa/Cocoa.h>
#import <ApplicationServices/ApplicationServices.h>

// Bundle id of the frontmost app, or NULL. Caller frees.
static char *loqui_frontmost_bundle_id(void) {
	NSRunningApplication *app = [[NSWorkspace sharedWorkspace] frontmostApplication];
	if (app == nil) { return NULL; }
	NSString *bid = [app bundleIdentifier];
	if (bid == nil) { return NULL; }
	return strdup([bid UTF8String]);
}

// AXSubrole of the focused element of the frontmost app, or NULL when it cannot be read.
// Walking from the SYSTEM-WIDE element is what makes this work across apps: asking a
// specific app's element would require knowing which one to ask first.
static char *loqui_focused_subrole(void) {
	if (!AXIsProcessTrusted()) { return NULL; }

	AXUIElementRef system = AXUIElementCreateSystemWide();
	if (system == NULL) { return NULL; }

	CFTypeRef focused = NULL;
	AXError err = AXUIElementCopyAttributeValue(system, kAXFocusedUIElementAttribute, &focused);
	CFRelease(system);
	if (err != kAXErrorSuccess || focused == NULL) { return NULL; }

	CFTypeRef subrole = NULL;
	err = AXUIElementCopyAttributeValue((AXUIElementRef)focused, kAXSubroleAttribute, &subrole);
	CFRelease(focused);
	if (err != kAXErrorSuccess || subrole == NULL) { return NULL; }

	char *out = NULL;
	if (CFGetTypeID(subrole) == CFStringGetTypeID()) {
		NSString *s = (__bridge NSString *)subrole;
		out = strdup([s UTF8String]);
	}
	CFRelease(subrole);
	return out;
}
*/
import "C"

import "unsafe"

// secureSubrole is the AX subrole macOS gives password fields.
const secureSubrole = "AXSecureTextField"

// FocusState is what the guard reads just before pasting.
type FocusState struct {
	// App is the frontmost app's bundle id, or "" when it could not be read.
	App string
	// SecureField is true only when the focused element is PROVABLY a password field.
	SecureField bool
}

// ReadFocusState reads the current focus. Never fails: an unreadable state comes back as
// the permissive one, which is the same posture as having no guard.
func ReadFocusState() FocusState {
	var st FocusState

	if bid := C.loqui_frontmost_bundle_id(); bid != nil {
		st.App = C.GoString(bid)
		C.free(unsafe.Pointer(bid))
	}
	if sub := C.loqui_focused_subrole(); sub != nil {
		st.SecureField = C.GoString(sub) == secureSubrole
		C.free(unsafe.Pointer(sub))
	}
	return st
}
