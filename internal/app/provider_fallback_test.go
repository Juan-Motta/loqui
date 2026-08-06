package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Juan-Motta/loqui-go/internal/permissions"
	"github.com/Juan-Motta/loqui-go/internal/store"
)

// The app must not sit on an engine it cannot use. Leaving it there means the next dictation fails
// far from the moment that broke it — and the moment that broke it is usually a key being deleted.
func TestAnUnusableEngineFallsBackToTheDefault(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Provider = "azure" // no key, no region: unconfigured
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	res := svc.EnsureUsableEngine()

	if res.Error != "" {
		t.Fatalf("unexpected failure: %s", res.Error)
	}
	if got := st.LoadSettings().Provider; got != "whisper" {
		t.Errorf("provider = %q, want whisper", got)
	}
	if res.Payload.Provider != "whisper" {
		t.Errorf("the returned payload still says %q", res.Payload.Provider)
	}
	// Silently is not good enough: the user chose Azure, and something else is now in effect.
	if res.Notice == "" {
		t.Error("the engine was changed under the user without a word")
	}
	if !strings.Contains(res.Notice, "Whisper") {
		t.Errorf("notice = %q — it does not say what is in effect now", res.Notice)
	}
}

// An engine that cannot run on THIS machine is just as unusable, and it is reachable: SetProvider
// only checks whether an engine is ported, while support depends on the OS and the helpers present.
func TestAnUnsupportedEngineAlsoFallsBack(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)
	svc.bootstrap.caps = func() store.HostCapabilities {
		return store.HostCapabilities{Platform: "darwin", OSMajor: 15} // macOS 15: no SpeechAnalyzer
	}
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Provider = "macos"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	res := svc.EnsureUsableEngine()

	if got := st.LoadSettings().Provider; got != "whisper" {
		t.Errorf("provider = %q, want whisper", got)
	}
	if res.Notice == "" {
		t.Error("said nothing")
	}
}

// THE CASE THAT MUST NOT MOVE ANYTHING. Unreadable credentials mean the app could not CHECK the
// key, not that there is none: a corrupt or truncated secrets.json says nothing about what was in it.
// Switching engines on that would take a working Azure setup away over a damaged file.
func TestUnreadableCredentialsNeverChangeTheEngine(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)
	svc.bootstrap.keyStatus = func(store.KeySlot) store.KeyStatus { return store.KeyUnreadable }
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Provider = "azure"
		cfg.Region = "eastus2"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	res := svc.EnsureUsableEngine()

	if got := st.LoadSettings().Provider; got != "azure" {
		t.Fatalf("provider = %q — a Keychain timeout moved the user off their engine", got)
	}
	if res.Notice == "" {
		t.Error("the app could not verify the engine and said nothing about it")
	}
	if !strings.Contains(res.Notice, "no se pudieron leer") {
		t.Errorf("notice = %q — it must say the check failed, not that the engine is misconfigured",
			res.Notice)
	}
}

// Falling back onto a second broken engine is not a fallback. Whisper needs a model file that the
// connection state knows nothing about, so its row can read "Disponible" while it cannot dictate.
func TestNoFallbackWhenTheDefaultEngineCannotRunEither(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)
	svc.defaultProblem = func() error { return errors.New("falta el modelo de Whisper") }
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Provider = "azure"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	res := svc.EnsureUsableEngine()

	if got := st.LoadSettings().Provider; got != "azure" {
		t.Errorf("provider = %q — it moved onto an engine that cannot dictate either", got)
	}
	if !strings.Contains(res.Notice, "modelo") {
		t.Errorf("notice = %q — it must say why the default could not take over", res.Notice)
	}
}

// A usable engine is left alone, and saying nothing is the right outcome: this runs on every launch,
// so a message here would be noise on every launch.
func TestAUsableEngineIsLeftAloneAndSaysNothing(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	vault.set(store.SlotAzureSpeech, "la-guardada")
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Provider = "azure"
		cfg.Region = "eastus2"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	res := svc.EnsureUsableEngine()

	if got := st.LoadSettings().Provider; got != "azure" {
		t.Errorf("provider = %q — a working engine was replaced", got)
	}
	if res.Notice != "" {
		t.Errorf("notice = %q — nothing happened, so there is nothing to say", res.Notice)
	}
}

// Deleting the key of the engine in use is the exact moment an engine becomes unusable, and the user
// is right there watching. It must not take until the next launch — or the next dictation — to find
// out that nothing can transcribe any more.
func TestDeletingTheActiveEngineKeyFallsBackImmediately(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	vault.set(store.SlotAzureSpeech, "la-guardada")
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Provider = "azure"
		cfg.Region = "eastus2"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	res := svc.DeleteKey("azure-speech")

	if res.Error != "" {
		t.Fatalf("delete failed: %s", res.Error)
	}
	if got := st.LoadSettings().Provider; got != "whisper" {
		t.Errorf("provider = %q, want whisper — the app stayed on an engine with no credential", got)
	}
	if !strings.Contains(res.Notice, "Whisper") {
		t.Errorf("notice = %q — it must report both the deletion and what is in effect now", res.Notice)
	}
	if !strings.Contains(strings.ToLower(res.Notice), "clave") {
		t.Errorf("notice = %q — it dropped the part about the key it just deleted", res.Notice)
	}
	// The PAYLOAD is what redraws the page, and it is computed separately from the disk write. A
	// regression that moved the engine on disk but handed back the pre-switch snapshot would leave
	// Azure in the picker under a sentence saying Whisper is now in effect.
	if res.Payload.Provider != "whisper" {
		t.Errorf("payload says %q — the screen would contradict the sentence under it",
			res.Payload.Provider)
	}
}

// A provider read off disk that this build does not know is the worst kind of stuck: nothing
// validates it on the way in, dictation refuses it, and without this it would never be repaired or
// explained. Settings copied from a newer version, or edited by hand, land here.
func TestAnUnknownStoredEngineIsRepaired(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Provider = "motor-de-otra-version"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	res := svc.EnsureUsableEngine()

	if !res.Changed {
		t.Error("Changed = false — nothing was reported as having moved")
	}
	if got := st.LoadSettings().Provider; got != "whisper" {
		t.Errorf("provider = %q, want whisper — the app would never dictate again", got)
	}
	if !strings.Contains(res.Notice, "motor-de-otra-version") {
		t.Errorf("notice = %q — it must name what it found stored", res.Notice)
	}
}

// The check reads the Keychain, which can take seconds, and the window is live throughout. A choice
// the user makes during that wait is NEWER than the decision this check reached, so it wins.
func TestALaterChoiceByTheUserIsNotOverwritten(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Provider = "azure" // unconfigured: the check is about to move it to whisper
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The payload is captured first, exactly as EnsureUsableEngine does...
	gen := svc.readinessNow()
	p := svc.Load()
	// ...and THEN the user picks something else, while the check is still deciding.
	vault.set(store.SlotAzureSpeech, "recién pegada")
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Provider = "macos"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	out, err := svc.repairEngine(p, repairEvidence{sampled: true, readiness: gen})
	if err != nil {
		t.Fatalf("repair: %v", err)
	}

	if got := st.LoadSettings().Provider; got != "macos" {
		t.Errorf("provider = %q — a stale decision overwrote what the user just chose", got)
	}
	if out.changed {
		t.Error("changed = true, but nothing was written")
	}
}

// A successful delete PROVES the slot is empty. If the payload computed right after happens to come
// back "unreadable", the ordinary caution would refuse to move — and the engine would stay selected
// with a credential that is provably gone.
func TestDeleteRepairsEvenWhenTheFollowUpReadIsUnreadable(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	vault.set(store.SlotAzureSpeech, "la-guardada")
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Provider = "azure"
		cfg.Region = "eastus2"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Every read from now on times out — the state this build hits routinely.
	svc.bootstrap.keyStatus = func(store.KeySlot) store.KeyStatus { return store.KeyUnreadable }

	res := svc.DeleteKey("azure-speech")

	if res.Error != "" {
		t.Fatalf("delete failed: %s", res.Error)
	}
	if got := st.LoadSettings().Provider; got != "whisper" {
		t.Errorf("provider = %q — it kept an engine whose key it had just deleted", got)
	}
}

// The real model check, with no override: this is what "comprueba su modelo" actually means, and
// replacing the whole function in a test leaves those lines unexercised.
func TestTheDefaultEngineProblemLooksForTheModelOnDisk(t *testing.T) {
	dir := t.TempDir()
	st := store.NewAt(dir)
	svc, _ := testService(t, st)
	svc.defaultProblem = nil // the real check, deliberately

	if err := svc.defaultEngineProblem(); err == nil {
		t.Fatal("no model on disk and no problem reported")
	} else if !strings.Contains(err.Error(), "modelo") {
		t.Errorf("problem = %v, want it to name the model", err)
	}

	model := WhisperModelPath(dir)
	if err := os.MkdirAll(filepath.Dir(model), 0o700); err != nil {
		t.Fatal(err)
	}

	// A few bytes at the right path is what a stopped download looks like, and it must not pass:
	// switching onto a Whisper that cannot load its model is a fallback onto a second silent failure.
	if err := os.WriteFile(model, []byte("descarga a medias"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := svc.defaultEngineProblem(); err == nil {
		t.Error("a truncated model passed as usable")
	} else if !strings.Contains(err.Error(), "no es el esperado") {
		t.Errorf("problem = %v, want it to say the file is not the expected model", err)
	}

	// One byte short of the real thing. This is the case a floor cannot catch and the pinned size can:
	// a download that stopped at 90% looks nothing like an empty file and everything like a model.
	almost, err := os.Create(model)
	if err != nil {
		t.Fatal(err)
	}
	if err := almost.Truncate(WhisperModelBytes - 1); err != nil {
		t.Fatal(err)
	}
	if err := almost.Close(); err != nil {
		t.Fatal(err)
	}
	if err := svc.defaultEngineProblem(); err == nil {
		t.Error("a model one byte short passed as usable")
	}

	// And one byte too many. A file larger than the pinned size is not this model — a mirror serving a
	// different one looks exactly like this — and blessing it would send the app to an engine that
	// fails at load just the same.
	extra, err := os.Create(model)
	if err != nil {
		t.Fatal(err)
	}
	if err := extra.Truncate(WhisperModelBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := extra.Close(); err != nil {
		t.Fatal(err)
	}
	if err := svc.defaultEngineProblem(); err == nil {
		t.Error("a file larger than the model passed as the model")
	}

	// A directory at that path is the other shape of "there but useless".
	if err := os.Remove(model); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(model, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := svc.defaultEngineProblem(); err == nil {
		t.Error("a directory passed as a model")
	}
	if err := os.Remove(model); err != nil {
		t.Fatal(err)
	}

	// And the exact size passes. Sparse, so the test does not write 465 MB to disk.
	f, err := os.Create(model)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(WhisperModelBytes); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := svc.defaultEngineProblem(); err != nil {
		t.Errorf("the model is there and it still complains: %v", err)
	}
}

// Configuring the SAME engine during the check is the stale case the provider name cannot catch: the
// snapshot said "azure, unusable", the user pastes a key, and the name is still azure.
func TestConfiguringTheSameEngineDuringTheCheckIsNotUndone(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Provider = "azure"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	gen := svc.readinessNow()
	p := svc.Load() // the snapshot the decision rests on: azure, unconfigured

	// While the check is still deciding, the user configures the very engine it is about to reject.
	if res := svc.SaveConnection("azure-speech", "eastus2", "recién-pegada"); res.Error != "" {
		t.Fatalf("arranging the concurrent save: %s", res.Error)
	}

	out, err := svc.repairEngine(p, repairEvidence{sampled: true, readiness: gen})
	if err != nil {
		t.Fatalf("repair: %v", err)
	}

	if got := st.LoadSettings().Provider; got != "azure" {
		t.Errorf("provider = %q — it undid a configuration the user had just finished", got)
	}
	if out.changed {
		t.Error("changed = true, but nothing should have been written")
	}
}

// A decision taken WHILE a setter is halfway through is the case a plain counter cannot express: at
// that instant nothing has "changed" yet — the key is still being written — so a before/after
// comparison sees equality and lets a payload that is already half-obsolete pass for current.
//
// The setters and the decision share one lock, so the check cannot look at all until the setter is
// done — and then it looks at the finished state. That also covers the messages: telling the user
// their engine is not ready while they are in the middle of configuring it is wrong even when
// nothing gets overwritten.
func TestNoDecisionIsTakenWhileASetterIsInFlight(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Provider = "azure"
		cfg.Region = "eastus2" // so the key is the only thing in doubt
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Reads time out from here on: without the in-flight guard this arrangement produces the
	// "no se pudo comprobar la clave" notice, which is exactly what must NOT be said mid-save.
	svc.bootstrap.keyStatus = func(store.KeySlot) store.KeyStatus { return store.KeyUnreadable }

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	svc.setSecret = func(slot store.KeySlot, secret string) error {
		close(started)
		<-release
		vault.set(slot, secret)
		return nil
	}
	go func() {
		defer close(done)
		svc.SaveConnection("azure-speech", "eastus2", "recién-pegada")
	}()

	<-started // the save is inside its critical section, credential not yet written

	// The check runs against a world that is mid-change. It does not get to peek: the lock the setter
	// holds makes it WAIT, which is why this has to run on its own goroutine.
	checked := make(chan EngineCheck, 1)
	go func() { checked <- svc.EnsureUsableEngine() }()

	select {
	case res := <-checked:
		t.Fatalf("the check decided while the save was still running: %+v", res)
	case <-time.After(150 * time.Millisecond):
		// Blocked, as it must be.
	}

	// Once the key lands, Azure is configured — and that is what the check must see.
	svc.bootstrap.keyStatus = func(store.KeySlot) store.KeyStatus { return store.KeyPresent }
	close(release)
	<-done

	res := <-checked
	if res.Error != "" {
		t.Fatalf("check: %s", res.Error)
	}
	if got := st.LoadSettings().Provider; got != "azure" {
		t.Errorf("provider = %q — it acted on the state from before the save finished", got)
	}
	if res.Changed {
		t.Error("Changed = true — it moved the user off an engine that had just been configured")
	}
	if res.Notice != "" {
		t.Errorf("notice = %q — nothing was wrong by the time it looked", res.Notice)
	}
}

// A check overtaken mid-read must START AGAIN, not report what it saw before it was overtaken.
//
// The difference is not academic: the page paints from the payload this returns. Handing back the
// snapshot from before the user's save would draw their card as unconfigured a heartbeat after they
// configured it — the check would be the thing undoing their work on screen, having correctly decided
// not to undo it on disk.
func TestACheckOvertakenMidReadStartsAgain(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Provider = "azure"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var configured atomic.Bool
	svc.bootstrap.keyStatus = func(store.KeySlot) store.KeyStatus {
		if configured.Load() {
			return store.KeyPresent
		}
		return store.KeyAbsent
	}

	// The permission read holds the FIRST payload open so a save can land inside it. It has to be
	// THIS seam and not the device list: permissions are read after the key states, so the payload
	// left waiting here is one that has already recorded the old credential — which is exactly the
	// stale snapshot this test is about.
	//
	// A token, not a sync.Once: the concurrent save computes a payload of its own, so it calls this
	// too, and Once would make it queue behind the very read it is meant to overtake.
	reading := make(chan struct{})
	release := make(chan struct{})
	first := make(chan struct{}, 1)
	first <- struct{}{}
	svc.bootstrap.perms = func() PermissionsState {
		select {
		case <-first:
			close(reading)
			<-release
		default:
		}
		return PermissionsState{Microphone: permissions.Granted}
	}

	checked := make(chan EngineCheck, 1)
	go func() { checked <- svc.EnsureUsableEngine() }()

	<-reading
	// Azure is configured while the check is mid-read. Its first attempt is now describing a world
	// that no longer exists.
	if res := svc.SaveConnection("azure-speech", "eastus2", "recién-pegada"); res.Error != "" {
		t.Fatalf("arranging the concurrent save: %s", res.Error)
	}
	configured.Store(true)
	vault.set(store.SlotAzureSpeech, "recién-pegada")
	close(release)

	res := <-checked

	if got := st.LoadSettings().Provider; got != "azure" {
		t.Errorf("provider = %q — it acted on the state from before the save", got)
	}
	if res.Changed {
		t.Error("Changed = true — it switched away from an engine that had just been configured")
	}
	if statusOf(res.Payload, "azure-speech") != store.KeyPresent {
		t.Error("the payload handed back is the one from before the save — the page would repaint the " +
			"card as unconfigured right after the user configured it")
	}
}

// A save that is REFUSED changed nothing, so it must not read as a change. The launch check treats a
// change as a reason to start over, and its retries are finite: a few refused saves landing in its
// reads would use them up and leave the app on an unusable engine for the rest of the session.
func TestARefusedSaveDoesNotCountAsAChange(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)

	before := svc.readinessNow()
	if res := svc.SaveConnection("azure-speech", "eastus2", ""); res.Error == "" {
		t.Fatal("the save was accepted; this case needs a refused one")
	}
	if got := svc.readinessNow(); got != before {
		t.Errorf("readiness went %d → %d — a refused save announced itself as a change", before, got)
	}

	// And one that lands does count, or the guard would never fire at all.
	if res := svc.SaveConnection("azure-speech", "eastus2", "una-clave"); res.Error != "" {
		t.Fatalf("save: %s", res.Error)
	}
	if got := svc.readinessNow(); got == before {
		t.Error("a save that landed did not count")
	}
}

// A FAILED credential operation changed nothing, so it must not count as a change.
//
// This is the INVERSE of what the Keychain backend needed, and the flip is the point. There, a
// timeout meant the cgo call had been abandoned and could still land seconds later, so a failure had
// to be counted as a possible change or the launch check would trust a snapshot taken moments before
// a credential vanished. The file backend has no abandoned call: a failed read never got as far as
// writing, and a rename either happened or it did not. Counting a failure now would spend the
// check's retries on nothing.
func TestAFailedCredentialOperationIsNotCountedAsAChange(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)

	for _, c := range []struct {
		name string
		err  error
	}{
		{"the credentials could not be read", store.ErrSecretsUnreadable},
		{"the delete was rejected outright", errors.New("el disco está lleno")},
	} {
		t.Run(c.name, func(t *testing.T) {
			svc.deleteSecret = func(store.KeySlot) error { return c.err }
			before := svc.readinessNow()

			if res := svc.DeleteKey("azure-speech"); res.Error == "" {
				t.Fatal("the failure was not reported")
			}
			if got := svc.readinessNow(); got != before {
				t.Errorf("readiness %d → %d — a failure that changed nothing announced itself as a change",
					before, got)
			}
		})
	}

	// And one that LANDS does count, or the guard would never fire at all and this test would pass
	// against a service that had stopped counting altogether.
	svc.deleteSecret = nil
	if err := st.SetKey(store.SlotAzureSpeech, "la-guardada"); err != nil {
		t.Fatal(err)
	}
	before := svc.readinessNow()
	if res := svc.DeleteKey("azure-speech"); res.Error != "" {
		t.Fatalf("delete: %s", res.Error)
	}
	if got := svc.readinessNow(); got == before {
		t.Error("a delete that landed did not count")
	}
}

// Choosing Whisper without its model is the one selection the connection model cannot warn about: its
// row says "Disponible" either way, so this notice is the only thing standing between the user and an
// engine that accepts the click and then transcribes nothing.
func TestChoosingTheDefaultEngineWithoutItsModelSaysSo(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)
	svc.defaultProblem = func() error { return errors.New("falta su modelo") }

	res := svc.SetProvider("whisper")

	if res.Error != "" {
		t.Fatalf("unexpected failure: %s", res.Error)
	}
	if !strings.Contains(res.Notice, "modelo") {
		t.Errorf("notice = %q — it reads as a plain success on an engine that cannot dictate", res.Notice)
	}
}

// An Azure configured for the sub-service this build cannot run is not "active", whatever the
// settings model says: the row follows azureService, and the engine that gets built always opens
// Speech. Reachable from a settings file written by another version or edited by hand.
func TestAnEngineConfiguredForAnUnrunnableSubserviceFallsBack(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	// Everything the realtime sub-service would need, and nothing Speech needs.
	vault.set(store.SlotAzureOpenAI, "la-de-openai")
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Provider = "azure"
		cfg.AzureService = "openai"
		cfg.AzureOpenAiResource = "mi-recurso"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	res := svc.EnsureUsableEngine()

	if got := st.LoadSettings().Provider; got != "whisper" {
		t.Errorf("provider = %q — the row said active for a service nothing will start", got)
	}
	if !res.Changed {
		t.Error("Changed = false, but the engine moved")
	}
}

// The fallback engine being ACTIVE is not the same as it working. Whisper's row says "Activo" as soon
// as its helper is there, and its model is invisible to that model — which is the state of a fresh
// install: nothing dictates and nothing explains it.
func TestAnActiveDefaultEngineWithNoModelIsReported(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)
	svc.defaultProblem = func() error { return errors.New("falta su modelo") }
	// whisper is the default provider already.

	res := svc.EnsureUsableEngine()

	if res.Changed {
		t.Error("it switched engines — there is nowhere to switch TO from the fallback")
	}
	if res.Notice == "" {
		t.Fatal("the app cannot dictate at all and said nothing")
	}
	if !strings.Contains(res.Notice, "modelo") {
		t.Errorf("notice = %q — it must name what is missing", res.Notice)
	}
}

// An unreadable Keychain shields the engine only when the KEY is the one thing in doubt. Azure with no
// region cannot dictate whatever the key turns out to be, so that case must still move.
func TestAnUnreadableKeychainDoesNotShieldAMissingRegion(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, _ := testService(t, st)
	svc.bootstrap.keyStatus = func(store.KeySlot) store.KeyStatus { return store.KeyUnreadable }
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Provider = "azure"
		cfg.Region = "" // definitely missing, whatever the key situation is
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	res := svc.EnsureUsableEngine()

	if got := st.LoadSettings().Provider; got != "whisper" {
		t.Errorf("provider = %q — an unverifiable key hid a requirement that is provably absent", got)
	}
	if !res.Changed {
		t.Error("Changed = false, but the engine moved")
	}
}

// Deleting a key that belongs to some OTHER engine must not change which engine is in use — and the
// arrangement is deliberately the awkward one: the active engine is ALREADY unusable.
//
// With a healthy active engine this case proves nothing, because there would be nothing to repair
// either way. Here there is something to repair, and the point is that THIS action is not the one
// that gets to do it: attributing an engine switch to "I deleted Grok's key" is the kind of surprise
// that makes an app feel haunted. The switch belongs to the launch check, or to deleting the key
// actually in use.
func TestDeletingAnUnrelatedKeyLeavesTheEngineAlone(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	vault.set(store.SlotGrok, "la-de-grok")
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Provider = "azure" // no key and no region: unusable, and not what is being deleted
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	res := svc.DeleteKey("grok")

	if got := st.LoadSettings().Provider; got != "azure" {
		t.Errorf("provider = %q — deleting Grok's key switched the engine", got)
	}
	if strings.Contains(res.Notice, "Whisper") {
		t.Errorf("notice = %q — nothing was switched", res.Notice)
	}
}

// A stale world must not produce a stale SENTENCE either, and this is the case where that is the only
// thing at stake.
//
// Every path that ends in a WRITE is guarded twice: repairEngine refuses up front, and moveToDefault
// compares again under the lock it writes inside. The paths that end in a notice and nothing else have
// only the first guard — so it is the only thing standing between the user and being told "no se pudo
// comprobar la clave de Azure" one heartbeat after they successfully pasted it. Nothing was
// overwritten, and the app still called their working configuration broken.
//
// Arranged as the awkward version on purpose: the region is set first, so the KEY is genuinely the one
// thing in doubt and the unreadable-Keychain notice is what the stale payload earns.
func TestAStaleWorldDoesNotProduceAStaleNotice(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Provider = "azure"
		cfg.Region = "eastus2" // so the key is the only thing unconfirmed
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var configured atomic.Bool
	svc.bootstrap.keyStatus = func(store.KeySlot) store.KeyStatus {
		if configured.Load() {
			return store.KeyPresent
		}
		return store.KeyUnreadable // the Keychain did not answer: notice-only path, no write
	}

	// Same seam as TestACheckOvertakenMidReadStartsAgain: the permission read holds the first payload
	// open, and it is read AFTER the key states, so what waits here has already recorded the timeout.
	reading := make(chan struct{})
	release := make(chan struct{})
	first := make(chan struct{}, 1)
	first <- struct{}{}
	svc.bootstrap.perms = func() PermissionsState {
		select {
		case <-first:
			close(reading)
			<-release
		default:
		}
		return PermissionsState{Microphone: permissions.Granted}
	}

	checked := make(chan EngineCheck, 1)
	go func() { checked <- svc.EnsureUsableEngine() }()

	<-reading
	// The key lands while the check is mid-read. Its first attempt now holds a payload describing a
	// Keychain that no longer matters.
	if res := svc.SaveConnection("azure-speech", "eastus2", "recién-pegada"); res.Error != "" {
		t.Fatalf("arranging the concurrent save: %s", res.Error)
	}
	configured.Store(true)
	vault.set(store.SlotAzureSpeech, "recién-pegada")
	close(release)

	res := <-checked

	if res.Error != "" {
		t.Fatalf("check: %s", res.Error)
	}
	if res.Notice != "" {
		t.Errorf("notice = %q — it reported a Keychain timeout that had already been superseded by "+
			"the user's save", res.Notice)
	}
	if res.Changed {
		t.Error("Changed = true — nothing should have moved")
	}
	if got := st.LoadSettings().Provider; got != "azure" {
		t.Errorf("provider = %q — it acted on the state from before the save", got)
	}
}

// A world that moves AFTER the check must withdraw the sentence too, not just the write.
//
// The write path is guarded twice — once up front, once under the lock it writes inside. The
// notice-only paths were guarded only up front, and what runs between that check and the sentence is
// the slow part: a settings read and a stat of a 465 MB file. A setter completing in that gap produces
// the failure this whole change exists to prevent, one layer in: the screen is correct and the sentence
// beneath it describes the engine the user just left.
//
// Arranged with the seam INSIDE defaultEngineProblem, because that is exactly where the real gap is.
func TestANoticeIsWithdrawnWhenTheWorldMovesUnderIt(t *testing.T) {
	st := store.NewAt(t.TempDir())
	svc, vault := testService(t, st)
	// Azure is fully configured and waiting: it is what the user picks mid-check, and the reason the
	// second attempt has something better to conclude.
	vault.set(store.SlotAzureSpeech, "la-guardada")
	if err := st.UpdateSettings(func(cfg *store.Settings) error {
		cfg.Provider = "whisper" // the default, active, and missing its model
		cfg.Region = "eastus2"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Fires once: the retry must be allowed to reach a conclusion instead of being overtaken forever.
	first := make(chan struct{}, 1)
	first <- struct{}{}
	svc.defaultProblem = func() error {
		select {
		case <-first:
			// The user picks a configured engine while the check is deciding about the old one. Called
			// on this goroutine deliberately: repairEngine holds no lock here, and if it ever did, this
			// would deadlock and say so loudly.
			if res := svc.SetProvider("azure"); res.Error != "" {
				t.Errorf("arranging the concurrent choice: %s", res.Error)
			}
		default:
		}
		return errors.New("falta su modelo")
	}

	res := svc.EnsureUsableEngine()

	if res.Error != "" {
		t.Fatalf("check: %s", res.Error)
	}
	if got := st.LoadSettings().Provider; got != "azure" {
		t.Fatalf("provider = %q — the user's newer choice did not survive", got)
	}
	// Azure is configured, so the second attempt has nothing to report. The stale sentence would have
	// been about Whisper's missing model — an engine that is no longer selected.
	if res.Notice != "" {
		t.Errorf("notice = %q — it described the engine the user had just left", res.Notice)
	}
	if res.Changed {
		t.Error("Changed = true — nothing was moved by the check")
	}
	if res.Payload.Provider != "azure" {
		t.Errorf("payload says %q — the page would repaint the engine the user just left",
			res.Payload.Provider)
	}
}

// Each landing counts ON ITS OWN, and saving key and region together cannot prove that.
//
// The combined save is the ordinary case and it hides a real gap: with both bumps present, dropping
// either one leaves the other to keep the count moving, so a test that only ever saves both passes
// with half the accounting gone. It matters because SaveConnection commits in two steps and the first
// can land while the second fails — the credential is in the Keychain, the region write errors out,
// and a check that concluded "nothing changed" from that would be reasoning about a world that had.
func TestEachLandingCountsOnItsOwn(t *testing.T) {
	t.Run("una clave sin región", func(t *testing.T) {
		st := store.NewAt(t.TempDir())
		svc, _ := testService(t, st)

		before := svc.readinessNow()
		// Grok needs no region, so this write is unambiguously the credential and nothing else.
		if res := svc.SaveConnection("grok", "", "la-de-grok"); res.Error != "" {
			t.Fatalf("save: %s", res.Error)
		}
		if got := svc.readinessNow(); got == before {
			t.Error("a credential landed and was not counted — the launch check would trust a snapshot " +
				"taken before it")
		}
	})

	t.Run("una región sin clave", func(t *testing.T) {
		st := store.NewAt(t.TempDir())
		svc, vault := testService(t, st)
		// A stored key is what makes a region-only save legal; without one it is refused.
		vault.set(store.SlotAzureSpeech, "la-guardada")

		before := svc.readinessNow()
		if res := svc.SaveConnection("azure-speech", "westeurope", ""); res.Error != "" {
			t.Fatalf("save: %s", res.Error)
		}
		if got := svc.readinessNow(); got == before {
			t.Error("a region landed and was not counted — and a region is exactly what decides whether " +
				"Azure can dictate at all")
		}
	})
}
