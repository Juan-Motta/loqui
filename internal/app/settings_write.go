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
	"strings"

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
}

// ok wraps a successful write.
func (s *SettingsService) ok() WriteResult {
	return WriteResult{Payload: s.bootstrap.Payload()}
}

// failed wraps a rejected or failed write. The payload is recomputed either way, so the page
// repaints from what is actually stored rather than from what it hoped it had stored.
func (s *SettingsService) failed(format string, args ...any) WriteResult {
	return WriteResult{Payload: s.bootstrap.Payload(), Error: fmt.Sprintf(format, args...)}
}

// SetProvider switches the active engine. Bound as Settings.SetProvider().
func (s *SettingsService) SetProvider(provider string) WriteResult {
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
	return s.ok()
}

// SetRegion stores the Azure Speech region, normalised. Bound as Settings.SetRegion().
//
// Normalised HERE rather than in the page: the user picks a display name ("East US 2") and every
// Azure endpoint needs the id ("eastus2"). settings.NormalizeRegion already owns that rule and is
// tested; doing it in the webview too would be the same rule in two languages.
func (s *SettingsService) SetRegion(region string) WriteResult {
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
	return s.ok()
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
	return s.ok()
}

// SetKey stores one provider's credential in the Keychain. Bound as Settings.SetKey().
//
// The secret is never logged and never echoed back: the payload carries presence only.
func (s *SettingsService) SetKey(slot string, secret string) WriteResult {
	keySlot, ok := knownKeySlot(slot)
	if !ok {
		return s.failed("ranura de clave desconocida: %q", slot)
	}
	if strings.TrimSpace(secret) == "" {
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
		return s.failed("%s", keychainMessage(err))
	}
	return s.ok()
}

// DeleteKey removes one provider's credential. Bound as Settings.DeleteKey().
//
// Refused while a LOQUI_*_KEY override is in force. The UI greys the button out for those slots,
// but that is not the enforcement: this binding is reachable from anything in the webview, and the
// item it would delete is one the user cannot currently see — the override is what dictation is
// using, so the slot would still read as configured afterwards and the deletion would look like it
// did nothing. Whatever is in the Keychain underneath is not what the user is looking at.
func (s *SettingsService) DeleteKey(slot string) WriteResult {
	keySlot, ok := knownKeySlot(slot)
	if !ok {
		return s.failed("ranura de clave desconocida: %q", slot)
	}
	if name := envOverrideFor(keySlot); name != "" {
		return s.failed("esta clave viene de la variable de entorno %s — quítala del entorno en vez de borrarla aquí", name)
	}
	if err := s.secretDeleter()(keySlot); err != nil {
		return s.failed("%s", keychainMessage(err))
	}
	return s.ok()
}

// SaveConnection stores a provider's region and key together, as ONE operation.
// Bound as Settings.SaveConnection().
//
// It exists because doing it from the page as SetRegion-then-SetKey commits half of it whenever the
// second call fails: the region lands, the Keychain write does not, and the user is left with a
// provider that looks configured and cannot connect. Everything is validated BEFORE anything is
// written, so a REJECTED save changes nothing at all.
//
// NOT FULLY ATOMIC, and worth being precise about rather than claiming otherwise: the credential
// lives in the Keychain and the region in a JSON file, and there is no transaction spanning both.
// What the commit order buys is that the LIKELY failure is the harmless one. The Keychain write is
// the part that actually fails on these builds — it hangs when the signature is not recognised — so
// it goes FIRST: if it fails, nothing has changed anywhere. The remaining window is a Keychain
// write that succeeds followed by a failing disk write, which means a new key against the old
// region. That needs the settings file to be unwritable, which is a different class of broken.
//
// An empty secret means "leave the stored key alone", which is how the form behaves when the user
// only wants to change the region. An empty region means the same for the region.
func (s *SettingsService) SaveConnection(slot string, region string, secret string) WriteResult {
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
	writeKey := strings.TrimSpace(secret) != ""

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

	if regionID == "" && !writeKey {
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

	// ---- then commit, Keychain first ----
	// The order matters: the Keychain is the half that actually fails here, so doing it first means
	// its failure leaves EVERYTHING untouched. Region-first would persist a region the user's key
	// was never saved against.
	if writeKey {
		if err := s.secretWriter()(keySlot, secret); err != nil {
			return s.failed("%s", keychainMessage(err))
		}
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
	}
	return s.ok()
}

// envOverrideFor reports the LOQUI_*_KEY variable currently supplying a slot's credential, or "".
//
// Every path that WRITES a secret has to consult it. An override is what dictation actually uses, so
// storing something in the Keychain underneath would report success and change nothing the user can
// observe — the slot goes on reading as configured from the variable, and the item just written is
// invisible until the variable is removed, at which point a credential the user has forgotten about
// silently becomes live.
func envOverrideFor(slot store.KeySlot) string {
	if name := envKeyOverride(slot); name != "" && os.Getenv(name) != "" {
		return name
	}
	return ""
}

// keychainMessage turns a Keychain failure into something the user can act on.
//
// The timeout gets its own wording because it is not a rejected write: the call was abandoned and
// may yet land, so the honest thing to say is "not confirmed", not "not saved". Telling someone
// their key was not saved when it might have been sends them to retype it, and on these builds the
// retype is just as likely to hang.
func keychainMessage(err error) string {
	if errors.Is(err, store.ErrKeychainTimeout) {
		return "el Keychain no respondió — la operación no está confirmada. " +
			"Es el síntoma de una firma de desarrollo inestable; vuelve a abrir Ajustes para ver el estado real."
	}
	return fmt.Sprintf("no se pudo acceder al Keychain: %v", err)
}

// knownKeySlot rejects anything that is not one of the declared credential slots, so a typo
// cannot create a Keychain item nothing will ever read.
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

// secretWriter is store.SetKey unless a test replaced it. The real one talks to the login
// Keychain, which a unit test must never write to — and which does not answer on an
// ad-hoc-signed build.
func (s *SettingsService) secretWriter() func(store.KeySlot, string) error {
	if s.setSecret != nil {
		return s.setSecret
	}
	return store.SetKey
}

func (s *SettingsService) secretDeleter() func(store.KeySlot) error {
	if s.deleteSecret != nil {
		return s.deleteSecret
	}
	return store.DeleteKey
}
