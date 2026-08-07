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
var goMessage = regexp.MustCompile(`\b(?:failed|invalid|probeFailed|ok|revealFailed)\(\s*(?:"[^"]*",\s*)?"((?:[^"\\]|\\.)*)"`)

func TestEveryGoMessageHasAnEntry(t *testing.T) {
	var missing []string
	for _, name := range []string{"settings_write.go", "settings_probe.go", "settings_reveal.go"} {
		src := read(t, "..", "app", name)
		for _, m := range goMessage.FindAllStringSubmatch(src, -1) {
			key := strings.ReplaceAll(m[1], `\"`, `"`)
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
