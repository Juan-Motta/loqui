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
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Juan-Motta/loqui-go/internal/audio"
	"github.com/Juan-Motta/loqui-go/internal/i18n"
	"github.com/Juan-Motta/loqui-go/internal/macos"
	"github.com/Juan-Motta/loqui-go/internal/permissions"
	"github.com/Juan-Motta/loqui-go/internal/settings"
	"github.com/Juan-Motta/loqui-go/internal/store"
	"github.com/Juan-Motta/loqui-go/internal/stt"
	"github.com/Juan-Motta/loqui-go/internal/stt/azure"
)

// KeyState is what the UI may know about one provider's credential: whether there is one,
// and where it came from. Never the value.
type KeyState struct {
	Slot string `json:"slot"`
	// Status is the EFFECTIVE answer to "would dictation find a credential here right now",
	// so it accounts for the env-var escape hatch as well as the stored credentials.
	//
	// Three states rather than a bool, deliberately. "Nothing stored" and "the keys could
	// not be read" are different facts: the first means type your key in, the second means the
	// credentials file is damaged and typing a key in will not say what was lost. Collapsing them — which is
	// what store.HasKey does — makes the user retype a credential that is already there.
	Status store.KeyStatus `json:"status"`
	// FromEnv marks a key supplied by LOQUI_*_KEY rather than the stored credentials. The UI needs
	// it for two reasons: it cannot offer to delete something it did not store, and the user has to
	// understand why the slot reads as configured while the field is blank.
	FromEnv bool `json:"fromEnv"`
	// Available is whether a credential here would ever be READ. Not derivable from provider
	// availability: a provider may expose multiple independently ported credential slots.
	Available bool `json:"available"`
	// Stored is "THIS APP holds a credential here that it can read". It is what the key field asks
	// before showing a mask, and it is deliberately NOT `Status == KeyPresent`.
	//
	// The gap is FromEnv, and it is the case that would lie. An env-var key is present, dictation
	// will happily use it, and the app never stored it — so a mask there would say "I have your key"
	// about a credential this app cannot read, cannot delete, and did not save. The comment on
	// FromEnv above already promises the opposite: that the field looks empty precisely so the user
	// can tell the two apart.
	//
	// Unreadable is excluded for the other half of that reasoning: "I could not check" is not "there
	// is one", and masking on it would tell the user their key is safely stored at the exact moment
	// the credentials file cannot be read.
	//
	// NOT reused for the "Borrar clave" button, whose condition only looks similar: that one keys off
	// the CARD's availability (settings.ts:579), not the slot's. Collapsing them would change
	// behaviour quietly.
	Stored bool `json:"stored"`
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
	Region string `json:"region"`
	// AzureService, AzureOpenAiResource and AzureOpenAiDeployment are the second Azure product's
	// configuration. Carried because the connection state depends on which sub-service is selected:
	// the realtime endpoint is addressed by resource name, the speech one by region.
	AzureService          string `json:"azureService"`
	AzureOpenAiResource   string `json:"azureOpenAiResource"`
	AzureOpenAiDeployment string `json:"azureOpenAiDeployment"`
	OpenAiModel           string `json:"openAiModel"`
	Mode                  string `json:"mode"`
	TriggerKey            string `json:"triggerKey"`
	Appearance            string `json:"appearance"`
	AppLanguage           string `json:"appLanguage"`
	// Locale is the language actually IN EFFECT, which is not the same as AppLanguage: the default
	// is empty, meaning "follow the system", and only Go can read the system's answer (NSLocale
	// through cgo — an app launched from Finder inherits no LANG). The page needs the resolved value
	// to translate itself, and it cannot work it out.
	Locale         string              `json:"locale"`
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
	// Providers is every engine the picker offers, each flagged with whether it can actually
	// dictate. AzureRegions is the region dropdown's options. Both are static, and both are sent
	// anyway: they are the lists the page has to render, and duplicating them in TypeScript would
	// put the same facts in two languages — the duplication this payload exists to remove.
	Providers    []ProviderOption  `json:"providers"`
	AzureRegions []settings.Region `json:"azureRegions"`
	// Connections is the Conexiones list: one row per engine with its kind line and its readiness
	// state, computed by store.ConnectionRows.
	//
	// It replaces status text the page used to invent. Those two are not the same thing: the real
	// model distinguishes "configured but not selected" from "nothing to configure" from "cannot run
	// on this machine at all", and a page guessing from key presence alone collapsed them.
	Connections []store.ConnectionRow `json:"connections"`
	// ProviderHint is the paragraph under the picker describing the ACTIVE engine.
	ProviderHint string `json:"providerHint"`
	// LanguageControls is one entry per language slot: the capability, its copy, the options it may
	// offer and what is currently chosen.
	//
	// The page draws a control from this and decides nothing. That matters here more than elsewhere:
	// the three capabilities take three different VALUE SPACES — full locales, base codes, or either
	// with an "auto" sentinel — and a page picking the list itself is how a picker ends up offering
	// "es" to an engine that needs "es-CO".
	LanguageControls []LanguageControl `json:"languageControls"`
	// Trigger is the state of the activation-shortcut control.
	Trigger TriggerControl `json:"trigger"`
	// Revision orders snapshots so a page painting from several producers can drop the stale ones.
	//
	// It is needed because paint() repaints the WHOLE window from one payload, and payloads arrive
	// from paths that do not queue against each other: the Conexiones queue, Sistema, idiomas,
	// onboarding and the permissions refresh. Without an order, a slow snapshot lands last and puts
	// back a state that has already been superseded.
	//
	// WHAT IT GUARANTEES, precisely: a snapshot that STARTED earlier never overwrites one that
	// started later — the counter is taken at the beginning of Payload(). It is not a total order over
	// the data: Payload reads the settings file, the credentials and the devices at different instants,
	// so a snapshot that started earlier can still hold a fresher value for one field.
	Revision uint64 `json:"revision"`
}

// TriggerControl is everything the shortcut control needs to draw itself.
type TriggerControl struct {
	// Key is the stored accelerator, or "" for no shortcut.
	Key string `json:"key"`
	// Label is the short human form: "fn (Globe)", "⌘⇧D", or "Sin atajo".
	Label string `json:"label"`
	// SupportsHold is whether hold-to-talk is possible at all with this trigger. The interface must
	// DISABLE that choice rather than accept it and downgrade underneath the user.
	SupportsHold bool `json:"supportsHold"`
	// AllowedModes is the modes this trigger can deliver.
	AllowedModes []string `json:"allowedModes"`
	// Note is the sentence under the control, which depends on the trigger actually configured.
	Note string `json:"note"`
	// ResetLabel is what the reset button offers, and ShowReset whether offering it means anything —
	// it is hidden when it would be a no-op, which on macOS means the trigger is already fn.
	ResetLabel string `json:"resetLabel"`
	ShowReset  bool   `json:"showReset"`
}

// triggerControl builds the shortcut control's state.
//
// The reset button is macOS-specific for a reason worth keeping: off macOS "Restaurar fn" is not
// merely useless, it is impossible — ValidateTriggerKey refuses fn there, so the button could only
// ever produce an error.
func triggerControl(cfg store.Settings) TriggerControl {
	mac := runtime.GOOS == "darwin"
	resetLabel := "Restaurar fn"
	showReset := mac && !store.IsFnTrigger(cfg.TriggerKey)
	if !mac {
		resetLabel = ""
		showReset = false
	}
	return TriggerControl{
		Key:          cfg.TriggerKey,
		Label:        store.FormatTrigger(cfg.TriggerKey),
		SupportsHold: store.SupportsHold(cfg.TriggerKey),
		AllowedModes: store.AllowedModes(cfg.TriggerKey),
		Note:         store.TriggerNote(cfg.TriggerKey),
		ResetLabel:   resetLabel,
		ShowReset:    showReset,
	}
}

// LanguageControl is everything the page needs to draw one slot's language control.
type LanguageControl struct {
	Slot string               `json:"slot"`
	Kind store.CapabilityKind `json:"kind"`
	// Max is the ceiling for the multi capability; zero for the others.
	Max      int                    `json:"max"`
	Label    string                 `json:"label"`
	Desc     string                 `json:"desc"`
	Options  []store.LanguageOption `json:"options"`
	Selected []string               `json:"selected"`
}

// languageControls builds one control per slot from the settings already loaded.
func languageControls(cfg store.Settings) []LanguageControl {
	byslot := languages(cfg)
	out := make([]LanguageControl, 0, len(store.AllLanguageSlots))
	for _, slot := range store.AllLanguageSlots {
		rule := store.LangCapabilityFor(slot)
		copy := store.LanguageCopyFor(slot)
		out = append(out, LanguageControl{
			Slot:     slot,
			Kind:     rule.Kind,
			Max:      rule.Max,
			Label:    copy.Label,
			Desc:     copy.Desc,
			Options:  store.LanguageOptionsFor(slot),
			Selected: byslot[slot],
		})
	}
	return out
}

// Bootstrap computes the payload. Every dependency that touches the machine is a field
// rather than a direct call, because all three are untestable in place: the credentials do not
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
	// caps reports what this machine can run. Nil means ask the machine — see hostCaps, which
	// exists because Bootstrap is also built as a literal, and a nil call there would panic.
	caps func() store.HostCapabilities
	// systemLocale is the OS language, for the "follow the system" default. A field for the same
	// reason as the rest: it reads NSLocale through cgo, so a test cannot arrange an answer.
	systemLocale func() string

	// revision stamps every snapshot. Atomic because Wails dispatches each bound call on its own
	// goroutine, so two payloads can be under construction at once — which is the situation the
	// stamp exists to sort out.
	revision atomic.Uint64
}

// NewBootstrap wires the real machine.
func NewBootstrap(st *store.Store) *Bootstrap {
	return &Bootstrap{
		store:        st,
		keyStatus:    st.KeyStatusFor,
		perms:        livePermissions,
		systemLocale: macos.SystemLocale,
		devices:      audio.ListInputDevices,
	}
}

// ProviderOption is one entry in the engine picker.
type ProviderOption struct {
	ID string `json:"id"`
	// Available is whether the engine can dictate today. The unported ones are still LISTED —
	// hiding them would make the app look like it supports less than it will — but the picker
	// must show them as unavailable, because selecting one replaces a working engine with one
	// that fails at the next dictation. SetProvider refuses them too.
	Available bool `json:"available"`
	// State and Selected describe this exact picker choice. Azure contributes two choices backed by
	// one stored provider ID, so the page cannot derive either fact from ConnectionRows["azure"].
	State    store.ConnectionState `json:"state"`
	Selected bool                  `json:"selected"`
}

func providerOptions(cfg store.Settings, keys map[store.KeySlot]bool, caps store.HostCapabilities) []ProviderOption {
	out := make([]ProviderOption, 0, len(store.AllProviders)+1)
	for _, id := range store.AllProviders {
		if id == "azure" {
			out = append(out,
				providerOption("azure-speech", "azure", "speech", cfg, keys, caps),
				providerOption("azure-openai", "azure", "openai", cfg, keys, caps),
			)
			continue
		}
		out = append(out, providerOption(id, id, "", cfg, keys, caps))
	}
	return out
}

func providerOption(id, provider, azureService string, cfg store.Settings, keys map[store.KeySlot]bool, caps store.HostCapabilities) ProviderOption {
	selected := cfg.Provider == provider
	candidate := cfg
	if provider == "azure" {
		selected = selected && (cfg.AzureService == azureService || cfg.AzureService == "" && azureService == "speech")
		candidate.AzureService = azureService
	}
	// ConnectionStateFor uses Settings.Provider to decide active vs configured. Replace it only for
	// the inactive picker choice, leaving every readiness rule in the store as the single authority.
	if selected {
		candidate.Provider = provider
	} else {
		candidate.Provider = ""
	}
	return ProviderOption{
		ID:        id,
		Available: store.IsAvailableProvider(provider),
		State:     store.ConnectionStateFor(provider, candidate, keys, caps),
		Selected:  selected,
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
	// Stamped FIRST, before anything is read. Taken at the end it would order snapshots by when they
	// finished, and the whole point is the opposite: a snapshot that started earlier must lose to one
	// that started later, however long each took to assemble.
	revision := b.revision.Add(1)

	// Named cfg, not settings: the internal/settings package is imported here.
	cfg := b.store.LoadSettings()

	devices, err := b.devices()
	if devices == nil {
		devices = []audio.InputDevice{}
	}
	devicesError := ""
	if err != nil {
		devicesError = err.Error()
	}

	keys := b.keyStates()
	caps := b.hostCaps()

	payload := SettingsPayload{
		Provider:              cfg.Provider,
		Region:                cfg.Region,
		AzureService:          cfg.AzureService,
		AzureOpenAiResource:   cfg.AzureOpenAiResource,
		AzureOpenAiDeployment: cfg.AzureOpenAiDeployment,
		OpenAiModel:           cfg.OpenAiModel,
		Mode:                  cfg.Mode,
		TriggerKey:            cfg.TriggerKey,
		Appearance:            cfg.Appearance,
		AppLanguage:           cfg.AppLanguage,
		Locale:                string(b.locale(cfg)),
		Onboarded:             cfg.Onboarded,
		LanguageBySlot:        languages(cfg),
		InputDeviceID:         cfg.InputDeviceID,

		Revision:     revision,
		Keys:         keys,
		Permissions:  b.perms(),
		InputDevices: devices,
		DevicesError: devicesError,
		DataDir:      b.store.Dir(),
		Providers:    providerOptions(cfg, presenceMap(keys), caps),
		AzureRegions: settings.Regions,
		// Computed from the SAME key states the payload reports, not from a second read: two reads
		// could disagree, and a row saying "Sin configurar" beside a field saying "clave guardada" is
		// the kind of contradiction that makes a user distrust the whole screen.
		Connections:      store.ConnectionRows(cfg, presenceMap(keys), caps),
		ProviderHint:     store.ProviderHint(cfg.Provider, cfg.AzureService),
		LanguageControls: languageControls(cfg),
		Trigger:          triggerControl(cfg),
	}
	// LAST, so nothing downstream can put Spanish back. Every rule above emits Spanish because the
	// catalogue's keys ARE Spanish source strings — see i18n_payload.go for why translating here
	// beats threading a locale through all of them.
	translatePayload(&payload)
	return payload
}

// presenceMap reduces the key states to what the connection model needs: which slots hold a usable
// credential. "Unreadable" counts as absent HERE, and only here — the credentials could not be
// consulted, so the honest thing for a readiness badge is not to claim readiness. The key field
// itself still shows the three-way state, which is where that distinction matters.
func presenceMap(states []KeyState) map[store.KeySlot]bool {
	out := make(map[store.KeySlot]bool, len(states))
	for _, k := range states {
		out[store.KeySlot(k.Slot)] = k.Status == store.KeyPresent
	}
	return out
}

// hostCapabilities is what this machine can actually run.
//
// Every field is optional by design: an unknown condition must not disqualify an engine. So a
// helper that cannot be found is reported as a definite false, while a version that cannot be read
// stays zero and the model ignores it.
// hostCaps is what the model should use: the injected capabilities when a caller supplied them,
// the real machine otherwise.
//
// The nil check is not defensive habit. Bootstrap is constructed as a literal in the test helpers,
// so a bare b.caps() would panic there — and a seam that only works when someone remembered to
// fill it in is a trap for the next person who adds a field.
func (b *Bootstrap) hostCaps() store.HostCapabilities {
	if b.caps != nil {
		return b.caps()
	}
	return b.hostCapabilities()
}

func (b *Bootstrap) hostCapabilities() store.HostCapabilities {
	return store.HostCapabilities{
		Platform: runtime.GOOS,
		OSMajor:  macos.ProductVersionMajor(),
		Helpers: map[string]bool{
			"whisper-stt": HelperPath("whisper-stt") != "",
			"macos-stt":   HelperPath("macos-stt") != "",
		},
	}
}

// SettingsService is the Wails service the Ajustes page calls. It exists as its own type,
// rather than binding Bootstrap directly, because everything the page needs to WRITE
// (choose an engine, store a key, pick a device) hangs off this same object next — and a
// service's exported methods are its public API to the webview, so Bootstrap's internals
// must not be part of it.
type SettingsService struct {
	bootstrap *Bootstrap

	// setSecret / deleteSecret / getSecret override the credential store. Only the tests set them — see
	// secretWriter and secretReader for why the real ones cannot run in a unit test.
	setSecret    func(store.KeySlot, string) error
	deleteSecret func(store.KeySlot) error
	getSecret    func(store.KeySlot) (string, error)

	// probeClient / probeTimeout override the network for "probar conexión". A unit test must not
	// depend on Azure being reachable, nor wait fifteen seconds to check that a deadline works.
	probeClient  azure.Doer
	probeTimeout time.Duration

	// readinessMu serialises everything that can change whether an engine is usable, and the decision
	// that acts on it.
	//
	// A COUNTER IS NOT ENOUGH, which is worth stating because a counter was tried: bumping on the way
	// in and out cannot express two setters overlapping — two entries leave it even again, reading as
	// "nothing is happening" while both are half done. And no counter closes the gap between the last
	// comparison and the write itself. A lock held across decide-and-commit closes both.
	//
	// Setters hold this across their whole body, including the credential write. That is cheap now that
	// the store is a file — it used to mean up to ten seconds of Keychain — and the alternative is a
	// launch check that silently reverts a configuration the user finished half a second ago.
	readinessMu sync.Mutex
	// readiness counts completed readiness changes. Guarded by readinessMu, so a plain integer says
	// what an atomic one could not: the count can only move while somebody holds the lock.
	readiness uint64

	// probers overrides the connection-test registry. Only the tests set it, and per instance rather
	// than by mutating the package map, which two probes in flight would race on.
	probers map[store.KeySlot]prober
	// azureOpenAIProbe replaces the realtime socket probe in tests. The production path always uses
	// azureopenai.TestConnection; keeping this per service avoids global test races.
	azureOpenAIProbe func(ctx context.Context, key, resource, deployment string) stt.ProbeResult

	// defaultProblem overrides the check for whether the fallback engine can run. Only the tests set
	// it: the real one looks for a 465 MB model file on disk.
	defaultProblem func() error

	// log records diagnostic lines, nil when nothing wired one. It exists so a probe can report
	// which configuration it used: a button inside a Wails webview cannot be driven from a script,
	// so without this there is no way to verify that from outside. NEVER called with a secret.
	log func(tag, msg string)

	// onModeChanged pushes a new mode into the running controller. Persisting alone is not enough:
	// the engine reads the mode once, at construction.
	onModeChanged func(mode string)
	// onTriggerChanged re-registers the shortcut listener. Persisting alone is not enough either: the
	// fn listener is a child process started at launch from the stored trigger, so without this the
	// new shortcut is saved while the old one keeps working.
	onTriggerChanged func(trigger string) error
	// onAppearanceChanged repaints the live windows. Same reason again: the appearance is applied once
	// at construction, so a persisted-only change waits for the next launch.
	onAppearanceChanged func(appearance string)
	// onLanguageChanged tells the parts of the app that are NOT the settings page. It is the same
	// class of problem as the three above — a persisted-only change waits for the next launch — and
	// it has two victims: the overlay is a separate window created once, and the tray menu is native
	// and built at startup.
	onLanguageChanged func(locale string)
}

// LiveHooks lets main connect the running engine and listener without this package importing Wails.
//
// PASSED AT CONSTRUCTION, not through setter methods, and that is not a style choice: Wails binds
// every EXPORTED method of a service to the webview. An OnModeChanged method would have been
// published to anything running script in that window — and it takes a Go func, which cannot be
// bound at all.
type LiveHooks struct {
	// ModeChanged pushes a new mode into the running controller.
	ModeChanged func(mode string)
	// TriggerChanged re-registers the shortcut listener, reporting why if it could not.
	TriggerChanged func(trigger string) error
	// AppearanceChanged applies the light/dark preference to the open windows.
	AppearanceChanged func(appearance string)
	// LanguageChanged tells the overlay window and the tray that the interface language moved.
	LanguageChanged func(locale string)
	// Log records a diagnostic line. Passed in rather than reached for because this package must
	// not decide how the app writes its log — and because a test wants it silent. NEVER called
	// with a secret or with transcript text.
	Log func(tag, msg string)
}

func NewSettingsService(st *store.Store, hooks LiveHooks) *SettingsService {
	return &SettingsService{
		bootstrap:           NewBootstrap(st),
		onModeChanged:       hooks.ModeChanged,
		onTriggerChanged:    hooks.TriggerChanged,
		onAppearanceChanged: hooks.AppearanceChanged,
		onLanguageChanged:   hooks.LanguageChanged,
		log:                 hooks.Log,
	}
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
// it outranks credentials that cannot be read, which is the whole reason the hatch exists. If the
// two ever disagreed the UI would contradict what dictation actually does. A slot answered by the
// environment is never looked up at all, which also means it costs nothing.
//
// The remaining slots are read CONCURRENTLY. This was a REQUIREMENT under the Keychain backend —
// five sequential reads each hitting a three-second timeout meant fifteen seconds with the Ajustes
// page blank — and is now merely harmless: the reads share one mutex over one file, so they
// serialise anyway and finish in microseconds. Kept because the fan-out costs nothing and the
// case is one timeout instead of five.
func (b *Bootstrap) keyStates() []KeyState {
	out := make([]KeyState, len(store.AllKeySlots))
	var wg sync.WaitGroup

	for i, slot := range store.AllKeySlots {
		out[i] = KeyState{Slot: string(slot), Available: store.IsAvailableKeySlot(slot)}
		if _, _, set := envCredential(slot); set {
			// In force, so the stored credentials are not consulted: whatever is in them is not what dictation
			// would read. Whether it can AUTHENTICATE is a second question — a variable holding
			// whitespace overrides everything and works for nothing, so reporting it as present would
			// put "Conectado" on a card whose engine cannot dictate.
			out[i].FromEnv = true
			if envCredentialUsable(slot) {
				out[i].Status = store.KeyPresent
			} else {
				out[i].Status = store.KeyAbsent
			}
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

	// Derived AFTER the fan-out, in one place, rather than inside each branch above. The env branch
	// `continue`s and the stored branch finishes in a goroutine, so setting it per-branch would mean
	// writing the same rule twice — and writing it inside the goroutine would be a race with this
	// loop's read.
	for i := range out {
		out[i].Stored = out[i].Status == store.KeyPresent && !out[i].FromEnv
	}
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

// locale is the interface language in effect for this snapshot.
//
// Resolved ONCE per payload rather than per string: every piece of wording in one snapshot has to
// describe the same world, and a language change landing halfway through would produce a payload
// that is half translated.
func (b *Bootstrap) locale(cfg store.Settings) i18n.Locale {
	system := ""
	if b.systemLocale != nil {
		system = b.systemLocale()
	}
	return i18n.ResolveLocale(cfg.AppLanguage, system)
}
