package app

import (
	"encoding/json"
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
			// Pinned rather than read from the machine: without this the connection states in the
			// payload depend on the macOS version and the helpers present on whoever runs the suite.
			caps: func() store.HostCapabilities { return store.HostCapabilities{} },
		},
		// The fallback engine is assumed able to run: whether a 465 MB model file happens to be on the
		// developer's disk is not what any of these cases are about, and the tests that DO care set
		// this themselves.
		defaultProblem: func() error { return nil },
		getSecret: func(slot store.KeySlot) (string, error) {
			if secret, ok := vault.get(slot); ok {
				return secret, nil
			}
			return "", store.ErrNoSecret
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

// A failed credential write must surface as an error AND leave the payload honest — the slot must
// not be painted as configured.
func TestAFailedCredentialWriteIsReportedAndNotPaintedAsConfigured(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	svc.setSecret = func(store.KeySlot, string) error { return store.ErrSecretsUnreadable }

	res := svc.SetKey("grok", "xai-abc123")
	if res.Error == "" {
		t.Fatal("a failed credential write was reported as success")
	}
	// The message says NOTHING CHANGED, which under the file backend is the truth and under the old
	// Keychain one would have been a lie: there, the abandoned call could still land, so the only
	// honest wording was "unconfirmed". A rename either happened or it did not.
	if !strings.Contains(res.Error, "no se cambió nada") {
		t.Errorf("error = %q, want it to say nothing was changed", res.Error)
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
func TestEveryNamedProviderIsNowOfferedAndAnUnknownOneIsRefused(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)
	if res := svc.SetProvider("whisper"); res.Error != "" {
		t.Fatalf("SetProvider(whisper): %s", res.Error)
	}

	// THIS TEST HAS BEEN REWRITTEN TWICE BY THE CODE CATCHING UP WITH IT, and both times that was the
	// point. It first named elevenlabs as "not ported", and failed when elevenlabs was ported; then
	// openai, and failed when openai was. There is no unported engine left to name, so what it guards
	// now is the two halves that remain true:
	//
	//  1. every engine the picker LISTS is one the backend will accept — a listed engine that
	//     SetProvider refuses is a dead option in the menu;
	//  2. something that is not a known engine at all is still refused without touching the working one.
	//
	// When the next named-but-unported engine appears, the first loop is what will fail, and the fix is
	// to exclude it here and mark it unavailable in store.availableProviders — not to delete the loop.
	res := svc.SetProvider("whisper")
	for _, p := range res.Payload.Providers {
		if !p.Available {
			t.Errorf("%s se ofrece como no disponible; si sigue sin portarse, este test debe excluirlo explícitamente", p.ID)
			continue
		}
		if r := svc.SetProvider(p.ID); r.Error != "" {
			t.Errorf("SetProvider(%s) falló pese a estar listado como disponible: %s", p.ID, r.Error)
		}
	}

	// Back to a known-good engine before the refusal case, so what it must preserve is unambiguous.
	if r := svc.SetProvider("whisper"); r.Error != "" {
		t.Fatalf("SetProvider(whisper): %s", r.Error)
	}
	if r := svc.SetProvider("no-existe"); r.Error == "" {
		t.Error("un motor desconocido fue aceptado")
	}
	if got := st.LoadSettings().Provider; got != "whisper" {
		t.Errorf("Provider = %q — un motor desconocido reemplazó el que funcionaba", got)
	}
}

func TestHomePickerOffersAndActivatesEachAzureProductExplicitly(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	vault.set(store.SlotAzureSpeech, "speech-key")
	vault.set(store.SlotAzureOpenAI, "openai-key")
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Region = "eastus2"
		cfg.AzureOpenAiResource = "my-resource"
		cfg.AzureOpenAiDeployment = "my-deployment"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	payload := svc.Load()
	raw := marshalForTest(t, payload)
	for _, want := range []string{`"id":"azure-speech"`, `"id":"azure-openai"`} {
		if !strings.Contains(raw, want) {
			t.Errorf("provider picker payload is missing %s: %s", want, raw)
		}
	}

	for selection, service := range map[string]string{
		"azure-speech": "speech",
		"azure-openai": "openai",
	} {
		res := svc.SetProvider(selection)
		if res.Error != "" {
			t.Errorf("SetProvider(%s): %s", selection, res.Error)
			continue
		}
		cfg := st.LoadSettings()
		if cfg.Provider != "azure" || cfg.AzureService != service {
			t.Errorf("SetProvider(%s) stored provider=%q service=%q", selection, cfg.Provider, cfg.AzureService)
		}
	}
}

func TestHomePickerReportsReadinessPerAzureProduct(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	vault.set(store.SlotAzureSpeech, "speech-key")
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Provider = "azure"
		cfg.AzureService = "speech"
		cfg.Region = "eastus2"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	raw := marshalForTest(t, svc.Load())
	var payload struct {
		Providers []struct {
			ID       string `json:"id"`
			State    string `json:"state"`
			Selected bool   `json:"selected"`
		} `json:"providers"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	got := map[string]struct {
		state    string
		selected bool
	}{}
	for _, option := range payload.Providers {
		got[option.ID] = struct {
			state    string
			selected bool
		}{option.State, option.Selected}
	}
	if option := got["azure-speech"]; option.state != string(store.ConnActive) || !option.selected {
		t.Errorf("azure-speech = %+v, want active and selected", option)
	}
	if option := got["azure-openai"]; option.state != string(store.ConnUnconfigured) || option.selected {
		t.Errorf("azure-openai = %+v, want unconfigured and not selected", option)
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
			_, err := d.buildProvider(1)

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

// A failed credential write must leave the region alone.
//
// There is no longer an "indeterminate" counterpart to this case, and that is the whole gain of the
// file backend: the Keychain's timeout could be abandoned and land later, so a rejected write and an
// unconfirmed one were different cases needing different tests. A write to the credentials file
// either renamed or it did not.
//
// The credential is committed first because it is the half that can fail on its own: if it fails,
// nothing has changed anywhere. Region-first would persist a region the user's key was never saved
// against, and then report the whole save as failed — leaving Azure pointing somewhere the credential
// does not belong.
func TestSaveConnectionLeavesTheRegionAloneWhenTheKeychainFails(t *testing.T) {
	st := store.NewAt(t.TempDir())
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Region = "eastus"
		return nil
	}); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	svc, _ := testService(t, st)
	svc.setSecret = func(store.KeySlot, string) error { return store.ErrSecretsUnreadable }

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
	svc, vault := testService(t, st)
	// A stored key is part of the arrangement, not scenery: without one the save is refused for the
	// missing credential and never reaches the disk write this case is about.
	vault.set(store.SlotAzureSpeech, "la-guardada")
	// A directory where the settings file should be makes the atomic rename fail.
	if err := os.MkdirAll(st.SettingsPath(), 0o700); err != nil {
		t.Fatalf("arranging the disk failure: %v", err)
	}

	res := svc.SaveConnection("azure-speech", "uksouth", "")
	if res.Error == "" {
		t.Fatal("the disk failure was not reported")
	}
	if !strings.Contains(res.Error, "región") {
		t.Errorf("error = %q — that is not the region write failing, so this case is not covering it",
			res.Error)
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
// THE RESIDUAL THIS REPLACES IS CLOSED, and that is worth stating rather than quietly deleting.
//
// Under the Keychain backend a write could be abandoned mid-flight and land seconds later, so
// SaveConnection could report a failure and end up with the new key stored against the OLD region —
// a pairing the user never asked for. TestALateLandingKeychainWriteIsAKnownResidual pinned exactly
// that, and it pinned it as a KNOWN DEFECT. The file backend cannot produce it: a failed write never
// renamed anything, so there is no late arrival to pair with a stale region.
//
// What this asserts instead is the property that replaces it: a failed credential write leaves BOTH
// halves untouched.
func TestAFailedCredentialWriteLeavesTheRegionAloneToo(t *testing.T) {
	st := store.NewAt(t.TempDir())
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Region = "eastus"
		return nil
	}); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	svc, vault := testService(t, st)
	svc.setSecret = func(store.KeySlot, string) error { return store.ErrSecretsUnreadable }

	res := svc.SaveConnection("azure-speech", "westeurope", "la-nueva")

	if res.Error == "" {
		t.Fatal("a failed write was reported as success")
	}
	if _, ok := vault.get(store.SlotAzureSpeech); ok {
		t.Error("the key was stored despite the reported failure")
	}
	// THE HALF THAT USED TO SLIP. The credential is written first precisely so its failure leaves the
	// region alone; if this ever reads "westeurope", a rejected save has half-applied itself.
	if got := st.LoadSettings().Region; got != "eastus" {
		t.Errorf("Region = %q, want eastus — a failed save moved the region anyway", got)
	}
}

// Azure OpenAI has its own runtime reader, so its independent credential must be writable.
func TestWritingAnAzureOpenAIKeyIsAcceptedNowThatTheRuntimeReadsIt(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)

	res := svc.SetKey("azure-openai", "una-clave")
	if res.Error != "" {
		t.Fatalf("Azure OpenAI key was refused: %s", res.Error)
	}
	if got, ok := vault.get(store.SlotAzureOpenAI); !ok || got != "una-clave" {
		t.Errorf("stored key = %q,%v", got, ok)
	}
	for _, k := range res.Payload.Keys {
		if k.Slot == "azure-openai" && !k.Available {
			t.Error("azure-openai is still reported unavailable")
		}
	}
}

func TestSaveAzureOpenAIConnectionStoresTheExactSubserviceConfiguration(t *testing.T) {
	st := store.NewAt(t.TempDir())
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Region = "eastus2"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	svc, vault := testService(t, st)
	vault.set(store.SlotAzureSpeech, "speech-key")

	res := svc.SaveAzureConnection("openai", "", "  mi-recurso  ", "  mi-whisper  ", "  una-clave  ")
	if res.Error != "" {
		t.Fatalf("SaveAzureConnection: %s", res.Error)
	}
	cfg := st.LoadSettings()
	if cfg.AzureService != "openai" || cfg.AzureOpenAiResource != "mi-recurso" || cfg.AzureOpenAiDeployment != "mi-whisper" {
		t.Errorf("stored Azure config = %+v", cfg)
	}
	if got, ok := vault.get(store.SlotAzureOpenAI); !ok || got != "una-clave" {
		t.Errorf("stored key = %q,%v", got, ok)
	}
	if cfg.Region != "eastus2" {
		t.Errorf("Azure Speech region was overwritten with %q", cfg.Region)
	}
	if got, ok := vault.get(store.SlotAzureSpeech); !ok || got != "speech-key" {
		t.Errorf("Azure Speech key was overwritten: %q,%v", got, ok)
	}
}

func TestSaveAzureSpeechConnectionKeepsTheOpenAIConfigurationSeparate(t *testing.T) {
	st := store.NewAt(t.TempDir())
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.AzureOpenAiResource = "openai-resource"
		cfg.AzureOpenAiDeployment = "openai-deployment"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	svc, vault := testService(t, st)
	vault.set(store.SlotAzureOpenAI, "openai-key")

	res := svc.SaveAzureConnection("speech", " eastus2 ", "", "", " speech-key ")
	if res.Error != "" {
		t.Fatalf("SaveAzureConnection: %s", res.Error)
	}
	cfg := st.LoadSettings()
	if cfg.AzureService != "speech" || cfg.Region != "eastus2" {
		t.Errorf("stored Azure Speech config = %+v", cfg)
	}
	if cfg.AzureOpenAiResource != "openai-resource" || cfg.AzureOpenAiDeployment != "openai-deployment" {
		t.Errorf("Azure OpenAI config was overwritten: %+v", cfg)
	}
	if got, ok := vault.get(store.SlotAzureSpeech); !ok || got != "speech-key" {
		t.Errorf("Azure Speech key = %q,%v", got, ok)
	}
	if got, ok := vault.get(store.SlotAzureOpenAI); !ok || got != "openai-key" {
		t.Errorf("Azure OpenAI key was overwritten: %q,%v", got, ok)
	}
}

func TestSaveAzureOpenAIConnectionRejectsUnsafeResourceBeforeWriting(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)

	res := svc.SaveAzureConnection("openai", "", "attacker.example/path", "deployment", "una-clave")
	if res.Error == "" || res.Field != "resource" {
		t.Fatalf("unsafe resource result = %+v", res)
	}
	if _, ok := vault.get(store.SlotAzureOpenAI); ok {
		t.Error("key was written before resource validation")
	}
	if got := st.LoadSettings().AzureService; got != "speech" {
		t.Errorf("service changed to %q on a rejected save", got)
	}
}

func TestSavePublicOpenAIModelDoesNotOverwriteAzureDeployment(t *testing.T) {
	st := store.NewAt(t.TempDir())
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.AzureOpenAiDeployment = "azure-deployment"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	svc, vault := testService(t, st)
	res := svc.SaveOpenAIConnection("gpt-4o-transcribe", "openai-key")
	if res.Error != "" {
		t.Fatalf("SaveOpenAIConnection: %s", res.Error)
	}
	cfg := st.LoadSettings()
	if cfg.OpenAiModel != "gpt-4o-transcribe" {
		t.Errorf("OpenAiModel = %q", cfg.OpenAiModel)
	}
	if cfg.AzureOpenAiDeployment != "azure-deployment" {
		t.Errorf("Azure deployment was overwritten with %q", cfg.AzureOpenAiDeployment)
	}
	if got, ok := vault.get(store.SlotOpenAI); !ok || got != "openai-key" {
		t.Errorf("stored key = %q,%v", got, ok)
	}
}

func TestSavePublicOpenAIRejectsAnUnknownModelBeforeWriting(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	res := svc.SaveOpenAIConnection("not-a-real-model", "openai-key")
	if res.Error == "" || res.Field != "model" {
		t.Fatalf("unknown model result = %+v", res)
	}
	if _, ok := vault.get(store.SlotOpenAI); ok {
		t.Error("key was written before model validation")
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
func TestAzureOpenAICannotBeSavedThroughTheSpeechRegionAPI(t *testing.T) {
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
		t.Fatal("Azure OpenAI accepted the Azure Speech save shape")
	}
	if !strings.Contains(res.Error, "no usa una región") {
		t.Errorf("error = %q, want the wrong-API rejection", res.Error)
	}
	if got := st.LoadSettings().Region; got != "eastus" {
		t.Errorf("Region = %q — Azure OpenAI moved the Speech endpoint", got)
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

// The tutorial's flag, which decides whether the wizard opens by itself at launch.
func TestSettingOnboardedPersistsAndComesBackInThePayload(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)

	if st.LoadSettings().Onboarded {
		t.Fatal("Onboarded debería empezar en false, o el wizard nunca se abriría solo")
	}

	res := svc.SetOnboarded(true)
	if res.Error != "" {
		t.Fatalf("SetOnboarded: %s", res.Error)
	}
	if !res.Payload.Onboarded {
		t.Error("el payload devuelto dice Onboarded=false tras marcarlo")
	}
	// Persisted, not merely reflected back: the flag's whole job is to survive the next launch.
	if !st.LoadSettings().Onboarded {
		t.Error("Onboarded no quedó en disco")
	}
}

// Reopening the tutorial from the footer must NOT clear the flag — that would make the wizard
// auto-open on every launch afterwards, which is the one failure the user cannot escape by using the
// app normally. So false has to be storable (nothing else could ever reset it) while the reopen path
// is required to leave it alone.
func TestSettingOnboardedFalseIsStorableAndIndependentOfOtherSettings(t *testing.T) {
	st := store.NewAt(t.TempDir())
	settings := st.LoadSettings()
	settings.Onboarded = true
	settings.Provider = "whisper"
	if err := st.SaveSettings(settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	svc, _ := testService(t, st)

	res := svc.SetOnboarded(false)
	if res.Error != "" {
		t.Fatalf("SetOnboarded(false): %s", res.Error)
	}
	if st.LoadSettings().Onboarded {
		t.Error("no se pudo volver a false")
	}
	// And it touched nothing else: a flag write that resets the engine would silently undo setup.
	if got := st.LoadSettings().Provider; got != "whisper" {
		t.Errorf("Provider = %q tras escribir el flag, quería whisper intacto", got)
	}
}

// THE CONTRAPOSITIVE OF TestWritingAKeyIntoAnUnusableSlotIsRefused, and the one that was missing.
//
// That test pins the refusal; nothing pinned the acceptance, so when OpenAI and ElevenLabs were ported
// and `availableKeySlots` was not widened, their keys were refused with "este servicio todavía no está
// disponible en esta versión" and the whole suite stayed green. Two ported engines were unusable from
// the interface for two sessions.
//
// It asserts through the SERVICE rather than the store's map, because that is what the user reaches:
// the map being right is necessary and not sufficient — SaveConnection and SetKey each consult it
// separately, and either could have its own gate.
func TestEveryPortedEngineAcceptsItsKeyThroughTheService(t *testing.T) {
	for _, c := range []struct {
		slot string
		key  store.KeySlot
	}{
		{"azure-speech", store.SlotAzureSpeech},
		{"grok", store.SlotGrok},
		{"openai", store.SlotOpenAI},
		{"elevenlabs", store.SlotElevenLabs},
	} {
		t.Run(c.slot, func(t *testing.T) {
			st := store.NewAt(t.TempDir())
			svc, vault := testService(t, st)

			res := svc.SetKey(c.slot, "una-clave-de-"+c.slot)
			if res.Error != "" {
				t.Fatalf("SetKey(%q) = %q — a ported engine's key was refused", c.slot, res.Error)
			}
			if got, ok := vault.get(c.key); !ok || got != "una-clave-de-"+c.slot {
				t.Errorf("stored %q, %v — the write reported success without storing", got, ok)
			}
			// And the payload must report it as usable, or the card goes on offering to configure
			// something it just accepted.
			for _, k := range res.Payload.Keys {
				if k.Slot == c.slot && !k.Available {
					t.Errorf("slot %q accepted the key but the payload says Available=false", c.slot)
				}
			}
		})
	}
}
