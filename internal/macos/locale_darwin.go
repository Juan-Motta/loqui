//go:build darwin

package macos

/*
#cgo CFLAGS: -x objective-c -fmodules -fobjc-arc
#cgo LDFLAGS: -framework Foundation
#import <Foundation/Foundation.h>

// The user's current locale identifier, e.g. "es_CO".
//
// WHY CGO. A GUI app launched from Finder inherits no LANG/LC_ALL — those are set by a shell, and
// there is no shell in that path — so reading the environment reports nothing on exactly the launch
// the user cares about. NSLocale is where macOS actually keeps this.
static const char *loqui_system_locale(void) {
	NSString *ident = [[NSLocale currentLocale] localeIdentifier];
	if (ident == nil) { return NULL; }
	return [ident UTF8String];
}
*/
import "C"

import "strings"

// SystemLocale is the OS locale as a BCP-47-ish tag ("es-CO"), or "" when it cannot be read.
//
// Normalised to hyphens because every other locale in this app is written that way — the language
// catalogue, the store's validation, and what the transcription APIs expect. Leaking Apple's
// underscore form into the view would make Acerca de disagree with the Idioma screens.
func SystemLocale() string {
	c := C.loqui_system_locale()
	if c == nil {
		return ""
	}
	return strings.ReplaceAll(strings.TrimSpace(C.GoString(c)), "_", "-")
}
