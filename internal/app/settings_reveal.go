// Showing the user their own stored credential — the ONE place the secret is allowed to cross.
//
// Everything else in this app is built the other way round. The settings payload carries presence and
// never the value (bootstrap.go:29), error messages are ours rather than the provider's, and the
// probe reports a classification instead of what it resolved. Two real leaks were closed on the
// branch before this one. So this file is a deliberate exception, and it is written to be the
// narrowest one available:
//
//   - It is NEVER part of a payload. Payloads are rebuilt and re-sent on every repaint — Sistema,
//     idiomas, onboarding and the permissions refresh all trigger one — so a secret carried there
//     would cross dozens of times per session, unasked. This crosses once, on a press.
//   - It answers for ONE slot, named by the caller.
//   - The value is never logged. The act is (REVEAL slot=… ok=…), because "someone displayed a
//     credential" is worth having in the record; the credential is not.
//   - It refuses everything it cannot answer honestly, and each refusal below is part of the
//     contract, not an edge case.
//
// Asked for by the owner on 2026-08-07, explicitly, after being shown the trade: the keys already sit
// in cleartext on disk (store/secrets.go) with FileVault off, so what this adds is the value reaching
// the webview's memory and whatever can see the screen.
package app

import (
	"errors"
	"fmt"

	"github.com/Juan-Motta/loqui-go/internal/store"
)

// RevealResult is what the eye button gets back. Bound as Settings.RevealKey().
//
// No payload, unlike every other result in this package: revealing changes nothing, so there is no
// state for the page to repaint from. Adding one would also mean this call answers with a payload
// AND a secret, and those should not travel together.
type RevealResult struct {
	OK bool `json:"ok"`
	// Key is the stored credential, and it is empty unless OK. Nothing else in the app puts a
	// secret in a struct that crosses to the page.
	Key string `json:"key"`
	// Error is already in Spanish and never contains credential material — not the stored one, and
	// not the environment's either.
	Error string `json:"error"`
}

// RevealKey hands back the credential stored for one slot.
func (s *SettingsService) RevealKey(slot string) RevealResult {
	keySlot, ok := knownKeySlot(slot)
	if !ok {
		return s.revealFailed("ranura de clave desconocida: %q", slot)
	}
	// Same gate the write path uses. A slot no engine reads is one whose form is meant to be inert,
	// and showing a credential there would advertise a configuration that does nothing.
	if !store.IsAvailableKeySlot(keySlot) {
		return s.revealFailed("este servicio todavía no está disponible en esta versión")
	}
	// An env-var credential is not ours to show. The app did not store it, cannot delete it, and the
	// user cannot tell from the field which of the two they would be looking at — so the button would
	// silently answer a different question depending on the slot. Naming the variable is the useful
	// answer instead.
	if name := envOverrideFor(keySlot); name != "" {
		return s.revealFailed("esta ranura la controla la variable de entorno %s — la clave que se usa "+
			"viene de ahí, no de las guardadas", name)
	}

	key, err := s.secretReader()(keySlot)
	switch {
	case err == nil:
		s.logLine("REVEAL", fmt.Sprintf("slot=%s ok=true", keySlot))
		return RevealResult{OK: true, Key: key}
	case errors.Is(err, store.ErrNoSecret):
		return s.revealFailed("no hay ninguna clave guardada para este servicio")
	case errors.Is(err, store.ErrSecretsUnreadable):
		return s.revealFailed("no se pudieron leer las claves guardadas — revisa el archivo de claves")
	default:
		return s.revealFailed("no se pudo leer la clave guardada: %v", err)
	}
}

// revealFailed words a refusal in the user's language.
//
// A METHOD, not a package function, and that was a real gap rather than a style point: as a bare
// function it had no way to reach a locale, so these three refusals crossed in Spanish onto an
// otherwise English page. The hardened coverage scan is what surfaced it.
func (s *SettingsService) revealFailed(format string, args ...any) RevealResult {
	locale := s.bootstrap.Payload().Locale
	return RevealResult{Error: s.phrase(locale, format, args...)}
}
