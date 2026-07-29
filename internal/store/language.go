// Which dictation-language configuration belongs to which engine, and what each engine can actually
// DO with it.
//
// Ported from the Electron build's shared/languageSlots.ts. THREE capability archetypes, each
// grounded in a real API limit rather than a preference:
//
//	multi         azure-speech — continuous LID across several full locales, at most ten, one per
//	              base language (the rules already live in internal/settings.ValidateCandidates)
//	auto-or-one   azure-openai / openai / grok / elevenlabs / whisper — ONE optional ISO-639-1 hint,
//	              or auto-detect when omitted
//	one-required  macos — Apple's SpeechAnalyzer needs a concrete supported locale and cannot
//	              auto-detect at all
//
// Unlike a key slot, EVERY engine has a language slot: whisper and macos need a language even though
// they need no credential.
//
// The point of validating per capability, rather than accepting any list everywhere, is that these
// APIs reject a wrong shape at DICTATION time — a full locale sent to an API expecting "es", or
// "auto" sent to one that cannot detect. Catching it here turns a failed dictation into a rejected
// form.
package store

import (
	"fmt"
	"regexp"
	"slices"

	"github.com/Juan-Motta/loqui-go/internal/settings"
)

// AutoLanguage is the value meaning "let the provider detect it".
const AutoLanguage = "auto"

// CapabilityKind is what an engine can do with a language list.
type CapabilityKind string

const (
	// CapMulti takes several full locales and switches between them.
	CapMulti CapabilityKind = "multi"
	// CapAutoOrOne takes one optional base-language hint, or nothing.
	CapAutoOrOne CapabilityKind = "auto-or-one"
	// CapOneRequired needs exactly one full locale and cannot detect.
	CapOneRequired CapabilityKind = "one-required"
)

// LangCapability is a slot's rule, with the ceiling that applies to the multi case.
type LangCapability struct {
	Kind CapabilityKind `json:"kind"`
	// Max is meaningful only for CapMulti; zero otherwise.
	Max int `json:"max"`
}

var (
	localeRe = regexp.MustCompile(`^[a-z]{2,3}-[A-Za-z0-9]+$`)
	baseRe   = regexp.MustCompile(`^[a-z]{2,3}$`)
)

// LangSlotFor is the slot an engine's language lives in.
//
// Azure splits by sub-service because Speech (multi-locale LID) and Azure OpenAI (one hint) differ
// in capability. NEVER empty: an unrecognised engine falls back to whisper's slot, matching the
// default provider, so a hand-edited settings file cannot leave the UI with no control to draw.
func LangSlotFor(provider, azureService string) string {
	switch provider {
	case "azure":
		if azureService == "openai" {
			return "azure-openai"
		}
		return "azure-speech"
	case "openai":
		return "openai"
	case "grok":
		return "grok"
	case "elevenlabs":
		return "elevenlabs"
	case "macos":
		return "macos"
	default:
		return "whisper"
	}
}

// LangCapabilityFor is what a slot can do.
func LangCapabilityFor(slot string) LangCapability {
	switch slot {
	case "azure-speech":
		return LangCapability{Kind: CapMulti, Max: 10}
	case "macos":
		return LangCapability{Kind: CapOneRequired}
	default:
		return LangCapability{Kind: CapAutoOrOne}
	}
}

// IsLangSlot reports whether a string names a language slot.
func IsLangSlot(slot string) bool { return slices.Contains(AllLanguageSlots, slot) }

// ValidateLanguagesFor checks a slot's value against that slot's capability, returning the value to
// store. The error message is shown to the user verbatim, so it says what is expected rather than
// only what was wrong.
func ValidateLanguagesFor(slot string, values []string) ([]string, error) {
	if !IsLangSlot(slot) {
		return nil, fmt.Errorf("ranura de idioma desconocida: %s", slot)
	}
	// Named rule rather than cap: `cap` is a builtin, and shadowing it compiles but reads as a call
	// site to anyone skimming — the same trap `min` set a few lines away in connection.go.
	rule := LangCapabilityFor(slot)

	if rule.Kind == CapMulti {
		// Reuses the tested continuous-LID rules instead of restating them.
		return settings.ValidateCandidates(values)
	}

	if len(values) != 1 {
		return nil, fmt.Errorf("%s admite exactamente un idioma (recibí %d)", slot, len(values))
	}
	v := values[0]

	if rule.Kind == CapAutoOrOne {
		if v == AutoLanguage {
			return []string{AutoLanguage}, nil
		}
		// These APIs take ISO-639-1. A full locale would be forwarded and rejected upstream, so it
		// is caught here rather than at dictation time.
		if !baseRe.MatchString(v) {
			return nil, fmt.Errorf("idioma inválido: %s (se espera \"auto\" o un código como \"es\")", v)
		}
		return []string{v}, nil
	}

	// one-required (macos)
	if v == AutoLanguage {
		return nil, fmt.Errorf("macOS no puede autodetectar: elige un idioma (\"auto\" no es válido)")
	}
	if !localeRe.MatchString(v) {
		return nil, fmt.Errorf("locale inválido: %s (se espera uno completo como \"es-CO\")", v)
	}
	return []string{v}, nil
}

// MigrateLanguageBySlot builds a COMPLETE map from whatever is on disk, handling three cases at
// once: a legacy global list, an existing but partial map, and nothing at all.
//
// Filling every slot is required rather than cosmetic. LoadSettings unmarshals ONTO the defaults, so
// a stored partial map replaces the default map wholesale and the slots it omits would simply
// vanish. Idempotent: migrating this function's own output returns it unchanged.
func MigrateLanguageBySlot(stored map[string][]string, legacy []string) map[string][]string {
	out := make(map[string][]string, len(AllLanguageSlots))
	defaults := DefaultSettings().LanguageBySlot
	for _, slot := range AllLanguageSlots {
		candidate, present := stored[slot]
		if !present {
			candidate = legacySeed(slot, legacy, defaults)
		}
		valid, err := ValidateLanguagesFor(slot, candidate)
		if err != nil {
			// A value that breaks its slot's rules — a hand-edited file, or a legacy list that
			// cannot map cleanly — falls back to that slot's default rather than making the whole
			// configuration unloadable.
			out[slot] = slices.Clone(defaults[slot])
			continue
		}
		out[slot] = valid
	}
	return out
}

// legacySeed is what a slot inherits from the pre-slot global list.
//
// Azure Speech keeps the whole list, being the only engine that ever used more than one; macOS takes
// the first locale; the auto-or-one engines move to auto-detect — a DELIBERATE change from the old
// behaviour of forcing the first configured language on providers that can detect for themselves.
func legacySeed(slot string, legacy []string, defaults map[string][]string) []string {
	if len(legacy) == 0 {
		return slices.Clone(defaults[slot])
	}
	switch slot {
	case "azure-speech":
		return legacy
	case "macos":
		return []string{legacy[0]}
	default:
		return []string{AutoLanguage}
	}
}
