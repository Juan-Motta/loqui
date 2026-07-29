package store

import (
	"slices"
	"strings"
	"testing"
)

func TestLangSlotForProvider(t *testing.T) {
	cases := []struct{ provider, azureService, want string }{
		{"azure", "speech", "azure-speech"},
		{"azure", "openai", "azure-openai"},
		{"azure", "", "azure-speech"}, // speech is the default
		{"openai", "", "openai"},
		{"grok", "", "grok"},
		{"elevenlabs", "", "elevenlabs"},
		{"macos", "", "macos"},
		{"whisper", "", "whisper"},
		// Never empty: an unrecognised engine falls back to whisper's slot, so a hand-edited file
		// cannot leave the UI with no control to draw.
		{"dragon", "", "whisper"},
		{"", "", "whisper"},
	}
	for _, c := range cases {
		if got := LangSlotFor(c.provider, c.azureService); got != c.want {
			t.Errorf("LangSlotFor(%q,%q) = %q, want %q", c.provider, c.azureService, got, c.want)
		}
	}
}

// The three archetypes, each grounded in a real API limit.
func TestLangCapabilityPerSlot(t *testing.T) {
	if got := LangCapabilityFor("azure-speech"); got.Kind != CapMulti || got.Max != 10 {
		t.Errorf("azure-speech = %+v, want multi/10", got)
	}
	if got := LangCapabilityFor("macos"); got.Kind != CapOneRequired {
		t.Errorf("macos = %+v, want one-required", got)
	}
	for _, slot := range []string{"whisper", "grok", "openai", "elevenlabs", "azure-openai"} {
		if got := LangCapabilityFor(slot); got.Kind != CapAutoOrOne {
			t.Errorf("%s = %+v, want auto-or-one", slot, got)
		}
	}
}

// Every slot in the declared set must have a capability, or the UI would have a control it cannot
// draw rules for.
func TestEveryLanguageSlotHasACapability(t *testing.T) {
	for _, slot := range AllLanguageSlots {
		if !IsLangSlot(slot) {
			t.Errorf("%q is in AllLanguageSlots but IsLangSlot says no", slot)
		}
		switch LangCapabilityFor(slot).Kind {
		case CapMulti, CapAutoOrOne, CapOneRequired:
		default:
			t.Errorf("%q has no recognised capability", slot)
		}
	}
}

// auto-or-one takes "auto" or a BASE code. A full locale is refused here rather than forwarded to an
// API that would reject it mid-dictation.
func TestAutoOrOneAcceptsAutoOrABaseCode(t *testing.T) {
	for _, slot := range []string{"whisper", "grok", "openai", "elevenlabs", "azure-openai"} {
		if got, err := ValidateLanguagesFor(slot, []string{"auto"}); err != nil || !slices.Equal(got, []string{"auto"}) {
			t.Errorf("%s auto: got %v, %v", slot, got, err)
		}
		if got, err := ValidateLanguagesFor(slot, []string{"es"}); err != nil || !slices.Equal(got, []string{"es"}) {
			t.Errorf("%s es: got %v, %v", slot, got, err)
		}
		// A full locale is the mistake this catches.
		_, err := ValidateLanguagesFor(slot, []string{"es-CO"})
		if err == nil {
			t.Errorf("%s accepted a full locale, which the API would reject at dictation time", slot)
		}
		if err != nil && !strings.Contains(err.Error(), "auto") {
			t.Errorf("%s error = %q, want it to say what IS expected", slot, err)
		}
	}
}

// Exactly one, not several: these engines take a single hint.
func TestAutoOrOneRefusesMoreThanOne(t *testing.T) {
	_, err := ValidateLanguagesFor("grok", []string{"es", "en"})
	if err == nil {
		t.Fatal("two languages were accepted for a single-hint engine")
	}
	if !strings.Contains(err.Error(), "exactamente un idioma") {
		t.Errorf("error = %q", err)
	}
	if _, err := ValidateLanguagesFor("grok", nil); err == nil {
		t.Error("an empty list was accepted")
	}
}

// macOS needs a CONCRETE locale: SpeechAnalyzer cannot auto-detect, so "auto" has to be refused with
// an explanation rather than stored and silently ignored.
func TestOneRequiredRefusesAutoAndBareCodes(t *testing.T) {
	if got, err := ValidateLanguagesFor("macos", []string{"es-CO"}); err != nil || !slices.Equal(got, []string{"es-CO"}) {
		t.Fatalf("macos es-CO: got %v, %v", got, err)
	}
	_, err := ValidateLanguagesFor("macos", []string{"auto"})
	if err == nil {
		t.Fatal("macos accepted auto, which it cannot honour")
	}
	if !strings.Contains(err.Error(), "autodetectar") {
		t.Errorf("error = %q, want it to explain why", err)
	}
	if _, err := ValidateLanguagesFor("macos", []string{"es"}); err == nil {
		t.Error("macos accepted a bare base code; it needs a full locale")
	}
}

// azure-speech defers to the tested continuous-LID rules instead of restating them.
func TestMultiUsesTheAzureCandidateRules(t *testing.T) {
	got, err := ValidateLanguagesFor("azure-speech", []string{"es-CO", "en-US"})
	if err != nil {
		t.Fatalf("two locales: %v", err)
	}
	if !slices.Equal(got, []string{"es-CO", "en-US"}) {
		t.Errorf("got %v", got)
	}
	// Past the ceiling the shared rules reject it, and that is where the limit lives.
	many := []string{"es-CO", "en-US", "fr-FR", "de-DE", "it-IT", "pt-BR", "ja-JP", "ko-KR", "zh-CN", "ru-RU", "nl-NL"}
	if _, err := ValidateLanguagesFor("azure-speech", many); err == nil {
		t.Error("eleven locales were accepted; Azure allows ten")
	}
}

func TestAnUnknownSlotIsRefused(t *testing.T) {
	if _, err := ValidateLanguagesFor("dragon", []string{"es"}); err == nil {
		t.Error("an unknown slot was accepted")
	}
}

// The migration must produce a COMPLETE map. A partial one replaces the defaults wholesale on load,
// so any slot it omits would simply vanish.
func TestMigrationFillsEverySlot(t *testing.T) {
	got := MigrateLanguageBySlot(map[string][]string{"grok": {"es"}}, nil)
	for _, slot := range AllLanguageSlots {
		if len(got[slot]) == 0 {
			t.Errorf("slot %q is empty after migration", slot)
		}
	}
	if !slices.Equal(got["grok"], []string{"es"}) {
		t.Errorf("the stored value was lost: %v", got["grok"])
	}
}

// A value that breaks its slot's rules falls back to that slot's default rather than making the
// whole configuration unloadable — the same principle LoadSettings follows for a corrupt file.
func TestMigrationFallsBackOnAnInvalidStoredValue(t *testing.T) {
	got := MigrateLanguageBySlot(map[string][]string{
		"macos": {"auto"},        // macos cannot autodetect
		"grok":  {"es-CO", "en"}, // grok takes one hint
	}, nil)

	defaults := DefaultSettings().LanguageBySlot
	if !slices.Equal(got["macos"], defaults["macos"]) {
		t.Errorf("macos = %v, want the default %v", got["macos"], defaults["macos"])
	}
	if !slices.Equal(got["grok"], defaults["grok"]) {
		t.Errorf("grok = %v, want the default %v", got["grok"], defaults["grok"])
	}
}

// The legacy global list seeds each slot by CAPABILITY: Azure Speech keeps the whole list, macOS
// takes the first locale, and the engines that can detect move to auto — deliberately, instead of
// forcing the first configured language on them as the old behaviour did.
func TestMigrationSeedsFromTheLegacyGlobalList(t *testing.T) {
	got := MigrateLanguageBySlot(nil, []string{"es-CO", "en-US"})

	if !slices.Equal(got["azure-speech"], []string{"es-CO", "en-US"}) {
		t.Errorf("azure-speech = %v, want the whole legacy list", got["azure-speech"])
	}
	if !slices.Equal(got["macos"], []string{"es-CO"}) {
		t.Errorf("macos = %v, want the first locale", got["macos"])
	}
	for _, slot := range []string{"whisper", "grok", "openai", "elevenlabs", "azure-openai"} {
		if !slices.Equal(got[slot], []string{"auto"}) {
			t.Errorf("%s = %v, want auto — these engines detect for themselves", slot, got[slot])
		}
	}
}

// Idempotent: migrating this function's own output returns it unchanged. Without that, every load
// would be free to drift the stored value.
func TestMigrationIsIdempotent(t *testing.T) {
	once := MigrateLanguageBySlot(map[string][]string{"grok": {"es"}}, []string{"es-CO", "en-US"})
	twice := MigrateLanguageBySlot(once, nil)
	for _, slot := range AllLanguageSlots {
		if !slices.Equal(once[slot], twice[slot]) {
			t.Errorf("slot %q changed on a second migration: %v -> %v", slot, once[slot], twice[slot])
		}
	}
}
