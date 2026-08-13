// Non-secret settings and the transcript history, on disk. Ported from the Electron
// build's src/main/configStore.ts and historyStore.ts.
//
// The file SHAPE is deliberately the same as Electron's —
// ~/Library/Application Support/<app>/settings.json and history.jsonl — so both remain
// readable while the port is in progress.
//
// THE DIRECTORY NAME IS NOT A COSMETIC CHOICE. The Electron app uses "loqui". This one
// cannot be "Loqui": macOS ships a case-INSENSITIVE filesystem, so those two names are the
// same directory — verified by inode. The first version of this file used "Loqui" and
// silently read the Electron app's settings.json, which is how this was found. Writing
// there would be worse than reading: the two apps validate the same keys differently, so
// one would corrupt the other's working install.
//
// Hence "LoquiGo", which matches the bundle id (com.jualopezmo.loquigo) and is not a case
// variant of anything. See TestAppDirCannotCollideWithTheElectronApp.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Juan-Motta/loqui-go/internal/history"
	"github.com/Juan-Motta/loqui-go/internal/settings"
)

// appDirName is the folder under Application Support. Read the package comment before
// changing it — a case variant of "loqui" silently shares the Electron app's data.
const appDirName = "LoquiGo"

// electronAppDirName is what the Electron build uses. Kept here only so the guard test can
// assert the two can never resolve to the same directory.
const electronAppDirName = "loqui"

// Settings is the persisted, non-secret configuration. A subset of the Electron model for
// now: the fields the currently-ported code actually reads. The rest arrive with their
// providers and their UI.
//
// KEYS THIS STRUCT DOES NOT MODEL SURVIVE A WRITE — see saveLocked, which merges onto the raw
// file rather than replacing it. That is load-bearing precisely BECAUSE this is a subset: the
// settings screen writes the whole file on every change, so without the merge the first click
// in Ajustes would silently delete every setting the port has not reached yet.
type Settings struct {
	// Provider is the active STT engine.
	Provider string `json:"provider"`
	// Region is the Azure Speech region.
	Region string `json:"region"`
	// AzureService selects which Azure product is in use: "speech" or "openai". They are separate
	// resources with separate keys and separate required fields, which is why the connection state
	// cannot be computed from the provider name alone.
	AzureService string `json:"azureService"`
	// AzureOpenAiResource and AzureOpenAiDeployment address the Azure OpenAI realtime endpoint,
	// which is named rather than regional — the reason "azure" has two sub-services here.
	AzureOpenAiResource   string `json:"azureOpenAiResource"`
	AzureOpenAiDeployment string `json:"azureOpenAiDeployment"`
	// AzureOpenAiModel is the base model behind the deployment. Azure lets users choose an unrelated
	// deployment name, so the request dialect cannot be inferred from AzureOpenAiDeployment.
	AzureOpenAiModel string `json:"azureOpenAiModel"`
	// OpenAiModel belongs to the public OpenAI provider. It must not reuse Azure's deployment name:
	// those are independent resources and changing one must never reroute the other.
	OpenAiModel string `json:"openAiModel"`
	// LanguageBySlot holds the dictation languages per provider slot. Per-slot because a
	// single global list only ever worked for Azure: every other provider silently used
	// just the first entry.
	LanguageBySlot map[string][]string `json:"languageBySlot"`
	// Mode is "hold" or "toggle".
	Mode string `json:"mode"`
	// TriggerKey is "fn" or an accelerator; empty means none configured.
	TriggerKey string `json:"triggerKey"`
	// InputDeviceID is the chosen microphone, empty for the system default.
	InputDeviceID string `json:"inputDeviceId"`
	// Appearance is "system", "light" or "dark".
	Appearance string `json:"appearance"`
	// AppLanguage is the interface locale, empty to follow the OS.
	AppLanguage string `json:"appLanguage"`
	// AutoUpdateChecks controls non-blocking background release checks. Manual checks remain
	// available when this is false.
	AutoUpdateChecks bool `json:"autoUpdateChecks"`
	// Onboarded is whether the tutorial has been completed or skipped. Its own flag
	// because the default engine works out of the box, so "not configured" cannot stand
	// in for "hasn't seen the tutorial".
	Onboarded bool `json:"onboarded"`
}

// AllLanguageSlots is every slot that carries its own dictation language list.
//
// A LANGUAGE SLOT IS NOT A PROVIDER. The provider is what Settings.Provider holds and what
// dictation.go switches on ("azure", "macos", "whisper", "grok"); "azure-speech" and
// "azure-openai" are two Azure subservices that need separate language lists under the single
// "azure" provider. Nor is this AllKeySlots: the local engines take a language and have no
// credential, and the cloud slots have a credential. Anything that needs one language list per
// slot enumerates THIS; nothing may treat it as a list of engines to offer in the picker.
var AllLanguageSlots = []string{
	"whisper",
	"macos",
	"azure-speech",
	"azure-openai",
	"openai",
	"grok",
	"elevenlabs",
}

// DefaultProvider is the engine the app falls back to.
//
// Whisper, and not because it happens to be first in the list: it is the only one that needs neither a
// credential nor a network, so choosing it for the user commits them to nothing. Named here so the
// fallback and the defaults cannot drift apart.
const DefaultProvider = "whisper"

// DefaultSettings mirrors the Electron defaults.
//
// The default provider is local whisper, not Azure: it needs no account, no key and no
// network, so a fresh install can dictate before the user has configured anything.
func DefaultSettings() Settings {
	return Settings{
		Provider: DefaultProvider,
		Region:   "",
		LanguageBySlot: map[string][]string{
			"azure-speech": {"es-CO", "en-US"},
			"macos":        {"es-CO"},
			"whisper":      {"auto"},
			// The cloud providers take ONE optional language, or none at all to
			// auto-detect — which is the default, deliberately. Note that for xAI the
			// parameter only controls how numbers and units are written out; the model
			// transcribes any supported language either way.
			//
			// All cloud slots are explicit because the settings UI paints a language control per slot.
			// A missing slot falls through LanguagesFor to the "en-US" last resort, which would silently
			// pin a cloud engine to English instead of auto-detecting.
			"grok":         {"auto"},
			"azure-openai": {"auto"},
			"openai":       {"auto"},
			"elevenlabs":   {"auto"},
		},
		Mode:             "hold",
		TriggerKey:       "fn",
		Appearance:       "system",
		AutoUpdateChecks: true,
		// Matching the Electron defaults so a settings.json written by either build reads the same.
		AzureService:          "speech",
		AzureOpenAiDeployment: "gpt-realtime-whisper",
		AzureOpenAiModel:      settings.AzureOpenAIRealtimeWhisper,
		OpenAiModel:           "gpt-realtime-whisper",
	}
}

// Store is the on-disk state. Safe for concurrent use.
type Store struct {
	dir string
	mu  sync.RWMutex
}

// New opens (and creates) the app's data directory.
func New() (*Store, error) {
	base, err := os.UserConfigDir() // ~/Library/Application Support on macOS
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(base, appDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("store: cannot create %s: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// NewAt opens a store rooted at an explicit directory, for tests in other packages: the real
// New writes to ~/Library/Application Support, which a test must never touch.
func NewAt(dir string) *Store { return &Store{dir: dir} }

// Dir is the data directory, shown in the About view and needed for bug reports.
func (s *Store) Dir() string { return s.dir }

// SettingsPath is the settings file.
func (s *Store) SettingsPath() string { return filepath.Join(s.dir, "settings.json") }

// HistoryPath is the append-only transcript log.
func (s *Store) HistoryPath() string { return filepath.Join(s.dir, "history.jsonl") }

// LoadSettings reads the settings, falling back to defaults.
//
// A missing OR CORRUPT file yields pure defaults rather than an error. Refusing to start
// because a JSON file was hand-edited would leave the user with an app that cannot even
// open its own settings screen to fix itself.
func (s *Store) LoadSettings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.loadLocked()
}

// loadLocked is LoadSettings without taking the lock, for callers that already hold it.
func (s *Store) loadLocked() Settings {
	out := DefaultSettings()
	raw, err := os.ReadFile(s.SettingsPath())
	if err != nil {
		return out
	}
	// Unmarshalling ONTO the defaults is what makes a partial file safe: absent keys keep
	// their default instead of becoming a zero value, so a file written by an older
	// version does not silently blank the trigger key or the language list.
	if err := json.Unmarshal(raw, &out); err != nil {
		return DefaultSettings()
	}
	if out.LanguageBySlot == nil {
		out.LanguageBySlot = DefaultSettings().LanguageBySlot
	}
	return out
}

// UpdateSettings applies a change to the settings as ONE transaction, and is what every setter
// must use.
//
// Load-then-Save is not enough. Those are two separate critical sections — LoadSettings takes the
// read lock, SaveSettings the write lock — so two callers can both read version A, and whichever
// saves second silently erases the other's change. Wails dispatches each binding call on its own
// goroutine, so two quick actions in the settings window are all it takes. `-race` cannot catch
// it either: nothing races on memory, the second write is simply built from stale data.
//
// The mutation runs under the write lock, so it must not call back into the Store.
func (s *Store) UpdateSettings(mutate func(*Settings) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	settings := s.loadLocked()
	if err := mutate(&settings); err != nil {
		return err // the caller rejected the change; nothing is written
	}
	return s.saveLocked(settings)
}

// SaveSettings writes the settings atomically: a crash mid-write must not leave a
// truncated file that reads as "reset everything to defaults".
func (s *Store) SaveSettings(settings Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.saveLocked(settings)
}

// saveLocked is SaveSettings without taking the lock, for callers that already hold it.
//
// It MERGES onto whatever is already in the file instead of overwriting it. Settings is a declared
// subset of the model, so marshalling the struct alone drops every key it does not name — and now
// that the UI writes settings, that would happen on the user's first click, taking with it anything
// an older or newer version of the app had stored. Known fields always win; unknown ones are carried
// through untouched.
func (s *Store) saveLocked(settings Settings) error {
	merged, err := mergeOntoRaw(s.SettingsPath(), settings)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.SettingsPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.SettingsPath())
}

// mergeOntoRaw returns the settings as a key/value map laid over whatever the file already holds.
//
// A missing or unparseable file yields just the settings: there is nothing to preserve, and refusing
// to write because the old file was corrupt would leave the user unable to fix it from the UI —
// which is the same reasoning LoadSettings applies in the other direction.
func mergeOntoRaw(path string, settings Settings) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	if raw, err := os.ReadFile(path); err == nil {
		// The error is ignored on purpose: a corrupt file simply contributes nothing to preserve.
		_ = json.Unmarshal(raw, &out)
	}
	// A file containing exactly `null` is the one input that makes Unmarshal SUCCEED and leave the
	// map nil — every other mismatch (an array, a string, a number) returns an error and leaves the
	// map alone. Assigning into a nil map panics, so without this a settings.json of `null` would
	// break every settings write from then on. Verified: "assignment to entry in nil map".
	if out == nil {
		out = map[string]json.RawMessage{}
	}

	// Round-tripping the struct through JSON is what keeps this honest: the keys written are exactly
	// the ones its tags declare, so a field added to Settings needs no change here.
	encoded, err := json.Marshal(settings)
	if err != nil {
		return nil, err
	}
	var known map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &known); err != nil {
		return nil, err
	}
	for k, v := range known {
		out[k] = v
	}
	return out, nil
}

// LanguagesFor returns the dictation languages for a slot, never empty.
func (s *Store) LanguagesFor(slot string) []string {
	return LanguagesIn(s.LoadSettings(), slot)
}

// LanguagesIn is LanguagesFor against settings that are ALREADY loaded, never empty.
//
// It exists so a caller that needs several slots reads the file once. LanguagesFor re-reads on
// every call, so asking it for all seven slots means eight loads of the same file — and worse,
// a save landing in between would compose the payload from two different versions of the
// settings, which is exactly what a one-shot snapshot is supposed to rule out.
func LanguagesIn(settings Settings, slot string) []string {
	if langs, ok := settings.LanguageBySlot[slot]; ok && len(langs) > 0 {
		return langs
	}
	if langs, ok := DefaultSettings().LanguageBySlot[slot]; ok {
		return langs
	}
	// Last resort for a slot nobody declared a default for. Reaching this is a bug in
	// DefaultSettings rather than a normal outcome — see AllLanguageSlots.
	return []string{"en-US"}
}

// AppendHistory adds one record.
func (s *Store) AppendHistory(rec history.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.HistoryPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

// ListHistory returns records newest first, at most limit of them.
//
// A malformed line is skipped rather than failing the whole read: one bad append (a crash
// mid-write, a disk full) must not make the entire history unreadable.
func (s *Store) ListHistory(limit int) []history.Record {
	s.mu.RLock()
	defer s.mu.RUnlock()

	raw, err := os.ReadFile(s.HistoryPath())
	if err != nil {
		return nil
	}
	var records []history.Record
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec history.Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		records = append(records, rec)
	}
	records = history.SortNewestFirst(records)
	if limit > 0 && len(records) > limit {
		records = records[:limit]
	}
	return records
}

// ClearHistory deletes every stored transcript.
func (s *Store) ClearHistory() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := os.Remove(s.HistoryPath())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// AllProviders is every engine Settings.Provider may name, in the order the picker shows them.
//
// NOT the language slots and NOT the key slots: this is what dictation.go switches on. "azure"
// is one provider whose two subservices ("azure-speech", "azure-openai") have their own language
// slots and their own credentials — see AllLanguageSlots.
var AllProviders = []string{"whisper", "macos", "azure", "openai", "grok", "elevenlabs"}

// IsKnownProvider reports whether a string names an engine at all. Says nothing about whether it
// works — see IsAvailableProvider.
func IsKnownProvider(provider string) bool {
	for _, known := range AllProviders {
		if known == provider {
			return true
		}
	}
	return false
}

// availableProviders is the engines that are actually ported and can dictate today. It must stay
// in step with the switch in app.(*Dictation).buildProvider, which is the code that would
// otherwise reject them at the worst possible moment.
var availableProviders = map[string]bool{
	"whisper":    true,
	"macos":      true,
	"azure":      true,
	"grok":       true,
	"elevenlabs": true,
	"openai":     true,
}

// IsAvailableProvider reports whether an engine can actually dictate.
//
// KNOWN IS NOT AVAILABLE, and conflating the two is a user-visible bug: the settings page lists
// six engines, and letting someone select one that buildProvider rejects replaces a WORKING engine
// with one that fails at the next dictation — far from the click that caused it. The picker greys
// the unavailable ones out from the payload; SetProvider refuses them so that is not merely
// cosmetic.
func IsAvailableProvider(provider string) bool { return availableProviders[provider] }
