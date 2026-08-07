package i18n

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// THIS IS THE TEST THAT KEEPS THE CATALOGUE ALIVE.
//
// Without it i18n rots silently and in one direction: someone adds a Spanish string to the markup,
// nobody adds a translation, and English users see a stray Spanish sentence that no other test in
// this repo would ever notice. It is the same guard the Electron original carries
// (../loqui/test/unit/i18nCoverage.test.ts), and the reason its catalogue survived the port at all.
//
// It scans the REAL markup, not a fixture. A fixture would pass forever while the app drifted.

// neverTranslated are the marked strings that must stay exactly as they are: locale codes, product
// names, identifiers and the ellipsis placeholders.
var neverTranslated = regexp.MustCompile(
	`^(?:[a-z]{2}-[A-Z]{2}|fn|Loqui|OpenAI|ElevenLabs|Whisper|macOS|Azure|Grok|gpt-[\w.-]+|sk-…|xai-…|…)$`)

var hasLetters = regexp.MustCompile(`[A-Za-zÁÉÍÓÚáéíóúñÑ¿¡]`)

// markedText pulls every `data-i18n` element's text out of the settings markup.
//
// Deliberately the same crude regex the original uses rather than a parser: it only has to match the
// shape this hand-written markup actually has, and a dependency-free test is one that still runs in
// five years.
var markedElement = regexp.MustCompile(`(?s)<(\w+)([^>]*?\bdata-i18n\b[^>]*?)>([^<>]*)</`)

var attrMarked = regexp.MustCompile(`<[^>]*\bdata-i18n-attr="([^"]+)"[^>]*>`)

var whitespace = regexp.MustCompile(`\s+`)

func markup(t *testing.T) string {
	t.Helper()
	// Relative to this package, so it works from `go test ./...` wherever it is invoked.
	raw, err := os.ReadFile(filepath.Join("..", "..", "frontend", "index.html"))
	if err != nil {
		t.Fatalf("reading the settings markup: %v", err)
	}
	body := string(raw)
	// The <style> block holds CSS content that looks like text nodes.
	if i := strings.Index(body, "</style>"); i >= 0 {
		body = body[i:]
	}
	return body
}

// formatVerb strips printf placeholders before asking whether a string contains words. Without it
// "%s" counts as prose, because it contains the letter s.
var formatVerb = regexp.MustCompile(`%[#+\-0-9.]*[a-zA-Z]`)

func translatable(s string) bool {
	s = strings.TrimSpace(whitespace.ReplaceAllString(s, " "))
	if s == "" || neverTranslated.MatchString(s) {
		return false
	}
	return hasLetters.MatchString(formatVerb.ReplaceAllString(s, ""))
}

func covered(s string) bool {
	_, inCatalog := englishCatalog[s]
	return inCatalog || sameInEnglish[s]
}

// Every marked string must have an English entry, or be declared as intentionally identical.
func TestEveryMarkedStringHasAnEnglishEntry(t *testing.T) {
	var missing []string
	for _, m := range markedElement.FindAllStringSubmatch(markup(t), -1) {
		text := strings.TrimSpace(whitespace.ReplaceAllString(m[3], " "))
		if !translatable(text) || covered(text) {
			continue
		}
		missing = append(missing, text)
	}
	if len(missing) > 0 {
		t.Errorf("%d marked strings have no English entry — they would stay Spanish:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// The same, for the attributes the markup marks (placeholders, titles).
func TestEveryMarkedAttributeHasAnEnglishEntry(t *testing.T) {
	body := markup(t)
	var missing []string
	for _, m := range attrMarked.FindAllStringSubmatch(body, -1) {
		tag := m[0]
		for _, attr := range strings.Split(m[1], ",") {
			attr = strings.TrimSpace(attr)
			re := regexp.MustCompile(regexp.QuoteMeta(attr) + `="([^"]*)"`)
			v := re.FindStringSubmatch(tag)
			if v == nil {
				continue
			}
			text := strings.TrimSpace(whitespace.ReplaceAllString(v[1], " "))
			if !translatable(text) || covered(text) {
				continue
			}
			missing = append(missing, attr+"="+text)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d marked attributes have no English entry:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// An entry whose value equals its key is "translated" without translating: it looks covered to the
// test above and reads as Spanish to the user. If a string really is the same in both languages it
// belongs in sameInEnglish, where that is a stated intention rather than an accident.
func TestNoEntryEqualsItsKey(t *testing.T) {
	for key, value := range englishCatalog {
		if key == value {
			t.Errorf("catalogue entry %q translates to itself — declare it in sameInEnglish instead", key)
		}
	}
}

// A string cannot be both translated and declared identical: one of the two is a mistake, and which
// one is not guessable from here.
func TestNothingIsBothTranslatedAndDeclaredIdentical(t *testing.T) {
	for key := range sameInEnglish {
		if _, ok := englishCatalog[key]; ok {
			t.Errorf("%q is in sameInEnglish AND has a translation — one of them is wrong", key)
		}
	}
}
