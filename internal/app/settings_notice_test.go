package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/Juan-Motta/loqui-go/internal/audio"
	"github.com/Juan-Motta/loqui-go/internal/store"
)

// A write that SUCCEEDS has to say so.
//
// This is the bug the whole change came from: run() paints WriteResult.Error, which is empty on
// success, so the status line flashed "…" and went blank. From the user's side a completed save and
// a click that never arrived looked identical — and for DeleteKey that meant a credential was
// destroyed in silence.
func TestASuccessfulWriteCarriesSomethingToSay(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	vault.set(store.SlotAzureSpeech, "la-guardada")

	cases := []struct {
		name string
		act  func() WriteResult
	}{
		{"save a key", func() WriteResult { return svc.SaveConnection("azure-speech", "eastus2", "nueva") }},
		{"save only the region", func() WriteResult { return svc.SaveConnection("azure-speech", "westeurope", "") }},
		{"delete a key", func() WriteResult { return svc.DeleteKey("azure-speech") }},
		{"choose an engine", func() WriteResult { return svc.SetProvider("whisper") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := c.act()
			if res.Error != "" {
				t.Fatalf("unexpected failure: %s", res.Error)
			}
			if res.Notice == "" {
				t.Error("a successful write said nothing — the status line would go blank")
			}
		})
	}
}

// What was written is not always what was offered: an empty key field means "leave the stored one
// alone". Saying "clave guardada" after a region-only save would be a plain lie about an operation
// that touched no credential.
func TestTheSaveNoticeNamesWhatWasActuallyWritten(t *testing.T) {
	// Exact strings, not substrings. "mentions región" is satisfied by "Región guardada", so the
	// combined case would pass while silently dropping the half about the key.
	cases := []struct {
		name, region, secret string
		want                 string
	}{
		{"key and region", "eastus2", "una-clave", "Clave y región guardadas"},
		{"only the key", "", "una-clave", "Clave guardada"},
		{"only the region", "westeurope", "", "Región guardada"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := store.NewAt(t.TempDir())
			svc, vault := testService(t, st)
			vault.set(store.SlotAzureSpeech, "la-guardada")

			res := svc.SaveConnection("azure-speech", c.region, c.secret)

			if res.Error != "" {
				t.Fatalf("unexpected failure: %s", res.Error)
			}
			if res.Notice != c.want {
				t.Errorf("notice = %q, want %q", res.Notice, c.want)
			}
		})
	}
}

// DeleteKey is idempotent: deleting a slot that was already empty is success ("Absent is success",
// store.DeleteKey). So the notice has to be a POSTCONDITION — what is true now — rather than a claim
// about an action that may not have happened.
func TestTheDeleteNoticeIsTrueEvenWhenThereWasNothingToDelete(t *testing.T) {
	for _, hadKey := range []bool{true, false} {
		st := store.NewAt(t.TempDir())
		svc, vault := testService(t, st)
		if hadKey {
			vault.set(store.SlotAzureSpeech, "la-guardada")
		}

		res := svc.DeleteKey("azure-speech")

		if res.Error != "" {
			t.Fatalf("hadKey=%v: %s", hadKey, res.Error)
		}
		if res.Notice == "" {
			t.Fatalf("hadKey=%v: said nothing", hadKey)
		}
		// "Clave borrada" would be false in the second case. The wording must describe the state.
		if strings.Contains(strings.ToLower(res.Notice), "borrada") {
			t.Errorf("hadKey=%v: notice = %q — it claims an action that may not have happened",
				hadKey, res.Notice)
		}
	}
}

// Choosing an engine succeeds even when that engine cannot dictate yet, so the notice is the only
// thing that stops "saved" from reading as "ready".
func TestTheProviderNoticeSaysWhetherTheEngineCanActuallyWork(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		arrange  func(*SettingsService, *testVault)
		says     string
	}{
		{
			name:     "ready to use",
			provider: "azure",
			arrange: func(svc *SettingsService, v *testVault) {
				v.set(store.SlotAzureSpeech, "la-guardada")
				if err := svc.store().UpdateSettings(func(cfg *store.Settings) error {
					cfg.Region = "eastus2"
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			},
			says: "activo",
		},
		{
			name:     "missing its configuration",
			provider: "azure",
			arrange:  func(svc *SettingsService, v *testVault) {},
			says:     "configuración",
		},
		{
			// Unreadable credentials are NOT missing configuration: the key may be right there.
			// Telling this user to "complete the configuration" sends them to retype a credential
			// they already have, which is the exact confusion KeyStatusFor's three states exist for.
			name:     "the stored credentials could not be read",
			provider: "azure",
			arrange: func(svc *SettingsService, v *testVault) {
				svc.bootstrap.keyStatus = func(store.KeySlot) store.KeyStatus { return store.KeyUnreadable }
				if err := svc.store().UpdateSettings(func(cfg *store.Settings) error {
					cfg.Region = "eastus2"
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			},
			says: "no se pudieron leer",
		},
		{
			// Reachable through the binding: SetProvider only checks IsAvailableProvider, which is a
			// global "is it ported" map, while unsupported depends on THIS machine.
			name:     "cannot run on this machine",
			provider: "macos",
			arrange: func(svc *SettingsService, v *testVault) {
				svc.bootstrap.caps = func() store.HostCapabilities {
					return store.HostCapabilities{Platform: "darwin", OSMajor: 15}
				}
			},
			says: "sistema",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := store.NewAt(t.TempDir())
			svc, vault := testService(t, st)
			c.arrange(svc, vault)

			res := svc.SetProvider(c.provider)

			if res.Error != "" {
				t.Fatalf("unexpected failure: %s", res.Error)
			}
			if !strings.Contains(res.Notice, c.says) {
				t.Errorf("notice = %q, want it to mention %q", res.Notice, c.says)
			}
		})
	}
}

// Saving with no credential anywhere is the case the user asked to be told about, and the message
// has to point AT THE FIELD rather than leaving them to guess which of the two inputs is wrong.
func TestSavingWithNoKeyAnywhereIsRefusedAndPointsAtTheKeyField(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)

	res := svc.SaveConnection("azure-speech", "eastus2", "")

	if res.Error == "" {
		t.Fatal("a connection with no credential at all was accepted")
	}
	if res.Field != "key" {
		t.Errorf("Field = %q, want key", res.Field)
	}
	if vault.size() != 0 {
		t.Error("something was written by a refused save")
	}
	// And nothing was committed: a refused save changes nothing at all.
	if got := st.LoadSettings().Region; got != "" {
		t.Errorf("region = %q — the refused save still moved the endpoint", got)
	}
}

// The three ways a stored credential can already exist all keep a region-only save legitimate.
// Breaking any of them would take away the way you change region without re-pasting the key.
func TestARegionOnlySaveStaysLegitimateWhenTheKeyAlreadyExists(t *testing.T) {
	t.Run("stored in the keychain", func(t *testing.T) {
		st := store.NewAt(t.TempDir())
		svc, vault := testService(t, st)
		vault.set(store.SlotAzureSpeech, "la-guardada")

		res := svc.SaveConnection("azure-speech", "westeurope", "")

		if res.Error != "" {
			t.Fatalf("refused a legitimate region-only save: %s", res.Error)
		}
		if got := st.LoadSettings().Region; got != "westeurope" {
			t.Errorf("region = %q, want westeurope", got)
		}
	})

	t.Run("supplied by the environment", func(t *testing.T) {
		st := store.NewAt(t.TempDir())
		svc, _ := testService(t, st)
		t.Setenv("LOQUI_AZURE_KEY", "la-del-entorno")
		// The env override answers the question on its own; consulting the Keychain would be work
		// whose answer cannot change the outcome.
		svc.getSecret = func(store.KeySlot) (string, error) {
			t.Error("the Keychain was consulted even though the environment supplies the key")
			return "", store.ErrNoSecret
		}

		res := svc.SaveConnection("azure-speech", "westeurope", "")

		if res.Error != "" {
			t.Fatalf("refused a region-only save with an env-supplied key: %s", res.Error)
		}
	})
}

// "The Keychain did not answer" must never be reported as "you have no key": one is fixed by
// signing the app, the other by typing. Sending the second message to the first user makes them
// retype a credential that is probably already stored.
func TestAnUnreadableKeychainIsNotReportedAsAMissingKey(t *testing.T) {
	for _, readErr := range []error{store.ErrSecretsUnreadable, errors.New("keychain exploded")} {
		st := store.NewAt(t.TempDir())
		svc, _ := testService(t, st)
		svc.getSecret = func(store.KeySlot) (string, error) { return "", readErr }

		res := svc.SaveConnection("azure-speech", "eastus2", "")

		if res.Error == "" {
			t.Fatalf("%v: an unverifiable credential was accepted silently", readErr)
		}
		if res.Field == "key" {
			t.Errorf("%v: Field = key — this is not something the user can fix by typing", readErr)
		}
		if strings.Contains(res.Error, "obligatoria") {
			t.Errorf("%v: error = %q — it accuses the user of not having a key", readErr, res.Error)
		}
	}
}

// A LOQUI_*_KEY that is set but holds only whitespace is in force AND useless: keyReaderFor hands it
// to dictation ahead of the Keychain, and it authenticates nothing. Every path has to say the same
// thing about it, or the card ends up describing a configuration that cannot dictate.
func TestABlankEnvironmentOverrideIsInForceAndUnusableEverywhere(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	// A perfectly good key underneath, which nothing will read while the variable is defined. That is
	// what makes "you have no key" the wrong thing to say here.
	vault.set(store.SlotAzureSpeech, "la-guardada")
	t.Setenv("LOQUI_AZURE_KEY", "   ")

	t.Run("the payload does not call it configured", func(t *testing.T) {
		p := svc.Load()
		var got KeyState
		for _, k := range p.Keys {
			if k.Slot == string(store.SlotAzureSpeech) {
				got = k
			}
		}
		if got.Status == store.KeyPresent {
			t.Error("status = present — the badge would say the engine is ready to dictate")
		}
		if !got.FromEnv {
			t.Error("fromEnv = false — the user cannot tell why the stored key is being ignored")
		}
	})

	t.Run("choosing the engine does not blame the configuration", func(t *testing.T) {
		res := svc.SetProvider("azure")
		if res.Error != "" {
			t.Fatalf("unexpected failure: %s", res.Error)
		}
		if !strings.Contains(res.Notice, "variable de entorno") {
			t.Errorf("notice = %q — it points at the form, but the form cannot fix this", res.Notice)
		}
	})

	t.Run("saving is refused and names the variable", func(t *testing.T) {
		res := svc.SaveConnection("azure-speech", "eastus2", "")
		if res.Error == "" {
			t.Fatal("the save was accepted, so the card can end up looking configured")
		}
		if !strings.Contains(res.Error, "LOQUI_AZURE_KEY") {
			t.Errorf("error = %q — without naming the variable the user cannot act on this", res.Error)
		}
	})
}

// An environment variable holding a REAL key is not the same case, and the difference is a working
// credential: Azure is unconfigured whenever the region is missing, whatever the key is. Telling that
// user to remove the variable would break the one part they had right.
func TestAUsableEnvironmentKeyIsNotReportedAsBlank(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)
	t.Setenv("LOQUI_AZURE_KEY", "una-clave-de-verdad")
	// No region, so the engine is unconfigured for a reason that has nothing to do with the key.

	res := svc.SetProvider("azure")

	if res.Error != "" {
		t.Fatalf("unexpected failure: %s", res.Error)
	}
	if strings.Contains(res.Notice, "vacía") {
		t.Errorf("notice = %q — it asks the user to delete a variable that holds a working key",
			res.Notice)
	}
	if !strings.Contains(res.Notice, "configuración") {
		t.Errorf("notice = %q, want it to point at the missing configuration", res.Notice)
	}
}

// What SetKey stores has to be what it validated, for the same reason SaveConnection does: the probe
// tests the trimmed value, and a credential stored with the padding it was pasted with fails at
// dictation while the test that blessed it said otherwise.
func TestSetKeyStoresTheKeyItValidated(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)

	if res := svc.SetKey("azure-speech", "  la-escrita\n"); res.Error != "" {
		t.Fatalf("SetKey: %s", res.Error)
	}

	got, ok := vault.get(store.SlotAzureSpeech)
	if !ok {
		t.Fatal("nothing was stored")
	}
	if got != "la-escrita" {
		t.Errorf("stored %q, want %q — the padding travelled into the Keychain", got, "la-escrita")
	}
}

// The payload carries its own recency, so a snapshot that started earlier can be recognised as the
// older one and dropped. Without it the page has no way to tell two payloads apart, and the probe's
// fresh state could be overwritten by a slower snapshot from Sistema or idiomas.
func TestEveryPayloadCarriesAGrowingRevision(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)

	first := svc.Load().Revision
	second := svc.Load().Revision
	third := svc.SetProvider("whisper").Payload.Revision

	if first == 0 {
		t.Error("the first payload has no revision — the page cannot arbitrate on it")
	}
	if second <= first || third <= second {
		t.Errorf("revisions did not grow: %d, %d, %d", first, second, third)
	}
}

// The order is by when a snapshot STARTED, not by when it finished — which is the whole guarantee.
//
// A payload that began earlier and took longer to assemble must come out as the older one, because
// that is what lets the page drop it. Stamping at the end would order them by completion and reverse
// exactly the case this exists for: the slow snapshot is the stale one.
func TestRevisionOrdersSnapshotsByWhenTheyStarted(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)

	started := make(chan struct{})
	release := make(chan struct{})
	slow := true
	svc.bootstrap.devices = func() ([]audio.InputDevice, error) {
		if slow {
			slow = false
			close(started)
			<-release
		}
		return nil, nil
	}

	type result struct{ rev uint64 }
	first := make(chan result, 1)
	go func() { first <- result{svc.Load().Revision} }()

	<-started // the first snapshot has taken its revision and is now stuck mid-assembly
	second := svc.Load().Revision
	close(release)

	got := <-first
	if got.rev >= second {
		t.Errorf("the snapshot that started first has revision %d and the later one %d — "+
			"they are ordered by completion, so the stale one would win", got.rev, second)
	}
}
