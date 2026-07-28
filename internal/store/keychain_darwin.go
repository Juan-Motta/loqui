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

// Upsert: delete then add. SecItemUpdate would need a different query shape for the
// attributes vs the value, and "replace" is the only semantics a key slot ever wants.
static int loqui_keychain_set(const char *account, const char *secret) {
	NSMutableDictionary *q = loqui_query(account);
	SecItemDelete((__bridge CFDictionaryRef)q);

	NSString *value = [NSString stringWithUTF8String:secret];
	q[(__bridge id)kSecValueData] = [value dataUsingEncoding:NSUTF8StringEncoding];
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

// SetKey stores a secret, replacing whatever was in the slot.
func SetKey(slot KeySlot, secret string) error {
	cAcct, cSecret := C.CString(string(slot)), C.CString(secret)
	defer C.free(unsafe.Pointer(cAcct))
	defer C.free(unsafe.Pointer(cSecret))

	if status := int(C.loqui_keychain_set(cAcct, cSecret)); status != 0 {
		return fmt.Errorf("store: keychain write failed for %s (OSStatus %d)", slot, status)
	}
	return nil
}

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
	type result struct {
		secret string
		err    error
	}
	// Buffered so the goroutine can always finish and be collected, even after a timeout:
	// the cgo call cannot be cancelled, so it must not be left blocked on a send.
	ch := make(chan result, 1)

	go func() {
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

	select {
	case r := <-ch:
		return r.secret, r.err
	case <-time.After(readTimeout):
		return "", ErrKeychainTimeout
	}
}

// HasKey reports whether a slot holds a secret. This is all the UI is ever told — the
// secrets themselves never cross into the frontend.
func HasKey(slot KeySlot) bool {
	_, err := GetKey(slot)
	return err == nil
}

// DeleteKey removes a secret. Absent is success: the caller wanted it gone.
func DeleteKey(slot KeySlot) error {
	cAcct := C.CString(string(slot))
	defer C.free(unsafe.Pointer(cAcct))

	status := int(C.loqui_keychain_delete(cAcct))
	if status != 0 && status != errSecItemNotFound {
		return fmt.Errorf("store: keychain delete failed for %s (OSStatus %d)", slot, status)
	}
	return nil
}

// KeyPresence reports which slots hold a key, as booleans only.
func KeyPresence() map[KeySlot]bool {
	out := make(map[KeySlot]bool, len(AllKeySlots))
	for _, slot := range AllKeySlots {
		out[slot] = HasKey(slot)
	}
	return out
}
