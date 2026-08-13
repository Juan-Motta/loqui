package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func readFrontendSource(t *testing.T, parts ...string) string {
	t.Helper()
	path := append([]string{"..", "..", "frontend"}, parts...)
	raw, err := os.ReadFile(filepath.Join(path...))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestAutomaticUpdatesHaveSettingsAndAboutControls(t *testing.T) {
	markup := readFrontendSource(t, "index.html")
	for _, required := range []string{
		`id="autoUpdateChecks"`,
		`data-i18n>Buscar actualizaciones automáticamente</`,
		`data-i18n>ACTUALIZACIONES</`,
		`id="aboutUpdateStatus"`,
		`class="btn" id="aboutCheckUpdates"`,
		`class="btn primary" id="aboutInstallUpdate"`,
		`class="btn primary" id="aboutRestartUpdate"`,
	} {
		if !strings.Contains(markup, required) {
			t.Errorf("update UI is missing %q", required)
		}
	}

	updatesRow := regexp.MustCompile(`<div class="srow">\s*<div>\s*<div class="srow-label" id="aboutUpdateStatus"`)
	if !updatesRow.MatchString(markup) {
		t.Error("update actions must use the same right-aligned row layout as other About actions")
	}
}

func TestAutomaticUpdatesUseTheAppOwnedBindings(t *testing.T) {
	system := readFrontendSource(t, "src", "system.ts")
	for _, required := range []string{
		"p.autoUpdateChecks",
		"Settings.SetAutoUpdateChecks(",
		"autoUpdateChecks",
	} {
		if !strings.Contains(system, required) {
			t.Errorf("system UI is missing %q", required)
		}
	}

	about := readFrontendSource(t, "src", "about.ts")
	for _, required := range []string{
		"Updates.Status()",
		"Updates.Check()",
		"Updates.Install()",
		"Updates.Restart()",
		`updates:available`,
		`updates:ready`,
	} {
		if !strings.Contains(about, required) {
			t.Errorf("about UI is missing %q", required)
		}
	}
}
