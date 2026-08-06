// The "Conexiones" model: for each engine, what it is and whether it is ready to use.
//
// Ported from the Electron build's shared/connectionStatus.ts and shared/secretSlots.ts, which were
// already pure and unit-tested there. It belongs on this side for the port's usual reason — the
// Ajustes page paints what this returns and decides nothing — and a first pass at that page invented
// its own status text instead, which is how a provider with no key came to be labelled the same as
// one that was simply not selected.
//
// AN ENGINE IS ONLY CONFIGURED WHEN ITS OWN SLOT IS FILLED, plus whatever non-secret config it
// needs. That is why a key saved for one provider never makes another look ready.
package store

import "slices"

// ConnectionState is what a row can say about an engine.
type ConnectionState string

const (
	// ConnActive is the engine currently selected AND ready.
	ConnActive ConnectionState = "active"
	// ConnConnected is configured but not selected. Only for engines that need a credential.
	ConnConnected ConnectionState = "connected"
	// ConnAvailable is ready with nothing to configure — the local engines.
	ConnAvailable ConnectionState = "available"
	// ConnUnconfigured is missing its key or a required field. Includes selected-but-incomplete,
	// deliberately: claiming "active" for an engine that cannot dictate is the exact confusion this
	// model exists to prevent.
	ConnUnconfigured ConnectionState = "unconfigured"
	// ConnUnsupported means it cannot run on this machine at all, whatever is configured.
	ConnUnsupported ConnectionState = "unsupported"
)

var connectionLabels = map[ConnectionState]string{
	ConnActive:       "Activo",
	ConnConnected:    "Conectado",
	ConnAvailable:    "Disponible",
	ConnUnconfigured: "Sin configurar",
	ConnUnsupported:  "No disponible en este sistema",
}

// ConnectionRow is one card in the Conexiones list.
type ConnectionRow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Cloud is whether this backend needs an API key at all.
	Cloud bool            `json:"cloud"`
	Kind  string          `json:"kind"`
	State ConnectionState `json:"state"`
	Label string          `json:"label"`
	// LangSlot is the language slot this row edits, resolved HERE rather than in the page.
	//
	// It has to travel with the row because Azure's slot depends on its sub-service: switching Speech
	// to OpenAI realtime changes which slot the row is editing, and a page reimplementing that rule
	// would silently save a language into the other service's slot.
	LangSlot string `json:"langSlot"`

	// KeySlot is where this engine's credential lives, empty for the local engines that need none.
	//
	// Travels with the row for exactly the reason LangSlot does: Azure's slot depends on its
	// sub-service, so a page working it out for itself would offer a key field that writes into the
	// other service's slot — stored successfully, and never read at dictation time. The tutorial is a
	// second place that asks for a key, which makes one shared answer worth more than two.
	KeySlot string `json:"keySlot"`
}

// connections is the engines and their order. The page paints them top to bottom, so the order is
// part of the design.
var connections = []struct {
	id, name string
	cloud    bool
}{
	{"whisper", "Whisper", false},
	{"macos", "macOS", false},
	{"azure", "Azure", true},
	{"openai", "OpenAI", true},
	{"grok", "Grok", true},
	{"elevenlabs", "ElevenLabs", true},
}

// helperPlatforms is the engines backed by a native helper, and the platforms that helper is built
// for. Offering one anywhere else is a dead end: the user selects it, presses dictate, and the
// binary is simply not there. An engine absent from this map needs no helper.
var helperPlatforms = map[string][]string{
	"macos":   {"darwin"},
	"whisper": {"darwin", "windows"},
}

// helperByProvider is the binary each helper-backed engine needs.
var helperByProvider = map[string]string{
	"macos":   "macos-stt",
	"whisper": "whisper-stt",
}

// minOSMajor is the oldest macOS whose API the engine exists in. Apple's SpeechAnalyzer arrived in
// macOS 26, so on macOS 15 the helper cannot even launch — the framework is not there. One build
// serves every Mac, which makes this a RUNTIME question about the user's machine.
var minOSMajor = map[string]int{"macos": 26}

// HostCapabilities is what this particular machine and build can offer.
//
// EVERY FIELD IS OPTIONAL and absent means "not known". An unknown condition never disqualifies an
// engine, so a caller that cannot look — a pure test, a page rendering before the host has answered
// — behaves exactly as it did before.
type HostCapabilities struct {
	// Platform is runtime.GOOS. Empty means darwin, which is the only one this build targets.
	Platform string
	// OSMajor is the macOS product major version — 26 for Tahoe. Zero means unknown.
	OSMajor int
	// Helpers reports whether each native helper is actually present next to the app. A missing
	// entry is unknown; false is a definite no.
	Helpers map[string]bool
}

func (c HostCapabilities) platform() string {
	if c.Platform == "" {
		return "darwin"
	}
	return c.Platform
}

// keySlotOrEmpty is KeySlotFor flattened for the row: "" means this engine takes no credential.
//
// The two-value form is right for callers that must not confuse "no key needed" with "key missing";
// for a row that only has to decide whether to show a field, an empty string says it.
func keySlotOrEmpty(provider, azureService string) string {
	if slot, ok := KeySlotFor(provider, azureService); ok {
		return string(slot)
	}
	return ""
}

// RuntimeKeySlotFor is the slot the DICTATION path actually reads for an engine.
//
// It differs from KeySlotFor in exactly one place, and that difference is a fact about this build
// rather than a design: Azure's settings row follows the selected sub-service, while the engine that
// gets built always opens Speech (app.(*Dictation).buildProvider). Until the realtime sub-service is
// ported, a configuration naming it describes something nothing will run — and the two functions
// disagreeing is how that goes unnoticed. They converge again when it is ported.
func RuntimeKeySlotFor(provider string) (KeySlot, bool) {
	if provider == "azure" {
		return SlotAzureSpeech, true
	}
	return KeySlotFor(provider, "")
}

// KeySlotFor is the slot a provider's key lives in, and whether it needs one at all.
//
// Azure Speech and Azure OpenAI are separate resources with separate keys, hence separate slots.
// An unrecognised provider needs none: better to report "no key" than to read someone else's secret.
func KeySlotFor(provider, azureService string) (KeySlot, bool) {
	switch provider {
	case "azure":
		if azureService == "openai" {
			return SlotAzureOpenAI, true
		}
		return SlotAzureSpeech, true // speech is the default
	case "openai":
		return SlotOpenAI, true
	case "grok":
		return SlotGrok, true
	case "elevenlabs":
		return SlotElevenLabs, true
	default:
		return "", false
	}
}

// IsAvailableOn reports whether an engine can run here at all.
//
// THREE conditions, none decorative: the right platform (no Apple engine on Windows), an OS new
// enough for its API, and a helper that actually shipped in this build — a build made without one
// carries no binary, and "you are on a Mac" would be a lie that only surfaces when the user tries to
// dictate.
func IsAvailableOn(provider string, caps HostCapabilities) bool {
	if supported, ok := helperPlatforms[provider]; ok && !slices.Contains(supported, caps.platform()) {
		return false
	}
	// Named floor rather than min: `min` is a builtin, and shadowing it here would compile but read
	// as a call site for anyone skimming.
	if floor, ok := minOSMajor[provider]; ok && caps.OSMajor != 0 && caps.OSMajor < floor {
		return false
	}
	if helper, ok := helperByProvider[provider]; ok && caps.Helpers != nil {
		if present, known := caps.Helpers[helper]; known && !present {
			return false
		}
	}
	return true
}

// HasNonSecretConfig reports whether everything an engine needs BESIDES its credential is in place.
//
// Exported because "the keys could not be read" only justifies leaving an engine alone when the key
// is the ONLY thing unconfirmed. Azure without a region cannot dictate whatever its key turns out to
// be, so unreadable credentials must not shield that case.
func HasNonSecretConfig(provider string, s Settings) bool { return hasRequiredConfig(provider, s) }

// hasRequiredConfig is everything an engine needs BESIDES its key. Azure is the only one with extra
// required fields, and which field depends on the sub-service: the realtime endpoint is addressed by
// resource name, the speech one by region.
func hasRequiredConfig(provider string, s Settings) bool {
	if provider != "azure" {
		return true
	}
	if s.AzureService == "openai" {
		return s.AzureOpenAiResource != ""
	}
	return s.Region != ""
}

// ConnectionStateFor computes one engine's state.
func ConnectionStateFor(provider string, s Settings, keys map[KeySlot]bool, caps HostCapabilities) ConnectionState {
	// Checked before anything else: no amount of configuration makes a missing helper, or an OS
	// without the API, work.
	if !IsAvailableOn(provider, caps) {
		return ConnUnsupported
	}
	slot, needsKey := KeySlotFor(provider, s.AzureService)
	hasKey := !needsKey || keys[slot]
	if !hasKey || !hasRequiredConfig(provider, s) {
		return ConnUnconfigured
	}
	if s.Provider == provider {
		return ConnActive
	}
	if !needsKey {
		return ConnAvailable
	}
	return ConnConnected
}

// kindOf is the one-line description under the engine's name.
func kindOf(provider string, s Settings) string {
	switch provider {
	case "whisper":
		return "Local · sin conexión ni clave"
	case "macos":
		return "On-device · Apple Speech"
	case "azure":
		if s.AzureService == "openai" {
			return "Nube · Azure OpenAI realtime"
		}
		return "Nube · Speech multilingüe"
	case "openai":
		return "Realtime · OpenAI"
	case "grok":
		return "Realtime · xAI"
	case "elevenlabs":
		return "Realtime · Scribe"
	default:
		return ""
	}
}

// providerHints is the paragraph shown under the engine picker, ported verbatim. It says what the
// selected engine actually does and what it needs — the difference between choosing an engine and
// discovering its limits mid-dictation.
var providerHints = map[string]string{
	"macos":   "On-device (Apple): sin clave ni internet. Transcribe en el primer idioma configurado (no cambia de idioma solo).",
	"whisper": "Local (whisper.cpp): sin clave ni internet, multiplataforma. Transcribe por frase (al detectar pausa), no palabra por palabra.",
	"azure":   "En la nube: requiere región + clave. Detecta y alterna idiomas automáticamente (LID continuo).",
	"openai":  "OpenAI (nube): transcripción realtime por WebSocket (gpt-realtime-whisper). Requiere tu API key de OpenAI.",
	"grok":    "Grok (xAI, nube): transcripción en streaming por WebSocket. Requiere tu API key de xAI; se usa solo en el proceso principal.",
	"elevenlabs": "ElevenLabs (Scribe, nube): transcripción en streaming por WebSocket. Requiere tu API key de ElevenLabs; " +
		"se usa solo en el proceso principal.",
}

// ProviderHint is the description of one engine, or "" for an unknown one.
func ProviderHint(provider string) string { return providerHints[provider] }

// ConnectionRows is every engine's row, in the order the page paints them.
func ConnectionRows(s Settings, keys map[KeySlot]bool, caps HostCapabilities) []ConnectionRow {
	out := make([]ConnectionRow, 0, len(connections))
	for _, c := range connections {
		state := ConnectionStateFor(c.id, s, keys, caps)
		out = append(out, ConnectionRow{
			ID:       c.id,
			Name:     c.name,
			Cloud:    c.cloud,
			Kind:     kindOf(c.id, s),
			State:    state,
			Label:    connectionLabels[state],
			LangSlot: LangSlotFor(c.id, s.AzureService),
			KeySlot:  keySlotOrEmpty(c.id, s.AzureService),
		})
	}
	return out
}
