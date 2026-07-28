package app

import (
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Juan-Motta/loqui-go/internal/audio"
	"github.com/Juan-Motta/loqui-go/internal/permissions"
	"github.com/Juan-Motta/loqui-go/internal/store"
)

// testBootstrap builds a Bootstrap isolated from the machine. Every seam is stubbed on
// purpose: the real ones read the login Keychain (which does not answer on an ad-hoc-signed
// build), the TCC database, and the actual audio hardware. None of those belong in a unit
// test, and all three vary per developer.
func testBootstrap(t *testing.T, st *store.Store) *Bootstrap {
	t.Helper()
	// A real override in the developer's environment would defeat the key-presence cases.
	for _, slot := range store.AllKeySlots {
		if name := envKeyOverride(slot); name != "" {
			t.Setenv(name, "")
		}
	}
	return &Bootstrap{
		store:     st,
		keyStatus: func(store.KeySlot) store.KeyStatus { return store.KeyAbsent },
		perms: func() PermissionsState {
			return PermissionsState{
				Microphone:        permissions.Granted,
				SpeechRecognition: permissions.NotDetermined,
				Accessibility:     false,
			}
		},
		devices: func() ([]audio.InputDevice, error) { return nil, nil },
	}
}

// marshalForTest renders the payload the way it actually crosses into the webview, so a leak
// check looks at the real wire form rather than at the fields the test happens to name.
func marshalForTest(t *testing.T, p SettingsPayload) string {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshalling the payload: %v", err)
	}
	return string(raw)
}

func keyStateFor(t *testing.T, p SettingsPayload, slot store.KeySlot) KeyState {
	t.Helper()
	for _, k := range p.Keys {
		if k.Slot == string(slot) {
			return k
		}
	}
	t.Fatalf("the payload has no entry for slot %q", slot)
	return KeyState{}
}

// The whole point of the payload is that ONE call gives the Ajustes page everything it needs
// to paint. If a field is missing the UI has to make a second round trip and paint a
// half-empty form in the meantime, which is exactly what this seam exists to avoid.
func TestPayloadCarriesEverythingAjustesNeedsToPaint(t *testing.T) {
	st := store.NewAt(t.TempDir())
	settings := st.LoadSettings()
	settings.Provider = "grok"
	settings.Mode = "toggle"
	settings.TriggerKey = "fn"
	settings.Appearance = "dark"
	settings.AppLanguage = "es"
	settings.InputDeviceID = "mic-2"
	settings.Onboarded = true
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("saving settings: %v", err)
	}

	b := testBootstrap(t, st)
	b.devices = func() ([]audio.InputDevice, error) {
		return []audio.InputDevice{
			{ID: "mic-1", Name: "MacBook Pro Microphone", Default: true},
			{ID: "mic-2", Name: "Shure MV7", Default: false},
		}, nil
	}

	p := b.Payload()

	if p.Provider != "grok" {
		t.Errorf("Provider = %q, want grok", p.Provider)
	}
	if p.Mode != "toggle" {
		t.Errorf("Mode = %q, want toggle", p.Mode)
	}
	if p.TriggerKey != "fn" {
		t.Errorf("TriggerKey = %q, want fn", p.TriggerKey)
	}
	if p.Appearance != "dark" {
		t.Errorf("Appearance = %q, want dark", p.Appearance)
	}
	if p.AppLanguage != "es" {
		t.Errorf("AppLanguage = %q, want es", p.AppLanguage)
	}
	if !p.Onboarded {
		t.Error("Onboarded = false, want true")
	}
	if p.InputDeviceID != "mic-2" {
		t.Errorf("InputDeviceID = %q, want mic-2", p.InputDeviceID)
	}
	if len(p.InputDevices) != 2 {
		t.Fatalf("InputDevices = %d entries, want 2", len(p.InputDevices))
	}
	// The About view shows the data directory and a bug report needs it.
	if p.DataDir != st.Dir() {
		t.Errorf("DataDir = %q, want %q", p.DataDir, st.Dir())
	}
	// Languages come per slot: a single global list only ever worked for Azure.
	if got := p.LanguageBySlot["azure-speech"]; len(got) == 0 {
		t.Error("LanguageBySlot has no entry for azure-speech")
	}
	// Every slot must appear, even the ones with no key: the picker lists them all.
	if len(p.Keys) != len(store.AllKeySlots) {
		t.Errorf("Keys = %d entries, want %d (one per slot)", len(p.Keys), len(store.AllKeySlots))
	}
}

// The payload crosses into the webview, so a key state may carry PRESENCE and nothing else.
// A secret leaked into the DOM is readable by anything that can run script in that window, and
// lands in every log or crash dump that captures the payload.
//
// Asserted against the SERIALIZED schema rather than by hunting for a sentinel value: the
// presence reader is not given a secret to leak in the first place, so a sentinel test would
// pass no matter what the code did. Pinning the field set is what actually holds — adding a
// secret to KeyState cannot help but break this.
func TestAKeyStateCarriesPresenceAndNothingElse(t *testing.T) {
	st := store.NewAt(t.TempDir())
	b := testBootstrap(t, st)
	b.keyStatus = func(store.KeySlot) store.KeyStatus { return store.KeyPresent }

	var wire struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.Unmarshal([]byte(marshalForTest(t, b.Payload())), &wire); err != nil {
		t.Fatalf("unmarshalling the payload: %v", err)
	}
	if len(wire.Keys) == 0 {
		t.Fatal("the payload carried no key states")
	}
	allowed := map[string]bool{"slot": true, "status": true, "fromEnv": true, "available": true}
	for _, k := range wire.Keys {
		for field := range k {
			if !allowed[field] {
				t.Errorf("key state carries unexpected field %q — presence only", field)
			}
		}
		if k["status"] != string(store.KeyPresent) {
			t.Errorf("status = %v, want %q", k["status"], store.KeyPresent)
		}
	}
}

// A key supplied by the env-var escape hatch must read as PRESENT, because dictation will
// work with it. Reporting "no key" while the engine happily dictates sends the user to
// re-enter a credential that is not the one in use.
//
// It must also be marked as coming from the environment: the UI cannot offer to delete it —
// there is nothing in the Keychain to delete — and the user needs to know why the field
// looks configured but empty.
func TestEnvOverrideCountsAsAKeyAndIsMarkedAsSuch(t *testing.T) {
	st := store.NewAt(t.TempDir())
	b := testBootstrap(t, st)
	t.Setenv("LOQUI_GROK_KEY", "xai-from-env")

	p := b.Payload()

	grok := keyStateFor(t, p, store.SlotGrok)
	if grok.Status != store.KeyPresent {
		t.Errorf("status = %q, want %q — an env-var key must count as present",
			grok.Status, store.KeyPresent)
	}
	if !grok.FromEnv {
		t.Error("an env-var key must be marked FromEnv so the UI does not offer to delete it")
	}
	// The other slots are untouched by one override.
	if got := keyStateFor(t, p, store.SlotAzureSpeech).Status; got == store.KeyPresent {
		t.Error("the Grok override leaked into the Azure slot")
	}
}

// A Keychain that does not ANSWER must not be reported as "no key configured".
//
// This is the same failure the permissions code already guards against, one layer down:
// guessing on an unreadable state. Here the guess costs more than a wrong label — told the
// slot is empty, the user retypes a credential that is already stored, and on an ad-hoc-signed
// build (which is every local build) the write is just as likely to hang as the read was. The
// two states send the user to completely different places, so the payload has to keep them
// apart. dictation.go:238 already refuses to collapse them for the same reason.
func TestAnUnreadableKeychainIsNotReportedAsAnEmptySlot(t *testing.T) {
	st := store.NewAt(t.TempDir())
	b := testBootstrap(t, st)
	b.keyStatus = func(slot store.KeySlot) store.KeyStatus {
		switch slot {
		case store.SlotGrok:
			return store.KeyUnreadable
		default:
			return store.KeyAbsent
		}
	}

	p := b.Payload()

	if got := keyStateFor(t, p, store.SlotGrok).Status; got != store.KeyUnreadable {
		t.Errorf("Grok status = %q, want %q — an unanswered Keychain was reported as a definite state",
			got, store.KeyUnreadable)
	}
	if got := keyStateFor(t, p, store.SlotAzureSpeech).Status; got != store.KeyAbsent {
		t.Errorf("Azure status = %q, want %q", got, store.KeyAbsent)
	}
}

// The env override outranks the Keychain, including an unreadable one — that is the entire
// point of the escape hatch: it exists BECAUSE the Keychain does not answer on these builds.
// Reporting "unreadable" while dictation happily uses the env key would be the same lie in
// the other direction.
func TestTheEnvOverrideWinsOverAnUnreadableKeychain(t *testing.T) {
	st := store.NewAt(t.TempDir())
	b := testBootstrap(t, st)
	b.keyStatus = func(store.KeySlot) store.KeyStatus { return store.KeyUnreadable }
	t.Setenv("LOQUI_GROK_KEY", "xai-from-env")

	p := b.Payload()

	grok := keyStateFor(t, p, store.SlotGrok)
	if grok.Status != store.KeyPresent {
		t.Errorf("status = %q, want %q — the env key is what dictation will actually use",
			grok.Status, store.KeyPresent)
	}
	if !grok.FromEnv {
		t.Error("FromEnv = false for an env-supplied key")
	}
}

// A slot whose key comes from the environment must not be looked up in the Keychain AT ALL.
//
// This is a latency bug, not a tidiness one. Each read is bounded by store's three-second
// timeout, and on an ad-hoc-signed build — every local build — the Keychain does not answer, so
// consulting all five slots costs fifteen seconds. This payload is what the Ajustes page paints
// from, so that time is the page hanging blank. The env hatch exists BECAUSE the Keychain is
// broken on these builds; paying for it anyway defeats the whole point.
func TestAnEnvOverriddenSlotIsNeverLookedUpInTheKeychain(t *testing.T) {
	st := store.NewAt(t.TempDir())
	b := testBootstrap(t, st)
	t.Setenv("LOQUI_GROK_KEY", "xai-from-env")

	var mu sync.Mutex
	var consulted []store.KeySlot
	b.keyStatus = func(slot store.KeySlot) store.KeyStatus {
		mu.Lock()
		consulted = append(consulted, slot)
		mu.Unlock()
		return store.KeyAbsent
	}

	b.Payload()

	mu.Lock()
	defer mu.Unlock()
	for _, slot := range consulted {
		if slot == store.SlotGrok {
			t.Error("the Keychain was consulted for a slot already satisfied by an env key")
		}
	}
	// The others still have to be read — only the overridden one is skipped.
	if len(consulted) != len(store.AllKeySlots)-1 {
		t.Errorf("consulted %d slots, want %d", len(consulted), len(store.AllKeySlots)-1)
	}
}

// The remaining slots must be read CONCURRENTLY. Sequentially they cost one timeout each, and
// the whole point of bounding this is that the page paints in one timeout, not five.
//
// The stub blocks every call until all of them have arrived, so a sequential implementation
// cannot finish and the test fails on its own deadline rather than on a timing guess.
func TestTheKeychainSlotsAreReadConcurrently(t *testing.T) {
	st := store.NewAt(t.TempDir())
	b := testBootstrap(t, st)

	want := len(store.AllKeySlots)
	arrived := make(chan struct{}, want)
	release := make(chan struct{})
	b.keyStatus = func(store.KeySlot) store.KeyStatus {
		arrived <- struct{}{}
		<-release // only unblocks once every slot is in flight
		return store.KeyAbsent
	}

	done := make(chan SettingsPayload, 1)
	go func() { done <- b.Payload() }()

	for i := 0; i < want; i++ {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d slot reads were in flight — they are running sequentially", i, want)
		}
	}
	close(release)

	select {
	case p := <-done:
		if len(p.Keys) != want {
			t.Errorf("Keys = %d entries, want %d", len(p.Keys), want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Payload never returned")
	}
}

// Azure Speech needs a region as much as it needs a key, and the store persists one. A payload
// that claims to carry everything the page paints from cannot omit it: with the region missing
// the Ajustes form would blank the field the user already filled in, and saving from that form
// would then wipe a working Azure configuration.
func TestPayloadCarriesTheAzureRegion(t *testing.T) {
	st := store.NewAt(t.TempDir())
	settings := st.LoadSettings()
	settings.Provider = "azure" // what dictation.go switches on; "azure-speech" is a slot
	settings.Region = "eastus2"
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("saving settings: %v", err)
	}
	b := testBootstrap(t, st)

	if got := b.Payload().Region; got != "eastus2" {
		t.Errorf("Region = %q, want eastus2", got)
	}
}

// Every language slot must arrive with its declared default, including the slots a hand-edited
// file left empty or null. store.LanguagesIn owns those fallback rules; if the payload passed
// the raw map through instead, the webview would have to reimplement the same defaulting —
// which is exactly the duplication this seam exists to remove — or paint an empty control.
func TestEverySlotGetsItsDeclaredLanguageDefault(t *testing.T) {
	st := store.NewAt(t.TempDir())
	settings := st.LoadSettings()
	// A file a user could plausibly have: one slot emptied, one explicitly null.
	settings.LanguageBySlot["whisper"] = []string{}
	settings.LanguageBySlot["grok"] = nil
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("saving settings: %v", err)
	}
	b := testBootstrap(t, st)

	p := b.Payload()

	// Exact values, not merely non-empty. "Non-empty" is what let the cloud slots fall through
	// to the "en-US" last resort unnoticed — which does not mean "no preference", it pins a
	// cloud engine to English and silently stops it auto-detecting the language.
	want := map[string][]string{
		"whisper":      {"auto"}, // emptied above, so this is the default coming back
		"grok":         {"auto"}, // nulled above, same
		"macos":        {"es-CO"},
		"azure-speech": {"es-CO", "en-US"},
		"azure-openai": {"auto"},
		"openai":       {"auto"},
		"elevenlabs":   {"auto"},
	}
	for _, slot := range store.AllLanguageSlots {
		got := p.LanguageBySlot[slot]
		if len(got) == 0 {
			t.Errorf("slot %q has no language list", slot)
			continue
		}
		if !slices.Equal(got, want[slot]) {
			t.Errorf("slot %q languages = %v, want %v", slot, got, want[slot])
		}
	}
	// Nothing outside the declared set, so the UI cannot be handed a slot it has no control for.
	if len(p.LanguageBySlot) != len(store.AllLanguageSlots) {
		t.Errorf("LanguageBySlot has %d slots, want %d", len(p.LanguageBySlot), len(store.AllLanguageSlots))
	}
}

// The three grants drive their own rows in Ajustes, and an unreadable one must arrive as
// unknown rather than as a guess. The Electron build learned this the hard way: defaulting to
// "granted" made a denied microphone look fine while every dictation died.
func TestPayloadReportsThePermissionsVerbatim(t *testing.T) {
	st := store.NewAt(t.TempDir())
	b := testBootstrap(t, st)
	b.perms = func() PermissionsState {
		return PermissionsState{
			Microphone:        permissions.Denied,
			SpeechRecognition: permissions.Unknown,
			Accessibility:     true,
		}
	}

	p := b.Payload()

	if p.Permissions.Microphone != permissions.Denied {
		t.Errorf("Microphone = %q, want denied", p.Permissions.Microphone)
	}
	if p.Permissions.SpeechRecognition != permissions.Unknown {
		t.Errorf("SpeechRecognition = %q, want unknown", p.Permissions.SpeechRecognition)
	}
	if !p.Permissions.Accessibility {
		t.Error("Accessibility = false, want true")
	}
}

// Enumerating the audio hardware is the one step here that talks to a device and can fail on
// its own. When it does, the page must still paint: the engine picker, the keys and the
// permissions have nothing to do with the microphone list. Failing the whole payload would
// leave the user with no way to reach the settings that could fix the problem.
func TestADeviceEnumerationFailureStillPaintsThePage(t *testing.T) {
	st := store.NewAt(t.TempDir())
	settings := st.LoadSettings()
	settings.Provider = "whisper"
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("saving settings: %v", err)
	}
	b := testBootstrap(t, st)
	b.devices = func() ([]audio.InputDevice, error) {
		return nil, errors.New("malgo: no context")
	}

	p := b.Payload()

	if p.Provider != "whisper" {
		t.Errorf("Provider = %q — the rest of the payload did not survive the device failure", p.Provider)
	}
	if p.DevicesError == "" {
		t.Error("DevicesError is empty — the UI cannot tell an empty list from a failed read")
	}
	// Not nil: a nil slice marshals to JSON null and the UI would have to guard for it.
	if p.InputDevices == nil {
		t.Error("InputDevices is nil, want an empty list")
	}
}

// A corrupt or hand-edited settings file already falls back to defaults in the store; the
// payload must inherit that rather than handing the UI empty strings it would paint as
// "nothing configured".
func TestPayloadFallsBackToDefaultsWithNoSettingsFile(t *testing.T) {
	st := store.NewAt(t.TempDir()) // nothing written
	b := testBootstrap(t, st)

	p := b.Payload()

	want := store.DefaultSettings()
	if p.Provider != want.Provider {
		t.Errorf("Provider = %q, want the default %q", p.Provider, want.Provider)
	}
	if p.TriggerKey != want.TriggerKey {
		t.Errorf("TriggerKey = %q, want the default %q", p.TriggerKey, want.TriggerKey)
	}
	if p.Appearance != want.Appearance {
		t.Errorf("Appearance = %q, want the default %q", p.Appearance, want.Appearance)
	}
	if p.Onboarded {
		t.Error("Onboarded = true on a fresh install")
	}
}
