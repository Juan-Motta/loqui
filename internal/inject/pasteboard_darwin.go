//go:build darwin

// NSPasteboard and synthetic keystrokes, reached directly instead of through a shell.
//
// WHY THIS IS BETTER THAN WHAT ELECTRON COULD DO. Loqui inserts text by putting it on the
// clipboard, pressing Cmd+V, and putting the user's clipboard back. The hard part is that
// last step: restoring blindly can destroy something the user copied during the paste
// window, and NOT restoring loses whatever they had before.
//
// Electron had no access to NSPasteboard.changeCount, so injection.ts approximated it:
// it embedded a unique invisible token in an HTML flavour and only restored if that exact
// HTML was still there. Its own header admits the residual — "custom/file-list flavors
// aren't preserved, and there is a tiny TOCTOU window — a fully lossless guarantee needs
// native NSPasteboard.changeCount". The PRD asked for it (R6) and it stayed unmet.
//
// Here we have it. changeCount is a monotonic counter that macOS bumps on EVERY write by
// ANY process, so "did anyone touch the clipboard since we wrote?" is an exact question
// with an exact answer. And the snapshot walks every item and every type, so a rich
// clipboard — files, custom flavours, multiple items — comes back as it was instead of
// being flattened to text.
package inject

/*
#cgo CFLAGS: -x objective-c -fmodules -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework CoreGraphics -framework ApplicationServices
#import <Cocoa/Cocoa.h>
#import <CoreGraphics/CoreGraphics.h>

// A snapshot is every item with every one of its types, retained across the cgo boundary
// as an opaque pointer. Copying the DATA (not just the types) is what makes the restore
// faithful: NSPasteboardItem contents are only guaranteed while they are on the board.
static void *loqui_pb_snapshot(long *changeCount) {
	NSPasteboard *pb = [NSPasteboard generalPasteboard];
	*changeCount = (long)[pb changeCount];

	NSMutableArray *saved = [NSMutableArray array];
	for (NSPasteboardItem *item in [pb pasteboardItems]) {
		NSMutableDictionary *copy = [NSMutableDictionary dictionary];
		for (NSPasteboardType type in [item types]) {
			NSData *data = [item dataForType:type];
			if (data != nil) { copy[type] = data; }
		}
		if ([copy count] > 0) { [saved addObject:copy]; }
	}
	return (void *)CFBridgingRetain(saved);
}

static void loqui_pb_release(void *handle) {
	if (handle != NULL) { CFBridgingRelease(handle); }
}

// Write our text and report the resulting changeCount — the value the restore decision
// compares against later.
static long loqui_pb_write_text(const char *utf8) {
	NSPasteboard *pb = [NSPasteboard generalPasteboard];
	NSString *text = [NSString stringWithUTF8String:utf8];
	[pb clearContents];
	[pb setString:(text == nil ? @"" : text) forType:NSPasteboardTypeString];
	return (long)[pb changeCount];
}

// Read the plain-text flavour back, for tests and diagnostics. Caller frees.
static char *loqui_pb_read_text(void) {
	NSString *s = [[NSPasteboard generalPasteboard] stringForType:NSPasteboardTypeString];
	if (s == nil) { return NULL; }
	return strdup([s UTF8String]);
}

static long loqui_pb_change_count(void) {
	return (long)[[NSPasteboard generalPasteboard] changeCount];
}

// Put the snapshot back, item by item and type by type.
static void loqui_pb_restore(void *handle) {
	if (handle == NULL) { return; }
	NSArray *saved = (__bridge NSArray *)handle;
	NSPasteboard *pb = [NSPasteboard generalPasteboard];
	[pb clearContents];

	NSMutableArray *items = [NSMutableArray array];
	for (NSDictionary *copy in saved) {
		NSPasteboardItem *item = [[NSPasteboardItem alloc] init];
		for (NSPasteboardType type in [copy allKeys]) {
			[item setData:copy[type] forType:type];
		}
		[items addObject:item];
	}
	if ([items count] > 0) { [pb writeObjects:items]; }
}

// Cmd+V into whatever has focus. CGEventPost rather than AppleScript: no process to
// spawn, no Apple Events permission, and it is the same mechanism the OS itself uses.
// Requires the Accessibility grant, which the paste needs regardless.
#define LOQUI_KEY_V 9
static void loqui_send_paste(void) {
	CGEventSourceRef src = CGEventSourceCreate(kCGEventSourceStateCombinedSessionState);
	CGEventRef down = CGEventCreateKeyboardEvent(src, LOQUI_KEY_V, true);
	CGEventRef up   = CGEventCreateKeyboardEvent(src, LOQUI_KEY_V, false);
	CGEventSetFlags(down, kCGEventFlagMaskCommand);
	CGEventSetFlags(up, kCGEventFlagMaskCommand);
	CGEventPost(kCGHIDEventTap, down);
	CGEventPost(kCGHIDEventTap, up);
	if (down) CFRelease(down);
	if (up) CFRelease(up);
	if (src) CFRelease(src);
}

// Whether this process is a trusted Accessibility client. Without the grant the paste
// keystroke is silently swallowed, so the app has to be able to SAY so rather than
// producing transcripts that go nowhere.
static bool loqui_ax_trusted(void) {
	return AXIsProcessTrusted();
}
*/
import "C"

import (
	"errors"
	"strings"
	"unsafe"
)

// pasteboardSnapshot is a retained copy of the clipboard, plus the change count it had.
type pasteboardSnapshot struct {
	handle      unsafe.Pointer
	changeCount int64
}

func snapshotPasteboard() pasteboardSnapshot {
	var cc C.long
	h := C.loqui_pb_snapshot(&cc)
	return pasteboardSnapshot{handle: h, changeCount: int64(cc)}
}

func (s pasteboardSnapshot) release() {
	if s.handle != nil {
		C.loqui_pb_release(s.handle)
	}
}

func (s pasteboardSnapshot) restore() {
	if s.handle != nil {
		C.loqui_pb_restore(s.handle)
	}
}

// writeText puts text on the clipboard and returns the change count it produced.
func writeText(text string) int64 {
	c := C.CString(text)
	defer C.free(unsafe.Pointer(c))
	return int64(C.loqui_pb_write_text(c))
}

func currentChangeCount() int64 {
	return int64(C.loqui_pb_change_count())
}

// readText returns the clipboard's plain-text flavour, "" when there is none.
func readText() string {
	out := C.loqui_pb_read_text()
	if out == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(out))
	return C.GoString(out)
}

func sendPasteKeystroke() {
	C.loqui_send_paste()
}

// AccessibilityTrusted reports whether the process may post synthetic keystrokes and read
// the focused element. Both the paste and the secure-field guard depend on it.
func AccessibilityTrusted() bool {
	return bool(C.loqui_ax_trusted())
}

// CopyText puts text on the clipboard and leaves it there.
//
// DELIBERATELY NOT THE INJECTION PATH. Injection writes the clipboard, presses Cmd+V and then
// RESTORES what the user had, because the clipboard was only ever a transport. A copy is the
// opposite: the whole point is that the text stays, so nothing is saved or put back.
//
// The change count is discarded for the same reason — it exists so the injection guard can tell
// whether the user copied something during the paste window, and there is no such window here.
func CopyText(text string) error {
	if strings.TrimSpace(text) == "" {
		// Refused rather than silently clearing the clipboard: a copy button that wipes what the
		// user had, because the row it belonged to happened to be empty, is worse than doing nothing.
		return errors.New("inject: nothing to copy")
	}
	writeText(text)
	return nil
}
