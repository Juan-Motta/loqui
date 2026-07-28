// The bootstrap payload: everything the Ajustes page needs to paint itself, computed in Go
// and handed over in ONE call.
//
// WHY IT IS ONE CALL. In the Electron build the settings renderer imported ten shared pure
// modules and re-derived this state itself, because main and renderer shared a language.
// Here those rules live in Go as the single source of truth, so the webview cannot derive
// anything — it can only ask. Asking field by field would mean a dozen round trips and a
// half-empty form painted in between; asking once means the page renders in a single pass
// from a snapshot that is internally consistent.
//
// WHAT IT MUST NEVER CARRY: an API key. Presence only — see KeyState and
// TestAKeyStateCarriesPresenceAndNothingElse.
package app

import (
	"os"
	"sync"

	"github.com/Juan-Motta/loqui-go/internal/audio"
	"github.com/Juan-Motta/loqui-go/internal/permissions"
	"github.com/Juan-Motta/loqui-go/internal/store"
)

// KeyState is what the UI may know about one provider's credential: whether there is one,
// and where it came from. Never the value.
type KeyState struct {
	Slot string `json:"slot"`
	// Status is the EFFECTIVE answer to "would dictation find a credential here right now",
	// so it accounts for the env-var escape hatch as well as the Keychain.
	//
	// Three states rather than a bool, deliberately. "Nothing stored" and "the Keychain did
	// not answer" are different facts: the first means type your key in, the second means the
	// signing identity is broken and typing it in will not help. Collapsing them — which is
	// what store.HasKey does — makes the user retype a credential that is already there.
	Status store.KeyStatus `json:"status"`
	// FromEnv marks a key supplied by LOQUI_*_KEY rather than the Keychain. The UI needs it
	// for two reasons: it cannot offer to delete something that is not in the Keychain, and
	// the user has to understand why the slot reads as configured while the field is blank.
	FromEnv bool `json:"fromEnv"`
}

// PermissionsState is the three grants dictation depends on. Statuses are passed through
// verbatim, including "unknown" — the UI must show an unreadable grant as unverified rather
// than guessing. The Electron build defaulted to "granted" and made a denied microphone look
// fine while every dictation died.
type PermissionsState struct {
	Microphone        permissions.Status `json:"microphone"`
	SpeechRecognition permissions.Status `json:"speechRecognition"`
	// Accessibility is a bool, not a Status, because AXIsProcessTrusted answers yes or no —
	// there is no "not determined" to report.
	Accessibility bool `json:"accessibility"`
}

// SettingsPayload is the snapshot the Ajustes page renders from.
type SettingsPayload struct {
	// ---- persisted settings ----
	Provider string `json:"provider"`
	// Region is the Azure Speech region. Carried even though only one engine uses it: Azure
	// needs it as much as it needs a key, and a form painted without it would blank a field
	// the user already filled in — then wipe a working configuration on the next save.
	Region         string              `json:"region"`
	Mode           string              `json:"mode"`
	TriggerKey     string              `json:"triggerKey"`
	Appearance     string              `json:"appearance"`
	AppLanguage    string              `json:"appLanguage"`
	Onboarded      bool                `json:"onboarded"`
	LanguageBySlot map[string][]string `json:"languageBySlot"`
	InputDeviceID  string              `json:"inputDeviceId"`

	// ---- computed state ----
	Keys        []KeyState       `json:"keys"`
	Permissions PermissionsState `json:"permissions"`
	// InputDevices is never nil: a nil slice marshals to JSON null and every consumer in the
	// webview would have to guard for it.
	InputDevices []audio.InputDevice `json:"inputDevices"`
	// DevicesError is set when enumerating the hardware failed. It exists so the UI can tell
	// "no microphones" apart from "could not look" — those need different messages.
	DevicesError string `json:"devicesError"`
	// DataDir backs the About view and any bug report.
	DataDir string `json:"dataDir"`
}

// Bootstrap computes the payload. Every dependency that touches the machine is a field
// rather than a direct call, because all three are untestable in place: the Keychain does not
// answer on an ad-hoc-signed build, TCC state varies per developer, and enumerating devices
// needs real hardware.
type Bootstrap struct {
	store *store.Store

	// keyStatus reports what is known about ONE slot. Per slot, not a batch, so keyStates can
	// skip the slots an env override already answers and fan the rest out concurrently —
	// see the comment there for why that matters.
	keyStatus func(store.KeySlot) store.KeyStatus
	// perms reads the three grants. Defaults to the live TCC lookups.
	perms func() PermissionsState
	// devices enumerates microphones. Defaults to audio.ListInputDevices.
	devices func() ([]audio.InputDevice, error)
}

// NewBootstrap wires the real machine.
func NewBootstrap(st *store.Store) *Bootstrap {
	return &Bootstrap{
		store:     st,
		keyStatus: store.KeyStatusFor,
		perms:     livePermissions,
		devices:   audio.ListInputDevices,
	}
}

// livePermissions reads the three grants from the OS.
func livePermissions() PermissionsState {
	return PermissionsState{
		Microphone:        permissions.Microphone(),
		SpeechRecognition: permissions.SpeechRecognition(),
		Accessibility:     permissions.Accessibility(),
	}
}

// Payload assembles the snapshot.
//
// It cannot fail. Each part degrades on its own instead: a failed device enumeration reports
// itself in DevicesError and leaves everything else intact, because the engine picker, the
// keys and the permissions have nothing to do with the microphone list — and returning an
// error would leave the user with no way to reach the settings that could fix the problem.
func (b *Bootstrap) Payload() SettingsPayload {
	settings := b.store.LoadSettings()

	devices, err := b.devices()
	if devices == nil {
		devices = []audio.InputDevice{}
	}
	devicesError := ""
	if err != nil {
		devicesError = err.Error()
	}

	return SettingsPayload{
		Provider:       settings.Provider,
		Region:         settings.Region,
		Mode:           settings.Mode,
		TriggerKey:     settings.TriggerKey,
		Appearance:     settings.Appearance,
		AppLanguage:    settings.AppLanguage,
		Onboarded:      settings.Onboarded,
		LanguageBySlot: languages(settings),
		InputDeviceID:  settings.InputDeviceID,

		Keys:         b.keyStates(),
		Permissions:  b.perms(),
		InputDevices: devices,
		DevicesError: devicesError,
		DataDir:      b.store.Dir(),
	}
}

// SettingsService is the Wails service the Ajustes page calls. It exists as its own type,
// rather than binding Bootstrap directly, because everything the page needs to WRITE
// (choose an engine, store a key, pick a device) hangs off this same object next — and a
// service's exported methods are its public API to the webview, so Bootstrap's internals
// must not be part of it.
type SettingsService struct {
	bootstrap *Bootstrap
}

func NewSettingsService(st *store.Store) *SettingsService {
	return &SettingsService{bootstrap: NewBootstrap(st)}
}

// ServiceName is the name Wails uses for this service in its own logs and diagnostics. It does
// NOT rename the generated binding: that follows the Go type, so the frontend imports
// bindings/.../internal/app/settingsservice.js.
func (s *SettingsService) ServiceName() string { return "Settings" }

// Load returns the snapshot the page renders from. Bound to the frontend as
// Settings.Load().
func (s *SettingsService) Load() SettingsPayload { return s.bootstrap.Payload() }

// keyStates reports presence for every slot, in the order of store.AllKeySlots so the UI
// gets a stable list. Every slot appears, including the empty ones: the picker lists them all
// and a missing entry would read as "this provider has no key field".
//
// The env override is checked FIRST, mirroring keyReaderFor exactly — including the fact that
// it outranks an unreadable Keychain, which is the whole reason the hatch exists. If the two
// ever disagreed the UI would contradict what dictation actually does. A slot answered by the
// environment is never looked up at all, which also means it costs nothing.
//
// The remaining slots are read CONCURRENTLY, and that is a requirement rather than an
// optimisation. Each read is bounded by store's three-second timeout, and on an ad-hoc-signed
// build the Keychain does not answer, so five sequential reads would cost fifteen seconds —
// spent with the Ajustes page blank, since this is what it paints from. Fanned out, the worst
// case is one timeout instead of five.
func (b *Bootstrap) keyStates() []KeyState {
	out := make([]KeyState, len(store.AllKeySlots))
	var wg sync.WaitGroup

	for i, slot := range store.AllKeySlots {
		out[i] = KeyState{Slot: string(slot)}
		if name := envKeyOverride(slot); name != "" && os.Getenv(name) != "" {
			out[i].Status = store.KeyPresent
			out[i].FromEnv = true
			continue
		}
		// Each goroutine owns its own element, so no lock is needed: the slice is sized up
		// front and nobody reads it until wg.Wait returns.
		wg.Add(1)
		go func(i int, slot store.KeySlot) {
			defer wg.Done()
			out[i].Status = b.keyStatus(slot)
		}(i, slot)
	}

	wg.Wait()
	return out
}

// languages returns one list per language slot, never empty.
//
// It cannot pass the persisted map straight through: a hand-edited file can leave any slot
// empty or null, and a slot absent from the file has a per-slot default. store.LanguagesIn owns
// those rules; running them here is what keeps the webview from reimplementing the same
// defaulting — the duplication this whole payload exists to remove.
//
// Derived from the settings the caller ALREADY loaded, not re-read per slot, so the languages
// belong to the same snapshot as the rest of the payload.
func languages(settings store.Settings) map[string][]string {
	out := make(map[string][]string, len(store.AllLanguageSlots))
	for _, slot := range store.AllLanguageSlots {
		out[slot] = store.LanguagesIn(settings, slot)
	}
	return out
}
