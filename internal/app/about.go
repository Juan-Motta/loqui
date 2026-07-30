package app

import (
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
func versionLabel(f AboutFacts) string {
	switch {
	case f.Version == "" && f.Packaged:
		return "No se pudo leer la información de la app"
	case f.Version == "":
		return "desarrollo"
	case f.Packaged:
		return "Versión " + f.Version
	default:
		return "Versión " + f.Version + "  ·  desarrollo"
	}
}

// BuildAbout resolves the facts into the rows the view shows.
//
// The rows are FIXED and ordered even when their values are unknown: this is what someone pastes
// into a bug report, and a list whose length depends on the machine makes two reports impossible to
// compare line by line.
func BuildAbout(f AboutFacts) AboutInfo {
	return AboutInfo{
		VersionLabel: versionLabel(f),
		System: []AboutRow{
			{Key: "Sistema operativo", Value: dash(f.OSName) + " " + dash(f.OSVersion) + " (" + dash(f.Arch) + ")"},
			{Key: "Idioma del sistema", Value: dash(f.Locale)},
			{Key: "Go", Value: dash(f.GoVersion)},
			{Key: "Wails", Value: dash(f.WailsVersion)},
		},
		Paths: []AboutRow{
			{Key: "Carpeta de datos", Value: dash(f.DataDir)},
			{Key: "Ajustes", Value: dash(f.SettingsFile)},
			{Key: "Historial", Value: dash(f.HistoryFile)},
		},
	}
}
