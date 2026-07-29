// Curated language and locale lists for the per-engine language pickers.
//
// Ported from the Electron build's shared/languageCatalog.ts, labels included — the interface is
// Spanish, and translating them here would put the same strings in two places.
//
// THREE DISTINCT VALUE SPACES, and mixing them is the bug this file's shape exists to prevent:
//
//	BaseLanguages   base ISO-639-1 codes ("es") for the five optional-hint engines, whose APIs take
//	                ISO-639-1 and reject a full locale
//	AzureLocales    full locales ("es-CO") for Azure Speech continuous LID
//	MacOSLocales    full locales for Apple's SpeechAnalyzer
//
// macOS's real supported set is only knowable at runtime (SpeechTranscriber.supportedLocales). This
// is a curated subset instead, because the Swift helper already falls back to any locale of the same
// base language and surfaces its own "locale not supported" error otherwise — so querying the real
// set would add a round trip to learn something the helper handles anyway.
package store

// LanguageOption is one entry in a picker.
type LanguageOption struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// BaseLanguages are the ISO-639-1 codes the optional-hint engines accept.
var BaseLanguages = []LanguageOption{
	{"es", "Español"},
	{"en", "Inglés"},
	{"pt", "Portugués"},
	{"fr", "Francés"},
	{"de", "Alemán"},
	{"it", "Italiano"},
	{"nl", "Neerlandés"},
	{"ja", "Japonés"},
	{"ko", "Coreano"},
	{"zh", "Chino"},
	{"ru", "Ruso"},
	{"ar", "Árabe"},
}

// AzureLocales are the full locales offered for Azure Speech's continuous LID.
var AzureLocales = []LanguageOption{
	{"es-CO", "Español (Colombia)"},
	{"es-ES", "Español (España)"},
	{"es-MX", "Español (México)"},
	{"es-US", "Español (Estados Unidos)"},
	{"en-US", "Inglés (Estados Unidos)"},
	{"en-GB", "Inglés (Reino Unido)"},
	{"pt-BR", "Portugués (Brasil)"},
	{"fr-FR", "Francés (Francia)"},
	{"de-DE", "Alemán (Alemania)"},
	{"it-IT", "Italiano (Italia)"},
	{"nl-NL", "Neerlandés (Países Bajos)"},
	{"ja-JP", "Japonés (Japón)"},
	{"ko-KR", "Coreano (Corea del Sur)"},
	{"zh-CN", "Chino (simplificado)"},
}

// MacOSLocales are the full locales offered for the Apple on-device engine.
var MacOSLocales = []LanguageOption{
	{"es-CO", "Español (Colombia)"},
	{"es-ES", "Español (España)"},
	{"es-MX", "Español (México)"},
	{"es-US", "Español (Estados Unidos)"},
	{"en-US", "Inglés (Estados Unidos)"},
	{"en-GB", "Inglés (Reino Unido)"},
	{"en-AU", "Inglés (Australia)"},
	{"en-CA", "Inglés (Canadá)"},
	{"pt-BR", "Portugués (Brasil)"},
	{"fr-FR", "Francés (Francia)"},
	{"de-DE", "Alemán (Alemania)"},
	{"it-IT", "Italiano (Italia)"},
	{"ja-JP", "Japonés (Japón)"},
	{"ko-KR", "Coreano (Corea del Sur)"},
	{"zh-CN", "Chino (simplificado)"},
}

// autoLabel is what "let the provider detect it" reads as in the picker.
const autoLabel = "Detección automática"

// LanguageLabel is the Spanish label for a stored value, falling back to the code itself.
//
// Falling back rather than erroring is deliberate: a settings file may hold a locale this curated
// list does not name, and showing the raw code is far better than showing nothing or refusing to
// paint the control at all.
func LanguageLabel(code string) string {
	if code == AutoLanguage {
		return autoLabel
	}
	for _, list := range [][]LanguageOption{AzureLocales, MacOSLocales, BaseLanguages} {
		for _, o := range list {
			if o.Code == code {
				return o.Label
			}
		}
	}
	return code
}

// LanguageCopy is the heading and explanation above a slot's control.
//
// It varies by CAPABILITY, not by engine, because what the user needs told is what the control can
// do: that Azure switches between several by itself, that "automatic" means no language is sent at
// all, or that Apple's engine cannot detect and so needs a choice. Ported verbatim.
type LanguageCopy struct {
	Label string `json:"label"`
	Desc  string `json:"desc"`
}

var languageCopy = map[CapabilityKind]LanguageCopy{
	CapMulti: {
		Label: "Idiomas del dictado",
		Desc:  "Azure Speech alterna entre ellos automáticamente (LID continuo). Un locale por idioma; hasta 10.",
	},
	CapAutoOrOne: {
		Label: "Idioma del dictado",
		Desc:  "Con detección automática no se envía ningún idioma y el proveedor lo deduce del audio.",
	},
	CapOneRequired: {
		Label: "Idioma del dictado",
		Desc:  "El reconocimiento on-device de Apple necesita un idioma concreto: no puede autodetectar.",
	},
}

// LanguageCopyFor is the copy for a slot's control.
func LanguageCopyFor(slot string) LanguageCopy { return languageCopy[LangCapabilityFor(slot).Kind] }

// LanguageOptionsFor is the list a slot's control should offer, given its capability.
//
// It is the one place that maps capability to value space, so a picker cannot end up offering base
// codes to an engine that needs full locales — the mix-up the three separate lists exist to prevent.
func LanguageOptionsFor(slot string) []LanguageOption {
	switch LangCapabilityFor(slot).Kind {
	case CapMulti:
		return AzureLocales
	case CapOneRequired:
		return MacOSLocales
	default:
		return BaseLanguages
	}
}
