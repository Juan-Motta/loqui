// Provider credentials, in a file next to the settings.
//
// WHY NOT THE KEYCHAIN, which is where this used to live. On a build signed ad-hoc — which is every
// development build of this project — `SecItemCopyMatching` never returns: macOS does not recognise
// the binary, so it wants to ask the user for authorisation, and the prompt cannot be presented. The
// app could not read its own credential, so it could not dictate. That was not a rare edge: it was
// the common case, and it cost the user a real credential once (see
// docs/solutions/silent-success-is-a-bug.md). A three-second timeout made it diagnosable, not fixed.
//
// THE TRADE, stated rather than buried: these are cloud API keys in CLEARTEXT on disk. Anything
// running as this user can read them, and so can a backup or a lost laptop — and on this machine
// FileVault is off, so nothing encrypts them at rest. The login Keychain did encrypt them with the
// user's password, so this is a deliberate DOWNGRADE in protection, accepted by the project's owner
// for a personal build after being shown exactly this. The keys bill by the hour (Azure per hour,
// Grok $0.20/h streaming), so a leak is somebody else's invoice. The alternative that keeps both the
// simplicity and the encryption is a stable self-signed identity — which would also stop macOS
// revoking Accessibility and Input Monitoring on every rebuild, something this file does NOT fix.
//
// WHAT IS KEPT FROM THE OLD BACKEND, deliberately, because the rest of the app is built on it:
// the three-state answer (present / absent / unreadable). "Unreadable" is not a Keychain artefact —
// a file can be corrupt, or unreadable after a restore from backup — and collapsing it into "absent"
// is what sends a user with a working key off to paste another one, and what would let the engine
// fallback move them off an engine that was fine.
//
// NOT IN settings.json, and that is not tidiness. That file is merged over raw JSON, is shown in the
// About view and gets pasted into bug reports; a credential in it leaks by being ordinary.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// secretsFileName is where the credentials live, beside settings.json in the data directory.
const secretsFileName = "secrets.json"

// secretsPerm is owner-read/write and nothing else. It is the ONLY protection this file has, so it is
// applied on every write rather than inherited: an atomic write creates a new file and renames it over
// the old one, and a fresh file takes the umask — which on a default macOS account is 0644.
const secretsPerm os.FileMode = 0o600

// ErrNoSecret means the slot is empty: the file was read and there is nothing under that name.
var ErrNoSecret = errors.New("store: no secret in that slot")

// ErrSecretsUnreadable means the credentials could not be consulted, so NOTHING is known about the
// slot — the file is corrupt, or its permissions or ownership are wrong.
//
// It is a distinct error from ErrNoSecret and every caller has to keep them apart: "you have no key"
// asks the user to paste one, "I could not read your keys" must not, because they may well have one.
var ErrSecretsUnreadable = errors.New("store: the stored credentials could not be read")

// KeySlot names one provider's credential.
type KeySlot string

const (
	SlotAzureSpeech KeySlot = "azure-speech"
	SlotAzureOpenAI KeySlot = "azure-openai"
	SlotOpenAI      KeySlot = "openai"
	SlotGrok        KeySlot = "grok"
	SlotElevenLabs  KeySlot = "elevenlabs"
)

// AllKeySlots is every slot, for reporting which ones hold a key.
var AllKeySlots = []KeySlot{SlotAzureSpeech, SlotAzureOpenAI, SlotOpenAI, SlotGrok, SlotElevenLabs}

// knownSlots is every name this build will accept for storage.
//
// A write to an unknown slot is refused rather than stored: a typo in a caller would otherwise put a
// credential under a name nothing ever reads, and the card would go on saying "no key" with the key
// sitting on disk.
var knownSlots = func() map[KeySlot]bool {
	m := make(map[KeySlot]bool, len(AllKeySlots))
	for _, slot := range AllKeySlots {
		m[slot] = true
	}
	return m
}()

// availableKeySlots is the credentials the app can actually USE today.
//
// It is not derivable from provider availability, and that is the whole point: "azure" is an
// available provider, but only through its SPEECH subservice. azure-openai is Azure's realtime
// service, which is not ported — so a key stored there would never be read, while the settings page
// happily offers to store one.
var availableKeySlots = map[KeySlot]bool{
	SlotAzureSpeech: true,
	SlotGrok:        true,
}

// IsAvailableKeySlot reports whether a credential in this slot would ever be used.
func IsAvailableKeySlot(slot KeySlot) bool { return availableKeySlots[slot] }

// slotsUsingAzureRegion is the credentials the Azure Speech region applies to.
//
// The region is a single global setting, so pairing it with an unrelated slot is meaningless: saving
// a Grok key must not be able to move the Azure endpoint.
//
// azure-openai is NOT here, even though it is Azure. Its endpoint is addressed by resource and
// deployment name, not by region — the settings form says as much, keeping the region in
// #speechConfig and resource/deployment in #openaiConfig. Listing it let a region be written through
// a slot that has no use for one.
var slotsUsingAzureRegion = map[KeySlot]bool{
	SlotAzureSpeech: true,
}

// UsesAzureRegion reports whether a slot's connection includes the Azure region.
func UsesAzureRegion(slot KeySlot) bool { return slotsUsingAzureRegion[slot] }

// KeyStatus is what is known about a slot, never the secret itself.
//
// Three values and not two, which is the load-bearing part: HasKey collapses them into a bool, which
// is why the UI must not use it — told a slot is empty, the user retypes a credential that is
// already stored.
type KeyStatus string

const (
	// KeyPresent means a secret was read back from the slot.
	KeyPresent KeyStatus = "present"
	// KeyAbsent means the credentials were read, and there is nothing in the slot.
	KeyAbsent KeyStatus = "absent"
	// KeyUnreadable means the credentials could not be consulted, so nothing is known. The UI must
	// show this as unverified rather than picking one of the other two.
	KeyUnreadable KeyStatus = "unreadable"
)

// secretsMu serialises the whole file, not one slot.
//
// PER-SLOT LOCKING WOULD BE WRONG HERE, and this is the one thing the file backend has to get right
// that the Keychain never had to: every write is a read-modify-write of a single document, so two
// writes to DIFFERENT slots can still lose one of them. Wails dispatches every bound call on its own
// goroutine, so that is a real interleaving and not a hypothetical.
//
// It is a package-level lock because the process opens one data directory. Two Stores over one
// directory would each hold their own lock and interleave — the same reason main.go opens the store
// once and shares it.
var secretsMu sync.Mutex

// SecretsPath is the credentials file inside this store's directory.
//
// Exported because the user is TOLD it: when the file cannot be read, the only useful instruction is
// which file to go and look at. A message that says "your keys could not be read" without saying
// where they live sends someone hunting through Application Support.
func (s *Store) SecretsPath() string { return filepath.Join(s.dir, secretsFileName) }

// readSecrets loads the whole document. A missing file is an EMPTY set and not an error: that is a
// fresh install, which is a known state, not an unknown one.
//
// Any other failure comes back as ErrSecretsUnreadable, because at that point the contents are
// genuinely unknown — which is a different thing to tell the user than "you have no keys".
func (s *Store) readSecrets() (map[KeySlot]string, error) {
	raw, err := os.ReadFile(s.SecretsPath())
	if errors.Is(err, os.ErrNotExist) {
		return map[KeySlot]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSecretsUnreadable, err)
	}
	s.tightenSecretsPerm()
	// A ZERO-BYTE FILE IS DAMAGE, NOT AN EMPTY SET, and the difference decides whether the app tells a
	// user with keys that they have none. An earlier version of this treated it as "no keys", reasoning
	// that a crash between create and write could leave one — which is false for THIS writer: the
	// destination is only ever replaced by a rename of a file that has already been written and synced,
	// so nothing here can produce an empty one. What can is a truncated copy, a failed restore or a
	// full disk mid-copy — cases where the contents are unknown, which is exactly ErrSecretsUnreadable.
	// Reporting "absent" would let the engine fallback move someone off a working engine over a file
	// that lost its contents.
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: %s is empty", ErrSecretsUnreadable, s.SecretsPath())
	}
	var out map[KeySlot]string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSecretsUnreadable, err)
	}
	if out == nil {
		out = map[KeySlot]string{}
	}
	return out, nil
}

// tightenSecretsPerm narrows a file that arrived wider than 0600, on every read.
//
// WRITING 0600 ONLY PROTECTS FILES THIS APP WROTE. A copy from a backup, an `scp`, or an editor that
// rewrites through a temp file of its own can all leave the credentials at 0644 — readable by every
// account on the machine — and the app would go on using it happily, since a wider mode reads fine.
// Cleartext keys have exactly one protection, so it is re-applied rather than assumed.
//
// It NARROWS and never widens, and a failure is ignored on purpose: a file owned by another user
// cannot be chmodded, and refusing to read a perfectly legible credential over that would lock
// someone out of their own app for a mode they can fix in one command.
func (s *Store) tightenSecretsPerm() {
	path := s.SecretsPath()
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if info.Mode().Perm()&^secretsPerm != 0 {
		_ = os.Chmod(path, secretsPerm)
	}
}

// writeSecrets replaces the document atomically: a temp file in the SAME directory, then a rename.
//
// The same directory matters — rename is only atomic within a filesystem, and /tmp can be a different
// one. Writing in place would mean a crash mid-write leaves a truncated file, which reads as
// "unreadable" and takes every credential with it.
func (s *Store) writeSecrets(secrets map[KeySlot]string) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("store: cannot create %s: %w", s.dir, err)
	}
	body, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return fmt.Errorf("store: cannot encode the credentials: %w", err)
	}
	body = append(body, '\n')

	tmp, err := os.CreateTemp(s.dir, secretsFileName+".*")
	if err != nil {
		return fmt.Errorf("store: cannot write the credentials: %w", err)
	}
	name := tmp.Name()
	// Removed on every failure path below. A leftover temp file holds the secrets with no owner.
	defer func() { _ = os.Remove(name) }()

	// Chmod BEFORE the write, so the secret never exists on disk under the umask's mode, not even for
	// the microseconds between write and chmod.
	if err := tmp.Chmod(secretsPerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("store: cannot restrict the credentials file: %w", err)
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("store: cannot write the credentials: %w", err)
	}
	// Flushed before the rename: rename only orders the directory entry, not the file's contents, so
	// without this a power loss can leave the new name pointing at an empty file.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("store: cannot flush the credentials: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("store: cannot close the credentials file: %w", err)
	}
	if err := os.Rename(name, s.SecretsPath()); err != nil {
		return fmt.Errorf("store: cannot replace the credentials: %w", err)
	}
	return nil
}

// GetKey reads a secret. ErrNoSecret when the slot is empty, ErrSecretsUnreadable when the
// credentials could not be consulted at all.
func (s *Store) GetKey(slot KeySlot) (string, error) {
	secretsMu.Lock()
	defer secretsMu.Unlock()

	secrets, err := s.readSecrets()
	if err != nil {
		return "", err
	}
	secret, ok := secrets[slot]
	if !ok || secret == "" {
		// A stored blank is treated as absent, not as a credential. SetKey refuses to create one, but a
		// hand-edited file can still contain it, and "configured" over a blank key means every request
		// fails auth with the card claiming to be fine.
		return "", ErrNoSecret
	}
	return secret, nil
}

// SetKey stores a secret, replacing whatever was in the slot.
//
// A blank secret is REFUSED rather than stored: the caller means "leave it alone" or "delete it", and
// both of those have their own path. Writing it would leave a slot that reads as configured and never
// authenticates.
func (s *Store) SetKey(slot KeySlot, secret string) error {
	if !knownSlots[slot] {
		return fmt.Errorf("store: unknown key slot %q", slot)
	}
	if secret == "" {
		return errors.New("store: refusing to store an empty secret — use DeleteKey to clear a slot")
	}

	secretsMu.Lock()
	defer secretsMu.Unlock()

	secrets, err := s.readSecrets()
	if err != nil {
		// Overwriting an unreadable file would destroy whatever the other slots held. Better to fail and
		// say so than to "fix" it by losing the rest.
		return err
	}
	secrets[slot] = secret
	return s.writeSecrets(secrets)
}

// DeleteKey clears a slot. An absent secret is SUCCESS: the postcondition the UI reports is "the key
// is no longer stored", and that sentence is true either way.
func (s *Store) DeleteKey(slot KeySlot) error {
	secretsMu.Lock()
	defer secretsMu.Unlock()

	secrets, err := s.readSecrets()
	if err != nil {
		return err
	}
	if _, ok := secrets[slot]; !ok {
		// Nothing to do, and nothing to write: rewriting the file here would rewrite every other slot
		// for no reason, turning a no-op into a chance to lose them.
		return nil
	}
	delete(secrets, slot)
	return s.writeSecrets(secrets)
}

// HasKey is presence as a bool. Deliberately lossy — see KeyStatus. The UI must use KeyStatusFor.
func (s *Store) HasKey(slot KeySlot) bool {
	_, err := s.GetKey(slot)
	return err == nil
}

// KeyStatusFor reports what is known about one slot, never the secret.
func (s *Store) KeyStatusFor(slot KeySlot) KeyStatus {
	_, err := s.GetKey(slot)
	switch {
	case err == nil:
		return KeyPresent
	case errors.Is(err, ErrNoSecret):
		return KeyAbsent
	default:
		return KeyUnreadable
	}
}
