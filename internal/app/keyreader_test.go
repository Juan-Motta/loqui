package app

import (
	"testing"

	"github.com/Juan-Motta/loqui-go/internal/session"
	"github.com/Juan-Motta/loqui-go/internal/store"
)

// silentUI is enough for the parts of Dictation that only log. The dictation pipeline
// itself needs a machine; these cases do not touch it.
type silentUI struct{ lines []string }

func (u *silentUI) ShowOverlay()                     {}
func (u *silentUI) HideOverlay()                     {}
func (u *silentUI) EmitOverlay(session.OverlayState) {}
func (u *silentUI) EmitLevel(float64)                {}
func (u *silentUI) HistoryChanged()                  {}
func (u *silentUI) Log(tag, msg string)              { u.lines = append(u.lines, tag+" "+msg) }

// stubbedDictation has the Keychain stubbed out. Every case here has to go through the seam:
// the real store.GetKey talks to the LOGIN KEYCHAIN, so a test without it would depend on what
// the developer running it happens to have stored, and would pay the multi-second Keychain
// timeout that an ad-hoc-signed build triggers.
func stubbedDictation() *Dictation {
	return &Dictation{
		ui:        &silentUI{},
		getSecret: func(store.KeySlot) (string, error) { return "", store.ErrNoSecret },
	}
}

// The escape hatch has to be per-slot. It used to be hardcoded to Azure, so a Grok key in
// the environment was silently ignored and the read fell through to a Keychain that does not
// answer on an ad-hoc-signed build — i.e. the provider was untestable for a reason unrelated
// to the provider.
func TestEnvKeyOverrideIsPerSlot(t *testing.T) {
	t.Setenv("LOQUI_GROK_KEY", "xai-from-env")

	d := stubbedDictation()

	got, err := d.keyReaderFor(store.SlotGrok)()
	if err != nil {
		t.Fatalf("reading the grok key: %v", err)
	}
	if got != "xai-from-env" {
		t.Errorf("got %q, want the value from LOQUI_GROK_KEY", got)
	}
}

// One slot's variable must never satisfy another's: dictating into the wrong service with
// the wrong credential is worse than not dictating.
func TestEnvKeyOverrideDoesNotLeakAcrossSlots(t *testing.T) {
	t.Setenv("LOQUI_GROK_KEY", "xai-from-env")

	d := stubbedDictation()

	// Azure has its own variable, unset here, so this must NOT return the Grok value. It
	// falls through to the Keychain, which in a test environment fails — any error is fine,
	// the point is that it did not hand back the other provider's key.
	if got, err := d.keyReaderFor(store.SlotAzureSpeech)(); err == nil && got == "xai-from-env" {
		t.Error("the grok override satisfied an azure key read")
	}
}

func TestEnvKeyOverrideLogsEveryUse(t *testing.T) {
	t.Setenv("LOQUI_GROK_KEY", "xai-from-env")

	d := stubbedDictation()
	ui := d.ui.(*silentUI)
	if _, err := d.keyReaderFor(store.SlotGrok)(); err != nil {
		t.Fatalf("reading the grok key: %v", err)
	}

	if len(ui.lines) == 0 {
		t.Fatal("using the escape hatch logged nothing; it must never be mistaken for the real path")
	}
}
