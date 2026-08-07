// Translating the payload on its way out.
//
// WHY AT THE BOUNDARY, and not by threading a locale through every function that writes a sentence.
//
// The rules that decide this wording are pure functions in `store` — ConnectionRows, the trigger
// notes, the language catalogue — and they emit SPANISH. The catalogue's keys are Spanish source
// strings, so those functions are already emitting keys without knowing it. Translating here is
// therefore a lookup, not a redesign: seventeen files keep their signatures, their purity and their
// tests, and there is exactly one place where a language decision happens.
//
// The alternative — a locale parameter on every rule function — would have rewritten the tests of
// every package that owns wording, to prove the same thing this file proves once.
//
// A string with no entry crosses unchanged. That is the fallback doing its job, and it is why an
// incomplete catalogue degrades to readable Spanish instead of to holes.
package app

import (
	"github.com/Juan-Motta/loqui-go/internal/i18n"
)

// translatePayload rewrites every user-facing string in a snapshot into the locale it carries.
//
// Mutates through the pointer because a SettingsPayload holds slices: copying the struct would share
// their backing arrays and translate the caller's copy anyway, which is worse than being explicit.
func translatePayload(p *SettingsPayload) {
	locale := i18n.Locale(p.Locale)
	if locale == i18n.Spanish {
		// The key IS the answer. Skipped rather than run through T for nothing — and it keeps the
		// authored language byte-identical, which is a property worth being able to state.
		return
	}
	tr := func(s string) string { return i18n.T(locale, s, nil) }

	for i := range p.Connections {
		row := &p.Connections[i]
		// Name is included deliberately even though most are product names: those are declared as
		// identical in English, so T returns them unchanged. Excluding them here would mean the
		// decision lived in two places.
		row.Name = tr(row.Name)
		row.Kind = tr(row.Kind)
		row.Label = tr(row.Label)
	}
	// ProviderOption carries no prose — the picker's labels come from the markup and are translated
	// by the page's own pass. Left out deliberately rather than forgotten.
	p.Trigger.Label = tr(p.Trigger.Label)
	p.Trigger.Note = tr(p.Trigger.Note)
	p.Trigger.ResetLabel = tr(p.Trigger.ResetLabel)
	p.DevicesError = tr(p.DevicesError)
	// Device names come from the hardware, not from us: translating them would rename the user's
	// microphone.
}

// translateRows does the same for the permission rows, which are served by their OWN service.
//
// PermissionsService is constructed without the store (main.go), so it cannot resolve a locale by
// itself — it is handed one. Found in design review: without this, pressing "Volver a comprobar"
// rebuilt the rows from Spanish metadata and put them back on an otherwise English page.
func translateRows(rows []PermissionRow, locale i18n.Locale) []PermissionRow {
	if locale == i18n.Spanish {
		return rows
	}
	for i := range rows {
		rows[i].Name = i18n.T(locale, rows[i].Name, nil)
		rows[i].Desc = i18n.T(locale, rows[i].Desc, nil)
		rows[i].Label = i18n.T(locale, rows[i].Label, nil)
	}
	return rows
}
