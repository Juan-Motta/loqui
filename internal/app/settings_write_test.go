package app

import (
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Juan-Motta/loqui-go/internal/audio"
	"github.com/Juan-Motta/loqui-go/internal/permissions"
	"github.com/Juan-Motta/loqui-go/internal/store"
)

// testVault stands in for the Keychain, guarded by a mutex.
//
// The lock is not decoration: keyStates fans its slot reads out across goroutines, so anything that
// also writes — a test simulating a write that lands late — races the payload it is about to inspect.
type testVault struct {
	mu      sync.Mutex
	secrets map[store.KeySlot]string
}

func newTestVault() *testVault {
	return &testVault{secrets: map[store.KeySlot]string{}}
}

func (v *testVault) set(slot store.KeySlot, secret string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.secrets[slot] = secret
}

func (v *testVault) get(slot store.KeySlot) (string, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	secret, ok := v.secrets[slot]
	return secret, ok
}

func (v *testVault) delete(slot store.KeySlot) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.secrets, slot)
}

func (v *testVault) size() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.secrets)
}

// testService builds a SettingsService isolated from the machine, with the Keychain replaced by an
// in-memory vault. The real one blocks on an ad-hoc-signed build, and a unit test must never write
// to the developer's login Keychain.
func testService(t *testing.T, st *store.Store) (*SettingsService, *testVault) {
	t.Helper()
	for _, slot := range store.AllKeySlots {
		if name := envKeyOverride(slot); name != "" {
			t.Setenv(name, "")
		}
	}
	vault := newTestVault()
	svc := &SettingsService{
		bootstrap: &Bootstrap{
			store: st,
			keyStatus: func(slot store.KeySlot) store.KeyStatus {
				if _, ok := vault.get(slot); ok {
					return store.KeyPresent
				}
				return store.KeyAbsent
			},
			perms: func() PermissionsState {
				return PermissionsState{Microphone: permissions.Granted}
			},
			devices: func() ([]audio.InputDevice, error) { return nil, nil },
		},
		setSecret: func(slot store.KeySlot, secret string) error {
			vault.set(slot, secret)
			return nil
		},
		deleteSecret: func(slot store.KeySlot) error {
			vault.delete(slot)
			return nil
		},
	}
	return svc, vault
}

// A setter returns the FRESH payload, not just an error.
//
// One round trip instead of two, and more importantly the UI repaints from the same authoritative
// snapshot the backend just computed. Making the page re-derive its new state — or call Load()
// again and paint whatever arrives — is how the two sides drift apart.
func TestSettingProviderReturnsTheRepaintedPayload(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)

	res := svc.SetProvider("grok")
	if res.Error != "" {
		t.Fatalf("SetProvider: %s", res.Error)
	}
	p := res.Payload
	if p.Provider != "grok" {
		t.Errorf("returned payload Provider = %q, want grok", p.Provider)
	}
	// And it is actually persisted, not just reflected back.
	if got := st.LoadSettings().Provider; got != "grok" {
		t.Errorf("persisted Provider = %q, want grok", got)
	}
}

// An engine the app cannot drive must be refused, and the stored setting left ALONE.
//
// Accepting it would leave the app pointing at a provider buildProvider rejects, so the next
// dictation fails with a confusing error — and the user's previously working engine is gone. The
// UI's <select> is not a guarantee: this binding is reachable from anything in the webview.
func TestAnUnknownProviderIsRefusedAndChangesNothing(t *testing.T) {
	st := store.NewAt(t.TempDir())
	settings := st.LoadSettings()
	settings.Provider = "whisper"
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("saving settings: %v", err)
	}
	svc, _ := testService(t, st)

	if res := svc.SetProvider("dragon-naturally-speaking"); res.Error == "" {
		t.Fatal("an unknown provider was accepted")
	}
	if got := st.LoadSettings().Provider; got != "whisper" {
		t.Errorf("Provider = %q — a rejected write still changed the settings", got)
	}
}

// Storing a key must put it in the Keychain and report the slot as present afterwards, so the
// UI can repaint the field as configured without asking again.
func TestStoringAKeyPersistsItAndReportsPresence(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)

	res := svc.SetKey("grok", "xai-abc123")
	if res.Error != "" {
		t.Fatalf("SetKey: %s", res.Error)
	}
	p := res.Payload
	if got, _ := vault.get(store.SlotGrok); got != "xai-abc123" {
		t.Errorf("the Keychain holds %q, want the key that was set", got)
	}
	for _, k := range p.Keys {
		if k.Slot == "grok" && k.Status != store.KeyPresent {
			t.Errorf("returned payload reports grok as %q, want present", k.Status)
		}
	}
	// The secret must not come back out in the repaint.
	if strings.Contains(marshalForTest(t, p), "xai-abc123") {
		t.Fatal("the payload returned by SetKey contains the key itself")
	}
}

// A slot the app has no credential concept for must be refused rather than silently creating a
// Keychain item nothing will ever read.
func TestAnUnknownKeySlotIsRefused(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)

	if res := svc.SetKey("not-a-provider", "secret"); res.Error == "" {
		t.Fatal("an unknown slot was accepted")
	}
	if n := vault.size(); n != 0 {
		t.Errorf("the Keychain gained %d items from a rejected write", n)
	}
}

// An empty key must be refused, not stored.
//
// Storing "" would make the slot read as PRESENT while every dictation fails authentication —
// the exact confusion the three-way status exists to prevent. Clearing a key is DeleteKey's job,
// which the UI reaches from a different control.
func TestAnEmptyKeyIsRefusedRatherThanStored(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)

	if res := svc.SetKey("grok", "   "); res.Error == "" {
		t.Fatal("a blank key was accepted")
	}
	if _, ok := vault.get(store.SlotGrok); ok {
		t.Error("a blank key was written to the Keychain")
	}
}

// Deleting is how the user clears a slot, and it must report the slot as absent afterwards.
func TestDeletingAKeyClearsTheSlot(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	if res := svc.SetKey("grok", "xai-abc123"); res.Error != "" {
		t.Fatalf("SetKey: %s", res.Error)
	}

	res := svc.DeleteKey("grok")
	if res.Error != "" {
		t.Fatalf("DeleteKey: %s", res.Error)
	}
	p := res.Payload
	if _, ok := vault.get(store.SlotGrok); ok {
		t.Error("the key is still in the Keychain")
	}
	for _, k := range p.Keys {
		if k.Slot == "grok" && k.Status != store.KeyAbsent {
			t.Errorf("returned payload reports grok as %q, want absent", k.Status)
		}
	}
}

// A Keychain write that fails must surface as an error AND leave the payload honest — the slot
// must not be painted as configured. On an ad-hoc-signed build this is a real outcome, not a
// hypothetical: the write can time out exactly as the read does.
func TestAFailedKeychainWriteIsReportedAndNotPaintedAsConfigured(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	svc.setSecret = func(store.KeySlot, string) error { return store.ErrKeychainTimeout }

	res := svc.SetKey("grok", "xai-abc123")
	if res.Error == "" {
		t.Fatal("a failed Keychain write was reported as success")
	}
	// The message must say the write is NOT CONFIRMED rather than "not saved": the abandoned call
	// may still land, and telling the user it failed sends them to retype a key that may be there.
	if !strings.Contains(res.Error, "no está confirmada") {
		t.Errorf("error = %q, want it to say the operation is unconfirmed", res.Error)
	}
	p := res.Payload
	if _, ok := vault.get(store.SlotGrok); ok {
		t.Error("the key was stored despite the reported failure")
	}
	// The payload still comes back so the form can repaint, but grok must not read as present.
	for _, k := range p.Keys {
		if k.Slot == "grok" && k.Status == store.KeyPresent {
			t.Error("a slot whose write failed is painted as configured")
		}
	}
}

// The region is normalised on the way in, because the user types a display name ("East US 2")
// and every Azure endpoint needs the id ("eastus2"). Normalising in the UI would put the rule in
// two places; store.NormalizeRegion already owns it.
func TestSettingTheRegionNormalisesIt(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)

	res := svc.SetRegion("East US 2")
	if res.Error != "" {
		t.Fatalf("SetRegion: %s", res.Error)
	}
	p := res.Payload
	if p.Region != "eastus2" {
		t.Errorf("Region = %q, want eastus2", p.Region)
	}
	if got := st.LoadSettings().Region; got != "eastus2" {
		t.Errorf("persisted Region = %q, want eastus2", got)
	}
}

// A region that cannot be normalised must be refused with the stored one untouched: an invalid
// region silently saved would break Azure at the next dictation, far from the mistake.
func TestAnInvalidRegionIsRefusedAndChangesNothing(t *testing.T) {
	st := store.NewAt(t.TempDir())
	settings := st.LoadSettings()
	settings.Region = "eastus"
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("saving settings: %v", err)
	}
	svc, _ := testService(t, st)

	if res := svc.SetRegion("not a region!!"); res.Error == "" {
		t.Fatal("an invalid region was accepted")
	}
	if got := st.LoadSettings().Region; got != "eastus" {
		t.Errorf("Region = %q — a rejected write still changed the settings", got)
	}
}

// An engine that is NAMED but not ported must be refused, with the working engine left in place.
//
// This is the difference between "known" and "available", and collapsing them is a user-visible
// bug: the picker lists six engines, so selecting an unported one would replace a working setup
// with one that fails at the next dictation — far from the click that caused it. buildProvider
// rejects it there; this rejects it here, where the user can still see why.
func TestAnUnportedProviderIsRefusedAndKeepsTheWorkingOne(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)
	if res := svc.SetProvider("whisper"); res.Error != "" {
		t.Fatalf("SetProvider(whisper): %s", res.Error)
	}

	res := svc.SetProvider("elevenlabs") // named in AllProviders, not ported
	if res.Error == "" {
		t.Fatal("an unported provider was accepted")
	}
	if got := st.LoadSettings().Provider; got != "whisper" {
		t.Errorf("Provider = %q — the working engine was replaced by an unavailable one", got)
	}
	// And the payload says so, so the picker can grey it out rather than just refusing on click.
	for _, p := range res.Payload.Providers {
		if p.ID == "elevenlabs" && p.Available {
			t.Error("elevenlabs is reported as available")
		}
		if p.ID == "whisper" && !p.Available {
			t.Error("whisper is reported as unavailable")
		}
	}
}

// SaveConnection must validate EVERYTHING before writing anything. A bad region with a good key
// must leave both untouched — committing the key against a region the app rejected would leave a
// credential for an endpoint that does not exist.
func TestSaveConnectionRejectsBeforeWritingAnything(t *testing.T) {
	st := store.NewAt(t.TempDir())
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Region = "eastus"
		return nil
	}); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	svc, vault := testService(t, st)

	res := svc.SaveConnection("azure-speech", "not a region!!", "azure-key")
	if res.Error == "" {
		t.Fatal("an invalid region was accepted")
	}
	if _, ok := vault.get(store.SlotAzureSpeech); ok {
		t.Error("the key was written despite the region being rejected")
	}
	if got := st.LoadSettings().Region; got != "eastus" {
		t.Errorf("Region = %q — a rejected save still changed it", got)
	}
}

// The happy path commits both halves and reports them in one payload.
func TestSaveConnectionStoresRegionAndKeyTogether(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)

	res := svc.SaveConnection("azure-speech", "West Europe", "azure-key")
	if res.Error != "" {
		t.Fatalf("SaveConnection: %s", res.Error)
	}
	if res.Payload.Region != "westeurope" {
		t.Errorf("Region = %q, want westeurope", res.Payload.Region)
	}
	if got, _ := vault.get(store.SlotAzureSpeech); got != "azure-key" {
		t.Error("the key was not stored")
	}
}

// An empty key means "leave the stored one alone", so changing only the region must not wipe a
// working credential — and must not be reported as an error either.
func TestSaveConnectionWithNoKeyKeepsTheStoredOne(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	if res := svc.SetKey("azure-speech", "already-there"); res.Error != "" {
		t.Fatalf("seeding the key: %s", res.Error)
	}

	res := svc.SaveConnection("azure-speech", "uksouth", "")
	if res.Error != "" {
		t.Fatalf("SaveConnection: %s", res.Error)
	}
	if got, _ := vault.get(store.SlotAzureSpeech); got != "already-there" {
		t.Errorf("the stored key became %q — an empty field must not clear it", got)
	}
	if res.Payload.Region != "uksouth" {
		t.Errorf("Region = %q, want uksouth", res.Payload.Region)
	}
}

// Two setters running at once must not lose one another's change.
//
// Load-modify-Save across two separate critical sections is a LOST UPDATE, and `-race` cannot see
// it: nothing races on memory, the second write is simply built from stale data. Wails dispatches
// each binding call on its own goroutine, so two quick clicks in the settings window are enough.
// This drives the two setters that touch different fields concurrently and demands both survive.
func TestConcurrentSettersDoNotLoseEachOthersChange(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)

	const rounds = 50
	for i := 0; i < rounds; i++ {
		if err := st.UpdateSettings(func(cfg *store.Settings) error {
			cfg.Provider = "whisper"
			cfg.Region = ""
			return nil
		}); err != nil {
			t.Fatalf("resetting: %v", err)
		}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); svc.SetProvider("grok") }()
		go func() { defer wg.Done(); svc.SetRegion("westeurope") }()
		wg.Wait()

		got := st.LoadSettings()
		if got.Provider != "grok" {
			t.Fatalf("round %d: Provider = %q — the region write erased it", i, got.Provider)
		}
		if got.Region != "westeurope" {
			t.Fatalf("round %d: Region = %q — the provider write erased it", i, got.Region)
		}
	}
}

// Availability has TWO authorities — store.IsAvailableProvider and the switch in buildProvider —
// and nothing in the language keeps them in step. This test does.
//
// They agree today. The failure it guards against is the next provider being added to one side
// only, which silently recreates the original bug: the picker offers an engine, SetProvider accepts
// it, and dictation then refuses it having already replaced a working engine. Divergence in the
// other direction is just as bad — a ported engine nobody can select.
func TestAvailabilityAgreesWithWhatBuildProviderAccepts(t *testing.T) {
	for _, provider := range store.AllProviders {
		t.Run(provider, func(t *testing.T) {
			d := testDictation(t, provider)
			_, err := d.buildProvider()

			// "Not ported" is the one error that means unavailable. Every other failure (a missing
			// key, for instance) belongs to a provider that IS available and merely unconfigured,
			// which is exactly the distinction this must not blur.
			unported := err != nil && strings.Contains(err.Error(), "todavía no está portado")

			if available := store.IsAvailableProvider(provider); available == unported {
				t.Errorf("store says available=%v but buildProvider reports unported=%v — the two "+
					"authorities have diverged for %q", available, unported, provider)
			}
		})
	}
}

// A DETERMINISTIC Keychain failure must leave the region alone.
//
// Note what this does and does not prove: the stub fails outright, so it covers the rejected-write
// path only. The indeterminate path — a timeout whose write lands later — is a different case, and
// TestALateLandingKeychainWriteIsAKnownResidual covers it.
//
// The Keychain is the half that actually fails on these builds, so it is committed first: if it
// fails, nothing has changed anywhere. Region-first would persist a region the user's key was never
// saved against, and then report the whole save as failed — leaving Azure pointing somewhere the
// credential does not belong.
func TestSaveConnectionLeavesTheRegionAloneWhenTheKeychainFails(t *testing.T) {
	st := store.NewAt(t.TempDir())
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Region = "eastus"
		return nil
	}); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	svc, _ := testService(t, st)
	svc.setSecret = func(store.KeySlot, string) error { return store.ErrKeychainTimeout }

	res := svc.SaveConnection("azure-speech", "westeurope", "azure-key")
	if res.Error == "" {
		t.Fatal("a failed Keychain write was reported as success")
	}
	if got := st.LoadSettings().Region; got != "eastus" {
		t.Errorf("Region = %q — it was changed even though the key was never stored", got)
	}
	// The repaint must show the region that is really stored, not the one the form tried to set.
	if res.Payload.Region != "eastus" {
		t.Errorf("payload Region = %q, want the stored eastus", res.Payload.Region)
	}
}

// An env-backed credential must be undeletable through the BINDING, not merely through a greyed-out
// button: the binding is reachable from anything in the webview, and the Keychain item underneath an
// override is one the user cannot see — deleting it would look like it did nothing, because the
// override is still what dictation uses.
func TestDeletingAnEnvBackedKeyIsRefused(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	// Something IS in the Keychain underneath, which is what makes the deletion dangerous.
	if res := svc.SetKey("grok", "in-the-keychain"); res.Error != "" {
		t.Fatalf("seeding: %s", res.Error)
	}
	t.Setenv("LOQUI_GROK_KEY", "from-the-environment")

	deleterCalled := false
	svc.deleteSecret = func(store.KeySlot) error {
		deleterCalled = true
		return nil
	}

	res := svc.DeleteKey("grok")
	if res.Error == "" {
		t.Fatal("deleting an env-backed key was accepted")
	}
	if deleterCalled {
		t.Error("the Keychain deleter ran for an env-backed slot")
	}
	if got, _ := vault.get(store.SlotGrok); got != "in-the-keychain" {
		t.Error("the hidden Keychain item was removed")
	}
	// The message has to name the variable, or the user cannot act on it.
	if !strings.Contains(res.Error, "LOQUI_GROK_KEY") {
		t.Errorf("error = %q, want it to name the environment variable", res.Error)
	}
}

// Writing a secret into a slot an env variable controls must be refused.
//
// It is not merely pointless — it is misleading in a way that lasts. The write would report success
// while dictation went on using the variable, and the item just stored stays invisible until the
// variable is removed, at which point a credential the user has forgotten about silently becomes
// the live one. Same reasoning as the deletion guard, on the other side of the same door.
func TestWritingAKeyIntoAnEnvBackedSlotIsRefused(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	t.Setenv("LOQUI_GROK_KEY", "from-the-environment")

	res := svc.SetKey("grok", "typed-by-hand")
	if res.Error == "" {
		t.Fatal("writing into an env-backed slot was accepted")
	}
	if _, ok := vault.get(store.SlotGrok); ok {
		t.Error("the secret was written to the Keychain anyway")
	}
	if !strings.Contains(res.Error, "LOQUI_GROK_KEY") {
		t.Errorf("error = %q, want it to name the variable", res.Error)
	}
}

// But a REGION-only save must still work on an env-backed slot: the region is not the credential,
// and refusing it would leave the user unable to change a setting the override says nothing about.
func TestARegionOnlySaveIsAllowedOnAnEnvBackedSlot(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)
	t.Setenv("LOQUI_AZURE_KEY", "from-the-environment")

	res := svc.SaveConnection("azure-speech", "uksouth", "")
	if res.Error != "" {
		t.Fatalf("a region-only save was refused: %s", res.Error)
	}
	if got := st.LoadSettings().Region; got != "uksouth" {
		t.Errorf("Region = %q, want uksouth", got)
	}
}

// A region-only save that fails on disk must not claim a key was saved.
//
// Small, but it is the kind of message that sends someone looking for a credential they never
// entered — and it would have been reported by whoever hit it, not by the code.
func TestARegionOnlyFailureDoesNotClaimTheKeyWasSaved(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)
	// A directory where the settings file should be makes the atomic rename fail.
	if err := os.MkdirAll(st.SettingsPath(), 0o700); err != nil {
		t.Fatalf("arranging the disk failure: %v", err)
	}

	res := svc.SaveConnection("azure-speech", "uksouth", "")
	if res.Error == "" {
		t.Fatal("the disk failure was not reported")
	}
	if strings.Contains(res.Error, "clave se guardó") {
		t.Errorf("error = %q — no key was written, so it must not say one was saved", res.Error)
	}
}

// A Keychain write that TIMES OUT and lands afterwards leaves a known inconsistency, and this test
// exists to pin it rather than to bless it.
//
// SaveConnection commits the Keychain first precisely so a failure leaves nothing changed. That
// works when the write is REJECTED. A timeout is different: store.SetKey abandons an uncancellable
// call, so the write may still land — and by then SaveConnection has skipped the region. The result
// is the new key against the old region, which for Azure is a credential for the wrong endpoint.
//
// WHY IT IS NOT FIXED HERE. Closing it properly means making the disk write the commit point for
// the credential too: store the secret under a versioned account and have the settings name the
// active version, so an unconfirmed write is never the one that gets read. That changes the
// credential format, every read path including dictation's, and needs a migration for keys already
// stored — its own change, not a clause in this one. It also only bites while the Keychain hangs,
// which is the ad-hoc-signing blocker the project is already committed to fixing at the root.
//
// Until then the user is told the operation is unconfirmed and sent back to Ajustes, where the
// payload reports what is actually stored. This test fails the moment the behaviour changes, in
// either direction, so the residual cannot quietly become something else.
func TestALateLandingKeychainWriteIsAKnownResidual(t *testing.T) {
	st := store.NewAt(t.TempDir())
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Region = "eastus"
		return nil
	}); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	svc, vault := testService(t, st)

	// Exactly what store.SetKey does on a hang: report the timeout, keep going underneath.
	//
	// The write is gated on returned, which SaveConnection's caller closes, so it lands strictly
	// AFTER the whole operation has finished. Without the gate the goroutine could be scheduled
	// first and the test would prove nothing about lateness — only that the combination is wrong.
	returned := make(chan struct{})
	landed := make(chan struct{})
	svc.setSecret = func(slot store.KeySlot, secret string) error {
		go func() {
			<-returned
			vault.set(slot, secret) // the abandoned call completing, late
			close(landed)
		}()
		return store.ErrKeychainTimeout
	}

	res := svc.SaveConnection("azure-speech", "westeurope", "late-key")
	close(returned) // only now may the abandoned write land

	if res.Error == "" {
		t.Fatal("an unconfirmed write was reported as success")
	}
	// The user must be told it is UNCONFIRMED, not that it failed: it may well have worked.
	if !strings.Contains(res.Error, "no está confirmada") {
		t.Errorf("error = %q, want it to say the operation is unconfirmed", res.Error)
	}

	<-landed
	stored, _ := vault.get(store.SlotAzureSpeech)

	// The residual, stated outright: the key is now the new one...
	if stored != "late-key" {
		t.Fatalf("the late write did not land (%q) — this test no longer reproduces the residual", stored)
	}
	// ...while the region is still the old one. If this ever becomes "westeurope", the versioned
	// credential work has been done and this test should be replaced by one asserting atomicity.
	if got := st.LoadSettings().Region; got != "eastus" {
		t.Errorf("Region = %q — the region/key pairing changed; revisit whether the residual is gone", got)
	}
}

// A credential slot nothing reads must refuse a key.
//
// azure-openai is Azure's realtime subservice, and it is not ported. The settings page offers it in
// the Azure card's service picker, so without this the user could enter their Azure OpenAI key,
// click Guardar, and have it stored where nothing will ever read it — and the form, which maps the
// Azure card to azure-speech, would write it over the credential that IS in use.
func TestWritingAKeyIntoAnUnusableSlotIsRefused(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)

	res := svc.SetKey("azure-openai", "una-clave")
	if res.Error == "" {
		t.Fatal("a key for an unported subservice was accepted")
	}
	if _, ok := vault.get(store.SlotAzureOpenAI); ok {
		t.Error("the key was stored for a slot nothing reads")
	}
	// And the payload says so, so the UI can disable the option rather than only refusing on click.
	for _, k := range res.Payload.Keys {
		switch k.Slot {
		case "azure-openai":
			if k.Available {
				t.Error("azure-openai is reported as available")
			}
		case "azure-speech", "grok":
			if !k.Available {
				t.Errorf("%s is reported as unavailable", k.Slot)
			}
		}
	}
}

// The Azure region is a single global setting, so it must not be settable through an unrelated
// slot's form: saving a Grok key has no business moving the Azure endpoint.
func TestARegionCannotBeSavedThroughAnUnrelatedSlot(t *testing.T) {
	st := store.NewAt(t.TempDir())
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Region = "eastus"
		return nil
	}); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	svc, vault := testService(t, st)

	res := svc.SaveConnection("grok", "westeurope", "xai-key")
	if res.Error == "" {
		t.Fatal("a region was accepted for a slot that does not use one")
	}
	if got := st.LoadSettings().Region; got != "eastus" {
		t.Errorf("Region = %q — an unrelated slot moved the Azure endpoint", got)
	}
	// Nothing at all was written: validation happens before any commit.
	if _, ok := vault.get(store.SlotGrok); ok {
		t.Error("the key was written despite the region being rejected")
	}
}

// Saving a Grok key WITHOUT a region must still work — the guard is about pairing, not about Grok.
func TestAKeyOnlySaveWorksForASlotWithNoRegion(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)

	res := svc.SaveConnection("grok", "", "xai-key")
	if res.Error != "" {
		t.Fatalf("a key-only save was refused: %s", res.Error)
	}
	if got, _ := vault.get(store.SlotGrok); got != "xai-key" {
		t.Errorf("the key was not stored (%q)", got)
	}
}

// A REGION-ONLY save through an unported slot must be refused too.
//
// The availability check used to run only when a key was being written, so this path went straight
// past it: no secret, no check, and the region — a single global Azure setting — was written through
// the form of a subservice the app cannot even use. The credential was guarded while the setting
// beside it was not.
func TestARegionOnlySaveThroughAnUnusableSlotIsRefused(t *testing.T) {
	st := store.NewAt(t.TempDir())
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Region = "eastus"
		return nil
	}); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	svc, _ := testService(t, st)

	res := svc.SaveConnection("azure-openai", "westeurope", "")
	if res.Error == "" {
		t.Fatal("a region-only save through an unported slot was accepted")
	}
	// The message must be the AVAILABILITY one, not the region-pairing one.
	//
	// Both checks would reject this call, and asserting merely "some error" is what let a missing fix
	// hide: the availability gate was still behind `if writeKey`, this path fell through to the region
	// check instead, and the test passed anyway. Naming the expected reason is what makes it isolate
	// the gate rather than the overlap.
	if !strings.Contains(res.Error, "no está disponible") {
		t.Errorf("error = %q, want the availability rejection — a test that accepts any error cannot "+
			"tell this gate from the region check that also happens to catch it", res.Error)
	}
	if got := st.LoadSettings().Region; got != "eastus" {
		t.Errorf("Region = %q — an unported subservice moved the live Azure endpoint", got)
	}
}

// The Azure region belongs to Speech alone. azure-openai is addressed by resource and deployment,
// which is what the settings form has always said — the region lives in #speechConfig and the
// resource/deployment pair in #openaiConfig.
func TestOnlyAzureSpeechUsesTheRegion(t *testing.T) {
	if !store.UsesAzureRegion(store.SlotAzureSpeech) {
		t.Error("azure-speech must use the region")
	}
	for _, slot := range []store.KeySlot{
		store.SlotAzureOpenAI, store.SlotOpenAI, store.SlotGrok, store.SlotElevenLabs,
	} {
		if store.UsesAzureRegion(slot) {
			t.Errorf("%s must not use the Azure Speech region", slot)
		}
	}
}
