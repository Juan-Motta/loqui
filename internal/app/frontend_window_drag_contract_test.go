package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Wails resolves drag intent from an inherited CSS custom property on the event target. This
// contract guards both sides of that boundary: the intended empty regions opt into dragging, while
// interactive sidebar descendants override the inherited value so clicks remain clicks.
func TestSettingsWindowUsesWailsDragRegions(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "frontend", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	markup := string(raw)
	if strings.Contains(markup, "-webkit-app-region") {
		t.Fatal("settings window still declares Electron drag regions instead of Wails drag regions")
	}

	for _, tc := range []struct {
		selector string
		value    string
	}{
		{selector: "#sidebar", value: "drag"},
		{selector: ".nav-item", value: "no-drag"},
		{selector: ".sidebar-foot", value: "no-drag"},
		{selector: ".link-more", value: "no-drag"},
		{selector: ".view-drag", value: "drag"},
		{selector: ".wiz-drag", value: "drag"},
	} {
		t.Run(tc.selector, func(t *testing.T) {
			pattern := `(?s)` + regexp.QuoteMeta(tc.selector) +
				`\s*\{[^}]*--wails-draggable:\s*` + regexp.QuoteMeta(tc.value) + `(?:\s*;|\s*})`
			if !regexp.MustCompile(pattern).MatchString(markup) {
				t.Errorf("%s does not declare --wails-draggable: %s", tc.selector, tc.value)
			}
		})
	}
}
