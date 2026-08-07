package app

import (
	"strings"
	"testing"

	"github.com/Juan-Motta/loqui-go/internal/i18n"
	"github.com/Juan-Motta/loqui-go/internal/store"
)

// TRANSLATION HAPPENS AT THE BOUNDARY, and that is the whole reason it is cheap.
//
// The rules that decide this wording are pure functions in `store` and they emit SPANISH — which is
// exactly the catalogue's key format. So nothing has to thread a locale through seventeen files and
// rewrite their tests: the payload is walked once, on its way out, and every user-facing string is
// looked up. A string with no entry comes back unchanged, which is the fallback working, not a bug.
func TestThePayloadIsTranslatedOnItsWayOut(t *testing.T) {
	st := store.NewAt(t.TempDir())
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.AppLanguage = "en"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	b := testBootstrap(t, st)
	b.systemLocale = func() string { return "es-CO" } // the explicit choice must win

	p := b.Payload()

	if p.Locale != "en" {
		t.Fatalf("locale = %q, want en", p.Locale)
	}
	var spanish []string
	for _, row := range p.Connections {
		for _, field := range []string{row.Kind, row.Label} {
			// Every one of these came from a Spanish source string. If it is still Spanish AND the
			// catalogue has an entry for it, the boundary pass did not run.
			if field == "" {
				continue
			}
			if translated := i18n.T(i18n.English, field, nil); translated != field {
				spanish = append(spanish, field+" → should be "+translated)
			}
		}
	}
	if len(spanish) > 0 {
		t.Errorf("%d connection strings crossed untranslated:\n  %s",
			len(spanish), strings.Join(spanish, "\n  "))
	}
}

// The authored language must come back byte-identical: in Spanish the key IS the answer, so a
// boundary pass that changed anything would mean it is doing something other than a lookup.
func TestSpanishIsUnchangedByTheBoundaryPass(t *testing.T) {
	st := store.NewAt(t.TempDir())
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.AppLanguage = "es"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	b := testBootstrap(t, st)
	b.systemLocale = func() string { return "en-GB" }

	p := b.Payload()

	for _, row := range p.Connections {
		if row.Label != "" && i18n.T(i18n.Spanish, row.Label, nil) != row.Label {
			t.Errorf("Spanish label was altered: %q", row.Label)
		}
	}
}

// Notices and errors are the OTHER half of what the user reads, and they do not travel in the
// payload — they come back beside it, from WriteResult and ProbeResult. Translating the payload
// alone leaves an English card with a Spanish sentence under it.
//
// The format string is what gets looked up, "%s" and all: it is the authored Spanish, so it is the
// key, and the arguments are filled in afterwards. That keeps every call site in the seventeen files
// unchanged — they go on writing Spanish, which is what the catalogue is keyed by.
func TestNoticesAndErrorsAreTranslated(t *testing.T) {
	st := store.NewAt(t.TempDir())
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.AppLanguage = "en"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	svc, vault, _, _ := probeService(t, st)
	svc.bootstrap.systemLocale = func() string { return "es-CO" }

	t.Run("an error", func(t *testing.T) {
		res := svc.SaveConnection("ranura-que-no-existe", "", "x")
		if res.Error == "" {
			t.Fatal("expected a refusal")
		}
		if strings.Contains(res.Error, "ranura de clave desconocida") {
			t.Errorf("the error crossed in Spanish: %q", res.Error)
		}
		if !strings.Contains(res.Error, "unknown key slot") {
			t.Errorf("error = %q, want the English wording", res.Error)
		}
	})

	// Most setters answer with an empty notice; this one speaks, because a credential vanishing
	// without a word is the failure its wording was written for.
	t.Run("a notice", func(t *testing.T) {
		vault.set(store.SlotOpenAI, "una-clave")
		res := svc.DeleteKey("openai")
		if res.Error != "" {
			t.Fatalf("unexpected error: %q", res.Error)
		}
		if res.Notice == "" {
			t.Fatal("a successful write must say something")
		}
		if i18n.T(i18n.English, res.Notice, nil) != res.Notice {
			t.Errorf("the notice crossed untranslated: %q", res.Notice)
		}
	})
}
