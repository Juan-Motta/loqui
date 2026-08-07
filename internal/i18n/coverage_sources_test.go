package i18n

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// THE SCANS THAT CATCH WHAT THE FIRST ONES CANNOT.
//
// coverage_test.go only looks at strings that are ALREADY marked `data-i18n`. That leaves three ways
// for a string to stay Spanish for ever without a single test complaining, and the Spanish-source
// fallback makes every one of them look deliberate:
//
//   1. markup nobody marked,
//   2. a `t("…")` call in the page whose key is not in the catalogue,
//   3. a Go message handed to failed()/invalid()/probeFailed()/ok() with no entry.
//
// The Electron original learned the same lesson and grew the same guards
// (../loqui/test/unit/i18nCoverage.test.ts). Its catalogue is still alive because of them.

func read(t *testing.T, parts ...string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(parts...))
	if err != nil {
		t.Fatalf("reading %v: %v", parts, err)
	}
	return string(raw)
}

// Text that is prose but carries no marker: a sentence nobody can ever translate.
var textNode = regexp.MustCompile(`(?s)<(\w+)([^>]*?)>([^<>]+)</`)

// Elements whose content is never user-visible prose.
var notProse = regexp.MustCompile(`^(?:script|style|option)$`)

func TestEveryTranslatableTextNodeIsMarked(t *testing.T) {
	body := markup(t)
	var unmarked []string
	for _, m := range textNode.FindAllStringSubmatch(body, -1) {
		tag, attrs, text := m[1], m[2], strings.TrimSpace(whitespace.ReplaceAllString(m[3], " "))
		if notProse.MatchString(tag) || !translatable(text) {
			continue
		}
		if strings.Contains(attrs, "data-i18n") {
			continue
		}
		unmarked = append(unmarked, text)
	}
	sort.Strings(unmarked)
	if len(unmarked) > 0 {
		t.Errorf("%d text nodes are prose but carry no data-i18n — nothing can ever translate them:\n  %s",
			len(unmarked), strings.Join(unmarked, "\n  "))
	}
}

// Keys the PAGE looks up at runtime. These never appear in the markup, so the markup scan is blind
// to them — keyStateLabel, the busy lines, the button titles.
var tCall = regexp.MustCompile(`\bt\(\s*"((?:[^"\\]|\\.)*)"\s*\)`)

func TestEveryPageLookupHasAnEntry(t *testing.T) {
	dir := filepath.Join("..", "..", "frontend", "src")
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the page sources: %v", err)
	}
	var missing []string
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".ts") {
			continue
		}
		src := read(t, dir, f.Name())
		for _, m := range tCall.FindAllStringSubmatch(src, -1) {
			key := strings.ReplaceAll(m[1], `\"`, `"`)
			if translatable(key) && !covered(key) {
				missing = append(missing, f.Name()+": "+key)
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d t() keys have no English entry:\n  %s", len(missing), strings.Join(missing, "\n  "))
	}
}

// Messages the Go services hand to the user. The format string IS the key — see
// SettingsService.phrase — so an entry has to exist for the whole thing, verbs and all.
//
// IT HAS TO FOLLOW GO'S CONCATENATION, and missing that was a real defect rather than a nicety. A
// message written across two lines as `"…la clave que se usa " + "viene de ahí"` is ONE string by the
// time phrase() looks it up, but the first version of this regex captured only the first literal —
// so the catalogue held two fragments, every lookup missed, and the whole message reached the user in
// Spanish while this very test reported it as covered. Found by a cross-engine review.
var goMessage = regexp.MustCompile(
	`\b(?:failed|invalid|probeFailed|ok|revealFailed)\(\s*(?:"[^"]*",\s*)?((?:"(?:[^"\\]|\\.)*"\s*\+?\s*)+)`)

// joinLiterals turns Go's `"a " + "b"` into `a b` — the string the runtime will actually build.
var oneLiteral = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)

func joinLiterals(expr string) string {
	var b strings.Builder
	for _, m := range oneLiteral.FindAllStringSubmatch(expr, -1) {
		b.WriteString(strings.ReplaceAll(m[1], `\"`, `"`))
	}
	return b.String()
}

func TestEveryGoMessageHasAnEntry(t *testing.T) {
	var missing []string
	for _, name := range []string{"settings_write.go", "settings_probe.go", "settings_reveal.go"} {
		src := read(t, "..", "app", name)
		for _, m := range goMessage.FindAllStringSubmatch(src, -1) {
			key := joinLiterals(m[1])
			// Pure format wrappers like "%s (%s)" carry no words of their own.
			if !translatable(key) || covered(key) {
				continue
			}
			missing = append(missing, name+": "+key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d Go messages have no English entry — they reach the user in Spanish:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// ---- the widest scan, and the one that decides whether the rest rots -----------------
//
// Everything above only sees a string that someone ALREADY routed through i18n: a marked element, a
// t() call, a message handed to a known constructor. That is a guard against forgetting to translate
// something you remembered to route. It is blind to the actual failure mode of a half-finished
// migration — prose that was never routed at all.
//
// So this pair goes the other way round: it looks for Spanish PROSE anywhere in the page sources and
// in the Go services, and fails on anything that is not demonstrably handled. That is a stricter
// contract than "the catalogue is complete", and it is the reason the remaining gaps are findable
// instead of folklore.

// spanishProse is text with the shape of a sentence a person reads. Accented characters and the
// Spanish-only words that appear in this app's copy; ASCII-only English identifiers do not match.
// "no" is deliberately ABSENT from this list even though it is a Spanish word: it is also an English
// one, and the diagnostic strings in these files ("no such card", "no language select") are English.
// A guard that cries wolf on those gets muted, which is worse than one gap.
var spanishProse = regexp.MustCompile(
	`[áéíóúñ¿¡]|\b(?:de|la|el|los|las|una|para|con|sin|que|se|al|del|tu|tus|est[áa]|son|hay|Pega|Deja)\b`)

// exemptTS are the literals in the page sources that are not user copy: selectors, event names,
// class names, ids, log tags and the debug grammar. Listed as PREFIXES and exact strings, because
// the alternative — a clever heuristic — is a guard nobody can predict.
var exemptTSPrefix = []string{
	".", "#", "<", "data-", "ui:", "debug:", "settings:", "history:", "overlay:", "dictation:",
	"meter:", "engine:", "http", "/", "--", "%", "&", "id=", "class=",
}

func exemptLiteral(s string) bool {
	if !spanishProse.MatchString(s) {
		return false // no Spanish shape at all: nothing to translate
	}
	for _, p := range exemptTSPrefix {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	// A comment sentence that happens to sit inside a string is not copy; copy has no line breaks and
	// is short enough to fit a control.
	return strings.Contains(s, "\n") || len(s) > 400
}

// tsLiteral finds double-quoted literals. Template literals and single quotes are not used for copy
// in this codebase — checked — so the simple form is enough and stays readable.
var tsLiteral = regexp.MustCompile(`"((?:[^"\\\n]|\\.)*)"`)

// alreadyRouted matches a literal that is the argument of t(), setText() or i18n's own helpers.
var alreadyRouted = regexp.MustCompile(`\b(?:t|setText|tr)\(\s*(?:[A-Za-z_$][\w$]*\s*,\s*)?"((?:[^"\\]|\\.)*)"`)

// TestNoUnroutedSpanishInThePage is the scan the previous version of this file was missing.
func TestNoUnroutedSpanishInThePage(t *testing.T) {
	dir := filepath.Join("..", "..", "frontend", "src")
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the page sources: %v", err)
	}
	var loose []string
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".ts") || strings.HasSuffix(f.Name(), ".d.ts") {
			continue
		}
		src := read(t, dir, f.Name())
		routed := map[string]bool{}
		for _, m := range alreadyRouted.FindAllStringSubmatch(src, -1) {
			routed[m[1]] = true
		}
		for _, line := range strings.Split(src, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
				continue // comments are for the reader, not the user
			}
			for _, m := range tsLiteral.FindAllStringSubmatch(line, -1) {
				lit := m[1]
				if lit == "" || routed[lit] || exemptLiteral(lit) || !spanishProse.MatchString(lit) {
					continue
				}
				loose = append(loose, f.Name()+": "+lit)
			}
		}
	}
	sort.Strings(loose)
	if len(loose) > 0 {
		t.Errorf("%d Spanish literals in the page are not routed through t():\n  %s",
			len(loose), strings.Join(loose, "\n  "))
	}
}

// The same scan for the Go services that build things the user reads.
//
// Restricted to the files that OWN user copy rather than the whole tree: log tags, provider protocol
// strings and error text meant for the log are not translated, and a scan that swept them would be a
// guard nobody keeps. The list is explicit for the same reason the exempt prefixes above are.
var userFacingGoFiles = []string{
	"about.go", "about_service.go", "permission_rows.go", "provider_fallback.go",
}

// goRouted matches a literal already handed to a translator.
var goRouted = regexp.MustCompile(`\b(?:T|phrase|t)\(\s*[^,]*,\s*"((?:[^"\\]|\\.)*)"`)

func TestNoUnroutedSpanishInTheUserFacingServices(t *testing.T) {
	var loose []string
	for _, name := range userFacingGoFiles {
		src := read(t, "..", "app", name)
		routed := map[string]bool{}
		for _, m := range goRouted.FindAllStringSubmatch(src, -1) {
			routed[m[1]] = true
		}
		for _, line := range strings.Split(src, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			for _, m := range tsLiteral.FindAllStringSubmatch(line, -1) {
				lit := m[1]
				if lit == "" || routed[lit] || exemptLiteral(lit) || !spanishProse.MatchString(lit) {
					continue
				}
				if covered(lit) {
					continue // has an entry; the boundary pass will find it
				}
				loose = append(loose, name+": "+lit)
			}
		}
	}
	sort.Strings(loose)
	if len(loose) > 0 {
		t.Errorf("%d Spanish literals in the user-facing services are neither routed nor in the catalogue:\n  %s",
			len(loose), strings.Join(loose, "\n  "))
	}
}
