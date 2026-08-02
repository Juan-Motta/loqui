// "Probar conexión": does this credential actually work, asked before anything depends on it.
//
// A READ, NOT A SETTER, and that is the whole shape of this file. Nothing here writes: the user is
// checking a key, not committing to it — which is why the button is worth pressing BEFORE Guardar,
// and why a failed probe must leave the stored configuration exactly as it was.
//
// It still returns a payload. Not because it changed anything, but because it is the next thing in
// the app that READS the Keychain, and on a build where writes time out and land afterwards the
// page can be painted from a snapshot that says there is no key when there is one. Answering "your
// key works" while the card beside it says "Sin configurar" is a worse bug than the silence this
// whole change is fixing.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Juan-Motta/loqui-go/internal/settings"
	"github.com/Juan-Motta/loqui-go/internal/store"
	"github.com/Juan-Motta/loqui-go/internal/stt/azure"
)

// ProbeResult is what the page shows after a connection test.
//
// No Go error, for the same reason WriteResult has none: Wails discards a bound method's result
// whenever it also returns an error, and here the payload and the message are the point.
type ProbeResult struct {
	OK bool `json:"ok"`
	// Error is empty on success, and already in Spanish.
	Error string `json:"error"`
	// Message is the success wording. Decided here rather than in the page because what a green
	// probe actually proves — the credential was accepted by the region's token endpoint — is a
	// fact about this exchange, not a fixed label.
	Message string `json:"message"`
	// Field names the input the user has to fix: "key", "region", or empty when there is nothing
	// for them to correct. The page paints the border from it and works nothing out for itself —
	// deciding "this error is about the key" by reading the message would put the validation rule
	// in two languages.
	Field string `json:"field"`
	// Payload is the freshly computed state, so the page can repaint from what is true now.
	Payload SettingsPayload `json:"payload"`
}

// probeHTTPTimeout bounds the exchange with Azure.
//
// It covers ONLY the HTTP call: resolving the region and the key happens before the context is
// created, and the Keychain read carries its own three-second limit inside store.GetKey. A budget
// that started earlier would hand the network whatever seconds the Keychain had left over.
const probeHTTPTimeout = 15 * time.Second

// slotsWithProbe is the credentials this app knows how to TEST, which is a different list from the
// ones it can use.
//
// An allowlist rather than store.IsAvailableKeySlot, and the difference is not academic: that
// function is true for grok, whose key would then be sent to Azure's token endpoint — a credential
// posted to another vendor's server, and a "your Grok key is invalid" verdict from a service that
// was never asked about it. Anything absent here is refused before it can leave the machine.
var slotsWithProbe = map[store.KeySlot]bool{
	store.SlotAzureSpeech: true,
}

// TestConnection checks one provider's credential. Bound as Settings.TestConnection().
//
// EVERYTHING THAT CAN FAIL WITHOUT THE NETWORK FAILS FIRST, in this order: the slot, then the
// region, then the key. The order is load-bearing rather than tidy — a region that cannot work does
// not justify paying the Keychain's three seconds to discover the key, and neither justifies a
// request to a URL built from a region Azure would reject.
func (s *SettingsService) TestConnection(slot string, region string, secret string) ProbeResult {
	keySlot, ok := knownKeySlot(slot)
	if !ok {
		return s.probeFailed("", "ranura de clave desconocida: %q", slot)
	}
	if !slotsWithProbe[keySlot] {
		return s.probeFailed("", "este servicio todavía no tiene prueba de conexión")
	}

	regionID, res, ok := s.resolveRegion(region)
	if !ok {
		return res
	}
	key, source, res, ok := s.resolveKey(keySlot, secret)
	if !ok {
		return res
	}
	s.logLine("PROBE", fmt.Sprintf("slot=%s region=%s source=%s", keySlot, regionID, source))

	ctx, cancel := context.WithTimeout(context.Background(), s.httpTimeout())
	defer cancel()

	conn := azure.TestConnection(ctx, regionID, key, s.probeDoer())
	s.logLine("PROBE-DONE", fmt.Sprintf("slot=%s ok=%t", keySlot, conn.OK))

	if conn.OK {
		return ProbeResult{
			OK:      true,
			Message: "Conexión correcta: Azure aceptó la clave para esa región",
			Payload: s.bootstrap.Payload(),
		}
	}
	// A deadline that ran out is NOT a rejected credential, and saying so would send the user to
	// re-copy a key that may be perfectly good.
	if ctx.Err() != nil {
		return s.probeFailed("", "Azure no respondió a tiempo (%s) — comprueba tu conexión a internet",
			s.httpTimeout())
	}
	if conn.Kind == azure.ConnNetwork {
		// Never reached Azure. conn.Error here is Go's transport text — English, and about sockets
		// rather than about anything the user can act on — so it goes to the log and they get a
		// sentence that tells them where to look.
		s.logLine("PROBE-NET", fmt.Sprintf("slot=%s region=%s: %s", keySlot, regionID, conn.Error))
		return s.probeFailed("", "No se pudo contactar con Azure — comprueba tu conexión a internet. "+
			"El detalle técnico está en el registro")
	}
	return s.probeFailed("", "%s", conn.Error)
}

// resolveRegion picks the region to test and validates it.
//
// The form's value wins so the user can try a region before saving it; the stored one is the
// fallback. It is validated even when it comes from disk, because nothing validates it on the way
// in — LoadSettings accepts any string that is valid JSON, so a settings file copied from another
// machine can hold a region no endpoint exists for.
func (s *SettingsService) resolveRegion(region string) (string, ProbeResult, bool) {
	candidate := strings.TrimSpace(region)
	if candidate == "" {
		candidate = s.store().LoadSettings().Region
	}
	if strings.TrimSpace(candidate) == "" {
		return "", s.probeFailed("region", "elige una región antes de probar la conexión"), false
	}
	id, err := settings.NormalizeRegion(candidate)
	if err != nil {
		return "", s.probeFailed("region", "%v", err), false
	}
	return id, ProbeResult{}, true
}

// resolveKey finds the credential to test, and says where it came from.
//
// THE TYPED FIELD WINS, and it is checked before anything else is consulted: trying a key BEFORE
// saving it is the reason this button is worth pressing, and looking up a stored credential the
// caller is not asking about would cost the Keychain's three seconds for nothing. With the field
// empty the precedence is dictation's own (keyReaderFor): the environment override, then the
// Keychain — so an empty field tests exactly what the microphone would use.
//
// THE THREE OUTCOMES OF A KEYCHAIN READ STAY APART. "Nothing stored" is fixed by typing a key;
// "the Keychain did not answer" is fixed by signing the app, and telling that user to type their
// key again wastes their time on a credential that is probably already there. The field marker
// follows the same split: only the first is something an input can fix.
func (s *SettingsService) resolveKey(slot store.KeySlot, typed string) (string, string, ProbeResult, bool) {
	if secret := strings.TrimSpace(typed); secret != "" {
		return secret, "typed", ProbeResult{}, true
	}
	if name, value, set := envCredential(slot); set {
		if !envCredentialUsable(slot) {
			// In force and unusable at once. Naming the variable is the only way the user finds out:
			// a good key may be sitting in the Keychain underneath, and nothing will read it while
			// this is defined.
			return "", "", s.probeFailed("", "la variable de entorno %s está definida pero vacía — "+
				"mientras lo esté, es la clave que se usa, y no puede autenticar nada", name), false
		}
		return value, "env", ProbeResult{}, true
	}

	key, err := s.secretReader()(slot)
	switch {
	case err == nil:
		return key, "keychain", ProbeResult{}, true
	case errors.Is(err, store.ErrNoSecret):
		return "", "", s.probeFailed("key", "falta la clave: escríbela o guárdala antes de probar"), false
	case errors.Is(err, store.ErrKeychainTimeout):
		// Deliberately NOT keychainMessage: that text describes a WRITE whose outcome is unconfirmed
		// and may still land, and tells the user to reopen Ajustes to see what happened. A read has
		// nothing pending to land.
		return "", "", s.probeFailed("", "el Keychain no respondió, así que no se pudo leer la clave "+
			"guardada — firma la app con una identidad estable, o pasa la clave en %s para probar",
			envKeyOverride(slot)), false
	default:
		return "", "", s.probeFailed("", "no se pudo leer la clave guardada: %v", err), false
	}
}

// probeFailed wraps a refused or failed probe. The payload is computed either way, so a page that
// repaints from it shows what is actually stored rather than what the attempt assumed.
func (s *SettingsService) probeFailed(field string, format string, args ...any) ProbeResult {
	return ProbeResult{
		Error:   fmt.Sprintf(format, args...),
		Field:   field,
		Payload: s.bootstrap.Payload(),
	}
}

// probeDoer is a real HTTP client unless a test replaced it.
//
// The client is built HERE rather than passing nil to azure.NewTokenService, which would install
// its own ten-second one: the timeout the code applies and the timeout this file documents have to
// be the same number.
func (s *SettingsService) probeDoer() azure.Doer {
	if s.probeClient != nil {
		return s.probeClient
	}
	return &http.Client{Timeout: probeHTTPTimeout}
}

func (s *SettingsService) httpTimeout() time.Duration {
	if s.probeTimeout > 0 {
		return s.probeTimeout
	}
	return probeHTTPTimeout
}

// secretReader is store.GetKey unless a test replaced it. The real one talks to the login Keychain,
// which a unit test must never read: the answer would depend on whether the developer running it
// happens to have a key stored, and it would pay the Keychain timeout on an ad-hoc-signed build.
func (s *SettingsService) secretReader() func(store.KeySlot) (string, error) {
	if s.getSecret != nil {
		return s.getSecret
	}
	return store.GetKey
}

// logLine records a diagnostic line, or nothing when no logger was wired.
//
// It exists for a specific and narrow reason: a button inside a Wails webview cannot be driven from
// a script, so the only way to verify from outside which configuration a probe actually used is for
// the probe to say so. It records the slot, the region and where the key came from — NEVER the key.
func (s *SettingsService) logLine(tag, msg string) {
	if s.log == nil {
		return
	}
	s.log(tag, msg)
}
