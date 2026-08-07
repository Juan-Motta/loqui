package i18n

import "testing"

// THE KEY IS THE SPANISH SOURCE STRING, and every behaviour below follows from that one decision.
//
// It is inherited from the Electron original deliberately: there is no invented key namespace to
// keep in sync, a missing translation degrades to readable Spanish instead of leaking something
// like "ui.settings.header" into the interface, and the diff that introduces i18n stays reviewable.
func TestSpanishIsTheKeyItself(t *testing.T) {
	if got := T(Spanish, "Conexiones", nil); got != "Conexiones" {
		t.Errorf("T(es) = %q, want the key back untouched", got)
	}
}

// The property that makes an incomplete catalogue safe: an untranslated string still reads as
// ordinary prose. Returning "" or a key name would put a hole in the interface instead.
func TestAMissingTranslationStaysSpanish(t *testing.T) {
	const key = "Una frase que nadie ha traducido todavía"
	if got := T(English, key, nil); got != key {
		t.Errorf("T(en, missing) = %q, want the Spanish key back", got)
	}
}

func TestATranslationIsUsedWhenThereIsOne(t *testing.T) {
	if got := T(English, "Conexiones", nil); got != "Connections" {
		t.Errorf("T(en) = %q, want %q", got, "Connections")
	}
}

func TestInterpolation(t *testing.T) {
	t.Run("substitutes named params", func(t *testing.T) {
		got := T(Spanish, "el servicio de {provider} no está disponible (status {status})",
			map[string]string{"provider": "Azure", "status": "503"})
		want := "el servicio de Azure no está disponible (status 503)"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	// An unknown placeholder is LEFT ALONE rather than blanked: a visible "{typo}" is a bug report,
	// a silently empty gap is a mystery.
	t.Run("leaves an unknown placeholder in place", func(t *testing.T) {
		got := T(Spanish, "hola {nombre} y {otro}", map[string]string{"nombre": "Juan"})
		if got != "hola Juan y {otro}" {
			t.Errorf("got %q", got)
		}
	})
}

// Empty storage means "follow the OS", which is the default a fresh install gets — so it speaks the
// user's language without being asked, and keeps following if they change the system language.
func TestResolveLocale(t *testing.T) {
	for _, c := range []struct {
		name, stored, system string
		want                 Locale
	}{
		{"nothing stored follows the OS", "", "en-GB", English},
		{"an explicit choice always wins", "es", "en-GB", Spanish},
		{"an explicit English wins over a Spanish OS", "en", "es-CO", English},
		{"an OS language we do not speak falls back", "", "de-DE", Spanish},
		{"an unknown stored value is not trusted", "klingon", "en-US", English},
		{"underscores are read too", "", "en_US", English},
		{"no OS locale at all", "", "", Spanish},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveLocale(c.stored, c.system); got != c.want {
				t.Errorf("ResolveLocale(%q, %q) = %q, want %q", c.stored, c.system, got, c.want)
			}
		})
	}
}
