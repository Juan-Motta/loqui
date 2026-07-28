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
)

// appDirName is the folder under Application Support. Read the package comment before
// changing it — a case variant of "loqui" silently shares the Electron app's data.
const appDirName = "LoquiGo"

// electronAppDirName is what the Electron build uses. Kept here only so the guard test can
// assert the two can never resolve to the same directory.
const electronAppDirName = "loqui"

// Settings is the persisted, non-secret configuration. A subset of the Electron model for
// now: the fields the currently-ported code actually reads. The rest arrive with their
// providers and their UI, and unknown keys already in the file survive a round trip
// because loading merges onto defaults rather than replacing them.
type Settings struct {
	// Provider is the active STT engine.
	Provider string `json:"provider"`
	// Region is the Azure Speech region.
	Region string `json:"region"`
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
	// Onboarded is whether the tutorial has been completed or skipped. Its own flag
	// because the default engine works out of the box, so "not configured" cannot stand
	// in for "hasn't seen the tutorial".
	Onboarded bool `json:"onboarded"`
}

// DefaultSettings mirrors the Electron defaults.
//
// The default provider is local whisper, not Azure: it needs no account, no key and no
// network, so a fresh install can dictate before the user has configured anything.
func DefaultSettings() Settings {
	return Settings{
		Provider: "whisper",
		Region:   "",
		LanguageBySlot: map[string][]string{
			"azure-speech": {"es-CO", "en-US"},
			"macos":        {"es-CO"},
			"whisper":      {"auto"},
			// The cloud providers take ONE optional language, or none at all to
			// auto-detect — which is the default, deliberately. Note that for xAI the
			// parameter only controls how numbers and units are written out; the model
			// transcribes any supported language either way.
			"grok": {"auto"},
		},
		Mode:       "hold",
		TriggerKey: "fn",
		Appearance: "system",
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

// SaveSettings writes the settings atomically: a crash mid-write must not leave a
// truncated file that reads as "reset everything to defaults".
func (s *Store) SaveSettings(settings Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.SettingsPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.SettingsPath())
}

// LanguagesFor returns the dictation languages for a slot, never empty.
func (s *Store) LanguagesFor(slot string) []string {
	settings := s.LoadSettings()
	if langs, ok := settings.LanguageBySlot[slot]; ok && len(langs) > 0 {
		return langs
	}
	if langs, ok := DefaultSettings().LanguageBySlot[slot]; ok {
		return langs
	}
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
