//go:build darwin

// API keys in the macOS Keychain.
//
// The Electron build used Electron's safeStorage, which encrypts with a key it keeps in
// the Keychain and writes the ciphertext to a file in userData. There is no safeStorage
// here, and reimplementing it would be strictly worse than what it wraps — so the secrets
// go straight into the Keychain as generic passwords.
//
// This is an improvement, not just a substitution: the value never exists as a file on
// disk at all, and access is governed by the Keychain's own ACL rather than by whoever can
// read a file in the user's Library.
//
// PER-PROVIDER SLOTS. Each provider has its own item, so saving an OpenAI key cannot
// overwrite an Azure one. The Electron build learned this the hard way — it started with a
// single key.bin and had to migrate.
package store

/*
#cgo CFLAGS: -x objective-c -fmodules -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework Security
#import <Foundation/Foundation.h>
#import <Security/Security.h>

static NSString *const kLoquiService = @"com.jualopezmo.loquigo";

static NSMutableDictionary *loqui_query(const char *account) {
	NSMutableDictionary *q = [NSMutableDictionary dictionary];
	q[(__bridge id)kSecClass] = (__bridge id)kSecClassGenericPassword;
	q[(__bridge id)kSecAttrService] = kLoquiService;
	q[(__bridge id)kSecAttrAccount] = [NSString stringWithUTF8String:account];
	return q;
}

// Upsert: UPDATE the value when the item exists, ADD only when it does not.
//
// NOT delete-then-add, which is what this used to do. That loses the stored key whenever the
// add fails — the user replaces a working credential with a typo, the add is rejected, and now
// there is no credential at all. SecItemUpdate needs the value in its own attributes-to-update
// dictionary, separate from the query, which is the only reason the two shapes exist here.
static int loqui_keychain_set(const char *account, const char *secret) {
	NSMutableDictionary *q = loqui_query(account);
	NSString *value = [NSString stringWithUTF8String:secret];
	NSData *data = [value dataUsingEncoding:NSUTF8StringEncoding];

	NSMutableDictionary *changes = [NSMutableDictionary dictionary];
	changes[(__bridge id)kSecValueData] = data;

	OSStatus err = SecItemUpdate((__bridge CFDictionaryRef)q, (__bridge CFDictionaryRef)changes);
	if (err != errSecItemNotFound) { return (int)err; }

	// No item yet: add one.
	q[(__bridge id)kSecValueData] = data;
	// Available without unlocking the device, but never synced to iCloud or included in
	// a backup: an API key belongs to this machine.
	q[(__bridge id)kSecAttrAccessible] = (__bridge id)kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly;
	return (int)SecItemAdd((__bridge CFDictionaryRef)q, NULL);
}

// Returns the secret (caller frees) or NULL when absent. status receives the OSStatus.
static char *loqui_keychain_get(const char *account, int *status) {
	NSMutableDictionary *q = loqui_query(account);
	q[(__bridge id)kSecReturnData] = @YES;
	q[(__bridge id)kSecMatchLimit] = (__bridge id)kSecMatchLimitOne;

	CFTypeRef result = NULL;
	OSStatus err = SecItemCopyMatching((__bridge CFDictionaryRef)q, &result);
	*status = (int)err;
	if (err != errSecSuccess || result == NULL) { return NULL; }

	NSData *data = (__bridge_transfer NSData *)result;
	NSString *value = [[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding];
	if (value == nil) { return NULL; }
	return strdup([value UTF8String]);
}

static int loqui_keychain_delete(const char *account) {
	NSMutableDictionary *q = loqui_query(account);
	return (int)SecItemDelete((__bridge CFDictionaryRef)q);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"
)

// ErrNoSecret means the slot is empty. Distinct from a real Keychain failure, because
// "not configured yet" is a normal state the UI shows, while a failure is a bug.
var ErrNoSecret = errors.New("store: no secret in that slot")

// errSecItemNotFound is the OSStatus for "no such item".
const errSecItemNotFound = -25300

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
// deployment name, not by region — the settings form says as much, keeping the region in #speechConfig
// and resource/deployment in #openaiConfig. Listing it let a region be written through a slot that has
// no use for one.
var slotsUsingAzureRegion = map[KeySlot]bool{
	SlotAzureSpeech: true,
}

// UsesAzureRegion reports whether a slot's connection includes the Azure region.
func UsesAzureRegion(slot KeySlot) bool { return slotsUsingAzureRegion[slot] }

// slotGates serialises Keychain operations PER SLOT: one buffered channel each, used as a mutex
// that a caller can give up waiting for.
//
// WHY SERIALISE. The cgo calls cannot be cancelled, so an operation this package abandoned at its
// timeout is still in flight. If a retry ran alongside it, which of the two landed last would be
// whatever the Keychain decided — the user's second attempt could be silently overwritten by the
// first one they were told had failed.
//
// WHY NOT A sync.Mutex. The gate must be released when the cgo call ACTUALLY returns, not when the
// caller stops waiting — otherwise it serialises nothing. But then a hung call would hold it for
// ever, and a plain Lock() would make every later caller hang too: the settings window would
// freeze on the second attempt instead of reporting the first one honestly. A channel can be
// acquired with a deadline, so "somebody else's abandoned call still owns this slot" comes back as
// the same indeterminate answer as the call itself timing out.
var slotGates sync.Map // KeySlot -> chan struct{}, capacity 1

// acquireSlot takes the slot's gate before the deadline. The returned release must be called by
// whoever actually finishes the Keychain call, NOT by a caller that timed out.
//
// ONE DEADLINE COVERS BOTH STAGES — waiting for the gate and then waiting for the call. Giving each
// its own budget would let an operation documented as bounded by writeTimeout take twice that,
// which is the difference between a settings window that pauses and one that looks hung.
func acquireSlot(slot KeySlot, deadline time.Time) (release func(), ok bool) {
	actual, _ := slotGates.LoadOrStore(slot, make(chan struct{}, 1))
	gate := actual.(chan struct{})
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, true
	case <-timer.C:
		return nil, false
	}
}

// waitFor blocks for the result until the deadline, so the caller's total budget is the deadline
// rather than one timeout per stage.
func waitFor[T any](ch <-chan T, deadline time.Time) (T, bool) {
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case v := <-ch:
		return v, true
	case <-timer.C:
		var zero T
		return zero, false
	}
}

// SetKey stores a secret, replacing whatever was in the slot. Returns ErrKeychainTimeout when
// the Keychain does not answer in time.
//
// A TIMEOUT IS INDETERMINATE, not a failure: the uncancellable call may still land afterwards. It
// is reported as an error because the caller has to be told the write is not confirmed, but the
// slot must then be treated as unknown rather than unchanged — which is exactly what
// KeyStatusFor's "unreadable" is for.
//
// BOUNDED FOR THE SAME REASON AS GetKey. SecItemAdd goes through the same access control that
// makes reads hang on an unrecognised signature, so a write can block for ever too. The read
// path learned this the hard way (see readTimeout); leaving the write unbounded meant the first
// person to type an API key into the settings window would freeze it with no way back.
func SetKey(slot KeySlot, secret string) error {
	deadline := time.Now().Add(writeTimeout)
	release, ok := acquireSlot(slot, deadline)
	if !ok {
		return ErrKeychainTimeout
	}

	ch := make(chan error, 1) // buffered: the cgo call cannot be cancelled, so it must never block on the send

	go func() {
		defer release() // when the call REALLY finishes, however long that takes

		cAcct, cSecret := C.CString(string(slot)), C.CString(secret)
		defer C.free(unsafe.Pointer(cAcct))
		defer C.free(unsafe.Pointer(cSecret))

		if status := int(C.loqui_keychain_set(cAcct, cSecret)); status != 0 {
			ch <- fmt.Errorf("store: keychain write failed for %s (OSStatus %d)", slot, status)
			return
		}
		ch <- nil
	}()

	err, ok := waitFor(ch, deadline)
	if !ok {
		return ErrKeychainTimeout
	}
	return err
}

// writeTimeout bounds a Keychain write. Longer than readTimeout because a write that is merely
// slow is worth waiting for — losing the key the user just typed is worse than a pause — while a
// read that stalls has a cheap fallback: report "unreadable" and move on.
const writeTimeout = 10 * time.Second

// readTimeout bounds a Keychain read.
//
// IT EXISTS BECAUSE SecItemCopyMatching CAN BLOCK FOR EVER, and it did: with a binary macOS
// does not recognise — an ad-hoc signature, which changes on every build — the Keychain
// wants to ask the user whether to allow access, and when that prompt cannot be presented
// the call simply never returns. A goroutine dump was the only way to see it: dictation
// started, the microphone never opened, and nothing was logged.
//
// A credential lookup that hangs must degrade to "no key, here is why" rather than freeze
// the app. Whatever the underlying cause on a given machine, blocking is never the right
// answer on the path between a key press and an open microphone.
const readTimeout = 3 * time.Second

// ErrKeychainTimeout means the Keychain did not answer. Almost always the prompt problem
// described above; a properly signed build does not hit it.
var ErrKeychainTimeout = errors.New("store: the keychain did not respond — is the app signed with a stable identity?")

// GetKey reads a secret. Returns ErrNoSecret when the slot is empty and
// ErrKeychainTimeout when the Keychain does not answer in time.
func GetKey(slot KeySlot) (string, error) {
	deadline := time.Now().Add(readTimeout)
	release, ok := acquireSlot(slot, deadline)
	if !ok {
		// Another operation on this slot has not come back. Nothing is known about the slot,
		// which is precisely what ErrKeychainTimeout means to every caller.
		return "", ErrKeychainTimeout
	}

	type result struct {
		secret string
		err    error
	}
	// Buffered so the goroutine can always finish and be collected, even after a timeout:
	// the cgo call cannot be cancelled, so it must not be left blocked on a send.
	ch := make(chan result, 1)

	go func() {
		defer release()

		cAcct := C.CString(string(slot))
		defer C.free(unsafe.Pointer(cAcct))

		var status C.int
		out := C.loqui_keychain_get(cAcct, &status)
		if out == nil {
			if int(status) == errSecItemNotFound {
				ch <- result{err: ErrNoSecret}
				return
			}
			ch <- result{err: fmt.Errorf("store: keychain read failed for %s (OSStatus %d)", slot, int(status))}
			return
		}
		defer C.free(unsafe.Pointer(out))
		ch <- result{secret: C.GoString(out)}
	}()

	r, ok := waitFor(ch, deadline)
	if !ok {
		return "", ErrKeychainTimeout
	}
	return r.secret, r.err
}

// HasKey reports whether a slot holds a secret, collapsing "empty" and "the Keychain did not
// answer" into false.
//
// NOT for the UI: that distinction is load-bearing there, which is what KeyStatusFor exists
// for. Use this only where a plain yes/no is genuinely enough.
func HasKey(slot KeySlot) bool {
	_, err := GetKey(slot)
	return err == nil
}

// DeleteKey removes a secret. Absent is success: the caller wanted it gone.
//
// Bounded and serialised for the same reasons as SetKey. It goes through the same access control,
// so it can hang the same way — and it is now reachable from the settings window, where a hang is
// a frozen UI rather than a slow background task.
func DeleteKey(slot KeySlot) error {
	deadline := time.Now().Add(writeTimeout)
	release, ok := acquireSlot(slot, deadline)
	if !ok {
		return ErrKeychainTimeout
	}

	ch := make(chan error, 1)
	go func() {
		defer release()

		cAcct := C.CString(string(slot))
		defer C.free(unsafe.Pointer(cAcct))

		status := int(C.loqui_keychain_delete(cAcct))
		if status != 0 && status != errSecItemNotFound {
			ch <- fmt.Errorf("store: keychain delete failed for %s (OSStatus %d)", slot, status)
			return
		}
		ch <- nil
	}()

	err, ok := waitFor(ch, deadline)
	if !ok {
		return ErrKeychainTimeout
	}
	return err
}

// KeyStatus is what can be said about one slot without revealing the secret. THREE states,
// not a boolean, because "no key stored" and "the Keychain did not answer" are different
// facts that send the user to different places — and on an ad-hoc-signed build the second one
// is the COMMON case, not an edge case (see ErrKeychainTimeout).
//
// HasKey collapses them into false, which is why the UI must not use it: told a slot is empty,
// the user retypes a credential that is already stored.
type KeyStatus string

const (
	// KeyPresent means a secret was read back from the slot.
	KeyPresent KeyStatus = "present"
	// KeyAbsent means the Keychain answered, and there is nothing in the slot.
	KeyAbsent KeyStatus = "absent"
	// KeyUnreadable means the Keychain could not be consulted, so nothing is known. The UI
	// must show this as unverified rather than picking one of the other two.
	KeyUnreadable KeyStatus = "unreadable"
)

// KeyStatusFor reports what is known about one slot, never the secret.
func KeyStatusFor(slot KeySlot) KeyStatus {
	_, err := GetKey(slot)
	switch {
	case err == nil:
		return KeyPresent
	case errors.Is(err, ErrNoSecret):
		return KeyAbsent
	default:
		// ErrKeychainTimeout, or any OSStatus that is not "not found": the read failed, so
		// the slot's contents are simply unknown.
		return KeyUnreadable
	}
}

// There is deliberately no KeyPresence-over-all-slots helper here. Reading five slots in
// sequence costs 5 × readTimeout — fifteen seconds — whenever the Keychain does not answer,
// which is the COMMON case on an ad-hoc-signed build, and that is precisely the path the
// settings UI paints from. Callers that need several slots must fan out and skip the ones they
// already have an answer for; see (*app.Bootstrap).keyStates.
