// Pure helpers for building and validating the Azure Speech configuration. Ported from
// the Electron build's src/shared/azureConfig.ts.
//
// Continuous language identification (verified against Microsoft Learn, the
// language-identification doc of 2025-12-19, and re-confirmed by the Go spike in
// docs/research/2026-07-27-azure-speech-go-macos.md) requires:
//   - the universal v2 endpoint: wss://<region>.stt.speech.microsoft.com/speech/universal/v2
//   - at most 10 candidate languages
//   - exactly ONE locale per base language (one Spanish, one English — not es-CO and es-MX)
//
// That last rule is the surprising one and the reason this validation exists at all:
// Azure does not reject a second locale of the same language, it just quietly degrades
// detection, so the only place it can be caught is here.
package settings

import (
	"fmt"
	"regexp"
	"strings"
)

const maxCandidates = 10

var (
	localeRe = regexp.MustCompile(`^[A-Za-z]{2,3}-[A-Za-z0-9]+$`)
	regionRe = regexp.MustCompile(`^[a-z0-9]+$`)
	spaceRe  = regexp.MustCompile(`\s+`)
)

// NormalizeRegion turns a user-entered region ("East US 2") into its id ("eastus2").
// Users copy the display name out of the Azure portal, so accepting only the id would
// reject the string most people actually have in their clipboard.
func NormalizeRegion(region string) (string, error) {
	if strings.TrimSpace(region) == "" {
		return "", fmt.Errorf("azure region is required")
	}
	id := strings.ToLower(spaceRe.ReplaceAllString(region, ""))
	if !regionRe.MatchString(id) {
		return "", fmt.Errorf("invalid Azure region: %s", region)
	}
	return id, nil
}

// BuildV2Endpoint returns the universal v2 websocket endpoint, which is required for
// continuous LID — the v1 endpoint silently ignores the language-id configuration.
func BuildV2Endpoint(region string) (string, error) {
	id, err := NormalizeRegion(region)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("wss://%s.stt.speech.microsoft.com/speech/universal/v2", id), nil
}

// BaseLanguage returns the base subtag, lowercased: "es-CO" -> "es".
func BaseLanguage(locale string) string {
	return strings.ToLower(strings.SplitN(locale, "-", 2)[0])
}

// ValidateCandidates checks the LID candidate list against Azure's rules, returning it
// unchanged when valid.
func ValidateCandidates(langs []string) ([]string, error) {
	if len(langs) == 0 {
		return nil, fmt.Errorf("provide at least one candidate language")
	}
	if len(langs) > maxCandidates {
		return nil, fmt.Errorf("azure allows at most %d candidate languages (got %d)", maxCandidates, len(langs))
	}
	seen := make(map[string]bool, len(langs))
	for _, locale := range langs {
		if !localeRe.MatchString(locale) {
			return nil, fmt.Errorf("invalid locale: %s (expected e.g. \"es-CO\")", locale)
		}
		base := BaseLanguage(locale)
		if seen[base] {
			return nil, fmt.Errorf("only one locale per base language is allowed for continuous LID (duplicate %q)", base)
		}
		seen[base] = true
	}
	return langs, nil
}
