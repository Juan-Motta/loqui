package app

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"

	"github.com/Juan-Motta/loqui-go/internal/i18n"
	"github.com/Juan-Motta/loqui-go/internal/macos"
	"github.com/Juan-Motta/loqui-go/internal/store"
)

// AboutService answers the Acerca de view.
//
// Read-only and stateless by design: nothing here can change the app, so the view cannot be a way to
// mutate settings by accident. The interesting part — deciding what the rows say — is BuildAbout,
// which is pure and tested. This file only asks the machine.
type AboutService struct {
	store *store.Store
}

func NewAboutService(st *store.Store) *AboutService {
	return &AboutService{store: st}
}

// aboutLocale is the language the row keys are shown in.
//
// Read on EVERY call, not captured: this view can be opened after a language change, and the About
// panel is precisely where someone goes to copy details into a bug report — in whatever language they
// are reading the app in. The store is the same instance the rest of the app uses.
func (s *AboutService) aboutLocale() i18n.Locale {
	if s.store == nil {
		return i18n.Default
	}
	return i18n.ResolveLocale(s.store.LoadSettings().AppLanguage, macos.SystemLocale())
}

// bundleVersion reads CFBundleShortVersionString from the running app's own Info.plist, and reports
// whether we are inside a bundle at all.
//
// The plist is found relative to the EXECUTABLE, not the working directory: an app launched from
// Finder has its cwd at /, so a relative path resolves nowhere — the same trap already documented in
// WhisperModelPath. Both values come from the same place so they cannot disagree.
func bundleVersion() (version string, packaged bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	if !isPackagedPath(exe) {
		return "", false
	}
	// .../loqui.app/Contents/MacOS/loqui -> .../loqui.app/Contents/Info.plist
	data, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "..", "Info.plist"))
	if err != nil {
		// Packaged but unreadable: say so rather than pretending it's a dev run, which is what
		// versionLabel turns into the "no se pudo leer" message.
		return "", true
	}
	return plistShortVersion(data), true
}

// wailsVersion digs the framework version out of the build info.
//
// Taken from the module graph rather than written down anywhere: a hardcoded string would go stale at
// the next upgrade, silently, and this row exists precisely to be trusted in a bug report.
func wailsVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, dep := range info.Deps {
		if dep.Path == "github.com/wailsapp/wails/v3" {
			return dep.Version
		}
	}
	return ""
}

// Info is what the page calls. Never errors: a machine that will not answer a question yields an em
// dash in that row, which is more useful than failing the whole view over one missing fact.
func (s *AboutService) Info() AboutInfo {
	version, packaged := bundleVersion()
	f := AboutFacts{
		Version:      version,
		Packaged:     packaged,
		OSName:       "macOS",
		OSVersion:    macos.ProductVersion(),
		Arch:         runtime.GOARCH,
		Locale:       macos.SystemLocale(),
		GoVersion:    runtime.Version(),
		WailsVersion: wailsVersion(),
	}
	if s.store != nil {
		f.DataDir = s.store.Dir()
		f.SettingsFile = s.store.SettingsPath()
		f.HistoryFile = s.store.HistoryPath()
	}
	// The locale goes IN rather than the rows being translated on the way out: the version label
	// interpolates a value, so its finished form can never be a catalogue key.
	//
	// The VALUES are never translated — a macOS version, an architecture, file paths, a locale code.
	// Translating those would corrupt the very details this panel exists to be copied from.
	return BuildAbout(s.aboutLocale(), f)
}
