package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Juan-Motta/loqui-go/internal/history"
)

// The bug this guards against actually happened: the directory was named "Loqui", and on
// macOS's case-insensitive filesystem that is the SAME directory as the Electron app's
// "loqui" — same inode. The port read the Electron app's settings and would have written
// over them. A case-sensitive comparison would have passed happily, so the test has to
// compare the way the filesystem does.
func TestAppDirCannotCollideWithTheElectronApp(t *testing.T) {
	if strings.EqualFold(appDirName, electronAppDirName) {
		t.Fatalf("appDirName %q is a case variant of the Electron app's %q — on a "+
			"case-insensitive filesystem these are one directory, and the two apps would "+
			"share and corrupt each other's settings", appDirName, electronAppDirName)
	}
}

// A store rooted at a temp dir, so the tests never touch the real one.
func testStore(t *testing.T) *Store {
	t.Helper()
	return &Store{dir: t.TempDir()}
}

func TestLoadSettingsReturnsDefaultsWhenAbsent(t *testing.T) {
	got := testStore(t).LoadSettings()
	want := DefaultSettings()
	if got.Provider != want.Provider || got.Mode != want.Mode || got.TriggerKey != want.TriggerKey {
		t.Errorf("got %+v, want the defaults %+v", got, want)
	}
}

// Refusing to start because a JSON file was hand-edited would leave the user unable to
// even open the settings screen that would fix it.
func TestLoadSettingsFallsBackOnCorruptJSON(t *testing.T) {
	s := testStore(t)
	if err := os.WriteFile(s.SettingsPath(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := s.LoadSettings(); got.Provider != DefaultSettings().Provider {
		t.Errorf("provider = %q, want the default after corrupt JSON", got.Provider)
	}
}

// A file written by an older version must not blank the fields it never knew about.
func TestLoadSettingsMergesOntoDefaults(t *testing.T) {
	s := testStore(t)
	partial := `{"provider":"azure","region":"eastus"}`
	if err := os.WriteFile(s.SettingsPath(), []byte(partial), 0o600); err != nil {
		t.Fatal(err)
	}

	got := s.LoadSettings()
	if got.Provider != "azure" || got.Region != "eastus" {
		t.Errorf("stored values lost: %+v", got)
	}
	if got.TriggerKey != "fn" {
		t.Errorf("triggerKey = %q, want the default \"fn\" to survive a partial file", got.TriggerKey)
	}
	if len(got.LanguageBySlot) == 0 {
		t.Error("languageBySlot was blanked by a partial file")
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	s := testStore(t)
	in := DefaultSettings()
	in.Provider = "azure"
	in.Region = "brazilsouth"
	in.Mode = "toggle"

	if err := s.SaveSettings(in); err != nil {
		t.Fatal(err)
	}
	got := s.LoadSettings()
	if got.Provider != "azure" || got.Region != "brazilsouth" || got.Mode != "toggle" {
		t.Errorf("got %+v, want the saved values", got)
	}
}

// A crash mid-write must not leave a truncated file that reads as "reset to defaults".
func TestSaveSettingsLeavesNoTempFileBehind(t *testing.T) {
	s := testStore(t)
	if err := s.SaveSettings(DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.SettingsPath() + ".tmp"); err == nil {
		t.Error("the temp file was not renamed away")
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover temp file %q", e.Name())
		}
	}
}

func TestLanguagesForFallsBackPerSlot(t *testing.T) {
	s := testStore(t)
	if got := s.LanguagesFor("azure-speech"); len(got) != 2 || got[0] != "es-CO" {
		t.Errorf("got %v, want the azure-speech defaults", got)
	}
	// An unknown slot must still yield something usable rather than an empty list, which
	// would make the recognizer fail validation at dictation time.
	if got := s.LanguagesFor("nonexistent"); len(got) == 0 {
		t.Error("an unknown slot must not return an empty language list")
	}
}

// The cloud providers auto-detect by DEFAULT. This is a real behaviour change the Electron
// build made deliberately (../loqui/src/shared/languageSlots.ts:53): these slots used to be
// forced to the first global language even when the user had several configured.
func TestGrokDefaultsToAutoDetect(t *testing.T) {
	s := testStore(t)
	got := s.LanguagesFor("grok")
	if len(got) != 1 || got[0] != "auto" {
		t.Errorf("got %v, want [auto] — grok must auto-detect out of the box", got)
	}
}

func TestHistoryAppendListAndClear(t *testing.T) {
	s := testStore(t)
	for i, text := range []string{"uno", "dos", "tres"} {
		rec := history.MakeRecord(text, "es-CO", "hold", int64(100+i))
		if err := s.AppendHistory(rec); err != nil {
			t.Fatal(err)
		}
	}

	got := s.ListHistory(0)
	if len(got) != 3 {
		t.Fatalf("got %d records, want 3", len(got))
	}
	if got[0].Text != "tres" {
		t.Errorf("first record = %q, want the newest (\"tres\")", got[0].Text)
	}

	if limited := s.ListHistory(2); len(limited) != 2 {
		t.Errorf("limit ignored: got %d records", len(limited))
	}

	if err := s.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	if got := s.ListHistory(0); len(got) != 0 {
		t.Errorf("got %d records after clear, want 0", len(got))
	}
}

// Clearing an empty history is what the user gets when they press the button twice.
func TestClearHistoryOnAnAbsentFileSucceeds(t *testing.T) {
	if err := testStore(t).ClearHistory(); err != nil {
		t.Errorf("clearing an absent history returned %v, want nil", err)
	}
}

// One bad append — a crash mid-write, a full disk — must not make the whole history
// unreadable.
func TestListHistorySkipsMalformedLines(t *testing.T) {
	s := testStore(t)
	good, _ := json.Marshal(history.MakeRecord("bueno", "es-CO", "hold", 200))
	body := "{broken\n" + string(good) + "\n\n"
	if err := os.WriteFile(s.HistoryPath(), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got := s.ListHistory(0)
	if len(got) != 1 || got[0].Text != "bueno" {
		t.Errorf("got %+v, want just the readable record", got)
	}
}
