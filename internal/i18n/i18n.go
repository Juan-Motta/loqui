// The interface language.
//
// KEYS ARE THE SPANISH SOURCE STRINGS. Ported from the Electron original's decision, and it is the
// one that shapes everything else here: the UI was authored in Spanish, so using the copy itself as
// the key means there is no invented key namespace to keep in sync, a missing translation degrades
// to READABLE SPANISH rather than leaking "ui.settings.header" into the interface, and the diff that
// introduces i18n stays reviewable.
//
// The price is real and is paid elsewhere: editing a Spanish string breaks its key silently. That is
// what the coverage test exists for — it scans the actual markup and fails on a marked string with
// no entry, so "missing" cannot quietly become permanent. Without it this package rots.
//
// STATELESS ON PURPOSE, unlike the original, which kept a module-level `current` locale and a
// setLocale(). Here the locale is a parameter. Two reasons: the settings payload is built
// concurrently (bootstrap.go fans out over the key slots), so a package-level mutable locale would
// be a data race waiting to happen; and a pure function is testable without arranging global state,
// which is the property that makes the store's rule functions worth having.
package i18n

import "strings"

// Locale is a language this app can present itself in.
type Locale string

const (
	Spanish Locale = "es"
	English Locale = "en"
)

// Default is the language the UI is authored in, and therefore the one a missing translation falls
// back to.
const Default = Spanish

// available are the locales with a finished catalogue, and so the only ones that may be selected.
//
// The original declares pt/fr/it/de as intended and deliberately does not offer them until they are
// translated and reviewed — a half-translated UI is worse than one honest language. Same rule here:
// this list is what is FINISHED, not what is planned.
var available = map[Locale]bool{Spanish: true, English: true}

// Available reports whether a locale may be selected.
func Available(l Locale) bool { return available[l] }

// ResolveLocale decides the interface language from what is stored plus what the OS reports.
//
// Empty storage means "follow the OS", which is the default: a fresh install speaks the user's
// language without being asked, and keeps following if they later change the system language. An
// explicit stored choice always wins. An OS language with no catalogue falls back to the authored
// language rather than showing half a translation.
func ResolveLocale(stored string, systemLocale string) Locale {
	if l := Locale(stored); Available(l) {
		return l
	}
	// The OS reports "es_CO" or "en-GB"; only the base language decides.
	base := strings.ToLower(systemLocale)
	if i := strings.IndexAny(base, "-_"); i >= 0 {
		base = base[:i]
	}
	if l := Locale(base); Available(l) {
		return l
	}
	return Default
}

// catalogs holds the translations away from the authored language. Spanish needs no table: the key
// is already Spanish.
var catalogs = map[Locale]map[string]string{English: englishCatalog}

// T translates a Spanish source string.
//
// Resolution order: the requested locale, then English, then THE KEY ITSELF. That last step is the
// whole safety property — an untranslated string still reads as ordinary prose instead of leaving a
// hole in the interface.
func T(locale Locale, key string, params map[string]string) string {
	if locale == Spanish {
		return interpolate(key, params)
	}
	if hit, ok := catalogs[locale][key]; ok {
		return interpolate(hit, params)
	}
	if hit, ok := catalogs[English][key]; ok {
		return interpolate(hit, params)
	}
	return interpolate(key, params)
}

// Catalog is the table for one locale, for handing to the page so it can translate the markup.
//
// A COPY, not the live map: the page's binding must not be able to hand callers something that
// mutating would corrupt every later lookup. Spanish gets an empty table, which is correct rather
// than missing — there is nothing to translate when the key is already the answer.
func Catalog(locale Locale) map[string]string {
	out := make(map[string]string, len(catalogs[locale]))
	for k, v := range catalogs[locale] {
		out[k] = v
	}
	return out
}

// interpolate replaces {name} with the matching param.
//
// An UNKNOWN placeholder is left exactly as it was. Blanking it would turn a typo in a format string
// into a mysterious gap in a sentence; leaving "{provider}" visible turns it into a bug report.
func interpolate(s string, params map[string]string) string {
	if len(params) == 0 || !strings.ContainsRune(s, '{') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for {
		open := strings.IndexByte(s, '{')
		if open < 0 {
			b.WriteString(s)
			return b.String()
		}
		close := strings.IndexByte(s[open:], '}')
		if close < 0 {
			b.WriteString(s)
			return b.String()
		}
		close += open
		name := s[open+1 : close]
		b.WriteString(s[:open])
		if value, ok := params[name]; ok {
			b.WriteString(value)
		} else {
			b.WriteString(s[open : close+1])
		}
		s = s[close+1:]
	}
}
