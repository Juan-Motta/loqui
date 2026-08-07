// Handing the catalogue to the page.
//
// The page has to translate the markup — 155 strings authored in Spanish and marked `data-i18n` —
// and the catalogue lives in Go so there is exactly ONE of it. Two catalogues would drift, and
// drift in i18n is invisible: it shows up as a single sentence in the wrong language that no test
// looks at.
//
// A SEPARATE CALL rather than a field on SettingsPayload, deliberately. The payload is rebuilt and
// re-sent on every repaint — Sistema, idiomas, onboarding and the permissions refresh all trigger
// one — and the catalogue is ~113 entries that never change within a run. Carrying it there would
// ship the whole table dozens of times per session to say nothing new.
package app

import "github.com/Juan-Motta/loqui-go/internal/i18n"

// Translations is the table the page applies to its own markup.
type Translations struct {
	// Locale is the language in effect, already resolved against the system.
	Locale string `json:"locale"`
	// Catalog maps Spanish source string to translation. EMPTY for Spanish, which is correct rather
	// than missing: the keys are Spanish, so there is nothing to look up.
	Catalog map[string]string `json:"catalog"`
}

// Translations returns the catalogue for the language in effect. Bound as Settings.Translations().
func (s *SettingsService) Translations() Translations {
	locale := s.bootstrap.locale(s.bootstrap.store.LoadSettings())
	return Translations{Locale: string(locale), Catalog: i18n.Catalog(locale)}
}
