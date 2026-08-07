package app

import (
	"github.com/Juan-Motta/loqui-go/internal/i18n"
	"regexp"
	"strings"
)

// The Acerca de view: a spec sheet the user can read out when something goes wrong.
//
// PORTED FROM the Electron renderAbout, rows and copy included, with ONE unavoidable change: the
// original listed Electron, Chromium, Node and V8, and none of those exist here. Go and Wails take
// their place. Everything else — the "Versión X · desarrollo" label, the em dash for anything the
// machine did not answer, and the three file paths — is the original's behaviour.
//
// The assembly lives in Go rather than in the page for the reason the rest of this port does: the
// page should not be deciding when a build counts as development, and a rule that is only in the DOM
// cannot be tested.

// AboutFacts is what the build and the machine can be asked. Every field is a plain string so a test
// can spoil exactly one of them, which is how the em-dash behaviour is checked without a real
// machine that refuses to answer.
type AboutFacts struct {
	Version      string
	Packaged     bool
	OSName       string
	OSVersion    string
	Arch         string
	Locale       string
	GoVersion    string
	WailsVersion string
	DataDir      string
	SettingsFile string
	HistoryFile  string
}

// AboutRow is one key/value line, the shape the page's .arow markup expects.
type AboutRow struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// AboutInfo is the whole view's content, already resolved.
type AboutInfo struct {
	VersionLabel string     `json:"versionLabel"`
	System       []AboutRow `json:"system"`
	Paths        []AboutRow `json:"paths"`
}

// shortVersionRe pulls CFBundleShortVersionString out of an Info.plist.
//
// Anchored on that exact key rather than on "the first version-looking string": the plist also
// carries CFBundleVersion (the build number), and confusing the two would show the user a number
// that matches nothing they can report. A real plist parser would be a dependency for one field.
var shortVersionRe = regexp.MustCompile(`<key>CFBundleShortVersionString</key>\s*<string>([^<]*)</string>`)

// plistShortVersion returns the app's user-facing version, or "" when the key isn't there.
func plistShortVersion(data []byte) string {
	m := shortVersionRe.FindSubmatch(data)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(string(m[1]))
}

// isPackagedPath says whether an executable path sits inside a .app bundle.
//
// Derived from the layout instead of a build flag on purpose — the "desarrollo" badge must be
// impossible to leave switched off in a release, and a constant someone edits by hand is exactly the
// thing that gets forgotten.
func isPackagedPath(exe string) bool {
	return strings.Contains(exe, ".app/Contents/MacOS/")
}

// dash keeps an unanswered question visible as "—" instead of collapsing into an empty cell that
// reads as a rendering bug.
func dash(v string) string {
	if v == "" {
		return "—"
	}
	return v
}

// versionLabel distinguishes three states that look alike but mean different things.
//
// An unpackaged run has no Info.plist to read, so a missing version there is EXPECTED and the badge
// alone says everything. A packaged app that cannot read its own plist is genuinely broken, and
// saying "Versión —" would understate that. The badge itself can never appear in a release build:
// it is derived from the bundle, not from a flag someone could forget to flip.
func versionLabel(locale i18n.Locale, f AboutFacts) string {
	t := func(key string, args map[string]string) string { return i18n.T(locale, key, args) }
	switch {
	case f.Version == "" && f.Packaged:
		return t("No se pudo leer la información de la app", nil)
	case f.Version == "":
		return t("desarrollo", nil)
	case f.Packaged:
		return t("Versión {v}", map[string]string{"v": f.Version})
	default:
		return t("Versión {v}", map[string]string{"v": f.Version}) + "  ·  desarrollo"
	}
}

// BuildAbout resolves the facts into the rows the view shows.
//
// The rows are FIXED and ordered even when their values are unknown: this is what someone pastes
// into a bug report, and a list whose length depends on the machine makes two reports impossible to
// compare line by line.
// The LOCALE is a parameter, not read from anywhere: this stays a pure function of its inputs, which
// is what makes it testable without a store or a machine. The wording genuinely depends on the
// language — "Versión {v}" interpolates a value, so the finished string can never be a catalogue key
// and the lookup has to happen here rather than at a boundary downstream.
func BuildAbout(locale i18n.Locale, f AboutFacts) AboutInfo {
	t := func(key string) string { return i18n.T(locale, key, nil) }
	return AboutInfo{
		VersionLabel: versionLabel(locale, f),
		System: []AboutRow{
			{Key: t("Sistema operativo"), Value: dash(f.OSName) + " " + dash(f.OSVersion) + " (" + dash(f.Arch) + ")"},
			{Key: t("Idioma del sistema"), Value: dash(f.Locale)},
			{Key: "Go", Value: dash(f.GoVersion)},
			{Key: "Wails", Value: dash(f.WailsVersion)},
		},
		Paths: []AboutRow{
			{Key: t("Carpeta de datos"), Value: dash(f.DataDir)},
			{Key: t("Ajustes"), Value: dash(f.SettingsFile)},
			{Key: t("Historial"), Value: dash(f.HistoryFile)},
		},
	}
}
