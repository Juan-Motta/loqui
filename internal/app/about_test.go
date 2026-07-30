package app

import "testing"

// fullFacts is a machine that answered every question, so each test can spoil exactly one thing.
func fullFacts() AboutFacts {
	return AboutFacts{
		Version:      "0.1.0",
		Packaged:     true,
		OSName:       "macOS",
		OSVersion:    "26.5.2",
		Arch:         "arm64",
		Locale:       "es-CO",
		GoVersion:    "go1.25.0",
		WailsVersion: "v3.0.0-alpha2.119",
		DataDir:      "/Users/x/Library/Application Support/LoquiGo",
		SettingsFile: "/Users/x/Library/Application Support/LoquiGo/settings.json",
		HistoryFile:  "/Users/x/Library/Application Support/LoquiGo/history.jsonl",
	}
}

func rowValue(t *testing.T, rows []AboutRow, key string) string {
	t.Helper()
	for _, r := range rows {
		if r.Key == key {
			return r.Value
		}
	}
	t.Fatalf("no hay fila %q en %v", key, rows)
	return ""
}

// The badge is the whole point of the packaged flag: it says "you are not looking at the shipped
// app". If it leaked into a release it would be lying to a user who cannot check.
func TestBuildAboutHidesDevBadgeWhenPackaged(t *testing.T) {
	got := BuildAbout(fullFacts()).VersionLabel
	if got != "Versión 0.1.0" {
		t.Fatalf("etiqueta de versión = %q, quería %q", got, "Versión 0.1.0")
	}
}

func TestBuildAboutMarksUnpackagedRunAsDevelopment(t *testing.T) {
	f := fullFacts()
	f.Packaged = false
	got := BuildAbout(f).VersionLabel
	if got != "Versión 0.1.0  ·  desarrollo" {
		t.Fatalf("etiqueta de versión = %q, quería la insignia de desarrollo", got)
	}
}

// An unpackaged run has no Info.plist to read, so a missing version there is normal and must not
// read as an error — but it must not print "Versión —" either.
func TestBuildAboutUnpackagedWithoutVersionSaysOnlyDevelopment(t *testing.T) {
	f := fullFacts()
	f.Packaged = false
	f.Version = ""
	got := BuildAbout(f).VersionLabel
	if got != "desarrollo" {
		t.Fatalf("etiqueta de versión = %q, quería %q", got, "desarrollo")
	}
}

// A packaged app whose own plist can't be read IS broken, and that is worth saying plainly.
func TestBuildAboutPackagedWithoutVersionReportsFailure(t *testing.T) {
	f := fullFacts()
	f.Version = ""
	got := BuildAbout(f).VersionLabel
	if got != "No se pudo leer la información de la app" {
		t.Fatalf("etiqueta de versión = %q, quería el mensaje de fallo", got)
	}
}

func TestBuildAboutFormatsTheOperatingSystemRow(t *testing.T) {
	rows := BuildAbout(fullFacts()).System
	if got := rowValue(t, rows, "Sistema operativo"); got != "macOS 26.5.2 (arm64)" {
		t.Fatalf("fila de SO = %q", got)
	}
}

// Every unknown becomes an em dash. The failure this guards against is a row reading "()" or
// "macOS  ()", which looks like a rendering bug rather than a machine that did not answer.
func TestBuildAboutReplacesMissingFactsWithADash(t *testing.T) {
	f := fullFacts()
	f.OSVersion = ""
	f.Arch = ""
	f.Locale = ""
	f.WailsVersion = ""
	f.DataDir = ""
	info := BuildAbout(f)

	if got := rowValue(t, info.System, "Sistema operativo"); got != "macOS — (—)" {
		t.Fatalf("fila de SO con datos ausentes = %q", got)
	}
	if got := rowValue(t, info.System, "Idioma del sistema"); got != "—" {
		t.Fatalf("fila de idioma = %q", got)
	}
	if got := rowValue(t, info.System, "Wails"); got != "—" {
		t.Fatalf("fila de Wails = %q", got)
	}
	if got := rowValue(t, info.Paths, "Carpeta de datos"); got != "—" {
		t.Fatalf("fila de carpeta = %q", got)
	}
}

// The rows are fixed and ordered: this is a spec sheet someone reads top to bottom when reporting a
// bug, and a row appearing or vanishing depending on the machine makes two reports incomparable.
func TestBuildAboutKeepsRowsFixedAndOrdered(t *testing.T) {
	info := BuildAbout(fullFacts())

	wantSystem := []string{"Sistema operativo", "Idioma del sistema", "Go", "Wails"}
	if len(info.System) != len(wantSystem) {
		t.Fatalf("filas de sistema = %d, quería %d: %v", len(info.System), len(wantSystem), info.System)
	}
	for i, key := range wantSystem {
		if info.System[i].Key != key {
			t.Fatalf("fila de sistema %d = %q, quería %q", i, info.System[i].Key, key)
		}
	}

	wantPaths := []string{"Carpeta de datos", "Ajustes", "Historial"}
	if len(info.Paths) != len(wantPaths) {
		t.Fatalf("filas de rutas = %d, quería %d: %v", len(info.Paths), len(wantPaths), info.Paths)
	}
	for i, key := range wantPaths {
		if info.Paths[i].Key != key {
			t.Fatalf("fila de rutas %d = %q, quería %q", i, info.Paths[i].Key, key)
		}
	}
}

// The plist holds TWO version keys. CFBundleVersion is the build number and is not what Acerca de
// shows, so the values here differ on purpose: a reader that matched the first <string> after any
// "version" key would return 99 and this would catch it.
func TestPlistShortVersionPrefersTheShortVersionKey(t *testing.T) {
	plist := []byte(`<dict>
	<key>CFBundleVersion</key>
	<string>99</string>
	<key>CFBundleShortVersionString</key>
	<string>0.1.0</string>
</dict>`)
	if got := plistShortVersion(plist); got != "0.1.0" {
		t.Fatalf("versión = %q, quería %q", got, "0.1.0")
	}
}

// Key order in a plist is not guaranteed, and Wails could emit either.
func TestPlistShortVersionFindsTheKeyInEitherOrder(t *testing.T) {
	plist := []byte(`<key>CFBundleShortVersionString</key><string>2.3.4</string>
	<key>CFBundleVersion</key><string>99</string>`)
	if got := plistShortVersion(plist); got != "2.3.4" {
		t.Fatalf("versión = %q, quería %q", got, "2.3.4")
	}
}

func TestPlistShortVersionReturnsEmptyWhenAbsent(t *testing.T) {
	if got := plistShortVersion([]byte(`<dict><key>CFBundleName</key><string>Loqui</string></dict>`)); got != "" {
		t.Fatalf("versión = %q, quería vacío", got)
	}
}

// Packaged-ness decides whether the dev badge shows, so it must come from the bundle layout and not
// from anything a build could forget to set.
func TestIsPackagedPathRecognisesABundleAndRejectsALooseBinary(t *testing.T) {
	cases := []struct {
		exe  string
		want bool
	}{
		{"/Applications/loqui.app/Contents/MacOS/loqui", true},
		{"/Users/x/projects/loqui-go/bin/loqui.app/Contents/MacOS/loqui", true},
		{"/Users/x/projects/loqui-go/bin/loqui", false},
		{"/tmp/go-build123/b001/exe/loqui", false},
		{"", false},
		// A directory whose NAME merely contains ".app" is not a bundle. Without this case the
		// check passes just as well as a bare strings.Contains(exe, ".app") — which was the
		// first thing a mutation proved, so the case earns its place.
		{"/Users/x/projects/loqui.app.old/bin/loqui", false},
	}
	for _, c := range cases {
		if got := isPackagedPath(c.exe); got != c.want {
			t.Fatalf("isPackagedPath(%q) = %v, quería %v", c.exe, got, c.want)
		}
	}
}

// The values have to arrive intact — a test that only counted rows would pass with them swapped.
func TestBuildAboutCarriesEachPathToItsOwnRow(t *testing.T) {
	f := fullFacts()
	info := BuildAbout(f)
	if got := rowValue(t, info.Paths, "Ajustes"); got != f.SettingsFile {
		t.Fatalf("fila de ajustes = %q, quería %q", got, f.SettingsFile)
	}
	if got := rowValue(t, info.Paths, "Historial"); got != f.HistoryFile {
		t.Fatalf("fila de historial = %q, quería %q", got, f.HistoryFile)
	}
	if got := rowValue(t, info.System, "Go"); got != f.GoVersion {
		t.Fatalf("fila de Go = %q, quería %q", got, f.GoVersion)
	}
}
