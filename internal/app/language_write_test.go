package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/Juan-Motta/loqui-go/internal/store"
)

func TestSettingLanguagesPersistsThem(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)

	res := svc.SetLanguages("grok", []string{"es"})
	if res.Error != "" {
		t.Fatalf("SetLanguages: %s", res.Error)
	}
	if got := st.LoadSettings().LanguageBySlot["grok"]; !slices.Equal(got, []string{"es"}) {
		t.Errorf("stored %v, want [es]", got)
	}
	// And the repainted payload shows it, so the control does not have to re-read.
	for _, c := range res.Payload.LanguageControls {
		if c.Slot == "grok" && !slices.Equal(c.Selected, []string{"es"}) {
			t.Errorf("the returned control still says %v", c.Selected)
		}
	}
}

// The value is validated against the slot's CAPABILITY, so a shape the API would reject is caught
// here instead of at dictation time.
func TestSettingAnInvalidLanguageIsRefused(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)
	before := st.LoadSettings().LanguageBySlot["grok"]

	// A full locale where the API expects ISO-639-1.
	res := svc.SetLanguages("grok", []string{"es-CO"})
	if res.Error == "" {
		t.Fatal("a full locale was accepted for a base-code engine")
	}
	if got := st.LoadSettings().LanguageBySlot["grok"]; !slices.Equal(got, before) {
		t.Errorf("a rejected write changed the stored value to %v", got)
	}

	// "auto" on the engine that cannot detect.
	if res := svc.SetLanguages("macos", []string{"auto"}); res.Error == "" {
		t.Error("macos accepted auto, which it cannot honour")
	} else if !strings.Contains(res.Error, "autodetectar") {
		t.Errorf("error = %q, want it to explain why", res.Error)
	}
}

// Setting ONE slot must not disturb the others. Language is per engine, and the whole point of a
// per-slot write is that a misconfigured engine cannot block editing another's language.
func TestSettingOneSlotLeavesTheOthersAlone(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)
	before := st.LoadSettings().LanguageBySlot

	if res := svc.SetLanguages("macos", []string{"en-US"}); res.Error != "" {
		t.Fatalf("SetLanguages: %s", res.Error)
	}

	after := st.LoadSettings().LanguageBySlot
	for _, slot := range store.AllLanguageSlots {
		if slot == "macos" {
			continue
		}
		if !slices.Equal(after[slot], before[slot]) {
			t.Errorf("slot %q changed from %v to %v", slot, before[slot], after[slot])
		}
	}
}

// Writing must not mutate the defaults map. LoadSettings can hand back that very map, so writing
// into it would change the defaults for every later read in the process — a bug that would only
// show up as one engine's language leaking into a fresh install's.
func TestSettingLanguagesDoesNotMutateTheDefaults(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)
	originalDefault := slices.Clone(store.DefaultSettings().LanguageBySlot["grok"])

	if res := svc.SetLanguages("grok", []string{"fr"}); res.Error != "" {
		t.Fatalf("SetLanguages: %s", res.Error)
	}

	if got := store.DefaultSettings().LanguageBySlot["grok"]; !slices.Equal(got, originalDefault) {
		t.Errorf("the defaults were mutated: grok is now %v, want %v", got, originalDefault)
	}
}

// Azure Speech takes several locales, and the ceiling and the one-per-base-language rule come from
// the shared Azure validation rather than a second copy here.
func TestSettingAzureSpeechLanguagesUsesTheSharedRules(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)

	if res := svc.SetLanguages("azure-speech", []string{"es-CO", "en-US"}); res.Error != "" {
		t.Fatalf("two locales: %s", res.Error)
	}
	// Two locales of the SAME base language is the rule that only exists here — Azure accepts it and
	// then quietly degrades detection, so this is the only place it can be caught.
	if res := svc.SetLanguages("azure-speech", []string{"es-CO", "es-MX"}); res.Error == "" {
		t.Error("two Spanish locales were accepted; Azure allows one per base language")
	}
}

// Every slot gets a control, with the option list its capability requires. A page choosing the list
// itself is how a picker ends up offering base codes to an engine that needs full locales.
func TestEverySlotGetsAControlWithTheRightOptions(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)

	controls := map[string]LanguageControl{}
	for _, c := range svc.Load().LanguageControls {
		controls[c.Slot] = c
	}
	if len(controls) != len(store.AllLanguageSlots) {
		t.Fatalf("got %d controls, want %d", len(controls), len(store.AllLanguageSlots))
	}

	// azure-speech: full locales, and the ceiling travels with it.
	if got := controls["azure-speech"]; got.Kind != store.CapMulti || got.Max != 10 {
		t.Errorf("azure-speech = %+v, want multi/10", got)
	}
	// Asserted on codes that DISTINGUISH the two locale lists, not on one they share.
	//
	// This is where a first version of this test was vacuous: it checked for "es-CO", which is in the
	// Azure list AND the macOS list, so handing macOS the Azure options passed unnoticed. A mutation
	// check found it. The lists really do differ — Azure carries nl-NL, macOS carries en-AU and
	// en-CA — and that difference is the whole reason there are two of them.
	if !hasCode(controls["azure-speech"].Options, "nl-NL") {
		t.Error("azure-speech is not offered the Azure locale list")
	}
	if hasCode(controls["azure-speech"].Options, "en-AU") {
		t.Error("azure-speech was handed the macOS locale list")
	}
	// macos: full locales too, but its own list, and exactly one.
	if got := controls["macos"].Kind; got != store.CapOneRequired {
		t.Errorf("macos kind = %q", got)
	}
	if !hasCode(controls["macos"].Options, "en-AU") || !hasCode(controls["macos"].Options, "en-CA") {
		t.Error("macos is not offered the macOS locale list")
	}
	if hasCode(controls["macos"].Options, "nl-NL") {
		t.Error("macos was handed the Azure locale list")
	}
	// The hint engines: BASE codes, never locales.
	for _, slot := range []string{"whisper", "grok", "openai", "elevenlabs", "azure-openai"} {
		c := controls[slot]
		if c.Kind != store.CapAutoOrOne {
			t.Errorf("%s kind = %q", slot, c.Kind)
		}
		if !hasCode(c.Options, "es") {
			t.Errorf("%s is not offered base codes", slot)
		}
		if hasCode(c.Options, "es-CO") {
			t.Errorf("%s is offered a full locale, which its API would reject", slot)
		}
	}
	// And every control carries its copy, or the control paints with no heading.
	for slot, c := range controls {
		if c.Label == "" || c.Desc == "" {
			t.Errorf("slot %q has no copy: %+v", slot, c)
		}
	}
}

func hasCode(options []store.LanguageOption, code string) bool {
	for _, o := range options {
		if o.Code == code {
			return true
		}
	}
	return false
}
