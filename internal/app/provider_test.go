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

	_, err := d.buildProvider(1)
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

	p, err := d.buildProvider(1)
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

func TestBuildAzureOpenAIProviderFromTheSelectedSubservice(t *testing.T) {
	d := testDictation(t, "azure")
	t.Setenv("LOQUI_AZURE_OPENAI_KEY", "azure-openai-test")
	if err := d.store.UpdateSettings(func(cfg *store.Settings) error {
		cfg.AzureService = "openai"
		cfg.AzureOpenAiResource = "mi-recurso"
		cfg.AzureOpenAiDeployment = "mi-whisper"
		cfg.Region = "" // proves this path does not accidentally validate Azure Speech
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	p, err := d.buildProvider(1)
	if err != nil {
		t.Fatalf("building Azure OpenAI: %v", err)
	}
	if p == nil || !p.WantsAudio() {
		t.Fatal("Azure OpenAI did not build a host-audio realtime provider")
	}
}

func TestBuildAzureOpenAIRequiresItsDeployment(t *testing.T) {
	d := testDictation(t, "azure")
	t.Setenv("LOQUI_AZURE_OPENAI_KEY", "azure-openai-test")
	if err := d.store.UpdateSettings(func(cfg *store.Settings) error {
		cfg.AzureService = "openai"
		cfg.AzureOpenAiResource = "mi-recurso"
		cfg.AzureOpenAiDeployment = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.buildProvider(1); err == nil || !strings.Contains(strings.ToLower(err.Error()), "deployment") {
		t.Fatalf("missing deployment error = %v", err)
	}
}

func TestBuildAzureOpenAIRejectsAnUnknownModelBeforeReadingTheKey(t *testing.T) {
	d := testDictation(t, "azure")
	reads := 0
	d.getSecret = func(store.KeySlot) (string, error) {
		reads++
		return "azure-openai-test", nil
	}
	if err := d.store.UpdateSettings(func(cfg *store.Settings) error {
		cfg.AzureService = "openai"
		cfg.AzureOpenAiResource = "mi-recurso"
		cfg.AzureOpenAiDeployment = "mi-deployment"
		cfg.AzureOpenAiModel = "not-a-model"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := d.buildProvider(1); err == nil || !strings.Contains(err.Error(), "modelo") {
		t.Fatalf("unknown model error = %v", err)
	}
	if reads != 0 {
		t.Errorf("credential reads = %d, want 0", reads)
	}
}

// An engine this build cannot construct must say so rather than silently substituting another one:
// dictating into the wrong service is worse than not dictating.
//
// The provider has to be one buildProvider genuinely does not know. Naming a PORTED engine here
// reaches its own branch and fails for a different reason — no key — so the test passes while the
// branch it claims to cover is never executed. That is what this case used to do with "elevenlabs",
// which has been ported since. Hence the assertion on the message: reaching the right branch is the
// thing being tested.
func TestUnportedProviderIsReported(t *testing.T) {
	d := testDictation(t, "un-motor-que-no-existe")

	_, err := d.buildProvider(1)
	if err == nil {
		t.Fatal("an unported provider was accepted")
	}
	if !strings.Contains(err.Error(), "no está portado") {
		t.Errorf("error = %q — that is not the unported branch, so this case is not covering it", err)
	}
}
