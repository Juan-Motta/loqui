package app

import (
	"strings"
	"testing"

	"github.com/Juan-Motta/loqui-go/internal/store"
)

// testDictation builds a Dictation isolated from the machine: its own settings directory, and a
// stubbed Keychain so these cases neither depend on what the developer has stored nor pay the
// Keychain timeout that an ad-hoc-signed build triggers.
func testDictation(t *testing.T, provider string) *Dictation {
	t.Helper()
	// Any real override in the developer's environment would defeat the point of the no-key
	// case, so clear them all.
	for _, slot := range store.AllKeySlots {
		if name := envKeyOverride(slot); name != "" {
			t.Setenv(name, "")
		}
	}
	st := store.NewAt(t.TempDir())
	settings := st.LoadSettings()
	settings.Provider = provider
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("saving settings: %v", err)
	}
	return &Dictation{
		store:     st,
		ui:        &silentUI{},
		getSecret: func(store.KeySlot) (string, error) { return "", store.ErrNoSecret },
	}
}

// Without a key this must fail as a CONFIGURATION problem, and the message has to distinguish
// "you never set a key" from "the Keychain did not answer" — they send the user to completely
// different places, and reporting the first for the second means re-entering a key that is
// already there. Azure does this (dictation.go:238) and Grok has the same two outcomes.
func TestBuildGrokProviderWithoutAKeyIsAConfigError(t *testing.T) {
	d := testDictation(t, "grok")

	_, err := d.buildProvider()
	if err == nil {
		t.Fatal("building the Grok provider with no key succeeded")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Ajustes") {
		t.Errorf("error = %q, want it to point the user at Ajustes", msg)
	}
	// A Keychain timeout has its own message naming the escape hatch; a missing key must not
	// borrow it.
	if strings.Contains(msg, "LOQUI_GROK_KEY") && !strings.Contains(msg, "Keychain") {
		t.Errorf("error = %q — a missing key was reported as a Keychain problem", msg)
	}
}

func TestBuildGrokProviderWithTheEnvKey(t *testing.T) {
	d := testDictation(t, "grok")
	// After testDictation, which clears every override to isolate the no-key case.
	t.Setenv("LOQUI_GROK_KEY", "xai-test")

	p, err := d.buildProvider()
	if err != nil {
		t.Fatalf("building the Grok provider: %v", err)
	}
	if p == nil {
		t.Fatal("no provider returned")
	}
	// Grok is fed by the host's capture, unlike the native helpers.
	if !p.WantsAudio() {
		t.Error("the Grok provider must want audio from the host")
	}
}

// An engine that is not ported yet must say so rather than silently substituting another one:
// dictating into the wrong service is worse than not dictating.
func TestUnportedProviderIsReported(t *testing.T) {
	d := testDictation(t, "elevenlabs")

	if _, err := d.buildProvider(); err == nil {
		t.Fatal("an unported provider was accepted")
	}
}
