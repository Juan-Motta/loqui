// "Probar conexión": does this credential actually work, asked before anything depends on it.
//
// A READ, NOT A SETTER, and that is the whole shape of this file. Nothing here writes: the user is
// checking a key, not committing to it — which is why the button is worth pressing BEFORE Guardar,
// and why a failed probe must leave the stored configuration exactly as it was.
//
// It still returns a payload. Not because it changed anything, but because it is the next thing in
// the app that READS the credentials, so it is the next chance to correct a card that is out of date.
// Answering "your key works" while the card beside it says "Sin configurar" is a worse bug than the
// silence this whole change is fixing. (Under the Keychain backend there was a sharper reason: a write
// could time out and land afterwards, so the page could be painted from a snapshot that said there was
// no key when there was one. The file backend cannot do that — see store/secrets.go.)
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
	"github.com/Juan-Motta/loqui-go/internal/stt"
	"github.com/Juan-Motta/loqui-go/internal/stt/azure"
	"github.com/Juan-Motta/loqui-go/internal/stt/elevenlabs"
	"github.com/Juan-Motta/loqui-go/internal/stt/grok"
	"github.com/Juan-Motta/loqui-go/internal/stt/openai"
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
// created. A budget that started earlier would hand the network whatever was left over from reading
// the credential — which used to be up to three seconds of Keychain timeout, and is now a file read.
const probeHTTPTimeout = 15 * time.Second

// prober is one provider's connection test, plus the name its messages speak in.
//
// The name is not decoration: the wording used to hard-code Azure, so a Grok test with no DNS said
// "No se pudo contactar con Azure" and a green OpenAI claimed Azure had accepted the key "for that
// region". Each entry words its own outcomes.
type prober struct {
	name string
	// usesRegion is Azure's alone. The region must NOT be demanded of a service that has none —
	// resolveRegion runs before the key, so widening the list without this would fail a Grok test with
	// "elige una región antes de probar la conexión".
	usesRegion bool
	probe      func(ctx context.Context, key, region string, doer azure.Doer) stt.ProbeResult
}

// probers is what this app knows how to TEST, and it replaces the allowlist that used to sit beside a
// separate dispatch. One map means "listed but unreachable" is no longer expressible.
//
// It is NOT store.IsAvailableKeySlot, and the difference is not academic: that function is true for
// grok, whose key would then have gone to Azure's token endpoint — a credential posted to another
// vendor's server, and a "your Grok key is invalid" verdict from a service never asked about it. What
// keeps the two lists honest is TestEveryStorableSlotHasAProber, not a comment.
var probers = map[store.KeySlot]prober{
	store.SlotAzureSpeech: {
		name:       "Azure",
		usesRegion: true,
		probe: func(ctx context.Context, key, region string, doer azure.Doer) stt.ProbeResult {
			return azureProbeResult(azure.TestConnection(ctx, region, key, doer))
		},
	},
	store.SlotGrok: {
		name: "xAI",
		probe: func(ctx context.Context, key, _ string, _ azure.Doer) stt.ProbeResult {
			return grok.TestConnection(ctx, key, grok.ProbeOptions{})
		},
	},
	store.SlotOpenAI: {
		name: "OpenAI",
		probe: func(ctx context.Context, key, _ string, _ azure.Doer) stt.ProbeResult {
			return openai.TestConnection(ctx, key, openai.ProbeOptions{})
		},
	},
	store.SlotElevenLabs: {
		name: "ElevenLabs",
		probe: func(ctx context.Context, key, _ string, _ azure.Doer) stt.ProbeResult {
			return elevenlabs.TestConnection(ctx, key, elevenlabs.ProbeOptions{})
		},
	},
}

// azureProbeResult adapts Azure's own result to the shared one, WITHOUT changing its behaviour.
//
// Azure groups 401 and 403 into a single credential verdict (token.go:124) and it keeps doing that: a
// shared shape that split them would have quietly changed behaviour verified against the live service.
// It is the only provider that can report a bad REGION, which has nowhere to go in the shared kinds —
// so it travels as a rejected key with the region named in the message, which is what the user has to
// fix either way.
func azureProbeResult(conn azure.ConnResult) stt.ProbeResult {
	switch {
	case conn.OK:
		return stt.ProbeResult{
			OK:      true,
			Kind:    stt.ProbeOK,
			Message: "Conexión correcta: Azure aceptó la clave para esa región",
		}
	case conn.Kind == azure.ConnNoKey:
		return stt.ProbeResult{Kind: stt.ProbeNoKey, Message: conn.Error}
	case conn.Kind == azure.ConnBadCredentials, conn.Kind == azure.ConnBadRegion:
		return stt.ProbeResult{Kind: stt.ProbeKeyRejected, Message: conn.Error}
	case conn.Kind == azure.ConnNetwork:
		// conn.Error here is Go's transport text. It goes to the log, never to the user.
		return stt.ProbeResult{Kind: stt.ProbeFailed, Code: "network", Detail: conn.Error}
	default:
		return stt.ProbeResult{Kind: stt.ProbeFailed, Message: conn.Error}
	}
}

// TestConnection checks one provider's credential. Bound as Settings.TestConnection().
//
// EVERYTHING THAT CAN FAIL WITHOUT THE NETWORK FAILS FIRST, in this order: the slot, then the region,
// then the key. The order is load-bearing rather than tidy — a region that cannot work does not justify
// reading the credential, and neither justifies a request to a URL built from a region Azure would
// reject.
func (s *SettingsService) TestConnection(slot string, region string, secret string) ProbeResult {
	keySlot, ok := knownKeySlot(slot)
	if !ok {
		return s.probeFailed("", "ranura de clave desconocida: %q", slot)
	}
	p, ok := s.proberFor(keySlot)
	if !ok {
		return s.probeFailed("", "este servicio todavía no tiene prueba de conexión")
	}

	regionID := ""
	if p.usesRegion {
		var res ProbeResult
		if regionID, res, ok = s.resolveRegion(region); !ok {
			return res
		}
	}
	key, source, res, ok := s.resolveKey(keySlot, secret)
	if !ok {
		return res
	}
	s.logLine("PROBE", fmt.Sprintf("slot=%s region=%s source=%s", keySlot, regionID, source))

	ctx, cancel := context.WithTimeout(context.Background(), s.httpTimeout())
	defer cancel()

	conn := p.probe(ctx, key, regionID, s.probeDoer())
	// The code is SERVER-CONTROLLED, so it is filtered before it reaches either the log or the user.
	// Logic below still keys off conn.Code, which is ours; only what is DISPLAYED is filtered.
	shownCode := safeProviderCode(conn.Code, key)
	s.logLine("PROBE-DONE", fmt.Sprintf("slot=%s ok=%t code=%s", keySlot, conn.OK, shownCode))

	if conn.OK {
		return ProbeResult{OK: true, Message: conn.Message, Payload: s.bootstrap.Payload()}
	}
	// A deadline that ran out is NOT a rejected credential, and saying so would send the user to
	// re-copy a key that may be perfectly good.
	if ctx.Err() != nil {
		return s.probeFailed("", "%s no respondió a tiempo (%s) — comprueba tu conexión a internet",
			p.name, s.httpTimeout())
	}
	if conn.Detail != "" {
		// The technical detail is kept for diagnosis and kept OUT of the message: it is English, it is
		// about sockets, and it is not something a person can act on.
		s.logLine("PROBE-NET", fmt.Sprintf("slot=%s region=%s: %s", keySlot, regionID, conn.Detail))
	}
	if conn.Code == "network" {
		return s.probeFailed("", "No se pudo contactar con %s — comprueba tu conexión a internet. "+
			"El detalle técnico está en el registro", p.name)
	}
	// The provider's own code is appended when it gave one: short, non-prose, no key material, and
	// exactly what the user would search for. It is never a substitute for the message.
	//
	// `shownCode`, not `conn.Code`: see safeProviderCode. "No key material" used to be an assertion in
	// this comment and nothing more.
	if shownCode != "" {
		return s.probeFailed("", "%s (%s)", conn.Message, shownCode)
	}
	return s.probeFailed("", "%s", conn.Message)
}

// proberFor is the registry lookup, overridable per SERVICE INSTANCE for tests.
//
// Per instance rather than by mutating the package-level map: parallel tests and two probes in flight
// would otherwise race on it.
func (s *SettingsService) proberFor(slot store.KeySlot) (prober, bool) {
	if s.probers != nil {
		p, ok := s.probers[slot]
		return p, ok && p.probe != nil
	}
	p, ok := probers[slot]
	return p, ok && p.probe != nil
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
// saving it is the reason this button is worth pressing. With the field empty the precedence is
// dictation's own (keyReaderFor): the environment override, then the stored credentials — so an empty
// field tests exactly what the microphone would use.
//
// THE THREE OUTCOMES OF THE READ STAY APART. "Nothing stored" is fixed by typing a key; "your keys
// could not be read" is fixed at the file, and telling that user to type their key again wastes their
// time on a credential that is probably already there. The field marker follows the same split: only
// the first is something an input can fix.
func (s *SettingsService) resolveKey(slot store.KeySlot, typed string) (string, string, ProbeResult, bool) {
	if secret := strings.TrimSpace(typed); secret != "" {
		return secret, "typed", ProbeResult{}, true
	}
	if name, value, set := envCredential(slot); set {
		if !envCredentialUsable(slot) {
			// In force and unusable at once. Naming the variable is the only way the user finds out:
			// a good key may be sitting in the store underneath, and nothing will read it while
			// this is defined.
			return "", "", s.probeFailed("", "la variable de entorno %s está definida pero vacía — "+
				"mientras lo esté, es la clave que se usa, y no puede autenticar nada", name), false
		}
		return value, "env", ProbeResult{}, true
	}

	key, err := s.secretReader()(slot)
	switch {
	case err == nil:
		return key, "stored", ProbeResult{}, true
	case errors.Is(err, store.ErrNoSecret):
		return "", "", s.probeFailed("key", "falta la clave: escríbela o guárdala antes de probar"), false
	case errors.Is(err, store.ErrSecretsUnreadable):
		// Deliberately NOT secretsMessage: that text is about a WRITE that changed nothing. Here nothing
		// was going to be written in the first place — the read is what failed, and the only useful
		// instruction is where the file is.
		return "", "", s.probeFailed("", "no se pudieron leer las claves guardadas, así que no se pudo "+
			"leer la de este motor — revisa el archivo de claves, o pasa la clave en %s para probar",
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

// secretReader is the store's GetKey unless a test replaced it. The real one reads the real data
// directory, which a unit test must never touch: the answer would depend on whether the developer
// running it happens to have a key saved.
func (s *SettingsService) secretReader() func(store.KeySlot) (string, error) {
	if s.getSecret != nil {
		return s.getSecret
	}
	return s.store().GetKey
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

// safeProviderCode decides whether a provider's machine-readable code may be shown.
//
// The design deliberately passes the provider's own code through — `insufficient_quota`,
// `unaccepted_terms`, `invalid_api_key` are short, searchable, and more honest than a bucket we
// invented from documentation we cannot test. But the field is written by the SERVER, and until this
// function existed "short, non-prose, no key material" was only a comment.
//
// That is not a hypothetical distinction. The same review pass found a real leak sitting behind an
// identically worded claim (see describeHandshake in each stt provider): a 401 appended the provider's
// prose — key tail and all — to our own wording, while a comment asserted it never did.
//
// Two rules, and the second is what a charset rule alone would miss: a token shape (so prose, spaces,
// colons and masked echoes like "sk-proj-****ueba" are refused), and never anything containing the
// credential — an Azure key is 32 hex characters, which is a perfectly well-formed "token".
//
// Refused codes are dropped silently rather than logged: the value is the thing that might BE the
// secret, so it must not be written down to explain why it was not written down. The message survives
// without it.
func safeProviderCode(code, secret string) string {
	const maxCodeLen = 40 // "invalid_request_error.invalid_api_key" is 37
	if code == "" || len(code) > maxCodeLen {
		return ""
	}
	if secret != "" && strings.Contains(code, secret) {
		return ""
	}
	for _, r := range code {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_', r == '-', r == '.':
		default:
			return ""
		}
	}
	return code
}
