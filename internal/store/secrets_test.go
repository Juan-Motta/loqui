package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestASecretComesBackOutAsItWentIn(t *testing.T) {
	s := NewAt(t.TempDir())
	if err := s.SetKey(SlotAzureSpeech, "la-clave-de-azure"); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := s.GetKey(SlotAzureSpeech)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != "la-clave-de-azure" {
		t.Errorf("got %q — a credential that comes back altered is worse than one that fails to save", got)
	}
}

// An empty slot is ErrNoSecret and NOT the unreadable error. The whole three-state model rests on
// this: "there is no key" sends the user to paste one, "I could not read it" must not.
func TestAnEmptySlotIsAbsentAndNotUnreadable(t *testing.T) {
	s := NewAt(t.TempDir())
	_, err := s.GetKey(SlotGrok)
	if !errors.Is(err, ErrNoSecret) {
		t.Fatalf("err = %v, want ErrNoSecret", err)
	}
	if errors.Is(err, ErrSecretsUnreadable) {
		t.Error("an empty slot reported as unreadable — the user would be told to fix a signature " +
			"instead of pasting their key")
	}
	if got := s.KeyStatusFor(SlotGrok); got != KeyAbsent {
		t.Errorf("status = %q, want absent", got)
	}
}

// A file that cannot be parsed is UNREADABLE, not empty. Collapsing it into "absent" is how a user
// with a perfectly good key gets told to paste one — and, worse, how the engine fallback would move
// them off a working engine over a corrupted file.
func TestACorruptFileIsUnreadableAndNotEmpty(t *testing.T) {
	dir := t.TempDir()
	s := NewAt(dir)
	if err := os.WriteFile(filepath.Join(dir, secretsFileName), []byte("{no es json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := s.GetKey(SlotAzureSpeech)
	if !errors.Is(err, ErrSecretsUnreadable) {
		t.Fatalf("err = %v, want ErrSecretsUnreadable", err)
	}
	if errors.Is(err, ErrNoSecret) {
		t.Error("a corrupt file read as an empty slot")
	}
	if got := s.KeyStatusFor(SlotAzureSpeech); got != KeyUnreadable {
		t.Errorf("status = %q, want unreadable", got)
	}
}

// A file that cannot be OPENED is unreadable too, and this is the case that is not about JSON: wrong
// ownership or permissions after a restore from backup.
func TestAnUnopenableFileIsUnreadable(t *testing.T) {
	dir := t.TempDir()
	s := NewAt(dir)
	if err := s.SetKey(SlotAzureSpeech, "algo"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, secretsFileName)
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	if _, err := s.GetKey(SlotAzureSpeech); !errors.Is(err, ErrSecretsUnreadable) {
		t.Fatalf("err = %v, want ErrSecretsUnreadable", err)
	}
}

// Deleting an empty slot is SUCCESS. The notice the UI shows is a postcondition ("the key is no
// longer stored"), and that sentence is only true if this never reports a failure for an empty slot.
func TestDeletingAnEmptySlotSucceeds(t *testing.T) {
	s := NewAt(t.TempDir())
	if err := s.DeleteKey(SlotOpenAI); err != nil {
		t.Errorf("delete on an empty slot returned %v — absent has to be success", err)
	}
}

func TestDeletingRemovesOnlyThatSlot(t *testing.T) {
	s := NewAt(t.TempDir())
	if err := s.SetKey(SlotAzureSpeech, "la-de-azure"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetKey(SlotGrok, "la-de-grok"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteKey(SlotGrok); err != nil {
		t.Fatal(err)
	}

	if _, err := s.GetKey(SlotGrok); !errors.Is(err, ErrNoSecret) {
		t.Errorf("grok survived the delete: %v", err)
	}
	if got, err := s.GetKey(SlotAzureSpeech); err != nil || got != "la-de-azure" {
		t.Errorf("azure = %q, %v — deleting one slot took another with it", got, err)
	}
}

// REPLACING, not appending. The Keychain backend used SecItemUpdate for this because delete-then-add
// lost the old key if the add failed; the file has the same duty for the same reason.
func TestWritingASlotTwiceKeepsTheSecondValue(t *testing.T) {
	s := NewAt(t.TempDir())
	if err := s.SetKey(SlotAzureSpeech, "la-vieja"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetKey(SlotAzureSpeech, "la-nueva"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetKey(SlotAzureSpeech); got != "la-nueva" {
		t.Errorf("got %q, want la-nueva", got)
	}
}

// THE FAILURE A SHARED FILE INVITES, and the one the per-slot Keychain gates never had to think
// about: every write is a read-modify-write of the whole file, so two of them at once can lose one.
// Nobody clicks two cards simultaneously, but Wails dispatches every bound call on its own goroutine,
// so "nobody would do that" is not a guarantee the code gets to rely on.
func TestConcurrentWritesToDifferentSlotsDoNotLoseEachOther(t *testing.T) {
	s := NewAt(t.TempDir())
	slots := []KeySlot{SlotAzureSpeech, SlotAzureOpenAI, SlotOpenAI, SlotGrok, SlotElevenLabs}

	var wg sync.WaitGroup
	for _, slot := range slots {
		wg.Add(1)
		go func(sl KeySlot) {
			defer wg.Done()
			if err := s.SetKey(sl, "secreto-de-"+string(sl)); err != nil {
				t.Errorf("set %s: %v", sl, err)
			}
		}(slot)
	}
	wg.Wait()

	for _, slot := range slots {
		got, err := s.GetKey(slot)
		if err != nil {
			t.Errorf("%s: %v — a concurrent write dropped it", slot, err)
			continue
		}
		if want := "secreto-de-" + string(slot); got != want {
			t.Errorf("%s = %q, want %q", slot, got, want)
		}
	}
}

// The file is READABLE ONLY BY ITS OWNER. It holds cloud credentials in the clear — a deliberate,
// documented trade for a personal build — so the one protection left has to actually be applied,
// not assumed from the directory.
func TestTheSecretsFileIsNotReadableByOthers(t *testing.T) {
	dir := t.TempDir()
	s := NewAt(dir)
	if err := s.SetKey(SlotAzureSpeech, "algo"); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(dir, secretsFileName))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %04o, want 0600 — group or other can read the API keys", perm)
	}
}

// Rewriting must not widen the permissions. An atomic write creates a NEW file and renames it over
// the old one, so the mode has to be set on the replacement — inheriting the umask here would leave
// 0644 and nothing would notice.
func TestRewritingKeepsThePermissionsNarrow(t *testing.T) {
	dir := t.TempDir()
	s := NewAt(dir)
	for _, secret := range []string{"una", "dos", "tres"} {
		if err := s.SetKey(SlotAzureSpeech, secret); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(filepath.Join(dir, secretsFileName))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode after rewrites = %04o, want 0600", perm)
	}
}

// SECRETS DO NOT GO IN settings.json. That file is merged over raw JSON, shown in bug reports and
// pasted into issues; a credential in it leaks by being ordinary. Separate files keep the blast
// radius of an accidental paste to the settings.
func TestSecretsNeverLandInTheSettingsFile(t *testing.T) {
	dir := t.TempDir()
	s := NewAt(dir)
	if err := s.UpdateSettings(func(cfg *Settings) error {
		cfg.Provider = "azure"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetKey(SlotAzureSpeech, "el-secreto-inconfundible"); err != nil {
		t.Fatal(err)
	}

	settings, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(settings), "el-secreto-inconfundible") {
		t.Error("the secret is in settings.json")
	}
}

// Nothing is created just by ASKING. A read on a fresh install must not leave a file behind: an empty
// secrets.json is indistinguishable from one whose contents were lost, and it would make the "have
// you ever saved a key?" question unanswerable.
func TestReadingDoesNotCreateTheFile(t *testing.T) {
	dir := t.TempDir()
	s := NewAt(dir)

	_, _ = s.GetKey(SlotAzureSpeech)
	_ = s.KeyStatusFor(SlotGrok)
	_ = s.HasKey(SlotOpenAI)

	if _, err := os.Stat(filepath.Join(dir, secretsFileName)); !os.IsNotExist(err) {
		t.Errorf("the file exists after only reading (stat err = %v)", err)
	}
}

// Deleting the last slot leaves no stray secret behind in the file.
func TestDeletingTheLastSlotLeavesNoSecretInTheFile(t *testing.T) {
	dir := t.TempDir()
	s := NewAt(dir)
	if err := s.SetKey(SlotAzureSpeech, "el-secreto-inconfundible"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteKey(SlotAzureSpeech); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, secretsFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return // removing the file entirely is a fine way to hold no secrets
		}
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "el-secreto-inconfundible") {
		t.Error("the deleted secret is still in the file")
	}
}

// An unknown slot is rejected rather than stored. Otherwise a typo in a caller writes a credential
// under a name nothing will ever read, and the card goes on saying "no key" with the key on disk.
func TestAnUnknownSlotIsRefusedForStorage(t *testing.T) {
	s := NewAt(t.TempDir())
	if err := s.SetKey(KeySlot("no-existe"), "algo"); err == nil {
		t.Error("an unknown slot was accepted")
	}
}

func TestHasKeyFollowsPresence(t *testing.T) {
	s := NewAt(t.TempDir())
	if s.HasKey(SlotAzureSpeech) {
		t.Error("HasKey on an empty slot")
	}
	if err := s.SetKey(SlotAzureSpeech, "algo"); err != nil {
		t.Fatal(err)
	}
	if !s.HasKey(SlotAzureSpeech) {
		t.Error("HasKey false with a key stored")
	}
}

// An empty secret is a DELETE, not a stored blank. A blank credential is the worst of both: the card
// reads "configured" and every request fails auth.
func TestStoringAnEmptySecretIsRefused(t *testing.T) {
	s := NewAt(t.TempDir())
	if err := s.SetKey(SlotAzureSpeech, ""); err == nil {
		t.Error("a blank secret was accepted — the card would read as configured and never authenticate")
	}
}

// A ZERO-BYTE file is damage, not an empty set — the P1 this replaces got it backwards.
//
// The old reasoning was that a crash between create and write could leave one, so treating it as "no
// keys" let the user start over. That is false for this writer: the destination is only ever replaced
// by renaming a file that has already been written and synced. What DOES produce an empty file is a
// truncated copy or a failed restore, and then the contents are unknown — so reporting "absent" would
// let the engine fallback move someone off a working engine over a file that lost its contents.
func TestAZeroByteFileIsUnreadableAndNotEmpty(t *testing.T) {
	dir := t.TempDir()
	s := NewAt(dir)
	if err := os.WriteFile(filepath.Join(dir, secretsFileName), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := s.GetKey(SlotAzureSpeech)
	if !errors.Is(err, ErrSecretsUnreadable) {
		t.Fatalf("err = %v, want ErrSecretsUnreadable", err)
	}
	if errors.Is(err, ErrNoSecret) {
		t.Error("a truncated file read as an empty slot — the fallback would act on it")
	}
	if got := s.KeyStatusFor(SlotAzureSpeech); got != KeyUnreadable {
		t.Errorf("status = %q, want unreadable", got)
	}
	// And a write must not paper over it by overwriting whatever else the file might have held.
	if err := s.SetKey(SlotGrok, "otra"); !errors.Is(err, ErrSecretsUnreadable) {
		t.Errorf("SetKey over a damaged file returned %v — it would discard the other slots", err)
	}
}

// A file that arrives WIDER than 0600 gets narrowed, on read.
//
// Writing 0600 only protects files this app wrote. A restore from backup, an scp or an editor with its
// own temp file can leave the credentials world-readable, and the app would never notice because a
// wider mode reads perfectly well. Cleartext keys have exactly one protection left.
func TestAWideOpenFileIsNarrowedOnRead(t *testing.T) {
	dir := t.TempDir()
	s := NewAt(dir)
	if err := s.SetKey(SlotAzureSpeech, "la-guardada"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, secretsFileName)
	if err := os.Chmod(path, 0o644); err != nil { // as a backup or an scp would leave it
		t.Fatal(err)
	}

	// The read still succeeds: refusing over a mode the user can fix in one command would lock them
	// out of their own credentials.
	if got, err := s.GetKey(SlotAzureSpeech); err != nil || got != "la-guardada" {
		t.Fatalf("get = %q, %v — a wide file must still be readable", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %04o after a read, want 0600 — the keys stayed readable by other accounts", perm)
	}
}

// THE ATOMIC WRITE'S FAILURE PROPERTIES, which the previous round of tests claimed and did not cover.
//
// Every earlier permission test starts from a file this code wrote successfully, so a regression to a
// plain in-place write would have kept the whole suite green. What atomicity actually promises is
// this: when the write cannot complete, the PREVIOUS contents survive intact and no half-written temp
// file is left holding the secrets.
func TestAFailedWriteLeavesThePreviousFileAndNoTempBehind(t *testing.T) {
	dir := t.TempDir()
	s := NewAt(dir)
	if err := s.SetKey(SlotAzureSpeech, "la-buena"); err != nil {
		t.Fatal(err)
	}

	// A read-only directory makes CreateTemp fail, which is the earliest failure the write can hit —
	// and the one that must not touch the destination.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := s.SetKey(SlotGrok, "la-nueva"); err == nil {
		t.Fatal("the write succeeded with an unwritable directory")
	}

	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// The old credential is still there, byte for byte.
	if got, err := s.GetKey(SlotAzureSpeech); err != nil || got != "la-buena" {
		t.Errorf("azure = %q, %v — a failed write destroyed the previous contents", got, err)
	}
	// And the slot that failed did not half-arrive.
	if _, err := s.GetKey(SlotGrok); !errors.Is(err, ErrNoSecret) {
		t.Errorf("grok = %v — a failed write landed anyway", err)
	}
	assertNoTempFiles(t, dir)
}

// A SUCCESSFUL write leaves no temp file either. A leftover holds the secrets with nobody's name on
// it and at whatever mode it was abandoned in.
func TestASuccessfulWriteLeavesNoTempBehind(t *testing.T) {
	dir := t.TempDir()
	s := NewAt(dir)
	for _, slot := range []KeySlot{SlotAzureSpeech, SlotGrok, SlotOpenAI} {
		if err := s.SetKey(slot, "secreto-de-"+string(slot)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteKey(SlotGrok); err != nil {
		t.Fatal(err)
	}
	assertNoTempFiles(t, dir)
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != secretsFileName && strings.HasPrefix(e.Name(), secretsFileName) {
			t.Errorf("temp file left behind: %s — it holds the secrets with no owner", e.Name())
		}
	}
}

// EVERY PORTED ENGINE THAT NEEDS A CREDENTIAL MUST BE ABLE TO STORE ONE.
//
// This test exists because the absence of it cost two engines. `availableKeySlots` was written when
// only Azure Speech and Grok were ported, and when OpenAI and ElevenLabs landed afterwards nobody
// widened it — so SetKey and SaveConnection refused their keys with "este servicio todavía no está
// disponible en esta versión", a sentence that had quietly become false. Two fully ported engines were
// unusable through the interface, and the suite was green throughout.
//
// The provider list already has a contract test tying it to what buildProvider can construct. This is
// the same idea one level down, and it ties the two lists that drifted: if an engine is available AND
// KeySlotFor says it needs a credential, that credential's slot has to be storable.
//
// The next provider to be ported now fails here until its slot is listed, which is the point.
func TestEveryAvailableEngineThatNeedsAKeyCanStoreOne(t *testing.T) {
	for _, provider := range AllProviders {
		if !IsAvailableProvider(provider) {
			continue // not ported yet: refusing its key is correct
		}
		slot, needsKey := KeySlotFor(provider, "")
		if !needsKey {
			continue // whisper and macos carry no credential
		}
		if !IsAvailableKeySlot(slot) {
			t.Errorf("engine %q is available and needs slot %q, but IsAvailableKeySlot(%q) is false — "+
				"the app cannot save its key, so a ported engine is unusable from the interface",
				provider, slot, slot)
		}
	}
}

// And the converse, so the list does not grow past what the app can use: a storable slot has to be one
// some available engine actually reads. Without this half, "fix the test by adding everything" passes.
//
// Azure has two runtime slots, so the converse must consider both selected subservices.
func TestNoSlotIsStorableWithoutAnEngineThatReadsIt(t *testing.T) {
	readable := map[KeySlot]string{}
	for _, provider := range AllProviders {
		if !IsAvailableProvider(provider) {
			continue
		}
		// The RUNTIME slot, not the settings one: for Azure those differ until the realtime subservice
		// is ported, and the runtime answer is the one that says what dictation will actually read.
		for _, service := range []string{"speech", "openai"} {
			if slot, ok := RuntimeKeySlotFor(provider, service); ok {
				readable[slot] = provider
			}
		}
	}
	for _, slot := range AllKeySlots {
		if IsAvailableKeySlot(slot) && readable[slot] == "" {
			t.Errorf("slot %q is storable but no available engine reads it — the UI would offer to save "+
				"a credential that nothing will ever use", slot)
		}
	}
	if !IsAvailableKeySlot(SlotAzureOpenAI) {
		t.Error("azure-openai is runnable but its credential cannot be stored")
	}
}
