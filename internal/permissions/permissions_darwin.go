//go:build darwin

// The macOS privacy grants Loqui depends on. Ported from the Electron build's
// src/main/main.ts permission handlers (systemPreferences.getMediaAccessStatus /
// askForMediaAccess / isTrustedAccessibilityClient) and src/shared/permissions.ts.
//
// WHY EACH ONE MATTERS, because the failure modes are all silent:
//
//   - Microphone: without it there is no audio. Worse, the NATIVE HELPERS are the ones that
//     open the device, and a child process's access is attributed to the responsible parent
//     app — so the helper just hangs or dies with nothing to show, and the dictation produces
//     no transcript for a reason that never appears in its own output. Observed exactly that:
//     the Apple helper reached "started" when run from a terminal that had the grant, and
//     stopped one step short of it when spawned from an app that did not.
//   - Accessibility: without it the synthetic Cmd+V is swallowed. Dictation transcribes
//     correctly and nothing appears at the cursor.
//   - Input Monitoring: without it the fn key is never seen. There is no API to read this
//     one, so it is inferred from whether fn events have actually arrived.
package permissions

/*
#cgo CFLAGS: -x objective-c -fmodules -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework AVFoundation -framework ApplicationServices -framework Speech
#import <Cocoa/Cocoa.h>
#import <AVFoundation/AVFoundation.h>
#import <Speech/Speech.h>
#import <ApplicationServices/ApplicationServices.h>

// 0 notDetermined, 1 restricted, 2 denied, 3 authorized — AVAuthorizationStatus.
static int loqui_mic_status(void) {
	return (int)[AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeAudio];
}

// Ask for microphone access. Returns 1 if granted.
//
// This BLOCKS until the user answers, which is why the Go side runs it off the main path with
// a timeout: the prompt can only appear for an app macOS considers presentable, and when it
// cannot appear the completion handler is never called. That is the same shape as the
// Keychain hang, and the same cause — an unrecognised signature.
static int loqui_mic_request(void) {
	dispatch_semaphore_t sem = dispatch_semaphore_create(0);
	__block BOOL result = NO;
	[AVCaptureDevice requestAccessForMediaType:AVMediaTypeAudio completionHandler:^(BOOL granted) {
		result = granted;
		dispatch_semaphore_signal(sem);
	}];
	dispatch_semaphore_wait(sem, DISPATCH_TIME_FOREVER);
	return result ? 1 : 0;
}

// Speech Recognition is a SEPARATE grant from the microphone, and the Apple engine needs
// both: SFSpeechRecognizer.requestAuthorization gates SpeechTranscriber. Asking from here
// matters for the same reason the microphone does — the helper is a child process, so its own
// request is attributed to this app and cannot usefully prompt.
//
// 0 notDetermined, 1 denied, 2 restricted, 3 authorized — SFSpeechRecognizerAuthorizationStatus.
static int loqui_speech_status(void) {
	return (int)[SFSpeechRecognizer authorizationStatus];
}

static int loqui_speech_request(void) {
	dispatch_semaphore_t sem = dispatch_semaphore_create(0);
	__block BOOL result = NO;
	[SFSpeechRecognizer requestAuthorization:^(SFSpeechRecognizerAuthorizationStatus status) {
		result = (status == SFSpeechRecognizerAuthorizationStatusAuthorized);
		dispatch_semaphore_signal(sem);
	}];
	dispatch_semaphore_wait(sem, DISPATCH_TIME_FOREVER);
	return result ? 1 : 0;
}

static int loqui_accessibility_trusted(void) {
	return AXIsProcessTrusted() ? 1 : 0;
}

// Open a specific pane of System Settings' Privacy & Security section.
static void loqui_open_settings(const char *anchor) {
	NSString *a = [NSString stringWithUTF8String:anchor];
	NSString *urlStr = [NSString stringWithFormat:@"x-apple.systempreferences:com.apple.preference.security?%@", a];
	[[NSWorkspace sharedWorkspace] openURL:[NSURL URLWithString:urlStr]];
}
*/
import "C"

import (
	"time"
	"unsafe"
)

// Status is the state of one grant. Unknown means "cannot be read", which the UI must show
// as unverified rather than guessing — the Electron build learned that claiming "granted"
// made a denied microphone look fine while every dictation died.
type Status string

const (
	Granted       Status = "granted"
	Denied        Status = "denied"
	NotDetermined Status = "not-determined"
	Unknown       Status = "unknown"
)

// Microphone reports the current microphone grant.
func Microphone() Status {
	switch int(C.loqui_mic_status()) {
	case 0:
		return NotDetermined
	case 3:
		return Granted
	case 1, 2:
		return Denied
	default:
		return Unknown
	}
}

// Accessibility reports whether this process may post synthetic keystrokes and read the
// focused element — what the paste and the secure-field guard both need.
func Accessibility() bool {
	return C.loqui_accessibility_trusted() == 1
}

// requestTimeout bounds the microphone prompt.
//
// The prompt blocks until answered, and when macOS cannot present it — an unrecognised
// signature, no GUI session — the completion handler never fires. Without a bound this would
// hang the caller for ever, which is the failure already seen with the Keychain.
const requestTimeout = 60 * time.Second

// RequestMicrophone asks for microphone access, returning whether it was granted and whether
// the prompt was answered at all.
//
// Call it at startup, before anything needs the microphone: the NATIVE HELPERS cannot trigger
// this prompt usefully on their own, since a child process's request is attributed to this
// app. Asking here is what makes the local engines work at all.
func RequestMicrophone() (granted bool, answered bool) {
	ch := make(chan bool, 1) // buffered: the cgo call cannot be cancelled
	go func() { ch <- C.loqui_mic_request() == 1 }()

	select {
	case ok := <-ch:
		return ok, true
	case <-time.After(requestTimeout):
		return false, false
	}
}

// SpeechRecognition reports the current Speech Recognition grant, which the Apple on-device
// engine needs in addition to the microphone.
func SpeechRecognition() Status {
	switch int(C.loqui_speech_status()) {
	case 0:
		return NotDetermined
	case 3:
		return Granted
	case 1, 2:
		return Denied
	default:
		return Unknown
	}
}

// RequestSpeechRecognition asks for Speech Recognition access. Bounded for the same reason as
// the microphone prompt.
func RequestSpeechRecognition() (granted bool, answered bool) {
	ch := make(chan bool, 1)
	go func() { ch <- C.loqui_speech_request() == 1 }()

	select {
	case ok := <-ch:
		return ok, true
	case <-time.After(requestTimeout):
		return false, false
	}
}

// Pane names the Privacy & Security section to open.
type Pane string

const (
	PaneMicrophone      Pane = "Privacy_Microphone"
	PaneAccessibility   Pane = "Privacy_Accessibility"
	PaneInputMonitoring Pane = "Privacy_ListenEvent"
	PaneSpeech          Pane = "Privacy_SpeechRecognition"
)

// OpenSettings reveals the pane where the user grants a permission. Guiding them there beats
// describing a path through a settings app that Apple rearranges every release.
func OpenSettings(pane Pane) {
	c := C.CString(string(pane))
	defer C.free(unsafe.Pointer(c))
	C.loqui_open_settings(c)
}
