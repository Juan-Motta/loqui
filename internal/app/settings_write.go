// The write half of the settings service: what the Ajustes page does when the user acts.
//
// NO SETTER RETURNS A GO ERROR. They return a WriteResult carrying both the fresh payload and an
// error MESSAGE, because Wails discards the result whenever a bound method also returns an error
// (pkg/application/bindings.go: `if len(errorOutputs) > 0 { err = cerr }`, and `result` is only set
// in the else branch). A setter that returned both would leave the page with nothing to repaint
// from at exactly the moment its picture of the world is known to be wrong. See WriteResult.
//
// EVERY SETTER VALIDATES, AND A REJECTED WRITE CHANGES NOTHING. These bindings are reachable from
// anything running script in that window, so the <select> in the markup is not a guarantee.
// Storing a provider the engine cannot drive, or a region Azure will not accept, breaks dictation
// later and far from the mistake — and takes the user's working configuration with it.
//
// EVERY SETTINGS CHANGE GOES THROUGH store.UpdateSettings, never Load-then-Save: those are two
// separate critical sections, and Wails dispatches each call on its own goroutine.
package app

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/Juan-Motta/loqui-go/internal/i18n"
	"github.com/Juan-Motta/loqui-go/internal/settings"
	"github.com/Juan-Motta/loqui-go/internal/store"
)

// WriteResult is what every setter returns: the state to repaint from, plus what went wrong.
//
// Error is a STRING and the method returns no Go error, so the payload always survives the trip.
// Wails drops a bound method's result as soon as it also returns an error, so "here is the error
// AND here is the state" is not expressible any other way — and the page needs both: a rejected
// engine change must repaint the picker back to the engine that is actually active, not leave it
// showing the choice that was refused.
type WriteResult struct {
	Payload SettingsPayload `json:"payload"`
	// Error is empty on success. It is a message for the user, already in Spanish.
	Error string `json:"error"`
	// Notice is what to say when the write SUCCEEDED, and it is empty on failure.
	//
	// It exists because an empty status line is indistinguishable from a click that never arrived:
	// the page paints Error, so a completed save said nothing at all. It is decided here rather than
	// in the page because these messages depend on facts only this side has — which of the two values
	// was actually written, and whether the engine just chosen can dictate.
	Notice string `json:"notice"`
	// Field names the input the user has to fix — "key", "region", or empty when the failure is not
	// about a form value. The page paints the invalid border from it and works nothing out for
	// itself; deducing "this message is about the key" by reading the text would put one validation
	// rule in two languages.
	Field string `json:"field"`
}

// ok wraps a successful write, with what to tell the user about it.
func (s *SettingsService) ok(notice string) WriteResult {
	p := s.bootstrap.Payload()
	return WriteResult{Payload: p, Notice: s.phrase(p.Locale, notice)}
}

// phrase translates a message on its way to the page.
//
// THE FORMAT STRING IS THE KEY, verbs and all: it is the authored Spanish, and the catalogue is
// keyed by authored Spanish. Translating it BEFORE the arguments are filled in is what keeps every
// call site in this package unchanged — they go on writing Spanish sentences, exactly as they did,
// and none of them has to know a locale exists.
//
// Notices and errors do not travel inside the payload, so the payload's own pass cannot reach them;
// without this an English card would carry a Spanish sentence underneath it.
func (s *SettingsService) phrase(locale string, format string, args ...any) string {
	if format == "" {
		return ""
	}
	translated := i18n.T(i18n.Locale(locale), format, nil)
	if len(args) == 0 {
		return translated
	}
	return fmt.Sprintf(translated, args...)
}

// failed wraps a rejected or failed write. The payload is recomputed either way, so the page
// repaints from what is actually stored rather than from what it hoped it had stored.
func (s *SettingsService) failed(format string, args ...any) WriteResult {
	p := s.bootstrap.Payload()
	return WriteResult{Payload: p, Error: s.phrase(p.Locale, format, args...)}
}

// invalid is failed for the case where the user can fix it in a specific input.
func (s *SettingsService) invalid(field string, format string, args ...any) WriteResult {
	p := s.bootstrap.Payload()
	return WriteResult{
		Payload: p,
		Error:   s.phrase(p.Locale, format, args...),
		Field:   field,
	}
}

// SetProvider switches the active engine. Bound as Settings.SetProvider().
func (s *SettingsService) SetProvider(provider string) WriteResult {
	defer s.beginReadinessChange()()
	if !store.IsKnownProvider(provider) {
		return s.failed("motor desconocido: %q", provider)
	}
	// Refused rather than stored, even though the settings file could hold it: an engine that is
	// not ported yet would be accepted here and then rejected at the first dictation, having
	// already replaced a working engine. The picker greys these out (Providers[].Available), and
	// this is the check that makes that more than a suggestion.
	if !store.IsAvailableProvider(provider) {
		return s.failed("el motor %q todavía no está disponible en esta versión", provider)
	}
	err := s.store().UpdateSettings(func(cfg *store.Settings) error {
		cfg.Provider = provider
		return nil
	})
	if err != nil {
		return s.failed("no se pudo guardar el motor: %v", err)
	}
	s.readinessChanged()
	// The payload is computed once and the notice read OUT of it, rather than recomputing the
	// readiness rule here: store.ConnectionRows already owns it, and a second copy would be free to
	// disagree with the badge the user is looking at.
	p := s.bootstrap.Payload()
	notice := providerNotice(p, provider)
	// The connection model cannot see Whisper's model file, so its row reads "Disponible" with or
	// without one and this is the only place the difference can be told. Chosen and unable to dictate
	// is exactly the silence this whole change is about.
	//
	// It reports rather than refuses: rejecting the selection would leave the user on whatever engine
	// they were escaping from, and the row they clicked would go on claiming to be available. Making
	// the model part of that row's state belongs with the model download, still to be ported.
	if provider == store.DefaultProvider {
		if problem := s.defaultEngineProblem(p.Locale); problem != nil {
			notice = i18n.T(i18n.Locale(p.Locale), "Motor guardado, pero {engine} no puede dictar: {reason}",
				map[string]string{"engine": provider, "reason": problem.Error()})
		}
	}
	return WriteResult{Payload: p, Notice: notice}
}

// providerNotice says whether the engine just chosen can actually dictate.
//
// Storing an engine SUCCEEDS even when it cannot work — the picker greys those out, but this binding
// is reachable from anything running script in that window, and "unsupported" additionally depends on
// the machine rather than on what is ported. So "saved" is not the same claim as "ready", and this is
// the sentence that keeps them apart.
// PARAMETERISED KEYS here, not concatenation. "Motor activo: " + row.Name cannot be a catalogue key,
// because the key would change with the engine — so these use {engine} and i18n's interpolation, and
// the sentence stays one lookup whatever the value is.
func providerNotice(p SettingsPayload, provider string) string {
	locale := i18n.Locale(p.Locale)
	t := func(key string, args map[string]string) string { return i18n.T(locale, key, args) }
	for _, row := range p.Connections {
		if row.ID != provider {
			continue
		}
		switch row.State {
		case store.ConnActive:
			return t("Motor activo: {engine}", map[string]string{"engine": row.Name})
		case store.ConnUnsupported:
			return t("Motor guardado, pero no puede funcionar en este sistema", nil)
		case store.ConnUnconfigured:
			// Credentials that could not be read are NOT missing configuration: the key may be sitting
			// right there. presenceMap collapses the two — deliberately, for the badge — so the
			// distinction has to be recovered from the key state before telling someone to go and
			// complete a configuration they may have completed already.
			key := keyStateIn(p, row.KeySlot)
			switch {
			case key.Status == store.KeyUnreadable:
				return t("Motor seleccionado, pero no se pudieron leer las claves guardadas — no se puede confirmar si la suya está disponible", nil)
			case key.FromEnv && key.Status == store.KeyAbsent:
				// In force and holding nothing usable — the only case where the form cannot help,
				// because whatever is typed there stays overridden. An env variable holding a REAL
				// key also lands here whenever some other field is missing (Azure without a region),
				// and telling that user to delete it would break a working credential.
				return t("Motor seleccionado, pero su variable de entorno está definida y vacía — quítala del entorno para poder usar una clave guardada", nil)
			}
			return t("Motor seleccionado, pero le falta configuración — no podrá dictar hasta que la completes", nil)
		}
		return t("Motor guardado: {engine}", map[string]string{"engine": row.Name})
	}
	return t("Motor guardado", nil)
}

// keyStateIn is what a payload knows about one slot, zero when the slot is not listed — which is the
// case for the engines that take no credential.
//
// The whole KeyState rather than just the status, because "nothing usable" and "nothing usable AND
// an environment variable is overriding everything" need different sentences: the second cannot be
// fixed in the form at all.
func keyStateIn(p SettingsPayload, slot string) KeyState {
	if slot == "" {
		return KeyState{}
	}
	for _, k := range p.Keys {
		if k.Slot == slot {
			return k
		}
	}
	return KeyState{}
}

// SetRegion stores the Azure Speech region, normalised. Bound as Settings.SetRegion().
//
// Normalised HERE rather than in the page: the user picks a display name ("East US 2") and every
// Azure endpoint needs the id ("eastus2"). settings.NormalizeRegion already owns that rule and is
// tested; doing it in the webview too would be the same rule in two languages.
func (s *SettingsService) SetRegion(region string) WriteResult {
	defer s.beginReadinessChange()()
	id, err := settings.NormalizeRegion(region)
	if err != nil {
		return s.failed("%v", err)
	}
	if err := s.store().UpdateSettings(func(cfg *store.Settings) error {
		cfg.Region = id
		return nil
	}); err != nil {
		return s.failed("no se pudo guardar la región: %v", err)
	}
	s.readinessChanged()
	return s.ok("")
}

// SetTrigger stores the activation shortcut. Bound as Settings.SetTrigger().
//
// TWO THINGS BEYOND PERSISTING, and both are the difference between a setting that works and one
// that looks saved:
//
//  1. The MODE is coerced. Only fn reports release, so hold-to-talk is impossible with any other
//     accelerator — leaving mode="hold" on a trigger that cannot deliver it would start dictations
//     that never end on their own.
//  2. The listener is RE-REGISTERED through onTriggerChanged. The fn listener is a child process
//     started at launch from the stored trigger; without restarting it, the new shortcut is saved and
//     the old one keeps working, which is the most confusing possible outcome.
func (s *SettingsService) SetTrigger(trigger string) WriteResult {
	canonical, err := store.ValidateTriggerKey(trigger, runtime.GOOS)
	if err != nil {
		return s.failed("%v", err)
	}

	var mode string
	if err := s.store().UpdateSettings(func(cfg *store.Settings) error {
		cfg.TriggerKey = canonical
		cfg.Mode = store.CoerceMode(canonical, cfg.Mode)
		mode = cfg.Mode
		return nil
	}); err != nil {
		return s.failed("no se pudo guardar el atajo: %v", err)
	}

	// The engine and the listener are told AFTER the write, so what they act on is what is stored.
	s.applyMode(mode)
	if s.onTriggerChanged != nil {
		if err := s.onTriggerChanged(canonical); err != nil {
			// The shortcut IS saved; only the registration failed. Saying so lets the user retry or
			// pick another, instead of believing the setting did not take.
			return s.failed("el atajo se guardó, pero no se pudo registrar: %v", err)
		}
	}
	return s.ok("")
}

// SetMode switches between hold and toggle. Bound as Settings.SetMode().
//
// Coerced against the current trigger for the same reason SetTrigger coerces: the interface disables
// the impossible choice, but this binding is reachable regardless of what the page does.
func (s *SettingsService) SetMode(mode string) WriteResult {
	if mode != "hold" && mode != "toggle" {
		return s.failed("modo desconocido: %q", mode)
	}
	var stored string
	if err := s.store().UpdateSettings(func(cfg *store.Settings) error {
		cfg.Mode = store.CoerceMode(cfg.TriggerKey, mode)
		stored = cfg.Mode
		return nil
	}); err != nil {
		return s.failed("no se pudo guardar el modo: %v", err)
	}
	s.applyMode(stored)
	if stored != mode {
		// Told plainly rather than silently changed: the user asked for hold and got toggle, and the
		// reason is the trigger they have, not a failure.
		return s.failed("%s no admite mantener: se guardó en modo Alternar", store.FormatTrigger(s.store().LoadSettings().TriggerKey))
	}
	return s.ok("")
}

// applyMode pushes the mode into the RUNNING controller.
//
// Load-bearing: the engine reads the mode ONCE, when it is constructed, so a mode that is only
// persisted takes effect at the next launch. Without this the setting appears to save and the app
// keeps behaving the old way until restarted.
func (s *SettingsService) applyMode(mode string) {
	if s.onModeChanged != nil {
		s.onModeChanged(mode)
	}
}

// SetAppearance stores the light/dark preference. Bound as Settings.SetAppearance().
//
// Applied to the LIVE windows as well as persisted. The window's appearance is set once at
// construction from the stored value, so a change that is only written to disk takes effect at the
// next launch — the user picks "Oscuro", nothing happens, and the setting looks broken.
func (s *SettingsService) SetAppearance(appearance string) WriteResult {
	if appearance != "system" && appearance != "light" && appearance != "dark" {
		return s.failed("apariencia desconocida: %q", appearance)
	}
	if err := s.store().UpdateSettings(func(cfg *store.Settings) error {
		cfg.Appearance = appearance
		return nil
	}); err != nil {
		return s.failed("no se pudo guardar la apariencia: %v", err)
	}
	if s.onAppearanceChanged != nil {
		s.onAppearanceChanged(appearance)
	}
	return s.ok("")
}

// SetAppLanguage stores the interface language. Bound as Settings.SetAppLanguage().
//
// Empty means "follow the OS", which is a real choice and not a missing value.
func (s *SettingsService) SetAppLanguage(language string) WriteResult {
	if language != "" && language != "es" && language != "en" {
		return s.failed("idioma de interfaz desconocido: %q", language)
	}
	if err := s.store().UpdateSettings(func(cfg *store.Settings) error {
		cfg.AppLanguage = language
		return nil
	}); err != nil {
		return s.failed("no se pudo guardar el idioma de la interfaz: %v", err)
	}
	// The resolved locale, not the stored value: "" means follow the system, and the overlay and the
	// tray need the answer rather than the instruction.
	if s.onLanguageChanged != nil {
		s.onLanguageChanged(string(s.bootstrap.locale(s.store().LoadSettings())))
	}
	return s.ok("")
}

// SetOnboarded records that the tutorial was completed or skipped. Bound as Settings.SetOnboarded().
//
// It is a flag of its own and NOT derived from "does the app look configured": the defaults are
// already usable (local whisper, no key, no internet), so anything inferred would either re-open the
// wizard for a user who chose those defaults on purpose, or never open it at all.
//
// Nothing to validate — a bool is a bool. The write still goes through UpdateSettings so it is
// transactional with whatever else is on disk, never Load-then-Save.
func (s *SettingsService) SetOnboarded(done bool) WriteResult {
	if err := s.store().UpdateSettings(func(cfg *store.Settings) error {
		cfg.Onboarded = done
		return nil
	}); err != nil {
		return s.failed("no se pudo guardar el estado del tutorial: %v", err)
	}
	return s.ok("")
}

// SetInputDevice stores the chosen microphone. Bound as Settings.SetInputDevice().
//
// NOT validated against the enumerated list, deliberately. A device id can be stored while the
// device is unplugged, and refusing it would mean a user cannot keep their usual microphone selected
// while working on battery with it detached. The capture path already falls back to the system
// default when the id no longer resolves.
//
// Empty means the system default.
func (s *SettingsService) SetInputDevice(id string) WriteResult {
	if err := s.store().UpdateSettings(func(cfg *store.Settings) error {
		cfg.InputDeviceID = id
		return nil
	}); err != nil {
		return s.failed("no se pudo guardar el dispositivo: %v", err)
	}
	return s.ok("")
}

// SetLanguages stores one slot's dictation languages. Bound as Settings.SetLanguages().
//
// PER SLOT, not per whole settings object, matching the original's light `settings:language` channel.
// The reason is concrete: language is per engine, so validating the whole object would let one
// misconfigured engine block editing another's language — the user could not fix Azure's region
// without also satisfying whatever is wrong with Grok.
//
// The value is validated against the slot's CAPABILITY, and the message is shown verbatim: a full
// locale sent to an API expecting "es", or "auto" sent to an engine that cannot detect, would
// otherwise be accepted here and rejected mid-dictation.
func (s *SettingsService) SetLanguages(slot string, values []string) WriteResult {
	valid, err := store.ValidateLanguagesFor(slot, values)
	if err != nil {
		return s.failed("%v", err)
	}
	if err := s.store().UpdateSettings(func(cfg *store.Settings) error {
		// The map is replaced rather than mutated in place: LoadSettings may hand back the defaults
		// map itself, and writing into that would change the defaults for the rest of the process.
		next := make(map[string][]string, len(cfg.LanguageBySlot)+1)
		for k, v := range cfg.LanguageBySlot {
			next[k] = v
		}
		next[slot] = valid
		cfg.LanguageBySlot = next
		return nil
	}); err != nil {
		return s.failed("no se pudo guardar el idioma: %v", err)
	}
	return s.ok("")
}

// SetKey stores one provider's credential. Bound as Settings.SetKey().
//
// The secret is never logged and never echoed back: the payload carries presence only.
func (s *SettingsService) SetKey(slot string, secret string) WriteResult {
	defer s.beginReadinessChange()()
	keySlot, ok := knownKeySlot(slot)
	if !ok {
		return s.failed("ranura de clave desconocida: %q", slot)
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return s.failed("la clave está vacía — para borrarla usa el botón de eliminar")
	}
	if !store.IsAvailableKeySlot(keySlot) {
		return s.failed("este servicio todavía no está disponible en esta versión — guardar una clave " +
			"aquí no serviría de nada")
	}
	if name := envOverrideFor(keySlot); name != "" {
		return s.failed("esta ranura la controla la variable de entorno %s — mientras esté definida, "+
			"guardar aquí no cambiaría la clave que se usa", name)
	}
	if err := s.secretWriter()(keySlot, secret); err != nil {
		// NOT counted, and that is a change from the Keychain backend: a failed write to the credentials
		// FILE wrote nothing. The old cgo call could be abandoned mid-flight and land seconds later, so
		// a failure had to count as a possible change; a rename either happened or it did not.
		return s.failed("%s", secretsMessage(err))
	}
	s.readinessChanged()
	return s.ok("")
}

// DeleteKey removes one provider's credential. Bound as Settings.DeleteKey().
//
// Refused while a LOQUI_*_KEY override is in force. The UI greys the button out for those slots,
// but that is not the enforcement: this binding is reachable from anything in the webview, and the
// item it would delete is one the user cannot currently see — the override is what dictation is
// using, so the slot would still read as configured afterwards and the deletion would look like it
// did nothing. Whatever is stored underneath is not what the user is looking at.
func (s *SettingsService) DeleteKey(slot string) WriteResult {
	defer s.beginReadinessChange()()
	keySlot, ok := knownKeySlot(slot)
	if !ok {
		return s.failed("ranura de clave desconocida: %q", slot)
	}
	if name := envOverrideFor(keySlot); name != "" {
		return s.failed("esta clave viene de la variable de entorno %s — quítala del entorno en vez de borrarla aquí", name)
	}
	if err := s.secretDeleter()(keySlot); err != nil {
		// A FAILED DELETE DELETED NOTHING, so readiness has not changed. The Keychain backend needed the
		// opposite rule — an abandoned cgo call could complete later and take the credential with it —
		// and losing that clause is one of the things the file backend buys.
		return s.failed("%s", secretsMessage(err))
	}
	s.readinessChanged()
	// A POSTCONDITION, not an account of what happened: store.DeleteKey treats an absent item as
	// success, and this method never learns whether there was one. "Clave borrada" would be false
	// exactly when the user pressed the button on an empty slot.
	notice := "La clave ya no está guardada"

	// Deleting the credential of the engine IN USE is the moment that engine stops working, and the
	// user is watching right now. Leaving it selected would move the discovery to the next dictation:
	// a key press that transcribes nothing, with no visible cause.
	p := s.bootstrap.Payload()
	if row := providerRow(p, p.Provider); row.KeySlot == string(keySlot) {
		// The delete is the evidence. Without it the repair would consult the payload computed just
		// now, and if THAT read came back "unreadable" it would refuse to act — leaving the engine
		// selected with a credential this method has already removed.
		out, err := s.repairEngine(p, repairEvidence{deletedSlot: keySlot})
		if err != nil {
			return s.failed("la clave ya no está guardada, pero %v", err)
		}
		if out.notice != "" {
			return WriteResult{Payload: s.bootstrap.Payload(), Notice: notice + ". " + out.notice}
		}
	}
	return s.ok(notice)
}

// SaveConnection stores a provider's region and key together, as ONE operation.
// Bound as Settings.SaveConnection().
//
// It exists because doing it from the page as SetRegion-then-SetKey commits half of it whenever the
// second call fails: the region lands, the credential write does not, and the user is left with a
// provider that looks configured and cannot connect. Everything is validated BEFORE anything is
// written, so a REJECTED save changes nothing at all.
//
// NOT FULLY ATOMIC, and worth being precise about rather than claiming otherwise: the credential
// lives in one file and the region in another, and there is no transaction spanning both.
// What the commit order buys is that the LIKELY failure is the harmless one. The credential write is
// the part that actually fails on these builds — it hangs when the signature is not recognised — so
// it goes FIRST: if it fails, nothing has changed anywhere. The remaining window is a credential
// write that succeeds followed by a failing disk write, which means a new key against the old
// region. That needs the settings file to be unwritable, which is a different class of broken.
//
// An empty secret means "leave the stored key alone", which is how the form behaves when the user
// only wants to change the region. An empty region means the same for the region.
func (s *SettingsService) SaveConnection(slot string, region string, secret string) WriteResult {
	defer s.beginReadinessChange()()
	keySlot, ok := knownKeySlot(slot)
	if !ok {
		return s.failed("ranura de clave desconocida: %q", slot)
	}

	// ---- validate everything first ----
	//
	// Availability is checked UNCONDITIONALLY, not only when a key is being written. Gating it on the
	// secret left a region-only save free to go through an unported subservice's form and move the
	// live Azure endpoint: the credential was guarded while the setting beside it was not.
	if !store.IsAvailableKeySlot(keySlot) {
		return s.failed("este servicio todavía no está disponible en esta versión")
	}
	// Trimmed HERE, so what gets stored is what was validated — and what "Probar conexión" tested.
	// A pasted credential arrives with a newline more often than not; testing the trimmed value and
	// storing the padded one would hand out a green tick for a key that then fails at dictation.
	secret = strings.TrimSpace(secret)
	writeKey := secret != ""

	regionID := ""
	if strings.TrimSpace(region) != "" {
		// The region is a SINGLE global Azure setting, so accepting it alongside an unrelated slot
		// would let saving a Grok key move the Azure endpoint.
		if !store.UsesAzureRegion(keySlot) {
			return s.failed("este servicio no usa una región de Azure")
		}
		id, err := settings.NormalizeRegion(region)
		if err != nil {
			return s.failed("%v", err)
		}
		regionID = id
	}

	// A save with no credential ANYWHERE is refused, and the message points at the input that has to
	// change. Without this the form accepted a region on its own and left a card that reads as
	// configured while dictation has nothing to authenticate with.
	//
	// Checked only when the user did not type one: a typed key is the credential, and looking up
	// what is stored could not change the answer, so it is not worth the read.
	if !writeKey {
		if res, ok := s.requireStoredKey(keySlot); !ok {
			return res
		}
	}
	if regionID == "" && !writeKey {
		// Reached only when a credential IS already stored: nothing was offered, so nothing is
		// missing — there is simply nothing to do.
		return s.failed("no hay nada que guardar")
	}
	// A region-only save stays allowed for an env-backed slot: the region is not the credential.
	// Writing the SECRET is what would be futile and misleading.
	if writeKey {
		if name := envOverrideFor(keySlot); name != "" {
			return s.failed("esta ranura la controla la variable de entorno %s — mientras esté definida, "+
				"guardar la clave aquí no cambiaría la que se usa", name)
		}
	}

	// ---- then commit, the credential first ----
	// The order matters: the credential write is the half that can fail on its own, so doing it first
	// means its failure leaves EVERYTHING untouched. Region-first would persist a region the user's key
	// was never saved against.
	if writeKey {
		if err := s.secretWriter()(keySlot, secret); err != nil {
			return s.failed("%s", secretsMessage(err))
		}
		// Counted here, not at the end: the region write below can fail, and the credential has
		// already landed by then.
		s.readinessChanged()
	}
	if regionID != "" {
		if err := s.store().UpdateSettings(func(cfg *store.Settings) error {
			cfg.Region = regionID
			return nil
		}); err != nil {
			// The wording depends on what actually happened: saying "the key was saved" after a
			// region-only save would be a plain lie about an operation that touched no credential.
			if writeKey {
				return s.failed("la clave se guardó, pero no se pudo guardar la región: %v", err)
			}
			return s.failed("no se pudo guardar la región: %v", err)
		}
		s.readinessChanged()
	}
	return s.ok(saveNotice(writeKey, regionID != ""))
}

// requireStoredKey answers "is there a credential for this slot already", with the same precedence
// the rest of the app reads by.
//
// THE THREE OUTCOMES STAY APART, for the same reason they do in the probe: only "there is nothing
// stored" is something the user fixes in the form, so only that one marks the field. Reporting a
// unreadable store as a missing key would send someone to re-paste a credential that is
// already there — and on this build that is the common failure, not the rare one.
func (s *SettingsService) requireStoredKey(slot store.KeySlot) (WriteResult, bool) {
	if name, _, set := envCredential(slot); set {
		if !envCredentialUsable(slot) {
			// In force and unusable. Saying "the key is required" would send the user to paste one
			// into a form whose value the environment would go on overriding.
			return s.failed("la variable de entorno %s está definida pero vacía — mientras lo esté, "+
				"es la clave que se usa, y no puede autenticar nada", name), false
		}
		// The environment IS the credential in force. Consulting the store could not change that.
		return WriteResult{}, true
	}
	_, err := s.secretReader()(slot)
	switch {
	case err == nil:
		return WriteResult{}, true
	case errors.Is(err, store.ErrNoSecret):
		return s.invalid("key", "la clave es obligatoria: pégala antes de guardar"), false
	case errors.Is(err, store.ErrSecretsUnreadable):
		return s.failed("no se pudieron leer las claves guardadas, así que no se pudo comprobar si ya " +
			"hay una — revisa el archivo de claves o bórralo para empezar de cero"), false
	default:
		return s.failed("no se pudo comprobar la clave guardada: %v", err), false
	}
}

// saveNotice names what was actually committed. Announcing a key that was never written would be a
// lie about the one operation the user cannot see the result of.
func saveNotice(wroteKey, wroteRegion bool) string {
	switch {
	case wroteKey && wroteRegion:
		return "Clave y región guardadas"
	case wroteKey:
		return "Clave guardada"
	default:
		return "Región guardada"
	}
}

// beginReadinessChange takes the readiness lock for a whole setter; the returned function releases it.
//
// Deferred at the TOP of every setter that can make an engine usable or unusable, so validation, the
// credential and the settings are covered together. Anything narrower leaves the window this exists to
// close: a change that has begun but not finished must not read as "nothing is happening".
//
// IT DOES NOT COUNT ANYTHING. The count is bumped where something actually landed — see
// readinessChanged. Counting on the way out would make a REJECTED save look like a change, and the
// launch check treats a change as a reason to start over: three refused saves in a row would use up
// its retries and leave the app on an unusable engine for the rest of the session.
func (s *SettingsService) beginReadinessChange() func() {
	s.readinessMu.Lock()
	return func() { s.readinessMu.Unlock() }
}

// readinessChanged records that something affecting engine readiness has just landed. Only ever
// called with the lock held, by the setter that did it.
func (s *SettingsService) readinessChanged() { s.readiness++ }

// readinessNow is the completed-change count, taken safely.
func (s *SettingsService) readinessNow() uint64 {
	s.readinessMu.Lock()
	defer s.readinessMu.Unlock()
	return s.readiness
}

// envOverrideFor reports the LOQUI_*_KEY variable currently supplying a slot's credential, or "".
//
// Every path that WRITES a secret has to consult it. An override is what dictation actually uses, so
// storing something underneath would report success and change nothing the user can
// observe — the slot goes on reading as configured from the variable, and the item just written is
// invisible until the variable is removed, at which point a credential the user has forgotten about
// silently becomes live.
func envOverrideFor(slot store.KeySlot) string {
	name, _, set := envCredential(slot)
	if set {
		return name
	}
	return ""
}

// envCredential is the ONE answer to "what is the environment supplying for this slot".
//
// Three outcomes, and the middle one is the reason this exists: a variable that is SET BUT BLANK is
// in force — keyReaderFor hands it to dictation ahead of the store — and cannot authenticate
// anything. Every path has to agree on that. They did not: the probe called it unusable while the
// save path took it as proof a credential existed and the payload reported the slot as configured,
// so a card could read "Conectado" while dictation received whitespace.
//
// name is the variable that governs the slot (empty for a slot with no hatch), value what it holds,
// and set whether it is in force at all.
func envCredential(slot store.KeySlot) (name string, value string, set bool) {
	name = envKeyOverride(slot)
	if name == "" {
		return "", "", false
	}
	value = os.Getenv(name)
	return name, value, value != ""
}

// envCredentialUsable reports whether the environment is supplying something that could actually
// authenticate. A blank variable is in force AND unusable, which is a state of its own.
func envCredentialUsable(slot store.KeySlot) bool {
	_, value, set := envCredential(slot)
	return set && strings.TrimSpace(value) != ""
}

// secretsMessage turns a credential-store failure into something the user can act on.
//
// NO MORE "unconfirmed". Under the Keychain backend a timeout meant the call had been abandoned and
// might still land, so the only honest wording was "not confirmed" and the advice was to reopen
// Ajustes and look. The file backend has no such state: the write either completed or it did not, so
// the message can say what happened and point at the file to inspect.
func secretsMessage(err error) string {
	if errors.Is(err, store.ErrSecretsUnreadable) {
		return "no se pudieron leer las claves guardadas, así que no se cambió nada — " +
			"revisa el archivo de claves (Acerca de muestra la carpeta) o bórralo para empezar de cero"
	}
	return fmt.Sprintf("no se pudo guardar la clave: %v", err)
}

// knownKeySlot rejects anything that is not one of the declared credential slots, so a typo
// cannot store a credential under a name nothing will ever read.
func knownKeySlot(slot string) (store.KeySlot, bool) {
	for _, known := range store.AllKeySlots {
		if string(known) == slot {
			return known, true
		}
	}
	return "", false
}

// store is the shared Store. Reached through the bootstrap so there is exactly one instance.
func (s *SettingsService) store() *store.Store { return s.bootstrap.store }

// secretWriter is the store's SetKey unless a test replaced it. The real one writes the real data
// directory, which a unit test must never touch — it would overwrite the developer's own credentials.
func (s *SettingsService) secretWriter() func(store.KeySlot, string) error {
	if s.setSecret != nil {
		return s.setSecret
	}
	return s.store().SetKey
}

func (s *SettingsService) secretDeleter() func(store.KeySlot) error {
	if s.deleteSecret != nil {
		return s.deleteSecret
	}
	return s.store().DeleteKey
}
